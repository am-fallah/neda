package httpapi_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/auth"
	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/httpapi"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/store"
)

// TestDirectHTTPIPLogin exercises the complete browser-facing path for an
// explicitly enabled plain-HTTP deployment: IP Host header, IP Origin, session
// cookie issuance, and the absence of HSTS.
func TestDirectHTTPIPLogin(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "direct-http.db"))
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

	st := store.New(database.DB)
	params := auth.Params{Memory: 8192, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 16}
	const password = "correct horse battery staple"
	if err := auth.SetPassword(ctx, st, password, params, time.Now()); err != nil {
		t.Fatalf("auth.SetPassword: %v", err)
	}

	publicURL, err := url.Parse("http://203.0.113.10:8080")
	if err != nil {
		t.Fatal(err)
	}
	manager, err := auth.NewManager(st, auth.Config{
		SessionTTL:          time.Hour,
		SessionIdle:         30 * time.Minute,
		TouchInterval:       5 * time.Minute,
		Secure:              true,
		PublicURL:           publicURL,
		Production:          true,
		Params:              params,
		MaxConcurrentHashes: 1,
	})
	if err != nil {
		t.Fatalf("auth.NewManager: %v", err)
	}
	limiter, err := auth.NewRateLimiter(auth.DefaultRateLimitConfig(), time.Now)
	if err != nil {
		t.Fatalf("auth.NewRateLimiter: %v", err)
	}
	guard := auth.NewLoginGuard(auth.OriginPolicy{PublicURL: publicURL, Production: true}, limiter, false)
	server := httpapi.New(httpapi.Config{
		Production: true,
		PublicURL:  publicURL,
		Auth:       manager,
		LoginGuard: guard,
	}, slog.New(slog.NewJSONHandler(io.Discard, nil)), authReadiness{}, applied)

	request := httptest.NewRequest(http.MethodPost, publicURL.String()+"/api/v1/auth/login",
		strings.NewReader(`{"password":"`+password+`"}`))
	request.Host = publicURL.Host
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", publicURL.String())
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "password") {
		t.Fatalf("login response leaked password wording: %s", recorder.Body.String())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("cookies = %d, want 2", len(cookies))
	}
	for _, cookie := range cookies {
		if cookie.Secure {
			t.Fatalf("HTTP IP cookie %q is Secure", cookie.Name)
		}
		if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
			t.Fatalf("cookie %q flags = HttpOnly:%v SameSite:%v", cookie.Name, cookie.HttpOnly, cookie.SameSite)
		}
	}
	if got := recorder.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("HTTP IP login returned HSTS %q", got)
	}

	sessionRequest := httptest.NewRequest(http.MethodGet, publicURL.String()+"/api/v1/auth/session", nil)
	sessionRequest.Host = publicURL.Host
	for _, cookie := range cookies {
		sessionRequest.AddCookie(cookie)
	}
	sessionRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(sessionRecorder, sessionRequest)
	if sessionRecorder.Code != http.StatusOK {
		t.Fatalf("session status = %d, body=%s", sessionRecorder.Code, sessionRecorder.Body.String())
	}
	if !strings.Contains(sessionRecorder.Body.String(), `"authenticated":true`) {
		t.Fatalf("session body = %s", sessionRecorder.Body.String())
	}
}
