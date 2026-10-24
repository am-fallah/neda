package mrbilit

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

func providerForServer(t *testing.T, server *httptest.Server, mutate func(*Config)) *Provider {
	t.Helper()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		BaseURL:        base,
		HTTPClient:     server.Client(),
		AttemptTimeout: 200 * time.Millisecond,
		RetryBackoff:   []time.Duration{0},
		Jitter:         func(delay time.Duration) time.Duration { return delay },
		Sleep:          func(context.Context, time.Duration) error { return nil },
		Now:            func() time.Time { return testNow },
	}
	if mutate != nil {
		mutate(&cfg)
	}
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

func TestProviderCheckSuccess(t *testing.T) {
	fixture := readFixture(t, "valid-mixed.json")
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path != EndpointPath {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got == "" {
			t.Error("User-Agent missing")
		}
		if got := r.Header.Get("Cookie"); got != "" {
			t.Errorf("Cookie = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	result, err := providerForServer(t, server, nil).Check(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(result.Journeys) != 2 || result.ObservedAt.IsZero() {
		t.Fatalf("result = %+v", result)
	}
	if gotQuery.Get("from") != "219" || gotQuery.Get("to") != "191" || gotQuery.Get("date") != "2026-10-20" {
		t.Fatalf("query = %v", gotQuery)
	}
	if gotQuery.Has("genderCode") || gotQuery.Has("exclusive") || gotQuery.Has("availableStatus") {
		t.Fatalf("unverified query parameter sent: %v", gotQuery)
	}
}

func TestProviderNeverSendsCookies(t *testing.T) {
	fixture := readFixture(t, "valid-mixed.json")
	var cookieHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookieHeader = r.Header.Get("Cookie")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	base, _ := url.Parse(server.URL)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(base, []*http.Cookie{{Name: "session", Value: "secret-cookie"}})
	httpClient := &http.Client{Transport: server.Client().Transport, Jar: jar}
	p, err := New(Config{
		BaseURL:        base,
		HTTPClient:     httpClient,
		AttemptTimeout: time.Second,
		RetryBackoff:   []time.Duration{0},
		Jitter:         func(delay time.Duration) time.Duration { return delay },
		Sleep:          func(context.Context, time.Duration) error { return nil },
		Now:            func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Check(context.Background(), testRequest()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if cookieHeader != "" {
		t.Fatalf("Cookie header = %q", cookieHeader)
	}
}

func TestProviderRetriesTransientFailures(t *testing.T) {
	fixture := readFixture(t, "valid-mixed.json")
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	result, err := providerForServer(t, server, nil).Check(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(result.Journeys) != 2 {
		t.Fatalf("journeys = %d", len(result.Journeys))
	}
	if attempts.Load() != 3 {
		t.Fatalf("attempts = %d", attempts.Load())
	}
}

func TestProviderHonorsRetryAfter(t *testing.T) {
	fixture := readFixture(t, "valid-mixed.json")
	var attempts atomic.Int32
	var slept time.Duration
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	p := providerForServer(t, server, func(cfg *Config) {
		cfg.Sleep = func(_ context.Context, delay time.Duration) error {
			slept = delay
			return nil
		}
	})
	result, err := p.Check(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(result.Journeys) != 2 {
		t.Fatalf("journeys = %d", len(result.Journeys))
	}
	if slept != time.Second {
		t.Fatalf("sleep = %v, want 1s", slept)
	}
}

func TestProviderDoesNotSleepPastCallerDeadline(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Retry-After", "10")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := providerForServer(t, server, nil).Check(ctx, testRequest())
	if err == nil {
		t.Fatal("expected rate-limit error")
	}
	if time.Since(started) > time.Second {
		t.Fatalf("Check waited past the caller deadline: %v", time.Since(started))
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts = %d, want 1", attempts.Load())
	}
	var providerErr *provider.Error
	if !errors.As(err, &providerErr) || providerErr.Kind != provider.ErrRateLimited {
		t.Fatalf("error = %v, want rate limited", err)
	}
}

func TestProviderRetriesAttemptTimeout(t *testing.T) {
	fixture := readFixture(t, "valid-mixed.json")
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			select {
			case <-time.After(100 * time.Millisecond):
			case <-r.Context().Done():
			}
			return
		}
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	p := providerForServer(t, server, func(cfg *Config) {
		cfg.AttemptTimeout = 20 * time.Millisecond
	})
	result, err := p.Check(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(result.Journeys) != 2 {
		t.Fatalf("journeys = %d", len(result.Journeys))
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d, want 2", attempts.Load())
	}
}

func TestProviderCallerDeadlineAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(200 * time.Millisecond):
			_, _ = w.Write(readFixture(t, "valid-mixed.json"))
		case <-r.Context().Done():
		}
	}))
	defer server.Close()

	t.Run("deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		started := time.Now()
		_, err := providerForServer(t, server, func(cfg *Config) { cfg.AttemptTimeout = time.Second }).Check(ctx, testRequest())
		if err == nil {
			t.Fatal("expected timeout")
		}
		if time.Since(started) > 500*time.Millisecond {
			t.Fatalf("deadline not respected: %v", time.Since(started))
		}
		var providerErr *provider.Error
		if !errors.As(err, &providerErr) || providerErr.Kind != provider.ErrTimeout {
			t.Fatalf("error = %v, want timeout", err)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := providerForServer(t, server, nil).Check(ctx, testRequest())
		var providerErr *provider.Error
		if !errors.As(err, &providerErr) || providerErr.Kind != provider.ErrCanceled {
			t.Fatalf("error = %v, want canceled", err)
		}
	})
}

func TestProviderRejectsRedirectAndStatusErrors(t *testing.T) {
	t.Run("redirect", func(t *testing.T) {
		var nextCalls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/next" {
				nextCalls.Add(1)
				_, _ = w.Write(readFixture(t, "valid-mixed.json"))
				return
			}
			http.Redirect(w, r, "/next", http.StatusFound)
		}))
		defer server.Close()
		_, err := providerForServer(t, server, nil).Check(context.Background(), testRequest())
		var providerErr *provider.Error
		if !errors.As(err, &providerErr) || providerErr.Kind != provider.ErrInvalidResponse {
			t.Fatalf("error = %v, want invalid response", err)
		}
		if nextCalls.Load() != 0 {
			t.Fatalf("redirect target called %d times", nextCalls.Load())
		}
	})

	for _, tc := range []struct {
		name       string
		status     int
		wantRetry  int32
		wantKind   provider.ErrorKind
		retryAfter string
	}{
		{"bad-request", http.StatusBadRequest, 1, provider.ErrHTTPStatus, ""},
		{"server-error", http.StatusInternalServerError, 1, provider.ErrHTTPStatus, ""},
		{"unavailable", http.StatusServiceUnavailable, 3, provider.ErrHTTPStatus, ""},
		{"rate-limited", http.StatusTooManyRequests, 3, provider.ErrRateLimited, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				if tc.retryAfter != "" {
					w.Header().Set("Retry-After", tc.retryAfter)
				}
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			_, err := providerForServer(t, server, nil).Check(context.Background(), testRequest())
			var providerErr *provider.Error
			if !errors.As(err, &providerErr) || providerErr.Kind != tc.wantKind {
				t.Fatalf("error = %v, want kind %s", err, tc.wantKind)
			}
			if attempts.Load() != tc.wantRetry {
				t.Fatalf("attempts = %d, want %d", attempts.Load(), tc.wantRetry)
			}
		})
	}
}

func TestProviderBoundsResponseAndParsesInvalidBody(t *testing.T) {
	t.Run("oversized", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(bytes.Repeat([]byte("x"), 100))
		}))
		defer server.Close()
		_, err := providerForServer(t, server, func(cfg *Config) { cfg.MaxResponseBytes = 32 }).Check(context.Background(), testRequest())
		var providerErr *provider.Error
		if !errors.As(err, &providerErr) || providerErr.Kind != provider.ErrInvalidResponse {
			t.Fatalf("error = %v, want invalid response", err)
		}
	})

	t.Run("invalid-json", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{`))
		}))
		defer server.Close()
		_, err := providerForServer(t, server, nil).Check(context.Background(), testRequest())
		var providerErr *provider.Error
		if !errors.As(err, &providerErr) || providerErr.Kind != provider.ErrInvalidResponse {
			t.Fatalf("error = %v, want invalid response", err)
		}
	})
}

func TestProviderValidatesBeforeIO(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	defer server.Close()
	req := testRequest()
	req.Config.GenderCode = "M"
	_, err := providerForServer(t, server, nil).Check(context.Background(), req)
	var providerErr *provider.Error
	if !errors.As(err, &providerErr) || providerErr.Kind != provider.ErrConfiguration {
		t.Fatalf("error = %v, want configuration", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("server called %d times", calls.Load())
	}
}

func TestProviderLogsDoNotContainQuery(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(readFixture(t, "valid-mixed.json"))
	}))
	defer server.Close()
	_, err := providerForServer(t, server, func(cfg *Config) { cfg.Logger = logger }).Check(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	rendered := logs.String()
	if !strings.Contains(rendered, EndpointPath) {
		t.Fatalf("log missing path: %s", rendered)
	}
	for _, forbidden := range []string{"from=219", "to=191", "date=2026", "adultCount=2", "childCount=0", "infantCount=0", "?"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("log leaked query material %q: %s", forbidden, rendered)
		}
	}
}

func TestNewRejectsUnsafeTransportConfig(t *testing.T) {
	base, _ := url.Parse("http://127.0.0.1:1234")
	cases := []Config{
		{BaseURL: base, AttemptTimeout: -time.Second},
		{BaseURL: base, AttemptTimeout: time.Minute},
		{BaseURL: base, MaxRetries: 3},
		{BaseURL: base, MaxResponseBytes: 9 << 20},
		{BaseURL: base, RetryBackoff: []time.Duration{-time.Second}},
	}
	for index, cfg := range cases {
		if _, err := New(cfg); err == nil {
			t.Fatalf("case %d accepted", index)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter("2"); got != 2*time.Second {
		t.Fatalf("seconds = %v", got)
	}
	if got := parseRetryAfter("not-a-date"); got != 0 {
		t.Fatalf("invalid = %v", got)
	}
	future := time.Now().Add(3 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(future); got <= 0 || got > 4*time.Second {
		t.Fatalf("http date = %v", got)
	}
	if got := parseRetryAfter("0"); got != 0 {
		t.Fatalf("zero = %v", got)
	}
}
