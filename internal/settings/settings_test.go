package settings_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/crypto"
	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/notify/telegramsettings"
	"github.com/am-fallah/neda/internal/settings"
	"github.com/am-fallah/neda/internal/store"
)

var settingsNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

const (
	testToken  = "123456:test-token_ABC-def"
	testChatID = "-1001234567890"
)

type fakeTester struct {
	mu         sync.Mutex
	getMeErr   error
	sendErr    error
	getMeCalls int
	sendCalls  int
	lastText   string
}

func (f *fakeTester) GetMe(context.Context, notify.TelegramCredentials) (notify.TelegramBot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getMeCalls++
	if f.getMeErr != nil {
		return notify.TelegramBot{}, f.getMeErr
	}
	return notify.TelegramBot{ID: 1, Username: "neda_test_bot", FirstName: "Neda"}, nil
}

func (f *fakeTester) SendMessage(_ context.Context, _ notify.TelegramCredentials, text string) (notify.TelegramMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendCalls++
	f.lastText = text
	if f.sendErr != nil {
		return notify.TelegramMessage{}, f.sendErr
	}
	return notify.TelegramMessage{MessageID: 1, ChatID: 1, SentAt: settingsNow}, nil
}

func (f *fakeTester) setErrors(getMeErr, sendErr error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getMeErr = getMeErr
	f.sendErr = sendErr
}

func newSettingsStore(t *testing.T) (*store.Store, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "settings-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	applied, err := db.Migrate(ctx, database.DB, migrations.FS)
	if err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	if err := database.Ready(ctx, applied); err != nil {
		t.Fatalf("database.Ready: %v", err)
	}
	return store.New(database.DB), database.DB
}

func settingsKey(t *testing.T) *crypto.Key {
	t.Helper()
	material := make([]byte, crypto.KeySize)
	for index := range material {
		material[index] = 0x42
	}
	key, err := crypto.NewKey(material)
	if err != nil {
		t.Fatalf("crypto.NewKey: %v", err)
	}
	return key
}

type settingsHarness struct {
	service  *settings.Service
	telegram *telegramsettings.Settings
	tester   *fakeTester
	store    *store.Store
	sqlDB    *sql.DB
}

func newSettingsHarness(t *testing.T, withTelegram bool) *settingsHarness {
	t.Helper()
	st, sqlDB := newSettingsStore(t)
	h := &settingsHarness{store: st, sqlDB: sqlDB}
	var telegram *telegramsettings.Settings
	var tester *fakeTester
	if withTelegram {
		var err error
		telegram, err = telegramsettings.New(st, settingsKey(t), func() time.Time { return settingsNow })
		if err != nil {
			t.Fatalf("telegramsettings.New: %v", err)
		}
		tester = &fakeTester{}
	}
	service, err := settings.New(st, telegram, tester, func() time.Time { return settingsNow })
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	h.service = service
	h.telegram = telegram
	h.tester = tester
	return h
}

func TestServiceViewDefaultsAndUpdate(t *testing.T) {
	h := newSettingsHarness(t, false)
	ctx := context.Background()

	view, err := h.service.View(ctx)
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	if view.PollIntervalSeconds != 300 || view.PushEnabled || view.Telegram.Available {
		t.Fatalf("default view = %+v", view)
	}

	poll := 120
	push := true
	updated, err := h.service.Update(ctx, settings.Update{PollIntervalSeconds: &poll, PushEnabled: &push})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.PollIntervalSeconds != 120 || !updated.PushEnabled {
		t.Fatalf("updated view = %+v", updated)
	}
	interval, err := h.store.PollInterval(ctx)
	if err != nil || interval != 2*time.Minute {
		t.Fatalf("stored interval = %v, %v", interval, err)
	}
	enabled, err := h.store.PushEnabled(ctx)
	if err != nil || !enabled {
		t.Fatalf("stored push = %v, %v", enabled, err)
	}

	invalid := 30
	var validation *settings.ValidationError
	if _, err := h.service.Update(ctx, settings.Update{PollIntervalSeconds: &invalid}); !errors.As(err, &validation) ||
		validation.Field != "pollIntervalSeconds" {
		t.Fatalf("invalid interval error = %v", err)
	}
	if _, err := h.service.Update(ctx, settings.Update{}); !errors.As(err, &validation) {
		t.Fatalf("empty update error = %v", err)
	}

	if _, err := settings.New(nil, nil, nil, nil); err == nil {
		t.Fatal("settings.New accepted a nil store")
	}
	withTelegram := newSettingsHarness(t, true)
	if _, err := settings.New(withTelegram.store, withTelegram.telegram, nil, nil); err == nil {
		t.Fatal("settings.New accepted telegram settings without a tester")
	}
}

func TestServiceTelegramSaveTestAndClear(t *testing.T) {
	h := newSettingsHarness(t, true)
	ctx := context.Background()

	view, err := h.service.SaveTelegram(ctx, testToken, testChatID)
	if err != nil {
		t.Fatalf("SaveTelegram: %v", err)
	}
	if !view.Telegram.Available || !view.Telegram.Configured || !view.Telegram.Readable {
		t.Fatalf("saved telegram view = %+v", view.Telegram)
	}
	if view.Telegram.ChatLabel != "chat ****7890" {
		t.Fatalf("chat label = %q", view.Telegram.ChatLabel)
	}
	if view.Telegram.LastTestAt != nil || view.Telegram.LastTestOK != nil || view.Telegram.LastErrorKind != "" {
		t.Fatalf("fresh test metadata = %+v", view.Telegram)
	}
	tokenRow, err := h.store.GetSetting(ctx, telegramsettings.TelegramBotTokenSetting)
	if err != nil {
		t.Fatalf("token setting: %v", err)
	}
	if !tokenRow.IsSecret || strings.Contains(tokenRow.Value, testToken) || tokenRow.Value == testToken {
		t.Fatalf("token row is not encrypted: %+v", tokenRow)
	}

	result, err := h.service.TestTelegram(ctx)
	if err != nil {
		t.Fatalf("TestTelegram success: %v", err)
	}
	if !result.OK || result.ErrorKind != "" {
		t.Fatalf("test result = %+v", result)
	}
	if result.View.Telegram.LastTestOK == nil || !*result.View.Telegram.LastTestOK ||
		result.View.Telegram.LastTestAt == nil || !result.View.Telegram.LastTestAt.Equal(settingsNow) {
		t.Fatalf("test metadata = %+v", result.View.Telegram)
	}
	if h.tester.getMeCalls != 1 || h.tester.sendCalls != 1 || !strings.Contains(h.tester.lastText, "Neda test notification") {
		t.Fatalf("tester calls = %d/%d text=%q", h.tester.getMeCalls, h.tester.sendCalls, h.tester.lastText)
	}

	// A getMe failure records a sanitized category and does not attempt a send.
	h.tester.setErrors(&notify.TelegramError{Kind: notify.TelegramErrorTransient, Op: "getMe"}, nil)
	failed, err := h.service.TestTelegram(ctx)
	if err != nil {
		t.Fatalf("TestTelegram getMe failure: %v", err)
	}
	if failed.OK || failed.ErrorKind != string(notify.TelegramErrorTransient) {
		t.Fatalf("failed result = %+v", failed)
	}
	if failed.View.Telegram.LastTestOK == nil || *failed.View.Telegram.LastTestOK ||
		failed.View.Telegram.LastErrorKind != string(notify.TelegramErrorTransient) {
		t.Fatalf("failed metadata = %+v", failed.View.Telegram)
	}
	if h.tester.sendCalls != 1 {
		t.Fatalf("send calls after getMe failure = %d, want unchanged 1", h.tester.sendCalls)
	}

	// A send failure records its permanent category.
	h.tester.setErrors(nil, &notify.TelegramError{Kind: notify.TelegramErrorPermanent, Op: "sendMessage"})
	failed, err = h.service.TestTelegram(ctx)
	if err != nil {
		t.Fatalf("TestTelegram send failure: %v", err)
	}
	if failed.OK || failed.ErrorKind != string(notify.TelegramErrorPermanent) {
		t.Fatalf("send failed result = %+v", failed)
	}
	if h.tester.sendCalls != 2 {
		t.Fatalf("send calls after send failure = %d, want 2", h.tester.sendCalls)
	}

	cleared, err := h.service.ClearTelegram(ctx)
	if err != nil {
		t.Fatalf("ClearTelegram: %v", err)
	}
	if cleared.Telegram.Configured || cleared.Telegram.Readable || cleared.Telegram.ChatLabel != "" {
		t.Fatalf("cleared view = %+v", cleared.Telegram)
	}
	if _, err := h.store.GetSetting(ctx, telegramsettings.TelegramBotTokenSetting); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("token row after clear = %v, want ErrNotFound", err)
	}
	if _, err := h.service.TestTelegram(ctx); !errors.Is(err, telegramsettings.ErrTelegramNotConfigured) {
		t.Fatalf("test after clear = %v, want ErrTelegramNotConfigured", err)
	}
}

func TestServiceTelegramValidationAndUnreadable(t *testing.T) {
	h := newSettingsHarness(t, true)
	ctx := context.Background()

	var validation *settings.ValidationError
	if _, err := h.service.SaveTelegram(ctx, "", testChatID); !errors.As(err, &validation) || validation.Field != "token" {
		t.Fatalf("empty token error = %v", err)
	}
	if _, err := h.service.SaveTelegram(ctx, testToken, ""); !errors.As(err, &validation) || validation.Field != "chatId" {
		t.Fatalf("empty chat error = %v", err)
	}
	if _, err := h.service.SaveTelegram(ctx, "bad token", testChatID); !errors.As(err, &validation) || validation.Field != "token" {
		t.Fatalf("invalid token error = %v", err)
	}

	if _, err := h.service.SaveTelegram(ctx, testToken, testChatID); err != nil {
		t.Fatalf("SaveTelegram: %v", err)
	}
	version := int64(crypto.CurrentKeyVersion)
	if err := h.store.SetSetting(ctx, store.Setting{
		Key:        telegramsettings.TelegramBotTokenSetting,
		Value:      "v1:not-a-valid-ciphertext",
		IsSecret:   true,
		KeyVersion: &version,
		UpdatedAt:  settingsNow,
	}); err != nil {
		t.Fatalf("corrupt token setting: %v", err)
	}
	view, err := h.service.View(ctx)
	if err != nil {
		t.Fatalf("View unreadable: %v", err)
	}
	if !view.Telegram.Configured || view.Telegram.Readable || view.Telegram.SettingsErrorKind == "" {
		t.Fatalf("unreadable view = %+v", view.Telegram)
	}
	if _, err := h.service.TestTelegram(ctx); !errors.Is(err, telegramsettings.ErrTelegramSettingsUnreadable) {
		t.Fatalf("TestTelegram unreadable error = %v, want ErrTelegramSettingsUnreadable", err)
	}
}

func TestServiceTelegramUnavailable(t *testing.T) {
	h := newSettingsHarness(t, false)
	ctx := context.Background()
	if _, err := h.service.SaveTelegram(ctx, testToken, testChatID); !errors.Is(err, settings.ErrTelegramUnavailable) {
		t.Fatalf("SaveTelegram without telegram = %v", err)
	}
	if _, err := h.service.ClearTelegram(ctx); !errors.Is(err, settings.ErrTelegramUnavailable) {
		t.Fatalf("ClearTelegram without telegram = %v", err)
	}
	if _, err := h.service.TestTelegram(ctx); !errors.Is(err, settings.ErrTelegramUnavailable) {
		t.Fatalf("TestTelegram without telegram = %v", err)
	}
}
