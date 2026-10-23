package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterSoftAndHardLimits(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC))
	limiter, err := NewRateLimiter(DefaultRateLimitConfig(), clock.Now)
	if err != nil {
		t.Fatalf("NewRateLimiter: %v", err)
	}
	if retry, allowed := limiter.Allow("203.0.113.1"); !allowed || retry != 0 {
		t.Fatalf("initial Allow = %v, %v", retry, allowed)
	}
	for i := 0; i < 5; i++ {
		limiter.Failure("203.0.113.1")
	}
	retry, allowed := limiter.Allow("203.0.113.1")
	if allowed {
		t.Fatal("Allow after soft limit succeeded")
	}
	if retry <= 0 || retry > 2*time.Second {
		t.Fatalf("soft retry = %v, want about 1s", retry)
	}

	clock.Advance(time.Second)
	if retry, allowed := limiter.Allow("203.0.113.1"); !allowed || retry != 0 {
		t.Fatalf("Allow after soft delay = %v, %v", retry, allowed)
	}
	for i := 0; i < 5; i++ {
		limiter.Failure("203.0.113.1")
	}
	retry, allowed = limiter.Allow("203.0.113.1")
	if allowed {
		t.Fatal("Allow after hard limit succeeded")
	}
	if retry < 14*time.Minute || retry > 16*time.Minute {
		t.Fatalf("hard retry = %v, want about 15m", retry)
	}

	clock.Advance(15 * time.Minute)
	if retry, allowed := limiter.Allow("203.0.113.1"); !allowed || retry != 0 {
		t.Fatalf("Allow after lock expiry = %v, %v", retry, allowed)
	}
}

func TestRateLimiterGlobalAndSuccessReset(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC))
	limiter, err := NewRateLimiter(DefaultRateLimitConfig(), clock.Now)
	if err != nil {
		t.Fatalf("NewRateLimiter: %v", err)
	}
	for i := 0; i < 10; i++ {
		limiter.Failure(string(rune('a' + i)))
	}
	if _, allowed := limiter.Allow("fresh-key"); allowed {
		t.Fatal("global limiter did not block")
	}

	limiter.Success("a")
	if _, allowed := limiter.Allow("fresh-key"); !allowed {
		t.Fatal("success did not reset global limiter")
	}

	for i := 0; i < 5; i++ {
		limiter.Failure("key")
	}
	if _, allowed := limiter.Allow("key"); allowed {
		t.Fatal("soft limit did not block")
	}
	limiter.Success("key")
	if _, allowed := limiter.Allow("key"); !allowed {
		t.Fatal("success did not reset key limiter")
	}
}

func TestRateLimiterWindowReset(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC))
	limiter, err := NewRateLimiter(DefaultRateLimitConfig(), clock.Now)
	if err != nil {
		t.Fatalf("NewRateLimiter: %v", err)
	}
	for i := 0; i < 5; i++ {
		limiter.Failure("key")
	}
	if _, allowed := limiter.Allow("key"); allowed {
		t.Fatal("soft limit did not block")
	}
	clock.Advance(16 * time.Minute)
	if _, allowed := limiter.Allow("key"); !allowed {
		t.Fatal("window expiry did not reset limiter")
	}
}

func TestRateLimiterConfigValidation(t *testing.T) {
	tests := []RateLimitConfig{
		{SoftLimit: 0},
		{SoftLimit: 5, HardLimit: 4, Window: time.Minute, LockDuration: time.Minute, SoftRetryAfter: time.Second, MaxEntries: 1},
		{SoftLimit: 5, HardLimit: 10, Window: 0, LockDuration: time.Minute, SoftRetryAfter: time.Second, MaxEntries: 1},
		{SoftLimit: 5, HardLimit: 10, Window: time.Minute, LockDuration: 0, SoftRetryAfter: time.Second, MaxEntries: 1},
		{SoftLimit: 5, HardLimit: 10, Window: time.Minute, LockDuration: time.Minute, SoftRetryAfter: 0, MaxEntries: 1},
		{SoftLimit: 5, HardLimit: 10, Window: time.Minute, LockDuration: time.Minute, SoftRetryAfter: time.Second, MaxEntries: 0},
	}
	for _, cfg := range tests {
		if _, err := NewRateLimiter(cfg, nil); err == nil {
			t.Fatalf("NewRateLimiter(%+v) succeeded", cfg)
		}
	}
}

func TestRetryAfterSeconds(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want int
	}{
		{0, 1},
		{time.Millisecond, 1},
		{time.Second, 1},
		{1100 * time.Millisecond, 2},
	}
	for _, test := range tests {
		if got := RetryAfterSeconds(test.in); got != test.want {
			t.Fatalf("RetryAfterSeconds(%v) = %d, want %d", test.in, got, test.want)
		}
	}
}

func TestClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://neda.example.com/login", nil)
	req.RemoteAddr = "203.0.113.9:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.1")
	if got := ClientIP(req, false); got != "203.0.113.9" {
		t.Fatalf("ClientIP untrusted = %q", got)
	}
	if got := ClientIP(req, true); got != "198.51.100.7" {
		t.Fatalf("ClientIP trusted = %q", got)
	}
	req.Header.Set("X-Forwarded-For", "not-an-ip")
	if got := ClientIP(req, true); got != "203.0.113.9" {
		t.Fatalf("ClientIP invalid forwarded = %q", got)
	}

	req.RemoteAddr = "[2001:db8::1]:443"
	if got := ClientIP(req, false); got != "2001:db8::1" {
		t.Fatalf("ClientIP ipv6 = %q", got)
	}
	req.RemoteAddr = "[::ffff:192.0.2.1]:443"
	if got := ClientIP(req, false); got != "192.0.2.1" {
		t.Fatalf("ClientIP mapped ipv4 = %q", got)
	}
}

func TestLoginGuard(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC))
	limiter, err := NewRateLimiter(DefaultRateLimitConfig(), clock.Now)
	if err != nil {
		t.Fatalf("NewRateLimiter: %v", err)
	}
	guard := NewLoginGuard(OriginPolicy{PublicURL: mustURL(t, "https://neda.example.com"), Production: true}, limiter, false)

	req := httptest.NewRequest(http.MethodPost, "https://neda.example.com/api/v1/auth/login", nil)
	req.Header.Set("Origin", "https://neda.example.com")
	req.RemoteAddr = "203.0.113.1:1234"
	if _, err := guard.Check(req); err != nil {
		t.Fatalf("Check: %v", err)
	}

	badOrigin := req.Clone(req.Context())
	badOrigin.Header.Set("Origin", "https://evil.example.com")
	if _, err := guard.Check(badOrigin); !errors.Is(err, ErrOriginNotAllowed) {
		t.Fatalf("Check bad origin = %v, want ErrOriginNotAllowed", err)
	}

	for i := 0; i < 5; i++ {
		guard.Failure(req)
	}
	retry, err := guard.Check(req)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("Check rate limited = %v, want ErrRateLimited", err)
	}
	if retry <= 0 {
		t.Fatalf("rate-limit retry = %v", retry)
	}
	guard.Success(req)
	if _, err := guard.Check(req); err != nil {
		t.Fatalf("Check after success: %v", err)
	}
}
