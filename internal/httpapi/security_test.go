package httpapi_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/am-fallah/neda/internal/auth"
	"github.com/am-fallah/neda/internal/httpapi"
	"github.com/am-fallah/neda/internal/webui"
)

type securityReadiness struct {
	err error
}

func (s securityReadiness) Ready(context.Context, int) error { return s.err }

func securityServer(t *testing.T, cfg httpapi.Config, readiness httpapi.ReadinessChecker) *httpapi.Server {
	t.Helper()
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC) }
	}
	return httpapi.New(cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)), readiness, 4)
}

// TestSecurityHeadersEverywhere proves the security header policy is applied
// to successful API responses, sanitized errors, host rejections, and the SPA
// shell. It also proves that no CORS permission is ever advertised.
func TestSecurityHeadersEverywhere(t *testing.T) {
	publicURL, _ := url.Parse(authOrigin)
	spa := webui.New(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>Neda</title>")},
	})
	server := securityServer(t, httpapi.Config{
		PublicURL:  publicURL,
		Production: true,
		WebUI:      spa,
	}, securityReadiness{})
	authServer := newAuthFixture(t, auth.DefaultRateLimitConfig(), true).server

	cases := []struct {
		name   string
		method string
		path   string
		host   string
		want   int
		csp    string
	}{
		{"health", http.MethodGet, "/healthz", authHost, http.StatusOK, "default-src 'none'"},
		{"private-unauthenticated", http.MethodGet, "/api/v1/monitors", authHost, http.StatusUnauthorized, "default-src 'none'"},
		{"unknown-api", http.MethodGet, "/api/v1/does-not-exist", authHost, http.StatusNotFound, "default-src 'none'"},
		{"method-not-allowed", http.MethodPost, "/healthz", authHost, http.StatusMethodNotAllowed, "default-src 'none'"},
		{"host-mismatch", http.MethodGet, "/healthz", "evil.example.com", http.StatusMisdirectedRequest, "default-src 'none'"},
		{"spa", http.MethodGet, "/", authHost, http.StatusOK, "script-src 'self'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "https://"+authHost+tc.path, nil)
			req.Host = tc.host
			recorder := httptest.NewRecorder()
			activeServer := server
			if tc.name == "private-unauthenticated" {
				activeServer = authServer
			}
			activeServer.Handler().ServeHTTP(recorder, req)
			response := recorder.Result()
			if response.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", response.StatusCode, tc.want, recorder.Body.String())
			}
			headers := response.Header
			if got := headers.Get("X-Content-Type-Options"); got != "nosniff" {
				t.Fatalf("X-Content-Type-Options = %q", got)
			}
			if got := headers.Get("Referrer-Policy"); got != "no-referrer" {
				t.Fatalf("Referrer-Policy = %q", got)
			}
			if got := headers.Get("X-Frame-Options"); got != "DENY" {
				t.Fatalf("X-Frame-Options = %q", got)
			}
			if got := headers.Get("Cross-Origin-Opener-Policy"); got != "same-origin" {
				t.Fatalf("Cross-Origin-Opener-Policy = %q", got)
			}
			if got := headers.Get("Cross-Origin-Resource-Policy"); got != "same-origin" {
				t.Fatalf("Cross-Origin-Resource-Policy = %q", got)
			}
			if got := headers.Get("X-Permitted-Cross-Domain-Policies"); got != "none" {
				t.Fatalf("X-Permitted-Cross-Domain-Policies = %q", got)
			}
			if got := headers.Get("Permissions-Policy"); !strings.Contains(got, "camera=()") {
				t.Fatalf("Permissions-Policy = %q", got)
			}
			if got := headers.Get("Content-Security-Policy"); !strings.Contains(got, tc.csp) {
				t.Fatalf("Content-Security-Policy = %q, want %q", got, tc.csp)
			}
			if got := headers.Get("Strict-Transport-Security"); got == "" {
				t.Fatal("production response lacks HSTS")
			}
			for _, header := range []string{
				"Access-Control-Allow-Origin",
				"Access-Control-Allow-Credentials",
				"Access-Control-Allow-Methods",
				"Access-Control-Allow-Headers",
			} {
				if got := headers.Get(header); got != "" {
					t.Fatalf("%s = %q, want empty", header, got)
				}
			}
			if tc.name != "spa" {
				if got := headers.Get("Cache-Control"); got != "no-store" {
					t.Fatalf("Cache-Control = %q", got)
				}
			}
			if got := headers.Get("X-Request-ID"); len(got) != 32 {
				t.Fatalf("request ID = %q", got)
			}
		})
	}
}

func TestOPTIONSDoesNotEnableCORS(t *testing.T) {
	server := securityServer(t, httpapi.Config{}, securityReadiness{})
	for _, path := range []string{"/api/v1/monitors", "/api/v1/overview", "/"} {
		req := httptest.NewRequest(http.MethodOptions, "http://localhost"+path, nil)
		req.Host = "localhost"
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, req)
		for _, header := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Methods", "Access-Control-Allow-Headers"} {
			if got := recorder.Header().Get(header); got != "" {
				t.Fatalf("%s %s returned %s=%q", http.MethodOptions, path, header, got)
			}
		}
	}
}

func TestCSRFRequiresCookieAndHeaderOnUnsafeMethods(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	sessionCookie, csrf, login := fixture.login(t, authTestPass)
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d %s", login.Code, login.Body.String())
	}

	requestWith := func(method string, session, csrfCookie, csrfHeader, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, authOrigin+"/api/v1/auth/logout", nil)
		req.Host = authHost
		req.RemoteAddr = "203.0.113.9:12345"
		req.AddCookie(&http.Cookie{Name: auth.DefaultSessionCookieName, Value: session})
		if csrfCookie != "" {
			req.AddCookie(&http.Cookie{Name: auth.DefaultCSRFCookieName, Value: csrfCookie})
		}
		if csrfHeader != "" {
			req.Header.Set(auth.CSRFHeaderName, csrfHeader)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		recorder := httptest.NewRecorder()
		fixture.server.Handler().ServeHTTP(recorder, req)
		return recorder
	}

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			for _, tc := range []struct {
				name       string
				csrfCookie string
				csrfHeader string
				origin     string
			}{
				{"missing-both", "", "", authOrigin},
				{"cookie-only", csrf, "", authOrigin},
				{"header-only", "", csrf, authOrigin},
				{"mismatched", csrf, csrf + "x", authOrigin},
				{"wrong-origin", csrf, csrf, "https://evil.example"},
			} {
				response := requestWith(method, sessionCookie, tc.csrfCookie, tc.csrfHeader, tc.origin)
				if response.Code != http.StatusForbidden {
					t.Fatalf("%s %s = %d, want 403; body=%s", method, tc.name, response.Code, response.Body.String())
				}
				if strings.Contains(response.Body.String(), csrf) {
					t.Fatalf("%s %s leaked the CSRF token", method, tc.name)
				}
			}
		})
	}
	// A safe request with only a session is not a CSRF failure. Logout is
	// POST-only, so the expected result is 405 rather than 403.
	safe := requestWith(http.MethodGet, sessionCookie, "", "", "")
	if safe.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET logout = %d, want 405", safe.Code)
	}
}

func TestPrivateGETDoesNotRequireCSRF(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	sessionCookie, _, login := fixture.login(t, authTestPass)
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d", login.Code)
	}
	response := fixture.request(t, http.MethodGet, "/api/v1/monitors", "", sessionCookie, "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("authenticated GET without CSRF = %d %s", response.Code, response.Body.String())
	}
}

func TestFailedLoginSetsNoSessionCookie(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	body := `{"password":"definitely wrong password"}`
	response := fixture.request(t, http.MethodPost, "/api/v1/auth/login", body, "", "", authOrigin)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == auth.DefaultSessionCookieName || cookie.Name == auth.DefaultCSRFCookieName {
			t.Fatalf("failed login set cookie %q", cookie.Name)
		}
	}
	if strings.Contains(strings.ToLower(response.Body.String()), "password") {
		t.Fatalf("error response leaks password wording: %s", response.Body.String())
	}
}

func TestOversizedBodyReturnsSanitized413(t *testing.T) {
	server := securityServer(t, httpapi.Config{MaxBodyBytes: 16}, securityReadiness{})
	req := httptest.NewRequest(http.MethodPost, "http://localhost/api/v1/auth/login", strings.NewReader(`{"password":"a-long-password-value"}`))
	req.Host = "localhost"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"code":"body_too_large"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "password") {
		t.Fatalf("error response leaked request content: %s", recorder.Body.String())
	}
}

func TestReadinessFailureDoesNotLeakInternals(t *testing.T) {
	server := securityServer(t, httpapi.Config{}, securityReadiness{err: errors.New("sqlite: /secret/path is locked")})
	req := httptest.NewRequest(http.MethodGet, "http://localhost/readyz", nil)
	req.Host = "localhost"
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "sqlite") || strings.Contains(body, "/secret/path") {
		t.Fatalf("readiness response leaked internals: %s", body)
	}
	if !strings.Contains(body, `"code":"not_ready"`) {
		t.Fatalf("body = %s", body)
	}
}
