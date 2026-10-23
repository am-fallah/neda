package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func lookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func writeKey(t *testing.T, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "encryption.key")
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	if err := os.WriteFile(path, []byte(key+"\n"), mode); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod key: %v", err)
	}
	return path
}

func TestLoadProductionDefaults(t *testing.T) {
	keyFile := writeKey(t, 0o600)
	cfg, err := LoadFrom(lookup(map[string]string{
		"NEDA_ENCRYPTION_KEY_FILE": keyFile,
		"NEDA_PUBLIC_URL":          "https://neda.example.com",
	}))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Env != Production {
		t.Fatalf("Env = %q, want production", cfg.Env)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.DBPath != "/data/neda.db" {
		t.Fatalf("DBPath = %q", cfg.DBPath)
	}
	if cfg.LogLevel != "info" || cfg.LogFormat != "json" {
		t.Fatalf("logging = %q/%q, want info/json", cfg.LogLevel, cfg.LogFormat)
	}
	if cfg.SessionTTL != 720*time.Hour || cfg.SessionIdle != 168*time.Hour {
		t.Fatalf("session durations = %v/%v", cfg.SessionTTL, cfg.SessionIdle)
	}
	if cfg.Workers != 2 {
		t.Fatalf("Workers = %d", cfg.Workers)
	}
	if cfg.PublicURL == nil || cfg.PublicURL.Host != "neda.example.com" {
		t.Fatalf("PublicURL = %v", cfg.PublicURL)
	}
}

func TestLoadDevelopmentDefaults(t *testing.T) {
	cfg, err := LoadFrom(lookup(map[string]string{"NEDA_ENV": "development"}))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Env != Development {
		t.Fatalf("Env = %q", cfg.Env)
	}
	if cfg.LogFormat != "text" {
		t.Fatalf("LogFormat = %q, want text", cfg.LogFormat)
	}
	if cfg.PublicURL != nil {
		t.Fatalf("PublicURL = %v, want nil", cfg.PublicURL)
	}
	if cfg.EncryptionKeyFile != "" {
		t.Fatalf("EncryptionKeyFile = %q, want empty", cfg.EncryptionKeyFile)
	}
}

func TestLoadProductionRequiresKeyAndPublicURL(t *testing.T) {
	if _, err := LoadFrom(lookup(map[string]string{})); err == nil {
		t.Fatal("LoadFrom succeeded without production key/public URL")
	}
	keyFile := writeKey(t, 0o600)
	if _, err := LoadFrom(lookup(map[string]string{"NEDA_ENCRYPTION_KEY_FILE": keyFile})); err == nil {
		t.Fatal("LoadFrom succeeded without production public URL")
	}
	if _, err := LoadFrom(lookup(map[string]string{"NEDA_PUBLIC_URL": "https://neda.example.com"})); err == nil {
		t.Fatal("LoadFrom succeeded without production key file")
	}
}

func TestLoadRejectsUnsafeKeyFile(t *testing.T) {
	t.Run("group-readable", func(t *testing.T) {
		_, err := LoadFrom(lookup(map[string]string{
			"NEDA_ENCRYPTION_KEY_FILE": writeKey(t, 0o644),
			"NEDA_PUBLIC_URL":          "https://neda.example.com",
		}))
		if err == nil || !strings.Contains(err.Error(), "owner-only") {
			t.Fatalf("error = %v, want owner-only permissions error", err)
		}
	})

	t.Run("wrong-length", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "short.key")
		contents := base64.StdEncoding.EncodeToString([]byte("too-short"))
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadFrom(lookup(map[string]string{
			"NEDA_ENCRYPTION_KEY_FILE": path,
			"NEDA_PUBLIC_URL":          "https://neda.example.com",
		}))
		if err == nil || !strings.Contains(err.Error(), "exactly 32 bytes") {
			t.Fatalf("error = %v, want length error", err)
		}
		if strings.Contains(err.Error(), contents) {
			t.Fatal("error exposed key contents")
		}
	})

	t.Run("not-base64", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.key")
		if err := os.WriteFile(path, []byte("not-base64!"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadFrom(lookup(map[string]string{
			"NEDA_ENCRYPTION_KEY_FILE": path,
			"NEDA_PUBLIC_URL":          "https://neda.example.com",
		}))
		if err == nil || !strings.Contains(err.Error(), "base64") {
			t.Fatalf("error = %v, want base64 error", err)
		}
	})

	t.Run("missing", func(t *testing.T) {
		_, err := LoadFrom(lookup(map[string]string{
			"NEDA_ENCRYPTION_KEY_FILE": filepath.Join(t.TempDir(), "missing.key"),
			"NEDA_PUBLIC_URL":          "https://neda.example.com",
		}))
		if err == nil || !strings.Contains(err.Error(), "unavailable") {
			t.Fatalf("error = %v, want unavailable error", err)
		}
	})
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	validKey := writeKey(t, 0o600)
	base := map[string]string{
		"NEDA_ENCRYPTION_KEY_FILE": validKey,
		"NEDA_PUBLIC_URL":          "https://neda.example.com",
	}

	cases := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{"environment", "NEDA_ENV", "staging", "NEDA_ENV"},
		{"listen-address", "NEDA_HTTP_ADDR", "localhost", "NEDA_HTTP_ADDR"},
		{"listen-port", "NEDA_HTTP_ADDR", ":70000", "invalid port"},
		{"log-level", "NEDA_LOG_LEVEL", "verbose", "NEDA_LOG_LEVEL"},
		{"log-format", "NEDA_LOG_FORMAT", "yaml", "NEDA_LOG_FORMAT"},
		{"trust-proxy", "NEDA_TRUST_PROXY", "sometimes", "NEDA_TRUST_PROXY"},
		{"fake-provider", "NEDA_FAKE_PROVIDER_ENABLED", "sometimes", "NEDA_FAKE_PROVIDER_ENABLED"},
		{"workers-low", "NEDA_WORKERS", "0", "NEDA_WORKERS"},
		{"workers-high", "NEDA_WORKERS", "17", "NEDA_WORKERS"},
		{"session-ttl", "NEDA_SESSION_TTL_HOURS", "0", "NEDA_SESSION_TTL_HOURS"},
		{"session-idle", "NEDA_SESSION_IDLE_HOURS", "abc", "NEDA_SESSION_IDLE_HOURS"},
		{"vapid-subject", "NEDA_VAPID_SUBJECT", "ftp://example.com", "NEDA_VAPID_SUBJECT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := make(map[string]string, len(base)+1)
			for key, value := range base {
				values[key] = value
			}
			values[tc.key] = tc.value
			_, err := LoadFrom(lookup(values))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestLoadRejectsIdleLongerThanAbsoluteSession(t *testing.T) {
	keyFile := writeKey(t, 0o600)
	_, err := LoadFrom(lookup(map[string]string{
		"NEDA_ENCRYPTION_KEY_FILE": keyFile,
		"NEDA_PUBLIC_URL":          "https://neda.example.com",
		"NEDA_SESSION_TTL_HOURS":   "100",
		"NEDA_SESSION_IDLE_HOURS":  "200",
	}))
	if err == nil || !strings.Contains(err.Error(), "must not exceed") {
		t.Fatalf("error = %v, want idle/absolute validation error", err)
	}
}

func TestLoadPublicURLValidation(t *testing.T) {
	keyFile := writeKey(t, 0o600)
	cases := []struct {
		name string
		env  string
		url  string
		want string
	}{
		{"http-in-production", "production", "http://neda.example.com", "https"},
		{"userinfo", "production", "https://user:pass@neda.example.com", "userinfo"},
		{"query", "production", "https://neda.example.com/?a=1", "query"},
		{"fragment", "production", "https://neda.example.com/#x", "fragment"},
		{"path", "production", "https://neda.example.com/app", "path"},
		{"no-host", "production", "https:///path", "host"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadFrom(lookup(map[string]string{
				"NEDA_ENV":                 tc.env,
				"NEDA_ENCRYPTION_KEY_FILE": keyFile,
				"NEDA_PUBLIC_URL":          tc.url,
			}))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestLoadAllowsHTTPPublicURLOffProduction(t *testing.T) {
	cfg, err := LoadFrom(lookup(map[string]string{
		"NEDA_ENV":        "development",
		"NEDA_PUBLIC_URL": "http://localhost:8080",
	}))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.PublicURL == nil || cfg.PublicURL.Scheme != "http" {
		t.Fatalf("PublicURL = %v", cfg.PublicURL)
	}
}

func TestLoadRejectsNilLookup(t *testing.T) {
	if _, err := LoadFrom(nil); err == nil {
		t.Fatal("LoadFrom(nil) succeeded")
	}
}
