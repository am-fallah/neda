package telegramsettings_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/crypto"
	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/notify/telegramsettings"
	"github.com/am-fallah/neda/internal/store"
)

func settingsTestKey(t *testing.T, fill byte) *crypto.Key {
	t.Helper()
	material := make([]byte, crypto.KeySize)
	for i := range material {
		material[i] = fill
	}
	key, err := crypto.NewKey(material)
	if err != nil {
		t.Fatalf("crypto.NewKey: %v", err)
	}
	return key
}

const (
	settingsToken  = "123456:test-token_ABC-def"
	settingsChatID = "-1001234567890"
)

var settingsTestNow = time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)

func newSettingsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "telegram-settings-test.db"))
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
	return database.DB
}

func settingsTestCredentials(t *testing.T) notify.TelegramCredentials {
	t.Helper()
	credentials, err := notify.NewTelegramCredentials(settingsToken, settingsChatID)
	if err != nil {
		t.Fatalf("NewTelegramCredentials: %v", err)
	}
	return credentials
}

func TestSettingsPersistEncryptedValues(t *testing.T) {
	ctx := context.Background()
	st := store.New(newSettingsTestDB(t))
	key := settingsTestKey(t, 1)
	settings, err := telegramsettings.New(st, key, func() time.Time { return settingsTestNow })
	if err != nil {
		t.Fatalf("NewTelegramSettings: %v", err)
	}
	credentials := settingsTestCredentials(t)
	if err := settings.Save(ctx, settingsToken, settingsChatID); err != nil {
		t.Fatalf("Save: %v", err)
	}

	tokenRow, err := st.GetSetting(ctx, telegramsettings.TelegramBotTokenSetting)
	if err != nil {
		t.Fatal(err)
	}
	chatRow, err := st.GetSetting(ctx, telegramsettings.TelegramChatIDSetting)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []store.Setting{tokenRow, chatRow} {
		if !row.IsSecret {
			t.Fatalf("%s is not marked secret", row.Key)
		}
		if row.KeyVersion == nil || *row.KeyVersion != crypto.CurrentKeyVersion {
			t.Fatalf("%s key version = %v", row.Key, row.KeyVersion)
		}
		if !strings.HasPrefix(row.Value, "v1:") {
			t.Fatalf("%s value is not versioned: %q", row.Key, row.Value)
		}
	}
	if tokenRow.Value == settingsToken || strings.Contains(tokenRow.Value, settingsToken) {
		t.Fatal("token stored in plaintext")
	}
	if chatRow.Value == settingsChatID || strings.Contains(chatRow.Value, settingsChatID) {
		t.Fatal("chat id stored in plaintext")
	}

	loaded, err := settings.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.ChatLabel() != credentials.ChatLabel() {
		t.Fatalf("loaded label = %q, want %q", loaded.ChatLabel(), credentials.ChatLabel())
	}
	if rendered := fmt.Sprintf("%v %+v %#v", loaded, loaded, loaded); strings.Contains(rendered, settingsToken) || strings.Contains(rendered, settingsChatID) {
		t.Fatalf("loaded credentials leaked: %q", rendered)
	}
}

func TestSettingsFailClosed(t *testing.T) {
	ctx := context.Background()
	database := newSettingsTestDB(t)
	st := store.New(database)
	settings, err := telegramsettings.New(st, settingsTestKey(t, 2), func() time.Time { return settingsTestNow })
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Save(ctx, settingsToken, settingsChatID); err != nil {
		t.Fatal(err)
	}

	t.Run("wrong-key", func(t *testing.T) {
		wrong, err := telegramsettings.New(st, settingsTestKey(t, 3), func() time.Time { return settingsTestNow })
		if err != nil {
			t.Fatal(err)
		}
		if _, err := wrong.Load(ctx); !errors.Is(err, telegramsettings.ErrTelegramSettingsUnreadable) {
			t.Fatalf("Load error = %v, want ErrTelegramSettingsUnreadable", err)
		}
		status, err := wrong.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !status.Configured || status.Readable || status.SettingsErrorKind != "secret_unreadable" {
			t.Fatalf("status = %+v", status)
		}
	})

	t.Run("tampered-token", func(t *testing.T) {
		row, err := st.GetSetting(ctx, telegramsettings.TelegramBotTokenSetting)
		if err != nil {
			t.Fatal(err)
		}
		row.Value = row.Value[:len(row.Value)-2] + "AA"
		if err := st.SetSetting(ctx, row); err != nil {
			t.Fatal(err)
		}
		if _, err := settings.Load(ctx); !errors.Is(err, telegramsettings.ErrTelegramSettingsUnreadable) {
			t.Fatalf("Load error = %v", err)
		}
	})

	t.Run("swapped-ciphertexts", func(t *testing.T) {
		tokenRow, err := st.GetSetting(ctx, telegramsettings.TelegramBotTokenSetting)
		if err != nil {
			t.Fatal(err)
		}
		chatRow, err := st.GetSetting(ctx, telegramsettings.TelegramChatIDSetting)
		if err != nil {
			t.Fatal(err)
		}
		tokenRow.Value, chatRow.Value = chatRow.Value, tokenRow.Value
		if err := st.SetSetting(ctx, tokenRow); err != nil {
			t.Fatal(err)
		}
		if err := st.SetSetting(ctx, chatRow); err != nil {
			t.Fatal(err)
		}
		if _, err := settings.Load(ctx); !errors.Is(err, telegramsettings.ErrTelegramSettingsUnreadable) {
			t.Fatalf("Load error = %v", err)
		}
	})

	t.Run("unsafe-metadata", func(t *testing.T) {
		// Re-save to restore valid ciphertexts.
		if err := settings.Save(ctx, settingsToken, settingsChatID); err != nil {
			t.Fatal(err)
		}
		row, err := st.GetSetting(ctx, telegramsettings.TelegramBotTokenSetting)
		if err != nil {
			t.Fatal(err)
		}
		row.IsSecret = false
		if err := st.SetSetting(ctx, row); err != nil {
			t.Fatal(err)
		}
		if _, err := settings.Load(ctx); !errors.Is(err, telegramsettings.ErrTelegramSettingsUnreadable) {
			t.Fatalf("Load error = %v", err)
		}
	})

	t.Run("missing-row", func(t *testing.T) {
		if err := settings.Save(ctx, settingsToken, settingsChatID); err != nil {
			t.Fatal(err)
		}
		if err := st.DeleteSetting(ctx, telegramsettings.TelegramChatIDSetting); err != nil {
			t.Fatal(err)
		}
		if _, err := settings.Load(ctx); !errors.Is(err, telegramsettings.ErrTelegramNotConfigured) {
			t.Fatalf("Load error = %v, want ErrTelegramNotConfigured", err)
		}
		status, err := settings.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !status.Configured || status.Readable || status.SettingsErrorKind != "settings_incomplete" {
			t.Fatalf("status = %+v", status)
		}
	})
}

func TestSettingsStatusAndTestMetadata(t *testing.T) {
	ctx := context.Background()
	st := store.New(newSettingsTestDB(t))
	settings, err := telegramsettings.New(st, settingsTestKey(t, 4), func() time.Time { return settingsTestNow })
	if err != nil {
		t.Fatal(err)
	}

	status, err := settings.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Configured || status.Readable {
		t.Fatalf("empty status = %+v", status)
	}

	if err := settings.Save(ctx, settingsToken, settingsChatID); err != nil {
		t.Fatal(err)
	}
	status, err = settings.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Configured || !status.Readable || status.ChatLabel == "" {
		t.Fatalf("configured status = %+v", status)
	}

	testErr := &notify.TelegramError{Kind: notify.TelegramErrorRateLimited, Op: "sendMessage"}
	if err := settings.RecordTestResult(ctx, settingsTestNow.Add(time.Minute), testErr); err != nil {
		t.Fatalf("RecordTestResult: %v", err)
	}
	status, err = settings.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.LastTestAt == nil || !status.LastTestAt.Equal(settingsTestNow.Add(time.Minute)) {
		t.Fatalf("LastTestAt = %v", status.LastTestAt)
	}
	if status.LastTestOK == nil || *status.LastTestOK {
		t.Fatalf("LastTestOK = %v, want false", status.LastTestOK)
	}
	if status.LastErrorKind != string(notify.TelegramErrorRateLimited) {
		t.Fatalf("LastErrorKind = %q", status.LastErrorKind)
	}

	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{settingsToken, settingsChatID} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("status JSON leaked %q: %s", secret, encoded)
		}
	}

	if err := settings.RecordTestResult(ctx, settingsTestNow.Add(2*time.Minute), nil); err != nil {
		t.Fatal(err)
	}
	status, err = settings.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.LastTestOK == nil || !*status.LastTestOK || status.LastErrorKind != "" {
		t.Fatalf("success test metadata = %+v", status)
	}

	// Saving credentials resets stale test metadata.
	if err := settings.Save(ctx, settingsToken, settingsChatID); err != nil {
		t.Fatal(err)
	}
	status, err = settings.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.LastTestAt != nil || status.LastTestOK != nil {
		t.Fatalf("test metadata was not reset: %+v", status)
	}

	if err := settings.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	status, err = settings.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Configured || status.Readable || status.ChatLabel != "" {
		t.Fatalf("status after clear = %+v", status)
	}
	if err := settings.Clear(ctx); err != nil {
		t.Fatalf("second Clear: %v", err)
	}
}

func TestSettingsErrorsDoNotLeakSecrets(t *testing.T) {
	ctx := context.Background()
	st := store.New(newSettingsTestDB(t))
	settings, err := telegramsettings.New(st, settingsTestKey(t, 5), func() time.Time { return settingsTestNow })
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Save(ctx, settingsToken, settingsChatID); err != nil {
		t.Fatal(err)
	}
	row, err := st.GetSetting(ctx, telegramsettings.TelegramBotTokenSetting)
	if err != nil {
		t.Fatal(err)
	}
	row.Value = "v1:not-base64!"
	if err := st.SetSetting(ctx, row); err != nil {
		t.Fatal(err)
	}
	_, err = settings.Load(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	rendered := fmt.Sprintf("%v %+v %#v", err, err, err)
	for _, secret := range []string{settingsToken, settingsChatID, row.Value} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("error leaked %q: %q", secret, rendered)
		}
	}
}
