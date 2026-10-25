package httpapi_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/auth"
	"github.com/am-fallah/neda/internal/crypto"
	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/httpapi"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/notify/telegramsettings"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/provider/fake"
	"github.com/am-fallah/neda/internal/push"
	appsettings "github.com/am-fallah/neda/internal/settings"
	"github.com/am-fallah/neda/internal/store"
)

const (
	authTestPass = "correct horse battery staple"
	authNextPass = "another correct horse battery"
	authOrigin   = "https://neda.example.com"
	authHost     = "neda.example.com"
)

type authReadiness struct{}

func (authReadiness) Ready(context.Context, int) error { return nil }

type authClock struct{ now time.Time }

func (c *authClock) Now() time.Time { return c.now }

type permissiveProvider struct{ id provider.ID }

func (p permissiveProvider) Metadata() provider.Metadata {
	return provider.Metadata{ID: p.id, DisplayName: "permissive test provider", TestOnly: true}
}

func (p permissiveProvider) ValidateConfig(provider.MonitorProviderConfig) error { return nil }

func (p permissiveProvider) Check(context.Context, provider.CheckRequest) (provider.Result, error) {
	return provider.Result{ObservedAt: time.Now().UTC()}, nil
}

type authFixture struct {
	server         *httpapi.Server
	manager        *auth.Manager
	monitors       *domain.MonitorService
	settings       *appsettings.Service
	store          *store.Store
	providers      *provider.Registry
	telegramTester *fakeTelegramTester
	push           *push.Service
	pushTester     *fakePushTester
	db             *sql.DB
	clock          *authClock
	logs           *bytes.Buffer
}

type fakePushTester struct {
	outcome push.TestOutcome
	err     error
	calls   int
	id      string
}

func (f *fakePushTester) Test(_ context.Context, id string) (push.TestOutcome, error) {
	f.calls++
	f.id = id
	return f.outcome, f.err
}

type fakeTelegramTester struct {
	mu         sync.Mutex
	getMeErr   error
	sendErr    error
	getMeCalls int
	sendCalls  int
	lastText   string
}

func (f *fakeTelegramTester) GetMe(context.Context, notify.TelegramCredentials) (notify.TelegramBot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getMeCalls++
	if f.getMeErr != nil {
		return notify.TelegramBot{}, f.getMeErr
	}
	return notify.TelegramBot{ID: 1, Username: "neda_test_bot", FirstName: "Neda"}, nil
}

func (f *fakeTelegramTester) SendMessage(_ context.Context, _ notify.TelegramCredentials, text string) (notify.TelegramMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendCalls++
	f.lastText = text
	if f.sendErr != nil {
		return notify.TelegramMessage{}, f.sendErr
	}
	return notify.TelegramMessage{MessageID: 1, ChatID: 1, SentAt: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}, nil
}

func (f *fakeTelegramTester) setErrors(getMeErr, sendErr error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getMeErr = getMeErr
	f.sendErr = sendErr
}

func newAuthFixture(t *testing.T, limiterConfig auth.RateLimitConfig, withPassword bool) *authFixture {
	t.Helper()
	return newAuthFixtureWithTelegram(t, limiterConfig, withPassword, true)
}

func newAuthFixtureWithTelegram(t *testing.T, limiterConfig auth.RateLimitConfig, withPassword, withTelegram bool, extra ...provider.Provider) *authFixture {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "httpapi-auth.db"))
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
	clock := &authClock{now: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}
	publicURL, _ := url.Parse(authOrigin)
	manager, err := auth.NewManager(st, auth.Config{
		SessionTTL: 2 * time.Hour, SessionIdle: time.Hour, TouchInterval: 5 * time.Minute,
		Secure: true, PublicURL: publicURL, Production: true,
		Params:              auth.Params{Memory: 8192, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 16},
		MaxConcurrentHashes: 1,
		Now:                 clock.Now,
	})
	if err != nil {
		t.Fatalf("auth.NewManager: %v", err)
	}
	limiter, err := auth.NewRateLimiter(limiterConfig, clock.Now)
	if err != nil {
		t.Fatalf("auth.NewRateLimiter: %v", err)
	}
	guard := auth.NewLoginGuard(auth.OriginPolicy{PublicURL: publicURL, Production: true}, limiter, false)
	registered := []provider.Provider{fake.NewWithClock(clock.Now), permissiveProvider{id: "alt"}}
	registered = append(registered, extra...)
	registry, err := provider.NewRegistry(registered...)
	if err != nil {
		t.Fatalf("provider.NewRegistry: %v", err)
	}
	monitors, err := domain.NewMonitorService(st, registry, domain.MonitorServiceConfig{
		Now: clock.Now,
		NearTermSchedule: func(now time.Time) time.Time {
			return now.Add(time.Second)
		},
	})
	if err != nil {
		t.Fatalf("domain.NewMonitorService: %v", err)
	}
	var telegramSettings *telegramsettings.Settings
	var telegramTester *fakeTelegramTester
	var pushService *push.Service
	var pushTester *fakePushTester
	if withTelegram {
		material := make([]byte, crypto.KeySize)
		for index := range material {
			material[index] = 0x42
		}
		key, err := crypto.NewKey(material)
		if err != nil {
			t.Fatalf("crypto.NewKey: %v", err)
		}
		telegramSettings, err = telegramsettings.New(st, key, clock.Now)
		if err != nil {
			t.Fatalf("telegramsettings.New: %v", err)
		}
		telegramTester = &fakeTelegramTester{}
		pushService, err = push.New(st, database.DB, key, "mailto:owner@example.com", clock.Now)
		if err != nil {
			t.Fatalf("push.New: %v", err)
		}
		if err := pushService.Ensure(ctx); err != nil {
			t.Fatalf("push.Ensure: %v", err)
		}
		pushTester = &fakePushTester{}
	}
	settingsService, err := appsettings.New(st, telegramSettings, telegramTester, clock.Now)
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	logs := &bytes.Buffer{}
	server := httpapi.New(httpapi.Config{
		Production: true,
		PublicURL:  publicURL,
		Auth:       manager,
		LoginGuard: guard,
		Monitors:   monitors,
		Settings:   settingsService,
		Store:      st,
		Providers:  registry,
		Push:       pushService,
		PushTester: pushTester,
		Now:        clock.Now,
	}, slog.New(slog.NewJSONHandler(logs, nil)), authReadiness{}, applied)
	if withPassword {
		if err := manager.SetPassword(ctx, authTestPass); err != nil {
			t.Fatalf("SetPassword: %v", err)
		}
	}
	return &authFixture{
		server: server, manager: manager, monitors: monitors, settings: settingsService,
		store: st, providers: registry, telegramTester: telegramTester,
		push: pushService, pushTester: pushTester,
		db: database.DB, clock: clock, logs: logs,
	}
}

func (f *authFixture) request(t *testing.T, method, path, body, sessionCookie, csrf, origin string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, authOrigin+path, strings.NewReader(body))
	req.Host = authHost
	req.RemoteAddr = "203.0.113.9:12345"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if sessionCookie != "" {
		req.AddCookie(&http.Cookie{Name: auth.DefaultSessionCookieName, Value: sessionCookie})
	}
	if csrf != "" {
		req.AddCookie(&http.Cookie{Name: auth.DefaultCSRFCookieName, Value: csrf})
		req.Header.Set("X-CSRF-Token", csrf)
	}
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, req)
	return recorder
}

func (f *authFixture) login(t *testing.T, password string) (sessionCookie, csrf string, recorder *httptest.ResponseRecorder) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"password": password})
	recorder = f.request(t, http.MethodPost, "/api/v1/auth/login", string(body), "", "", authOrigin)
	if recorder.Code != http.StatusOK {
		return "", "", recorder
	}
	var payload struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("login JSON: %v", err)
	}
	csrf = payload.CSRFToken
	for _, cookie := range recorder.Result().Cookies() {
		switch cookie.Name {
		case auth.DefaultSessionCookieName:
			sessionCookie = cookie.Value
		case auth.DefaultCSRFCookieName:
			if cookie.Value != csrf {
				t.Fatalf("CSRF cookie/body mismatch: %q/%q", cookie.Value, csrf)
			}
		}
	}
	return sessionCookie, csrf, recorder
}

func TestLoginSessionAndCookieSecurity(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	sessionCookie, csrf, recorder := fixture.login(t, authTestPass)
	if recorder.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if sessionCookie == "" || csrf == "" {
		t.Fatalf("login cookies = %q/%q", sessionCookie, csrf)
	}
	if strings.Contains(recorder.Body.String(), sessionCookie) {
		t.Fatal("login response leaked the session cookie")
	}
	for _, cookie := range recorder.Result().Cookies() {
		if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
			t.Fatalf("cookie flags = %+v", cookie)
		}
	}
	if recorder.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("unexpected CORS header")
	}

	session := fixture.request(t, http.MethodGet, "/api/v1/auth/session", "", sessionCookie, csrf, "")
	if session.Code != http.StatusOK {
		t.Fatalf("session status = %d body=%s", session.Code, session.Body.String())
	}
	var payload struct {
		Authenticated bool   `json:"authenticated"`
		CSRFToken     string `json:"csrfToken"`
		SetupRequired bool   `json:"setupRequired"`
	}
	if err := json.Unmarshal(session.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Authenticated || payload.CSRFToken != csrf || payload.SetupRequired {
		t.Fatalf("session payload = %+v", payload)
	}

	anonymous := fixture.request(t, http.MethodGet, "/api/v1/auth/session", "", "", "", "")
	if anonymous.Code != http.StatusOK || !strings.Contains(anonymous.Body.String(), `"authenticated":false`) {
		t.Fatalf("anonymous session = %d %s", anonymous.Code, anonymous.Body.String())
	}
}

func TestLoginRejectsOriginAndRateLimits(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	body, _ := json.Marshal(map[string]string{"password": authTestPass})
	if recorder := fixture.request(t, http.MethodPost, "/api/v1/auth/login", string(body), "", "", ""); recorder.Code != http.StatusForbidden {
		t.Fatalf("missing-origin status = %d", recorder.Code)
	}
	if recorder := fixture.request(t, http.MethodPost, "/api/v1/auth/login", string(body), "", "", "https://evil.example"); recorder.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d", recorder.Code)
	}

	limited := newAuthFixture(t, auth.RateLimitConfig{
		SoftLimit: 1, HardLimit: 2, Window: time.Minute, LockDuration: time.Minute,
		SoftRetryAfter: time.Second, MaxEntries: 10,
	}, true)
	wrongBody, _ := json.Marshal(map[string]string{"password": "wrong password value"})
	first := limited.request(t, http.MethodPost, "/api/v1/auth/login", string(wrongBody), "", "", authOrigin)
	if first.Code != http.StatusUnauthorized {
		t.Fatalf("first failure status = %d", first.Code)
	}
	second := limited.request(t, http.MethodPost, "/api/v1/auth/login", string(wrongBody), "", "", authOrigin)
	if second.Code != http.StatusTooManyRequests || second.Header().Get("Retry-After") == "" {
		t.Fatalf("rate-limit status/headers = %d/%q", second.Code, second.Header().Get("Retry-After"))
	}
}

func TestLoginSetupRequired(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), false)
	body, _ := json.Marshal(map[string]string{"password": authTestPass})
	login := fixture.request(t, http.MethodPost, "/api/v1/auth/login", string(body), "", "", authOrigin)
	if login.Code != http.StatusServiceUnavailable || !strings.Contains(login.Body.String(), `"code":"setup_required"`) {
		t.Fatalf("setup login = %d %s", login.Code, login.Body.String())
	}
	probe := fixture.request(t, http.MethodGet, "/api/v1/auth/session", "", "", "", "")
	if probe.Code != http.StatusOK || !strings.Contains(probe.Body.String(), `"setupRequired":true`) {
		t.Fatalf("setup probe = %d %s", probe.Code, probe.Body.String())
	}
}

func TestPrivateAuthRequiresSessionCSRFAndOrigin(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	sessionCookie, csrf, _ := fixture.login(t, authTestPass)

	if response := fixture.request(t, http.MethodPost, "/api/v1/auth/logout", "", "", "", authOrigin); response.Code != http.StatusUnauthorized {
		t.Fatalf("no-session logout = %d", response.Code)
	}
	if response := fixture.request(t, http.MethodPost, "/api/v1/auth/logout", "", sessionCookie, "", authOrigin); response.Code != http.StatusForbidden {
		t.Fatalf("no-CSRF logout = %d", response.Code)
	}
	if response := fixture.request(t, http.MethodPost, "/api/v1/auth/logout", "", sessionCookie, csrf, "https://evil.example"); response.Code != http.StatusForbidden {
		t.Fatalf("wrong-origin logout = %d", response.Code)
	}
	response := fixture.request(t, http.MethodPost, "/api/v1/auth/logout", "", sessionCookie, csrf, authOrigin)
	if response.Code != http.StatusNoContent {
		t.Fatalf("logout = %d %s", response.Code, response.Body.String())
	}
	cleared := 0
	for _, cookie := range response.Result().Cookies() {
		if cookie.MaxAge < 0 {
			cleared++
		}
	}
	if cleared != 2 {
		t.Fatalf("cleared cookies = %d", cleared)
	}
	probe := fixture.request(t, http.MethodGet, "/api/v1/auth/session", "", sessionCookie, csrf, "")
	if !strings.Contains(probe.Body.String(), `"authenticated":false`) {
		t.Fatalf("post-logout probe = %s", probe.Body.String())
	}
}

func TestPasswordChangeAndValidation(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	sessionCookie, csrf, _ := fixture.login(t, authTestPass)

	weak, _ := json.Marshal(map[string]string{"currentPassword": authTestPass, "newPassword": "short"})
	if response := fixture.request(t, http.MethodPost, "/api/v1/auth/password", string(weak), sessionCookie, csrf, authOrigin); response.Code != http.StatusBadRequest {
		t.Fatalf("weak password = %d %s", response.Code, response.Body.String())
	}
	wrongCurrent, _ := json.Marshal(map[string]string{"currentPassword": "wrong password", "newPassword": authNextPass})
	if response := fixture.request(t, http.MethodPost, "/api/v1/auth/password", string(wrongCurrent), sessionCookie, csrf, authOrigin); response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current password = %d %s", response.Code, response.Body.String())
	}
	change, _ := json.Marshal(map[string]string{"currentPassword": authTestPass, "newPassword": authNextPass})
	if response := fixture.request(t, http.MethodPost, "/api/v1/auth/password", string(change), sessionCookie, csrf, authOrigin); response.Code != http.StatusNoContent {
		t.Fatalf("password change = %d %s", response.Code, response.Body.String())
	}
	if _, _, response := fixture.login(t, authTestPass); response.Code != http.StatusUnauthorized {
		t.Fatalf("old password login = %d", response.Code)
	}
	if _, _, response := fixture.login(t, authNextPass); response.Code != http.StatusOK {
		t.Fatalf("new password login = %d %s", response.Code, response.Body.String())
	}
}

func TestAuthJSONValidationAndMethodHandling(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	for _, tc := range []struct {
		name string
		body string
	}{
		{"malformed", "{"},
		{"unknown-field", `{"password":"x","extra":true}`},
		{"empty", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := fixture.request(t, http.MethodPost, "/api/v1/auth/login", tc.body, "", "", authOrigin)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
		})
	}
	if response := fixture.request(t, http.MethodGet, "/api/v1/auth/login", "", "", "", authOrigin); response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET login = %d", response.Code)
	}
	if response := fixture.request(t, http.MethodPost, "/api/v1/auth/session", "", "", "", ""); response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST session = %d", response.Code)
	}
}
