package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/config"
	"github.com/am-fallah/neda/internal/crypto"
	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/provider/fake"
	"github.com/am-fallah/neda/internal/store"
)

func TestProviderRegistryForConfig(t *testing.T) {
	tests := []struct {
		name           string
		cfg            config.Config
		wantFake       bool
		wantFakeLookup bool
		wantMrBilit    bool
		wantMrBilitUse bool
		wantErr        bool
	}{
		{
			name:           "development fake enabled",
			cfg:            config.Config{Env: config.Development, FakeProviderEnabled: true},
			wantFake:       true,
			wantFakeLookup: true,
			wantMrBilit:    true,
			wantMrBilitUse: true,
		},
		{
			name:           "production real provider enabled",
			cfg:            config.Config{Env: config.Production, MrBilitEnabled: true},
			wantFake:       false,
			wantFakeLookup: false,
			wantMrBilit:    true,
			wantMrBilitUse: true,
		},
		{
			name:    "production requires real provider",
			cfg:     config.Config{Env: config.Production},
			wantErr: true,
		},
		{
			name:    "production rejects fake provider",
			cfg:     config.Config{Env: config.Production, MrBilitEnabled: true, FakeProviderEnabled: true},
			wantErr: true,
		},
		{
			name:           "development fake disabled",
			cfg:            config.Config{Env: config.Development},
			wantFake:       false,
			wantFakeLookup: false,
			wantMrBilit:    true,
			wantMrBilitUse: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, flags, err := providerRegistryForConfig(test.cfg, nil)
			if test.wantErr {
				if err == nil {
					t.Fatalf("providerRegistryForConfig succeeded, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("providerRegistryForConfig: %v", err)
			}
			if flags.FakeEnabled != test.wantFake {
				t.Fatalf("fakeEnabled = %v, want %v", flags.FakeEnabled, test.wantFake)
			}
			if flags.MrBilitEnabled != test.wantMrBilit {
				t.Fatalf("mrbilitEnabled = %v, want %v", flags.MrBilitEnabled, test.wantMrBilit)
			}
			_, found := registry.Lookup(fake.ID)
			if found != test.wantFakeLookup {
				t.Fatalf("fake lookup = %v, want %v", found, test.wantFakeLookup)
			}
			_, found = registry.Lookup("mrbilit")
			if found != test.wantMrBilitUse {
				t.Fatalf("mrbilit lookup = %v, want %v", found, test.wantMrBilitUse)
			}
		})
	}
}

func TestValidateRuntimeUser(t *testing.T) {
	if err := validateRuntimeUser(0, true); err == nil {
		t.Fatal("production accepted uid 0")
	}
	if err := validateRuntimeUser(1000, true); err != nil {
		t.Fatalf("production rejected uid 1000: %v", err)
	}
	if err := validateRuntimeUser(0, false); err != nil {
		t.Fatalf("development rejected uid 0: %v", err)
	}
}

func TestValidateKeyDatabaseConsistency(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "neda.db")
	database, err := db.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	keyA, err := crypto.NewKey(bytes.Repeat([]byte{1}, crypto.KeySize))
	if err != nil {
		t.Fatalf("NewKey A: %v", err)
	}
	keyB, err := crypto.NewKey(bytes.Repeat([]byte{2}, crypto.KeySize))
	if err != nil {
		t.Fatalf("NewKey B: %v", err)
	}

	if err := validateKeyDatabaseConsistency(ctx, database.DB, keyA); err != nil {
		t.Fatalf("fresh database rejected the key: %v", err)
	}
	if _, err := db.Migrate(ctx, database.DB, migrations.FS); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	ciphertext, err := keyA.Encrypt("telegram.bot_token", []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO settings(key, value, is_secret, updated_at) VALUES(?, ?, 1, ?)`,
		"telegram.bot_token", ciphertext, time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("insert encrypted setting: %v", err)
	}
	if err := validateKeyDatabaseConsistency(ctx, database.DB, keyA); err != nil {
		t.Fatalf("matching key rejected: %v", err)
	}
	if err := validateKeyDatabaseConsistency(ctx, database.DB, keyB); err == nil || !strings.Contains(err.Error(), "cannot be decrypted") {
		t.Fatalf("mismatched key error = %v, want decrypt failure", err)
	}
}

func TestRunServeFailsClosedBeforeCreatingDatabase(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{name: "missing real provider", env: map[string]string{"NEDA_MRBILIT_ENABLED": ""}, wantErr: "NEDA_MRBILIT_ENABLED"},
		{name: "fake provider", env: map[string]string{"NEDA_FAKE_PROVIDER_ENABLED": "true"}, wantErr: "NEDA_FAKE_PROVIDER_ENABLED"},
		{name: "missing key file", env: map[string]string{"NEDA_ENCRYPTION_KEY_FILE": ""}, wantErr: "NEDA_ENCRYPTION_KEY_FILE"},
		{name: "insecure public URL", env: map[string]string{"NEDA_PUBLIC_URL": "http://neda.example.com"}, wantErr: "https"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workDir := t.TempDir()
			keyPath := filepath.Join(workDir, "encryption.key")
			key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
			if err := os.WriteFile(keyPath, []byte(key), 0o600); err != nil {
				t.Fatalf("write key: %v", err)
			}

			dbDir := filepath.Join(workDir, "data")
			dbPath := filepath.Join(dbDir, "neda.db")
			t.Setenv("NEDA_ENV", "production")
			t.Setenv("NEDA_HTTP_ADDR", ":8080")
			t.Setenv("NEDA_DB_PATH", dbPath)
			t.Setenv("NEDA_LOG_LEVEL", "info")
			t.Setenv("NEDA_LOG_FORMAT", "json")
			t.Setenv("NEDA_TRUST_PROXY", "false")
			t.Setenv("NEDA_SESSION_TTL_HOURS", "720")
			t.Setenv("NEDA_SESSION_IDLE_HOURS", "168")
			t.Setenv("NEDA_WORKERS", "2")
			t.Setenv("NEDA_PUBLIC_URL", "https://neda.example.com")
			t.Setenv("NEDA_VAPID_SUBJECT", "mailto:test@example.com")
			t.Setenv("NEDA_ENCRYPTION_KEY_FILE", keyPath)
			t.Setenv("NEDA_MRBILIT_ENABLED", "true")
			t.Setenv("NEDA_FAKE_PROVIDER_ENABLED", "false")
			for key, value := range test.env {
				t.Setenv(key, value)
			}

			var stdout, stderr bytes.Buffer
			if code := runServe(&stdout, &stderr); code != 1 {
				t.Fatalf("runServe = %d, want 1; stderr=%s", code, stderr.String())
			}
			if !strings.Contains(stderr.String(), test.wantErr) {
				t.Fatalf("stderr = %q, want substring %q", stderr.String(), test.wantErr)
			}
			if _, err := os.Stat(dbDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("database directory was created before config validation: %v", err)
			}
		})
	}
}

func TestProductionCoverageFailureDoesNotSeedSettings(t *testing.T) {
	workDir := t.TempDir()
	keyPath := filepath.Join(workDir, "encryption.key")
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
	if err := os.WriteFile(keyPath, []byte(key), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	dbPath := filepath.Join(workDir, "data", "neda.db")
	ctx := context.Background()

	database, err := db.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if _, err := db.Migrate(ctx, database.DB, migrations.FS); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	now := time.Now().UTC()
	st := store.New(database.DB)
	if _, err := st.CreateMonitor(ctx, domain.Monitor{
		ID:              "coverage-fake",
		Label:           "Coverage fake",
		OriginName:      "Yazd",
		DestinationName: "Mashhad",
		DepartureDate:   "2030-01-01",
		Adults:          1,
		Enabled:         true,
		ExpiresAt:       now.Add(24 * time.Hour),
		CreatedAt:       now,
		UpdatedAt:       now,
		Providers: []domain.MonitorProvider{{
			MonitorID:     "coverage-fake",
			ProviderID:    fake.ID,
			Active:        true,
			OriginID:      "yazd",
			DestinationID: "mashhad",
			ConfigVersion: 1,
			CreatedAt:     now,
			UpdatedAt:     now,
		}},
	}); err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close setup database: %v", err)
	}

	t.Setenv("NEDA_ENV", "production")
	t.Setenv("NEDA_HTTP_ADDR", ":8080")
	t.Setenv("NEDA_DB_PATH", dbPath)
	t.Setenv("NEDA_PUBLIC_URL", "https://neda.example.com")
	t.Setenv("NEDA_VAPID_SUBJECT", "mailto:test@example.com")
	t.Setenv("NEDA_ENCRYPTION_KEY_FILE", keyPath)
	t.Setenv("NEDA_MRBILIT_ENABLED", "true")
	t.Setenv("NEDA_FAKE_PROVIDER_ENABLED", "false")
	t.Setenv("NEDA_LOG_LEVEL", "info")
	t.Setenv("NEDA_LOG_FORMAT", "json")

	var stdout, stderr bytes.Buffer
	if code := runServe(&stdout, &stderr); code != 1 {
		t.Fatalf("runServe = %d, want 1; stderr=%s", code, stderr.String())
	}
	logs := stdout.String() + stderr.String()
	if !strings.Contains(logs, "unregistered providers: fake") {
		t.Fatalf("logs = %q, want unregistered-provider error", logs)
	}

	check, err := db.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer check.Close()
	var settings int
	if err := check.QueryRowContext(ctx, `SELECT COUNT(*) FROM settings`).Scan(&settings); err != nil {
		t.Fatalf("count settings: %v", err)
	}
	if settings != 0 {
		t.Fatalf("settings rows after coverage failure = %d, want 0", settings)
	}
	var integrity string
	if err := check.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatalf("integrity_check: %v", err)
	}
	if integrity != "ok" {
		t.Fatalf("integrity_check = %q, want ok", integrity)
	}
}

type staticSchedule struct {
	targets []domain.ScheduleTarget
	err     error
}

func (s staticSchedule) ListScheduleTargets(context.Context, time.Time) ([]domain.ScheduleTarget, error) {
	return s.targets, s.err
}

func TestValidateProviderCoverage(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	registry, err := provider.NewRegistry(fake.New())
	if err != nil {
		t.Fatalf("provider.NewRegistry: %v", err)
	}

	if err := validateProviderCoverage(context.Background(), staticSchedule{
		targets: []domain.ScheduleTarget{{ProviderID: fake.ID}},
	}, registry, now, true, nil); err != nil {
		t.Fatalf("covered production target: %v", err)
	}

	if err := validateProviderCoverage(context.Background(), staticSchedule{
		targets: []domain.ScheduleTarget{{ProviderID: "missing"}},
	}, registry, now, true, nil); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing production provider error = %v", err)
	}

	if err := validateProviderCoverage(context.Background(), staticSchedule{
		targets: []domain.ScheduleTarget{{ProviderID: "missing"}},
	}, registry, now, false, nil); err != nil {
		t.Fatalf("development warning path: %v", err)
	}

	sentinel := errors.New("schedule failed")
	if err := validateProviderCoverage(context.Background(), staticSchedule{err: sentinel}, registry, now, true, nil); !errors.Is(err, sentinel) {
		t.Fatalf("schedule error = %v", err)
	}
}

func TestComposeDeliverySources(t *testing.T) {
	ctx := context.Background()
	if source := composeDeliverySources(nil, nil); source != nil {
		t.Fatal("nil-only composition should be nil")
	}
	telegram := func(context.Context, *sql.Tx) ([]notify.Delivery, error) {
		return []notify.Delivery{{Channel: notify.ChannelTelegram, TargetKey: "telegram"}}, nil
	}
	push := func(context.Context, *sql.Tx) ([]notify.Delivery, error) {
		return []notify.Delivery{{Channel: notify.ChannelPush, TargetKey: "push:device"}}, nil
	}
	combined := composeDeliverySources(telegram, nil, push)
	deliveries, err := combined(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 2 || deliveries[0].Channel != notify.ChannelTelegram || deliveries[1].Channel != notify.ChannelPush {
		t.Fatalf("deliveries = %+v", deliveries)
	}

	sentinel := errors.New("source failed")
	failing := func(context.Context, *sql.Tx) ([]notify.Delivery, error) { return nil, sentinel }
	if _, err := composeDeliverySources(telegram, failing)(ctx, nil); !errors.Is(err, sentinel) {
		t.Fatalf("source error = %v", err)
	}
}
