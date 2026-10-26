package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/store"
)

const (
	// DefaultSessionCookieName is the HttpOnly session cookie.
	DefaultSessionCookieName = "neda_session"
	// DefaultCSRFCookieName stores the raw CSRF token for /auth/session.
	DefaultCSRFCookieName = "neda_csrf"

	defaultSessionTTL   = 720 * time.Hour
	defaultSessionIdle  = 168 * time.Hour
	defaultTouchEvery   = 5 * time.Minute
	defaultMaxHashCalls = 2
)

var (
	// ErrSetupRequired means no owner password row exists.
	ErrSetupRequired = errors.New("auth: password setup required")
	// ErrInvalidCredentials is deliberately generic.
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	// ErrSessionInvalid covers missing, revoked, expired, and idle sessions.
	ErrSessionInvalid = errors.New("auth: session invalid")
	// ErrPasswordChanged means a concurrent password change invalidated login.
	ErrPasswordChanged = errors.New("auth: password changed")
)

// Config controls the auth manager. Zero durations receive documented defaults.
type Config struct {
	SessionTTL          time.Duration
	SessionIdle         time.Duration
	TouchInterval       time.Duration
	CookieName          string
	CSRFCookieName      string
	Secure              bool
	PublicURL           *url.URL
	Production          bool
	Params              Params
	MaxConcurrentHashes int
	Now                 func() time.Time
}

// Manager owns password, session, and CSRF operations.
type Manager struct {
	store     *store.Store
	cfg       Config
	origins   OriginPolicy
	verifySem chan struct{}
}

// Session is the authenticated context attached by RequireSession. Token and
// CSRF hashes are not exposed in JSON.
type Session struct {
	ID         string    `json:"id"`
	ExpiresAt  time.Time `json:"expiresAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`

	csrfHash []byte
}

// VerifyCSRF compares a raw token to the stored hash in constant time.
func (s *Session) VerifyCSRF(raw string) bool {
	if s == nil || raw == "" || len(s.csrfHash) != 32 {
		return false
	}
	got := hashToken(raw)
	return subtle.ConstantTimeCompare(got, s.csrfHash) == 1
}

// LoginResult returns the raw CSRF token to the API. The session token is
// marked json:"-" so it can only be placed in the HttpOnly cookie.
type LoginResult struct {
	SessionToken string    `json:"-"`
	CSRFToken    string    `json:"csrfToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

// SessionInfo is the auth probe result.
type SessionInfo struct {
	Authenticated bool     `json:"authenticated"`
	CSRFToken     string   `json:"csrfToken,omitempty"`
	SetupRequired bool     `json:"setupRequired,omitempty"`
	Session       *Session `json:"-"`
}

// NewManager validates cfg and constructs an auth manager.
func NewManager(st *store.Store, cfg Config) (*Manager, error) {
	if st == nil {
		return nil, errors.New("auth: nil store")
	}
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = defaultSessionTTL
	}
	if cfg.SessionIdle == 0 {
		cfg.SessionIdle = defaultSessionIdle
	}
	if cfg.TouchInterval == 0 {
		cfg.TouchInterval = defaultTouchEvery
	}
	if cfg.CookieName == "" {
		cfg.CookieName = DefaultSessionCookieName
	}
	if cfg.CSRFCookieName == "" {
		cfg.CSRFCookieName = DefaultCSRFCookieName
	}
	if cfg.Params == (Params{}) {
		cfg.Params = DefaultParams
	}
	if cfg.MaxConcurrentHashes == 0 {
		cfg.MaxConcurrentHashes = defaultMaxHashCalls
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.SessionTTL <= 0 || cfg.SessionIdle <= 0 {
		return nil, errors.New("auth: session durations must be positive")
	}
	if cfg.SessionIdle > cfg.SessionTTL {
		return nil, errors.New("auth: session idle timeout must not exceed absolute TTL")
	}
	if cfg.TouchInterval <= 0 || cfg.TouchInterval >= cfg.SessionIdle {
		return nil, errors.New("auth: touch interval must be positive and shorter than idle timeout")
	}
	if strings.TrimSpace(cfg.CookieName) == "" || strings.TrimSpace(cfg.CSRFCookieName) == "" {
		return nil, errors.New("auth: cookie names must not be empty")
	}
	if cfg.CookieName == cfg.CSRFCookieName {
		return nil, errors.New("auth: session and CSRF cookie names must differ")
	}
	if cfg.MaxConcurrentHashes < 1 {
		return nil, errors.New("auth: max concurrent hashes must be positive")
	}
	if err := cfg.Params.validate(); err != nil {
		return nil, err
	}
	if cfg.Production && cfg.PublicURL == nil {
		return nil, errors.New("auth: production requires a public URL for origin checks")
	}
	if cfg.Production {
		// Secure cookies only work over HTTPS. Production may deliberately use
		// plain HTTP for direct IP access, but that mode is gated by config.
		cfg.Secure = strings.EqualFold(cfg.PublicURL.Scheme, "https")
	}

	return &Manager{
		store:     st,
		cfg:       cfg,
		origins:   OriginPolicy{PublicURL: cfg.PublicURL, Production: cfg.Production},
		verifySem: make(chan struct{}, cfg.MaxConcurrentHashes),
	}, nil
}

// SetPassword hashes and stores a new owner password, revoking every session.
// It is used by the offline TTY-only CLI.
func SetPassword(ctx context.Context, st *store.Store, password string, params Params, now time.Time) error {
	if st == nil {
		return errors.New("auth: nil store")
	}
	if now.IsZero() {
		return errors.New("auth: password timestamp must be set")
	}
	if err := ValidatePassword(password); err != nil {
		return err
	}
	hash, err := HashPassword(password, params)
	if err != nil {
		return err
	}
	return st.WithTx(ctx, func(tx *store.Store) error {
		if err := tx.UpsertUser(ctx, hash, now); err != nil {
			return err
		}
		if _, err := tx.RevokeAllSessions(ctx, now); err != nil {
			return err
		}
		return nil
	})
}

// SetPassword is the manager-bound variant used by tests and future callers.
func (m *Manager) SetPassword(ctx context.Context, password string) error {
	return SetPassword(ctx, m.store, password, m.cfg.Params, m.cfg.Now())
}

// ChangePassword verifies the current password, updates the hash, and revokes
// every session except currentSessionID.
func (m *Manager) ChangePassword(ctx context.Context, currentSessionID, currentPassword, newPassword string) error {
	if strings.TrimSpace(currentSessionID) == "" {
		return errors.New("auth: current session id is required")
	}
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	if !passwordWithinLoginBounds(currentPassword) {
		return ErrInvalidCredentials
	}
	user, err := m.store.GetUser(ctx)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrSetupRequired
		}
		return err
	}
	ok, err := m.verify(ctx, currentPassword, user.PasswordHash)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalidCredentials
	}
	hash, err := HashPassword(newPassword, m.cfg.Params)
	if err != nil {
		return err
	}
	now := m.cfg.Now()
	return m.store.WithTx(ctx, func(tx *store.Store) error {
		if err := tx.UpdateUserPassword(ctx, hash, now, user.PasswordChangedAt); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return ErrPasswordChanged
			}
			return err
		}
		if _, err := tx.RevokeOtherSessions(ctx, currentSessionID, now); err != nil {
			return err
		}
		return nil
	})
}

// Login verifies the password and creates a fresh session. It never accepts a
// pre-authenticated session; the caller sets the returned cookie.
func (m *Manager) Login(ctx context.Context, password, userAgent string) (LoginResult, error) {
	if !passwordWithinLoginBounds(password) {
		return LoginResult{}, ErrInvalidCredentials
	}
	user, err := m.store.GetUser(ctx)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return LoginResult{}, ErrSetupRequired
		}
		return LoginResult{}, err
	}
	ok, err := m.verify(ctx, password, user.PasswordHash)
	if err != nil {
		return LoginResult{}, err
	}
	if !ok {
		return LoginResult{}, ErrInvalidCredentials
	}

	token, err := randomToken(sessionTokenBytes)
	if err != nil {
		return LoginResult{}, err
	}
	csrf, err := randomToken(csrfTokenBytes)
	if err != nil {
		return LoginResult{}, err
	}
	id, err := randomToken(sessionIDBytes)
	if err != nil {
		return LoginResult{}, err
	}
	now := m.cfg.Now()
	expires := now.Add(m.cfg.SessionTTL)
	session := store.Session{
		ID:         id,
		TokenHash:  hashToken(token),
		CSRFHash:   hashToken(csrf),
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  expires,
		UserAgent:  sanitizeUserAgent(userAgent),
	}

	err = m.store.WithTx(ctx, func(tx *store.Store) error {
		current, err := tx.GetUser(ctx)
		if err != nil {
			return err
		}
		if !current.PasswordChangedAt.Equal(user.PasswordChangedAt) {
			return ErrPasswordChanged
		}
		return tx.CreateSession(ctx, session)
	})
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{SessionToken: token, CSRFToken: csrf, ExpiresAt: expires}, nil
}

// ValidateSession verifies a raw session cookie and applies idle/absolute
// expiry. It throttles last_seen_at writes.
func (m *Manager) ValidateSession(ctx context.Context, rawToken string) (*Session, error) {
	if strings.TrimSpace(rawToken) == "" {
		return nil, ErrSessionInvalid
	}
	stored, err := m.store.GetSessionByTokenHash(ctx, hashToken(rawToken))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrSessionInvalid
		}
		return nil, err
	}
	if stored.RevokedAt != nil {
		return nil, ErrSessionInvalid
	}
	now := m.cfg.Now()
	if !now.Before(stored.ExpiresAt) {
		return nil, ErrSessionInvalid
	}
	if now.Sub(stored.LastSeenAt) >= m.cfg.SessionIdle {
		return nil, ErrSessionInvalid
	}

	session := &Session{
		ID:         stored.ID,
		ExpiresAt:  stored.ExpiresAt,
		LastSeenAt: stored.LastSeenAt,
		csrfHash:   stored.CSRFHash,
	}
	if now.Sub(stored.LastSeenAt) >= m.cfg.TouchInterval {
		if err := m.store.TouchSession(ctx, stored.ID, now); err == nil {
			session.LastSeenAt = now
		} else if errors.Is(err, store.ErrNotFound) {
			return nil, ErrSessionInvalid
		}
	}
	return session, nil
}

// Logout revokes one session.
func (m *Manager) Logout(ctx context.Context, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return errors.New("auth: session id is required")
	}
	return m.store.RevokeSession(ctx, sessionID, m.cfg.Now())
}

// SessionInfo validates a session plus the CSRF cookie and returns the probe
// result. A valid session with a missing/mismatched CSRF cookie is treated as
// unauthenticated so the client is forced to log in again.
func (m *Manager) SessionInfo(ctx context.Context, rawToken, rawCSRF string) (SessionInfo, error) {
	if _, err := m.store.GetUser(ctx); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return SessionInfo{SetupRequired: true}, nil
		}
		return SessionInfo{}, err
	}
	if strings.TrimSpace(rawToken) == "" {
		return SessionInfo{}, nil
	}
	session, err := m.ValidateSession(ctx, rawToken)
	if err != nil {
		if errors.Is(err, ErrSessionInvalid) {
			return SessionInfo{}, nil
		}
		return SessionInfo{}, err
	}
	if !session.VerifyCSRF(rawCSRF) {
		return SessionInfo{}, nil
	}
	return SessionInfo{Authenticated: true, CSRFToken: rawCSRF, Session: session}, nil
}

// VerifyCSRF validates a raw CSRF token against an authenticated session.
func (m *Manager) VerifyCSRF(session *Session, raw string) bool {
	return session != nil && session.VerifyCSRF(raw)
}

func (m *Manager) verify(ctx context.Context, password, encoded string) (bool, error) {
	select {
	case m.verifySem <- struct{}{}:
		defer func() { <-m.verifySem }()
	case <-ctx.Done():
		return false, ctx.Err()
	}
	return VerifyPassword(password, encoded)
}

func (m *Manager) cookieMaxAge() int {
	return int(m.cfg.SessionTTL.Seconds())
}
