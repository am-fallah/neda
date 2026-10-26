package auth

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", raw, err)
	}
	return parsed
}

func TestOriginPolicyProduction(t *testing.T) {
	policy := OriginPolicy{PublicURL: mustURL(t, "https://neda.example.com"), Production: true}

	tests := []struct {
		name    string
		origin  string
		referer string
		wantErr bool
	}{
		{"origin match", "https://neda.example.com", "", false},
		{"origin default port", "https://neda.example.com:443", "", false},
		{"referer fallback", "", "https://neda.example.com/events?x=1", false},
		{"missing both", "", "", true},
		{"wrong host", "https://evil.example.com", "", true},
		{"wrong scheme", "http://neda.example.com", "", true},
		{"wrong port", "https://neda.example.com:8443", "", true},
		{"null origin", "null", "", true},
		{"malformed origin", "not a url", "", true},
		{"origin with path", "https://neda.example.com/path", "", true},
		{"origin with userinfo", "https://user@neda.example.com", "", true},
		{"invalid referer", "", "not a url", true},
		{"cross-origin referer", "", "https://evil.example.com/path", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "https://neda.example.com/api/v1/auth/login", nil)
			if test.origin != "" {
				req.Header.Set("Origin", test.origin)
			}
			if test.referer != "" {
				req.Header.Set("Referer", test.referer)
			}
			err := policy.Check(req)
			if test.wantErr && err == nil {
				t.Fatal("Check succeeded, want error")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("Check: %v", err)
			}
		})
	}
}

func TestOriginPolicyProductionHTTPIP(t *testing.T) {
	policy := OriginPolicy{PublicURL: mustURL(t, "http://203.0.113.10:8080"), Production: true}

	tests := []struct {
		name    string
		origin  string
		referer string
		wantErr bool
	}{
		{"origin match", "http://203.0.113.10:8080", "", false},
		{"referer fallback", "", "http://203.0.113.10:8080/events?x=1", false},
		{"wrong scheme", "https://203.0.113.10:8080", "", true},
		{"wrong port", "http://203.0.113.10:9090", "", true},
		{"wrong host", "http://198.51.100.10:8080", "", true},
		{"missing both", "", "", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://203.0.113.10:8080/api/v1/auth/login", nil)
			if test.origin != "" {
				req.Header.Set("Origin", test.origin)
			}
			if test.referer != "" {
				req.Header.Set("Referer", test.referer)
			}
			err := policy.Check(req)
			if test.wantErr && err == nil {
				t.Fatal("Check succeeded, want error")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("Check: %v", err)
			}
		})
	}
}

func TestOriginPolicyDevelopment(t *testing.T) {
	policy := OriginPolicy{}
	req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/api/v1/auth/login", nil)
	req.Header.Set("Origin", "http://localhost:8080")
	if err := policy.Check(req); err != nil {
		t.Fatalf("Check same-origin: %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "http://localhost:8080/api/v1/auth/login", nil)
	req.Header.Set("Origin", "http://localhost:9090")
	if err := policy.Check(req); err == nil {
		t.Fatal("Check wrong port succeeded")
	}

	req = httptest.NewRequest(http.MethodPost, "http://localhost:8080/api/v1/auth/login", nil)
	req.Header.Set("Referer", "http://localhost:8080/login")
	if err := policy.Check(req); err != nil {
		t.Fatalf("Check referer: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "https://localhost/", nil)
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Origin", "https://localhost")
	if err := policy.Check(req); err != nil {
		t.Fatalf("Check TLS origin: %v", err)
	}
}

func TestOriginPolicyProductionRequiresPublicURL(t *testing.T) {
	policy := OriginPolicy{Production: true}
	req := httptest.NewRequest(http.MethodPost, "https://neda.example.com/login", nil)
	req.Header.Set("Origin", "https://neda.example.com")
	if err := policy.Check(req); err == nil {
		t.Fatal("production policy without public URL succeeded")
	}
}
