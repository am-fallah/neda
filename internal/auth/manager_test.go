package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/store"
)

var authTestTime = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func newAuthStore(t *testing.T) (*store.Store, *sql.DB) {
	t.Helper()
	database, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "auth-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("database.Close: %v", err)
		}
	})
	applied, err := db.Migrate(context.Background(), database.DB, migrations.FS)
	if err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	if err := database.Ready(context.Background(), applied); err != nil {
		t.Fatalf("database.Ready: %v", err)
	}
	return store.New(database.DB), database.DB
}

func newAuthManager(t *testing.T, secure bool) (*Manager, *store.Store, *sql.DB, *fakeClock) {
	t.Helper()
	st, sqlDB := newAuthStore(t)
	clock := newFakeClock(authTestTime)
	manager, err := NewManager(st, Config{
		SessionTTL:          2 * time.Hour,
		SessionIdle:         time.Hour,
		TouchInterval:       5 * time.Minute,
		Secure:              secure,
		PublicURL:           mustURL(t, "https://neda.example.com"),
		Production:          secure,
		Params:              testParams,
		MaxConcurrentHashes: 1,
		Now:                 clock.Now,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return manager, st, sqlDB, clock
}

func TestLoginRequiresSetup(t *testing.T) {
	manager, _, _, _ := newAuthManager(t, true)
	ctx := context.Background()

	if _, err := manager.Login(ctx, testPassword, "agent"); !errors.Is(err, ErrSetupRequired) {
		t.Fatalf("Login without user = %v, want ErrSetupRequired", err)
	}
	info, err := manager.SessionInfo(ctx, "", "")
	if err != nil {
		t.Fatalf("SessionInfo: %v", err)
	}
	if !info.SetupRequired || info.Authenticated {
		t.Fatalf("SessionInfo = %+v, want setup required", info)
	}
}

func TestSetPasswordLoginAndSessionLifecycle(t *testing.T) {
	manager, st, _, _ := newAuthManager(t, true)
	ctx := context.Background()
	if err := manager.SetPassword(ctx, testPassword); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	user, err := st.GetUser(ctx)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if !strings.HasPrefix(user.PasswordHash, "$argon2id$v=19$") {
		t.Fatalf("password hash = %q, want argon2id PHC", user.PasswordHash)
	}

	result, err := manager.Login(ctx, testPassword, "test-agent")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if result.SessionToken == "" || result.CSRFToken == "" {
		t.Fatal("Login returned empty token")
	}
	if !result.ExpiresAt.Equal(authTestTime.Add(2 * time.Hour)) {
		t.Fatalf("ExpiresAt = %v", result.ExpiresAt)
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(payload), result.SessionToken) {
		t.Fatal("session token was included in JSON")
	}
	if !strings.Contains(string(payload), "csrfToken") {
		t.Fatal("CSRF token missing from JSON")
	}

	session, err := manager.ValidateSession(ctx, result.SessionToken)
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	if session.ID == "" || !session.VerifyCSRF(result.CSRFToken) {
		t.Fatalf("session = %+v, CSRF verification failed", session)
	}
	if session.VerifyCSRF("wrong") {
		t.Fatal("VerifyCSRF accepted wrong token")
	}

	info, err := manager.SessionInfo(ctx, result.SessionToken, result.CSRFToken)
	if err != nil {
		t.Fatalf("SessionInfo: %v", err)
	}
	if !info.Authenticated || info.CSRFToken != result.CSRFToken || info.Session == nil {
		t.Fatalf("SessionInfo = %+v", info)
	}
	info, err = manager.SessionInfo(ctx, result.SessionToken, "")
	if err != nil {
		t.Fatalf("SessionInfo without CSRF: %v", err)
	}
	if info.Authenticated {
		t.Fatal("SessionInfo without CSRF cookie was authenticated")
	}

	stored, err := st.GetSessionByTokenHash(ctx, hashToken(result.SessionToken))
	if err != nil {
		t.Fatalf("GetSessionByTokenHash: %v", err)
	}
	if len(stored.TokenHash) != 32 || len(stored.CSRFHash) != 32 {
		t.Fatalf("stored hashes = %d/%d bytes", len(stored.TokenHash), len(stored.CSRFHash))
	}
	if string(stored.TokenHash) == result.SessionToken || string(stored.CSRFHash) == result.CSRFToken {
		t.Fatal("raw token stored in database")
	}
	if stored.UserAgent != "test-agent" {
		t.Fatalf("UserAgent = %q", stored.UserAgent)
	}

	if err := manager.Logout(ctx, session.ID); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := manager.ValidateSession(ctx, result.SessionToken); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("ValidateSession after logout = %v, want ErrSessionInvalid", err)
	}
	if _, err := manager.Login(ctx, "wrong-password-123", "agent"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login wrong password = %v, want ErrInvalidCredentials", err)
	}
	if _, err := manager.Login(ctx, strings.Repeat("x", MaxPasswordBytes+1), "agent"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login oversized password = %v, want ErrInvalidCredentials", err)
	}
}

func TestSessionIdleAndAbsoluteExpiry(t *testing.T) {
	t.Run("idle", func(t *testing.T) {
		manager, _, _, clock := newAuthManager(t, true)
		ctx := context.Background()
		if err := manager.SetPassword(ctx, testPassword); err != nil {
			t.Fatalf("SetPassword: %v", err)
		}
		result, err := manager.Login(ctx, testPassword, "agent")
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		clock.Advance(61 * time.Minute)
		if _, err := manager.ValidateSession(ctx, result.SessionToken); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("idle ValidateSession = %v, want ErrSessionInvalid", err)
		}
	})

	t.Run("absolute", func(t *testing.T) {
		manager, _, _, clock := newAuthManager(t, true)
		ctx := context.Background()
		if err := manager.SetPassword(ctx, testPassword); err != nil {
			t.Fatalf("SetPassword: %v", err)
		}
		result, err := manager.Login(ctx, testPassword, "agent")
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		clock.Advance(2*time.Hour + time.Second)
		if _, err := manager.ValidateSession(ctx, result.SessionToken); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("absolute ValidateSession = %v, want ErrSessionInvalid", err)
		}
	})
}

func TestSessionTouchThrottling(t *testing.T) {
	manager, st, _, clock := newAuthManager(t, true)
	ctx := context.Background()
	if err := manager.SetPassword(ctx, testPassword); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	result, err := manager.Login(ctx, testPassword, "agent")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	clock.Advance(6 * time.Minute)
	session, err := manager.ValidateSession(ctx, result.SessionToken)
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	if !session.LastSeenAt.Equal(authTestTime.Add(6 * time.Minute)) {
		t.Fatalf("LastSeenAt = %v, want touched time", session.LastSeenAt)
	}
	stored, err := st.GetSessionByTokenHash(ctx, hashToken(result.SessionToken))
	if err != nil {
		t.Fatalf("GetSessionByTokenHash: %v", err)
	}
	if !stored.LastSeenAt.Equal(authTestTime.Add(6 * time.Minute)) {
		t.Fatalf("stored LastSeenAt = %v", stored.LastSeenAt)
	}

	clock.Advance(time.Minute)
	if _, err := manager.ValidateSession(ctx, result.SessionToken); err != nil {
		t.Fatalf("ValidateSession after throttle: %v", err)
	}
	stored, err = st.GetSessionByTokenHash(ctx, hashToken(result.SessionToken))
	if err != nil {
		t.Fatalf("GetSessionByTokenHash after throttle: %v", err)
	}
	if !stored.LastSeenAt.Equal(authTestTime.Add(6 * time.Minute)) {
		t.Fatalf("stored LastSeenAt changed inside throttle = %v", stored.LastSeenAt)
	}
}

func TestPasswordChangeRevokesOtherSessions(t *testing.T) {
	manager, _, _, _ := newAuthManager(t, true)
	ctx := context.Background()
	if err := manager.SetPassword(ctx, testPassword); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	first, err := manager.Login(ctx, testPassword, "agent-one")
	if err != nil {
		t.Fatalf("Login first: %v", err)
	}
	second, err := manager.Login(ctx, testPassword, "agent-two")
	if err != nil {
		t.Fatalf("Login second: %v", err)
	}
	firstSession, err := manager.ValidateSession(ctx, first.SessionToken)
	if err != nil {
		t.Fatalf("ValidateSession first: %v", err)
	}

	newPassword := "new-password-456"
	if err := manager.ChangePassword(ctx, firstSession.ID, "wrong-current", "another-password-789"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("ChangePassword wrong current = %v, want ErrInvalidCredentials", err)
	}
	if err := manager.ChangePassword(ctx, firstSession.ID, testPassword, newPassword); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if _, err := manager.ValidateSession(ctx, first.SessionToken); err != nil {
		t.Fatalf("first session after password change: %v", err)
	}
	if _, err := manager.ValidateSession(ctx, second.SessionToken); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("second session after password change = %v, want ErrSessionInvalid", err)
	}
	if _, err := manager.Login(ctx, testPassword, "agent"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password login = %v, want ErrInvalidCredentials", err)
	}
	if _, err := manager.Login(ctx, newPassword, "agent"); err != nil {
		t.Fatalf("new password login: %v", err)
	}
}

func TestSetPasswordRevokesAllSessions(t *testing.T) {
	manager, _, _, _ := newAuthManager(t, true)
	ctx := context.Background()
	if err := manager.SetPassword(ctx, testPassword); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	result, err := manager.Login(ctx, testPassword, "agent")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	newPassword := "replacement-password-789"
	if err := manager.SetPassword(ctx, newPassword); err != nil {
		t.Fatalf("SetPassword replacement: %v", err)
	}
	if _, err := manager.ValidateSession(ctx, result.SessionToken); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("session after reset = %v, want ErrSessionInvalid", err)
	}
	if _, err := manager.Login(ctx, testPassword, "agent"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password login = %v, want ErrInvalidCredentials", err)
	}
	if _, err := manager.Login(ctx, newPassword, "agent"); err != nil {
		t.Fatalf("new password login: %v", err)
	}
}

func TestUserAgentSanitized(t *testing.T) {
	manager, _, _, _ := newAuthManager(t, true)
	ctx := context.Background()
	if err := manager.SetPassword(ctx, testPassword); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	agent := "  agent\x00\n" + strings.Repeat("x", 300)
	result, err := manager.Login(ctx, testPassword, agent)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	session, err := manager.ValidateSession(ctx, result.SessionToken)
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	stored, err := manager.store.GetSessionByTokenHash(ctx, hashToken(result.SessionToken))
	if err != nil {
		t.Fatalf("GetSessionByTokenHash: %v", err)
	}
	if session.ID != stored.ID {
		t.Fatalf("session id = %q, stored = %q", session.ID, stored.ID)
	}
	if len(stored.UserAgent) > 255 || strings.ContainsAny(stored.UserAgent, "\x00\n") {
		t.Fatalf("sanitized user agent = %q", stored.UserAgent)
	}
	if !strings.HasPrefix(stored.UserAgent, "agent") {
		t.Fatalf("user agent = %q, want agent prefix", stored.UserAgent)
	}
}

func TestNewManagerForcesSecureCookiesInProduction(t *testing.T) {
	st, _ := newAuthStore(t)
	manager, err := NewManager(st, Config{
		SessionTTL:          time.Hour,
		SessionIdle:         30 * time.Minute,
		TouchInterval:       5 * time.Minute,
		Production:          true,
		Secure:              false,
		PublicURL:           mustURL(t, "https://neda.example.com"),
		Params:              testParams,
		MaxConcurrentHashes: 1,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if !manager.cfg.Secure {
		t.Fatal("production manager did not force Secure cookies")
	}
}

func TestProductionHTTPIPUsesNonSecureCookies(t *testing.T) {
	st, _ := newAuthStore(t)
	manager, err := NewManager(st, Config{
		SessionTTL:          time.Hour,
		SessionIdle:         30 * time.Minute,
		TouchInterval:       5 * time.Minute,
		Production:          true,
		Secure:              true,
		PublicURL:           mustURL(t, "http://203.0.113.10:8080"),
		Params:              testParams,
		MaxConcurrentHashes: 1,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if manager.cfg.Secure {
		t.Fatal("HTTP IP production manager requested Secure cookies")
	}
	recorder := httptest.NewRecorder()
	manager.SetSessionCookies(recorder, LoginResult{
		SessionToken: "session-token",
		CSRFToken:    "csrf-token",
		ExpiresAt:    time.Now().Add(time.Hour),
	})
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
}

func TestManagerConfigValidation(t *testing.T) {
	st, _ := newAuthStore(t)
	base := Config{
		SessionTTL:          time.Hour,
		SessionIdle:         30 * time.Minute,
		TouchInterval:       5 * time.Minute,
		Params:              testParams,
		MaxConcurrentHashes: 1,
	}
	tests := []struct {
		name string
		cfg  Config
	}{
		{"idle longer than ttl", func() Config { c := base; c.SessionIdle = 2 * time.Hour; return c }()},
		{"touch not shorter than idle", func() Config { c := base; c.TouchInterval = 30 * time.Minute; return c }()},
		{"same cookie names", func() Config { c := base; c.CookieName = "same"; c.CSRFCookieName = "same"; return c }()},
		{"production without url", func() Config { c := base; c.Production = true; return c }()},
		{"invalid params", func() Config {
			c := base
			c.Params = Params{Memory: 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
			return c
		}()},
		{"zero max hashes", func() Config { c := base; c.MaxConcurrentHashes = -1; return c }()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewManager(st, test.cfg); err == nil {
				t.Fatal("NewManager succeeded, want error")
			}
		})
	}
	if _, err := NewManager(nil, base); err == nil {
		t.Fatal("NewManager(nil store) succeeded")
	}
}
