// Package settings owns the non-secret settings view and the authenticated
// Telegram configuration/test workflow. Secret encryption remains in
// telegramsettings; this package never returns or logs token/chat material.
package settings

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/notify/telegramsettings"
	"github.com/am-fallah/neda/internal/store"
)

// ErrTelegramUnavailable reports that no encryption key/settings service was
// configured. Poll interval and Push settings remain usable in that state.
var ErrTelegramUnavailable = errors.New("settings: telegram settings are unavailable")

const telegramTestMessage = "Neda test notification: Telegram delivery is configured."

// TelegramTester is the bounded Bot API surface used by the test endpoint.
type TelegramTester interface {
	GetMe(context.Context, notify.TelegramCredentials) (notify.TelegramBot, error)
	SendMessage(context.Context, notify.TelegramCredentials, string) (notify.TelegramMessage, error)
}

var _ TelegramTester = (*notify.TelegramClient)(nil)

// ValidationError identifies one settings request field.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e == nil {
		return "settings: validation failed"
	}
	if e.Field == "" {
		return "settings: " + e.Message
	}
	return "settings: " + e.Field + ": " + e.Message
}

// TelegramStatus is the non-secret Telegram view. Available reports whether the
// encryption/settings subsystem is configured at all; Configured/Readable
// describe the stored credentials.
type TelegramStatus struct {
	Available         bool
	Configured        bool
	Readable          bool
	ChatLabel         string
	SettingsErrorKind string
	LastTestAt        *time.Time
	LastTestOK        *bool
	LastErrorKind     string
}

// View is the complete non-secret settings response.
type View struct {
	PollIntervalSeconds        int
	PushEnabled                bool
	MonitoringEnabled          bool
	UnavailableNotifyAfterMins int
	Telegram                   TelegramStatus
}

// Update is a partial settings edit. At least one field must be present.
type Update struct {
	PollIntervalSeconds        *int
	PushEnabled                *bool
	MonitoringEnabled          *bool
	UnavailableNotifyAfterMins *int
}

// TestResult reports the persisted post-test settings view and the test
// outcome. ErrorKind is a stable category, never a raw Telegram description.
type TestResult struct {
	View      View
	OK        bool
	ErrorKind string
}

// Service coordinates settings persistence and the Telegram test workflow.
type Service struct {
	store    *store.Store
	telegram *telegramsettings.Settings
	tester   TelegramTester
	now      func() time.Time
	wake     func()
}

// New validates dependencies. Telegram may be nil when no encryption key is
// configured; the Telegram-specific operations then fail closed.
func New(st *store.Store, telegram *telegramsettings.Settings, tester TelegramTester, now func() time.Time) (*Service, error) {
	if st == nil {
		return nil, errors.New("settings: nil store")
	}
	if telegram != nil && tester == nil {
		return nil, errors.New("settings: nil telegram tester")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{store: st, telegram: telegram, tester: tester, now: now}, nil
}

// SetWake installs the post-commit scheduler wake hook. It is safe to call
// after construction because the HTTP server and scheduler share this service.
func (s *Service) SetWake(wake func()) {
	if s == nil {
		return
	}
	if wake == nil {
		wake = func() {}
	}
	s.wake = wake
}

// View returns the current non-secret settings. A missing poll-interval row
// falls back to the documented default rather than failing the whole view.
func (s *Service) View(ctx context.Context) (View, error) {
	if s == nil || s.store == nil {
		return View{}, errors.New("settings: not initialized")
	}
	interval, err := s.PollInterval(ctx)
	if err != nil {
		return View{}, err
	}
	pushEnabled, err := s.store.PushEnabled(ctx)
	if err != nil {
		return View{}, err
	}
	monitoringEnabled, err := s.store.MonitoringEnabled(ctx)
	if err != nil {
		return View{}, err
	}
	grace, err := s.store.UnavailableNotifyAfter(ctx)
	if err != nil {
		return View{}, err
	}
	view := View{
		PollIntervalSeconds:        int(interval / time.Second),
		PushEnabled:                pushEnabled,
		MonitoringEnabled:          monitoringEnabled,
		UnavailableNotifyAfterMins: int(grace / time.Minute),
	}
	if s.telegram == nil {
		return view, nil
	}
	status, err := s.telegram.Status(ctx)
	if err != nil {
		return View{}, err
	}
	view.Telegram = TelegramStatus{
		Available:         true,
		Configured:        status.Configured,
		Readable:          status.Readable,
		ChatLabel:         status.ChatLabel,
		SettingsErrorKind: status.SettingsErrorKind,
		LastTestAt:        status.LastTestAt,
		LastTestOK:        status.LastTestOK,
		LastErrorKind:     status.LastErrorKind,
	}
	return view, nil
}

// PollInterval returns the persisted global interval, falling back to the
// documented default only when the row has not been seeded yet.
func (s *Service) PollInterval(ctx context.Context) (time.Duration, error) {
	if s == nil || s.store == nil {
		return 0, errors.New("settings: not initialized")
	}
	interval, err := s.store.PollInterval(ctx)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return domain.DefaultPollInterval, nil
		}
		return 0, err
	}
	return interval, nil
}

// Update applies a partial settings edit atomically and returns the new view.
func (s *Service) Update(ctx context.Context, update Update) (View, error) {
	if s == nil || s.store == nil {
		return View{}, errors.New("settings: not initialized")
	}
	if update.PollIntervalSeconds == nil && update.PushEnabled == nil &&
		update.MonitoringEnabled == nil && update.UnavailableNotifyAfterMins == nil {
		return View{}, &ValidationError{Message: "at least one setting is required"}
	}
	var interval time.Duration
	if update.PollIntervalSeconds != nil {
		seconds := *update.PollIntervalSeconds
		minSeconds := int(domain.MinPollInterval / time.Second)
		maxSeconds := int(domain.MaxPollInterval / time.Second)
		if seconds < minSeconds || seconds > maxSeconds {
			return View{}, &ValidationError{
				Field:   "pollIntervalSeconds",
				Message: fmt.Sprintf("must be between %d and %d seconds", minSeconds, maxSeconds),
			}
		}
		interval = time.Duration(seconds) * time.Second
	}
	var grace time.Duration
	if update.UnavailableNotifyAfterMins != nil {
		minutes := *update.UnavailableNotifyAfterMins
		minMinutes := int(store.MinUnavailableNotifyAfter / time.Minute)
		maxMinutes := int(store.MaxUnavailableNotifyAfter / time.Minute)
		if minutes < minMinutes || minutes > maxMinutes {
			return View{}, &ValidationError{
				Field:   "unavailableNotifyAfterMinutes",
				Message: fmt.Sprintf("must be between %d and %d minutes", minMinutes, maxMinutes),
			}
		}
		grace = time.Duration(minutes) * time.Minute
	}
	now := s.now().UTC()
	if now.IsZero() {
		return View{}, errors.New("settings: update timestamp is required")
	}
	err := s.store.WithTx(ctx, func(tx *store.Store) error {
		if update.PollIntervalSeconds != nil {
			if err := tx.SetPollInterval(ctx, interval, now); err != nil {
				return err
			}
		}
		if update.PushEnabled != nil {
			if err := tx.SetPushEnabled(ctx, *update.PushEnabled, now); err != nil {
				return err
			}
		}
		if update.MonitoringEnabled != nil {
			if err := tx.SetMonitoringEnabled(ctx, *update.MonitoringEnabled, now); err != nil {
				return err
			}
		}
		if update.UnavailableNotifyAfterMins != nil {
			if err := tx.SetUnavailableNotifyAfter(ctx, grace, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return View{}, err
	}
	// The scheduler must re-evaluate a paused/resumed switch immediately rather
	// than waiting for its next timer. The hook runs only after commit.
	if update.MonitoringEnabled != nil && s.wake != nil {
		s.wake()
	}
	return s.View(ctx)
}

// SaveTelegram validates and stores encrypted credentials, resetting prior test
// metadata.
func (s *Service) SaveTelegram(ctx context.Context, token, chatID string) (View, error) {
	if s == nil || s.telegram == nil {
		return View{}, ErrTelegramUnavailable
	}
	token = strings.TrimSpace(token)
	chatID = strings.TrimSpace(chatID)
	if err := notify.ValidateTelegramToken(token); err != nil {
		return View{}, &ValidationError{Field: "token", Message: err.Error()}
	}
	if err := notify.ValidateTelegramChatID(chatID); err != nil {
		return View{}, &ValidationError{Field: "chatId", Message: err.Error()}
	}
	if err := s.telegram.Save(ctx, token, chatID); err != nil {
		return View{}, err
	}
	return s.View(ctx)
}

// ClearTelegram removes stored credentials and test metadata. Missing rows are
// not an error.
func (s *Service) ClearTelegram(ctx context.Context) (View, error) {
	if s == nil || s.telegram == nil {
		return View{}, ErrTelegramUnavailable
	}
	if err := s.telegram.Clear(ctx); err != nil {
		return View{}, err
	}
	return s.View(ctx)
}

// TestTelegram validates the configured bot with getMe, sends one test message,
// records the sanitized result, and returns the updated view. A Telegram
// failure is reported in the result rather than as a transport error.
func (s *Service) TestTelegram(ctx context.Context) (TestResult, error) {
	if s == nil || s.telegram == nil || s.tester == nil {
		return TestResult{}, ErrTelegramUnavailable
	}
	credentials, err := s.telegram.Load(ctx)
	if err != nil {
		return TestResult{}, err
	}

	var testErr error
	if _, err := s.tester.GetMe(ctx, credentials); err != nil {
		testErr = err
	} else if _, err := s.tester.SendMessage(ctx, credentials, telegramTestMessage); err != nil {
		testErr = err
	}
	now := s.now().UTC()
	if now.IsZero() {
		return TestResult{}, errors.New("settings: test timestamp is required")
	}
	if err := s.telegram.RecordTestResult(ctx, now, testErr); err != nil {
		return TestResult{}, err
	}
	view, err := s.View(ctx)
	if err != nil {
		return TestResult{}, err
	}
	return TestResult{
		View:      view,
		OK:        testErr == nil,
		ErrorKind: telegramErrorKind(testErr),
	}, nil
}

func telegramErrorKind(err error) string {
	if err == nil {
		return ""
	}
	var telegramErr *notify.TelegramError
	if errors.As(err, &telegramErr) && telegramErr != nil && telegramErr.Kind != "" {
		return string(telegramErr.Kind)
	}
	return "unknown"
}
