package main

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/am-fallah/neda/internal/config"
)

func TestHealthTarget(t *testing.T) {
	cases := []struct {
		name       string
		addr       string
		publicURL  string
		wantTarget string
		wantHost   string
	}{
		{"all-interfaces", ":8080", "", "http://127.0.0.1:8080/healthz", ""},
		{"ipv4-all", "0.0.0.0:9000", "", "http://127.0.0.1:9000/healthz", ""},
		{"loopback", "127.0.0.1:8081", "", "http://127.0.0.1:8081/healthz", ""},
		{"public-host", ":8080", "https://neda.example.com", "http://127.0.0.1:8080/healthz", "neda.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Config{HTTPAddr: tc.addr}
			if tc.publicURL != "" {
				parsed, err := config.LoadFrom(func(key string) (string, bool) {
					switch key {
					case "NEDA_ENV":
						return "development", true
					case "NEDA_PUBLIC_URL":
						return tc.publicURL, true
					default:
						return "", false
					}
				})
				if err != nil {
					t.Fatalf("config: %v", err)
				}
				cfg.PublicURL = parsed.PublicURL
			}
			target, host, err := healthTarget(cfg)
			if err != nil {
				t.Fatalf("healthTarget: %v", err)
			}
			if target != tc.wantTarget || host != tc.wantHost {
				t.Fatalf("healthTarget = (%q,%q), want (%q,%q)", target, host, tc.wantTarget, tc.wantHost)
			}
		})
	}
}

func TestHealthTargetRejectsBadAddress(t *testing.T) {
	if _, _, err := healthTarget(config.Config{HTTPAddr: "localhost"}); err == nil {
		t.Fatal("healthTarget accepted an invalid address")
	}
}

func TestRunHealthcheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	t.Setenv("NEDA_ENV", "development")
	t.Setenv("NEDA_HTTP_ADDR", strings.TrimPrefix(server.URL, "http://"))
	t.Setenv("NEDA_PUBLIC_URL", "")
	t.Setenv("NEDA_ENCRYPTION_KEY_FILE", "")

	var stdout, stderr bytes.Buffer
	if code := runHealthcheck(&stdout, &stderr); code != 0 {
		t.Fatalf("runHealthcheck = %d; stderr=%s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "ok" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunHealthcheckUsesPublicHostHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "neda.example.com" {
			http.Error(w, "wrong host", http.StatusMisdirectedRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	keyPath := filepath.Join(t.TempDir(), "encryption.key")
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	if err := os.WriteFile(keyPath, []byte(key), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NEDA_ENV", "production")
	t.Setenv("NEDA_HTTP_ADDR", strings.TrimPrefix(server.URL, "http://"))
	t.Setenv("NEDA_PUBLIC_URL", "https://neda.example.com")
	t.Setenv("NEDA_ENCRYPTION_KEY_FILE", keyPath)

	var stdout, stderr bytes.Buffer
	if code := runHealthcheck(&stdout, &stderr); code != 0 {
		t.Fatalf("runHealthcheck = %d; stderr=%s", code, stderr.String())
	}
}

func TestRunHealthcheckReportsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	t.Setenv("NEDA_ENV", "development")
	t.Setenv("NEDA_HTTP_ADDR", strings.TrimPrefix(server.URL, "http://"))
	t.Setenv("NEDA_PUBLIC_URL", "")
	t.Setenv("NEDA_ENCRYPTION_KEY_FILE", "")

	var stdout, stderr bytes.Buffer
	if code := runHealthcheck(&stdout, &stderr); code != 1 {
		t.Fatalf("runHealthcheck = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "HTTP 503") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
