package telegramsettings

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/crypto"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/store"
)

const (
	// TelegramBotTokenSetting is the encrypted settings key for the bot token.
	TelegramBotTokenSetting = "telegram.bot_token"
	// TelegramChatIDSetting is the encrypted settings key for the chat ID.
	TelegramChatIDSetting = "telegram.chat_id"

	telegramLastTestAtSetting    = "telegram.last_test_at"
	telegramLastTestOKSetting    = "telegram.last_test_ok"
	telegramLastErrorKindSetting = "telegram.last_error_kind"
)

var (
	// ErrTelegramNotConfigured reports that one or both credential rows are
	// absent. Callers should treat this as "Telegram is not set up".
	ErrTelegramNotConfigured = errors.New("notify: telegram is not configured")
	// ErrTelegramSettingsUnreadable reports a present but unsafe/undecryptable
	// configuration. It fails closed and never carries ciphertext.
	ErrTelegramSettingsUnreadable = errors.New("notify: telegram settings are unreadable")
)

// Status is a non-secret settings view suitable for an API response or
// logs. It never contains the token or the raw chat ID.
type Status struct {
	Configured        bool       `json:"configured"`
	Readable          bool       `json:"readable"`
	ChatLabel         string     `json:"chat_label,omitempty"`
	SettingsErrorKind string     `json:"settings_error_kind,omitempty"`
	LastTestAt        *time.Time `json:"last_test_at,omitempty"`
	LastTestOK        *bool      `json:"last_test_ok,omitempty"`
	LastErrorKind     string     `json:"last_error_kind,omitempty"`
}

// Settings owns encrypted Telegram credential persistence. It depends
// on the caller-supplied master key and never creates or rotates keys.
type Settings struct {
	store *store.Store
	key   *crypto.Key
	now   func() time.Time
}

// New validates dependencies and applies a UTC clock.
func New(st *store.Store, key *crypto.Key, now func() time.Time) (*Settings, error) {
	if st == nil {
		return nil, errors.New("notify: nil settings store")
	}
	if key == nil {
		return nil, errors.New("notify: nil encryption key")
	}
	if now == nil {
		now = time.Now
	}
	return &Settings{store: st, key: key, now: now}, nil
}

// Save validates and atomically stores encrypted credentials, resetting stale
// test metadata. Encryption happens before the transaction; plaintext is never
// written to SQLite.
func (s *Settings) Save(ctx context.Context, token, chatID string) error {
	if s == nil || s.store == nil || s.key == nil {
		return errors.New("notify: telegram settings are not initialized")
	}
	token = strings.TrimSpace(token)
	chatID = strings.TrimSpace(chatID)
	if _, err := notify.NewTelegramCredentials(token, chatID); err != nil {
		return err
	}
	now := s.now().UTC()
	if now.IsZero() {
		return errors.New("notify: settings timestamp is required")
	}

	tokenValue, err := s.key.Encrypt(TelegramBotTokenSetting, []byte(token))
	if err != nil {
		return fmt.Errorf("notify: encrypt telegram token: %w", err)
	}
	chatValue, err := s.key.Encrypt(TelegramChatIDSetting, []byte(chatID))
	if err != nil {
		return fmt.Errorf("notify: encrypt telegram chat id: %w", err)
	}

	version := int64(s.key.Version())
	return s.store.WithTx(ctx, func(tx *store.Store) error {
		if err := tx.SetSetting(ctx, store.Setting{
			Key:        TelegramBotTokenSetting,
			Value:      tokenValue,
			IsSecret:   true,
			KeyVersion: &version,
			UpdatedAt:  now,
		}); err != nil {
			return err
		}
		if err := tx.SetSetting(ctx, store.Setting{
			Key:        TelegramChatIDSetting,
			Value:      chatValue,
			IsSecret:   true,
			KeyVersion: &version,
			UpdatedAt:  now,
		}); err != nil {
			return err
		}
		for _, key := range telegramTestMetadataKeys() {
			if err := tx.DeleteSetting(ctx, key); err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
		}
		return nil
	})
}

// Load decrypts and returns credentials for use by TelegramClient. Callers
// must not render or serialize the returned value; its formatters redact it.
func (s *Settings) Load(ctx context.Context) (notify.TelegramCredentials, error) {
	if s == nil || s.store == nil || s.key == nil {
		return notify.TelegramCredentials{}, errors.New("notify: telegram settings are not initialized")
	}
	tokenSetting, err := s.store.GetSetting(ctx, TelegramBotTokenSetting)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return notify.TelegramCredentials{}, ErrTelegramNotConfigured
		}
		return notify.TelegramCredentials{}, err
	}
	chatSetting, err := s.store.GetSetting(ctx, TelegramChatIDSetting)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return notify.TelegramCredentials{}, ErrTelegramNotConfigured
		}
		return notify.TelegramCredentials{}, err
	}

	token, err := s.decryptSecret(TelegramBotTokenSetting, tokenSetting)
	if err != nil {
		return notify.TelegramCredentials{}, err
	}
	defer wipe(token)
	chatID, err := s.decryptSecret(TelegramChatIDSetting, chatSetting)
	if err != nil {
		return notify.TelegramCredentials{}, err
	}
	defer wipe(chatID)

	credentials, err := notify.NewTelegramCredentials(string(token), string(chatID))
	if err != nil {
		return notify.TelegramCredentials{}, fmt.Errorf("%w: invalid stored credentials", ErrTelegramSettingsUnreadable)
	}
	return credentials, nil
}

// Clear removes credentials and test metadata. Missing rows are not an error.
func (s *Settings) Clear(ctx context.Context) error {
	if s == nil || s.store == nil {
		return errors.New("notify: telegram settings are not initialized")
	}
	keys := append([]string{TelegramBotTokenSetting, TelegramChatIDSetting}, telegramTestMetadataKeys()...)
	return s.store.WithTx(ctx, func(tx *store.Store) error {
		for _, key := range keys {
			if err := tx.DeleteSetting(ctx, key); err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
		}
		return nil
	})
}

// Status returns a non-secret view. A present but undecryptable configuration
// reports Configured=true, Readable=false and a safe category.
func (s *Settings) Status(ctx context.Context) (Status, error) {
	if s == nil || s.store == nil {
		return Status{}, errors.New("notify: telegram settings are not initialized")
	}
	_, tokenErr := s.store.GetSetting(ctx, TelegramBotTokenSetting)
	_, chatErr := s.store.GetSetting(ctx, TelegramChatIDSetting)

	status := Status{}
	tokenMissing := errors.Is(tokenErr, store.ErrNotFound)
	chatMissing := errors.Is(chatErr, store.ErrNotFound)
	if tokenErr != nil && !tokenMissing {
		return Status{}, tokenErr
	}
	if chatErr != nil && !chatMissing {
		return Status{}, chatErr
	}
	if tokenMissing && chatMissing {
		return s.attachTestMetadata(ctx, status)
	}
	status.Configured = true
	if tokenMissing || chatMissing {
		status.SettingsErrorKind = "settings_incomplete"
		return s.attachTestMetadata(ctx, status)
	}

	credentials, err := s.Load(ctx)
	if err != nil {
		status.SettingsErrorKind = "secret_unreadable"
		return s.attachTestMetadata(ctx, status)
	}
	status.Readable = true
	status.ChatLabel = credentials.ChatLabel()
	return s.attachTestMetadata(ctx, status)
}

// RecordTestResult stores sanitized last-test metadata. The error is reduced to
// a stable category; no description, URL, token, or chat ID is persisted.
func (s *Settings) RecordTestResult(ctx context.Context, at time.Time, testErr error) error {
	if s == nil || s.store == nil {
		return errors.New("notify: telegram settings are not initialized")
	}
	at = at.UTC()
	if at.IsZero() {
		return errors.New("notify: test timestamp is required")
	}
	ok := testErr == nil
	category := ""
	if testErr != nil {
		category = "unknown"
		var telegramErr *notify.TelegramError
		if errors.As(testErr, &telegramErr) && telegramErr != nil {
			category = string(telegramErr.Kind)
		}
	}
	return s.store.WithTx(ctx, func(tx *store.Store) error {
		rows := []store.Setting{
			{Key: telegramLastTestAtSetting, Value: at.Format(time.RFC3339Nano), UpdatedAt: at},
			{Key: telegramLastTestOKSetting, Value: strconv.FormatBool(ok), UpdatedAt: at},
			{Key: telegramLastErrorKindSetting, Value: category, UpdatedAt: at},
		}
		for _, row := range rows {
			if err := tx.SetSetting(ctx, row); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Settings) decryptSecret(settingKey string, setting store.Setting) ([]byte, error) {
	if !setting.IsSecret || setting.KeyVersion == nil || *setting.KeyVersion != int64(crypto.CurrentKeyVersion) {
		return nil, fmt.Errorf("%w: %s has an unsupported format", ErrTelegramSettingsUnreadable, settingKey)
	}
	plaintext, err := s.key.Decrypt(settingKey, setting.Value)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrTelegramSettingsUnreadable, settingKey)
	}
	return plaintext, nil
}

func (s *Settings) attachTestMetadata(ctx context.Context, status Status) (Status, error) {
	atSetting, err := s.store.GetSetting(ctx, telegramLastTestAtSetting)
	if err == nil {
		if at, parseErr := time.Parse(time.RFC3339Nano, atSetting.Value); parseErr == nil {
			at = at.UTC()
			status.LastTestAt = &at
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return Status{}, err
	}

	okSetting, err := s.store.GetSetting(ctx, telegramLastTestOKSetting)
	if err == nil {
		if ok, parseErr := strconv.ParseBool(strings.TrimSpace(okSetting.Value)); parseErr == nil {
			status.LastTestOK = &ok
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return Status{}, err
	}

	kindSetting, err := s.store.GetSetting(ctx, telegramLastErrorKindSetting)
	if err == nil {
		status.LastErrorKind = strings.TrimSpace(kindSetting.Value)
	} else if !errors.Is(err, store.ErrNotFound) {
		return Status{}, err
	}
	return status, nil
}

func telegramTestMetadataKeys() []string {
	return []string{
		telegramLastTestAtSetting,
		telegramLastTestOKSetting,
		telegramLastErrorKindSetting,
	}
}

func wipe(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
