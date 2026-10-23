package auth

import (
	"errors"
	"math"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// ErrRateLimited reports that the login guard is temporarily blocking attempts.
var ErrRateLimited = errors.New("auth: rate limited")

// RateLimitConfig controls the in-memory login limiter. It is intentionally
// process-local; a restart resets it.
type RateLimitConfig struct {
	SoftLimit      int
	HardLimit      int
	Window         time.Duration
	LockDuration   time.Duration
	SoftRetryAfter time.Duration
	MaxEntries     int
}

// DefaultRateLimitConfig is the documented provisional policy.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		SoftLimit:      5,
		HardLimit:      10,
		Window:         15 * time.Minute,
		LockDuration:   15 * time.Minute,
		SoftRetryAfter: time.Second,
		MaxEntries:     10000,
	}
}

func (c RateLimitConfig) validate() error {
	switch {
	case c.SoftLimit < 1:
		return errors.New("auth: rate-limit soft limit must be positive")
	case c.HardLimit < c.SoftLimit:
		return errors.New("auth: rate-limit hard limit must be at least the soft limit")
	case c.Window <= 0:
		return errors.New("auth: rate-limit window must be positive")
	case c.LockDuration <= 0:
		return errors.New("auth: rate-limit lock duration must be positive")
	case c.SoftRetryAfter <= 0:
		return errors.New("auth: rate-limit soft retry delay must be positive")
	case c.MaxEntries < 1:
		return errors.New("auth: rate-limit max entries must be positive")
	}
	return nil
}

type limitEntry struct {
	failures    int
	windowStart time.Time
	lastFailure time.Time
	notBefore   time.Time
	lockedUntil time.Time
}

// RateLimiter tracks failures per client key and globally.
type RateLimiter struct {
	mu          sync.Mutex
	cfg         RateLimitConfig
	now         func() time.Time
	entries     map[string]*limitEntry
	global      limitEntry
	lastCleanup time.Time
}

// NewRateLimiter constructs a limiter. A nil clock uses time.Now.
func NewRateLimiter(cfg RateLimitConfig, now func() time.Time) (*RateLimiter, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	return &RateLimiter{
		cfg:     cfg,
		now:     now,
		entries: make(map[string]*limitEntry),
	}, nil
}

// Allow reports whether a login attempt may proceed. When false, retryAfter is
// the minimum time the caller should advertise before another attempt.
func (r *RateLimiter) Allow(key string) (time.Duration, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	r.cleanupLocked(now)
	entry := r.entryLocked(key)

	if retry, blocked := evaluateLimit(entry, r.cfg, now); blocked {
		return retry, false
	}
	if retry, blocked := evaluateLimit(&r.global, r.cfg, now); blocked {
		return retry, false
	}
	return 0, true
}

// Failure records one failed login for key and globally.
func (r *RateLimiter) Failure(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	entry := r.entryLocked(key)
	recordFailure(entry, r.cfg, now)
	recordFailure(&r.global, r.cfg, now)
}

// Success clears the per-key and global failure state.
func (r *RateLimiter) Success(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.entries, normalizeKey(key))
	r.global = limitEntry{}
}

func (r *RateLimiter) entryLocked(key string) *limitEntry {
	key = normalizeKey(key)
	entry, ok := r.entries[key]
	if !ok {
		if len(r.entries) >= r.cfg.MaxEntries {
			r.pruneLocked(r.now())
		}
		entry = &limitEntry{}
		r.entries[key] = entry
	}
	return entry
}

func normalizeKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "unknown"
	}
	return key
}

func evaluateLimit(entry *limitEntry, cfg RateLimitConfig, now time.Time) (time.Duration, bool) {
	if entry == nil {
		return 0, false
	}
	if !entry.lockedUntil.IsZero() && entry.lockedUntil.After(now) {
		return entry.lockedUntil.Sub(now), true
	}
	if !entry.lockedUntil.IsZero() && !entry.lockedUntil.After(now) {
		*entry = limitEntry{}
	}
	if !entry.windowStart.IsZero() && now.Sub(entry.windowStart) > cfg.Window {
		*entry = limitEntry{}
	}
	if !entry.notBefore.IsZero() && entry.notBefore.After(now) {
		return entry.notBefore.Sub(now), true
	}
	return 0, false
}

func recordFailure(entry *limitEntry, cfg RateLimitConfig, now time.Time) {
	if entry == nil || (!entry.lockedUntil.IsZero() && entry.lockedUntil.After(now)) {
		return
	}
	if !entry.lockedUntil.IsZero() {
		*entry = limitEntry{}
	}
	if entry.windowStart.IsZero() || now.Sub(entry.windowStart) > cfg.Window {
		*entry = limitEntry{windowStart: now}
	}
	entry.failures++
	entry.lastFailure = now
	if entry.failures >= cfg.HardLimit {
		entry.lockedUntil = now.Add(cfg.LockDuration)
		return
	}
	if entry.failures >= cfg.SoftLimit {
		entry.notBefore = now.Add(cfg.SoftRetryAfter)
	}
}

func (r *RateLimiter) cleanupLocked(now time.Time) {
	if !r.lastCleanup.IsZero() && now.Sub(r.lastCleanup) < time.Minute {
		return
	}
	r.lastCleanup = now
	r.pruneLocked(now)
}

func (r *RateLimiter) pruneLocked(now time.Time) {
	maxAge := r.cfg.Window
	if r.cfg.LockDuration > maxAge {
		maxAge = r.cfg.LockDuration
	}
	for key, entry := range r.entries {
		if entry == nil {
			delete(r.entries, key)
			continue
		}
		if !entry.lockedUntil.IsZero() && entry.lockedUntil.After(now) {
			continue
		}
		if entry.lastFailure.IsZero() || now.Sub(entry.lastFailure) > maxAge {
			delete(r.entries, key)
		}
	}
	for len(r.entries) >= r.cfg.MaxEntries {
		var oldestKey string
		var oldest time.Time
		for key, entry := range r.entries {
			if entry == nil || (!entry.lockedUntil.IsZero() && entry.lockedUntil.After(now)) {
				continue
			}
			if oldestKey == "" || entry.lastFailure.Before(oldest) {
				oldestKey = key
				oldest = entry.lastFailure
			}
		}
		if oldestKey == "" {
			break
		}
		delete(r.entries, oldestKey)
	}
}

// RetryAfterSeconds converts a retry duration to a positive Retry-After value.
func RetryAfterSeconds(duration time.Duration) int {
	if duration <= 0 {
		return 1
	}
	seconds := int(math.Ceil(duration.Seconds()))
	if seconds < 1 {
		return 1
	}
	return seconds
}

// LoginGuard combines login origin validation with rate limiting. Successful
// logins reset the limiter; failed password checks call Failure.
type LoginGuard struct {
	origins    OriginPolicy
	limiter    *RateLimiter
	trustProxy bool
}

// NewLoginGuard constructs a login guard.
func NewLoginGuard(origins OriginPolicy, limiter *RateLimiter, trustProxy bool) *LoginGuard {
	return &LoginGuard{origins: origins, limiter: limiter, trustProxy: trustProxy}
}

// Check validates the request origin and rate limit before password hashing.
func (g *LoginGuard) Check(r *http.Request) (time.Duration, error) {
	if g == nil {
		return 0, ErrOriginNotAllowed
	}
	if err := g.origins.Check(r); err != nil {
		return 0, ErrOriginNotAllowed
	}
	if g.limiter == nil {
		return 0, nil
	}
	retry, allowed := g.limiter.Allow(ClientIP(r, g.trustProxy))
	if !allowed {
		return retry, ErrRateLimited
	}
	return 0, nil
}

// Failure records a failed password check.
func (g *LoginGuard) Failure(r *http.Request) {
	if g == nil || g.limiter == nil {
		return
	}
	g.limiter.Failure(ClientIP(r, g.trustProxy))
}

// Success clears the limiter after a successful login.
func (g *LoginGuard) Success(r *http.Request) {
	if g == nil || g.limiter == nil {
		return
	}
	g.limiter.Success(ClientIP(r, g.trustProxy))
}

// ClientIP returns a canonical client address. Forwarded headers are honored
// only when trustProxy is enabled.
func ClientIP(r *http.Request, trustProxy bool) string {
	if r == nil {
		return "unknown"
	}
	if trustProxy {
		if raw := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); raw != "" {
			first := strings.TrimSpace(strings.Split(raw, ",")[0])
			if addr, err := netip.ParseAddr(first); err == nil {
				return addr.Unmap().String()
			}
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.Unmap().String()
	}
	if host == "" {
		return "unknown"
	}
	return host
}
