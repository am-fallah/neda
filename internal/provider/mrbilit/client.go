package mrbilit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

const (
	defaultAttemptTimeout = 6 * time.Second
	defaultMaxResponse    = 2 << 20
	defaultMaxRetries     = 2
	maxRetryDelay         = 24 * time.Hour
)

var errRedirectRejected = errors.New("mrbilit: redirect rejected")

type clientConfig struct {
	BaseURL          *url.URL
	HTTPClient       *http.Client
	AttemptTimeout   time.Duration
	MaxResponseBytes int64
	MaxRetries       int
	RetryBackoff     []time.Duration
	Jitter           func(time.Duration) time.Duration
	Sleep            func(context.Context, time.Duration) error
	Logger           *slog.Logger
}

type httpClient struct {
	base       *url.URL
	client     *http.Client
	timeout    time.Duration
	maxBody    int64
	maxRetries int
	backoff    []time.Duration
	jitter     func(time.Duration) time.Duration
	sleep      func(context.Context, time.Duration) error
	logger     *slog.Logger
}

func newHTTPClient(cfg clientConfig) (*httpClient, error) {
	base := cfg.BaseURL
	if base == nil {
		parsed, err := url.Parse(DefaultBaseURL)
		if err != nil {
			return nil, configurationError("base url", err)
		}
		base = parsed
	}
	if err := validateBaseURL(base); err != nil {
		return nil, err
	}
	if cfg.AttemptTimeout == 0 {
		cfg.AttemptTimeout = defaultAttemptTimeout
	}
	if cfg.AttemptTimeout < 0 || cfg.AttemptTimeout > 30*time.Second {
		return nil, configurationError("attempt timeout", errors.New("must be between 0 and 30s"))
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = defaultMaxResponse
	}
	if cfg.MaxResponseBytes < 0 || cfg.MaxResponseBytes > 8<<20 {
		return nil, configurationError("response limit", errors.New("must be between 0 and 8 MiB"))
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = defaultMaxRetries
	}
	if cfg.MaxRetries < 0 || cfg.MaxRetries > 2 {
		return nil, configurationError("retries", errors.New("must be between 0 and 2"))
	}
	backoff := cfg.RetryBackoff
	if backoff == nil {
		backoff = []time.Duration{500 * time.Millisecond, time.Second}
	}
	if len(backoff) == 0 {
		return nil, configurationError("retry backoff", errors.New("must not be empty"))
	}
	for _, delay := range backoff {
		if delay < 0 || delay > maxRetryDelay {
			return nil, configurationError("retry backoff", errors.New("delay is out of range"))
		}
	}
	jitter := cfg.Jitter
	if jitter == nil {
		jitter = defaultJitter
	}
	sleep := cfg.Sleep
	if sleep == nil {
		sleep = sleepContext
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	httpClientValue := http.Client{}
	if cfg.HTTPClient != nil {
		httpClientValue = *cfg.HTTPClient
	}
	httpClientValue.Timeout = cfg.AttemptTimeout
	httpClientValue.Jar = nil
	httpClientValue.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errRedirectRejected
	}

	clonedBase := *base
	clonedBase.Path = strings.TrimRight(clonedBase.Path, "/")
	return &httpClient{
		base:       &clonedBase,
		client:     &httpClientValue,
		timeout:    cfg.AttemptTimeout,
		maxBody:    cfg.MaxResponseBytes,
		maxRetries: cfg.MaxRetries,
		backoff:    append([]time.Duration(nil), backoff...),
		jitter:     jitter,
		sleep:      sleep,
		logger:     logger,
	}, nil
}

func (c *httpClient) get(ctx context.Context, endpoint *url.URL) ([]byte, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if endpoint == nil {
		return nil, 0, configurationError("endpoint", errors.New("is required"))
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, contextError(err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, 0, contextError(err)
		}
		body, status, retryAfter, err := c.attempt(ctx, endpoint)
		if err == nil {
			return body, status, nil
		}
		lastErr = err
		if attempt == c.maxRetries || !retryable(err) {
			return nil, status, err
		}

		delay := c.backoff[min(attempt, len(c.backoff)-1)]
		if retryAfter > delay {
			delay = retryAfter
		}
		delay = c.jitter(delay)
		if delay < 0 {
			delay = 0
		}
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= delay {
			return nil, status, err
		}
		if err := c.sleep(ctx, delay); err != nil {
			return nil, status, contextError(err)
		}
	}
	return nil, 0, lastErr
}

func (c *httpClient) attempt(ctx context.Context, endpoint *url.URL) ([]byte, int, time.Duration, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, 0, 0, &provider.Error{Kind: provider.ErrConfiguration, Op: "mrbilit.request", Err: errors.New("invalid request")}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Neda/0.1.0 (self-hosted monitor)")

	started := time.Now()
	response, err := c.client.Do(request)
	duration := time.Since(started)
	if err != nil {
		if errors.Is(err, errRedirectRejected) {
			c.logAttempt(endpoint, 0, duration, "redirect_rejected")
			return nil, 0, 0, &provider.Error{Kind: provider.ErrInvalidResponse, Op: "mrbilit.http", StatusCode: http.StatusFound, Err: errors.New("redirect rejected")}
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			c.logAttempt(endpoint, 0, duration, "caller_done")
			return nil, 0, 0, contextError(ctxErr)
		}
		if isTimeoutError(err) || attemptCtx.Err() == context.DeadlineExceeded {
			c.logAttempt(endpoint, 0, duration, "timeout")
			return nil, 0, 0, &provider.Error{Kind: provider.ErrTimeout, Op: "mrbilit.http", Err: context.DeadlineExceeded}
		}
		c.logAttempt(endpoint, 0, duration, "network_error")
		return nil, 0, 0, &provider.Error{Kind: provider.ErrNetwork, Op: "mrbilit.http", Err: errors.New("network error")}
	}
	defer response.Body.Close()
	c.logAttempt(endpoint, response.StatusCode, duration, "response")

	retryAfter := parseRetryAfter(response.Header.Get("Retry-After"))
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return nil, response.StatusCode, retryAfter, &provider.Error{
			Kind:       provider.ErrRateLimited,
			Op:         "mrbilit.http",
			StatusCode: response.StatusCode,
			RetryAfter: retryAfter,
			Err:        errors.New("rate limited"),
		}
	case response.StatusCode == http.StatusBadGateway ||
		response.StatusCode == http.StatusServiceUnavailable ||
		response.StatusCode == http.StatusGatewayTimeout:
		return nil, response.StatusCode, retryAfter, &provider.Error{
			Kind:       provider.ErrHTTPStatus,
			Op:         "mrbilit.http",
			StatusCode: response.StatusCode,
			RetryAfter: retryAfter,
			Err:        errors.New("retryable status"),
		}
	case response.StatusCode >= 300 && response.StatusCode <= 399:
		return nil, response.StatusCode, 0, &provider.Error{
			Kind:       provider.ErrInvalidResponse,
			Op:         "mrbilit.http",
			StatusCode: response.StatusCode,
			Err:        errors.New("unexpected redirect status"),
		}
	case response.StatusCode < 200 || response.StatusCode > 299:
		return nil, response.StatusCode, 0, &provider.Error{
			Kind:       provider.ErrHTTPStatus,
			Op:         "mrbilit.http",
			StatusCode: response.StatusCode,
			Err:        errors.New("http status"),
		}
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, c.maxBody+1))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, response.StatusCode, 0, contextError(ctxErr)
		}
		if attemptCtx.Err() == context.DeadlineExceeded || isTimeoutError(err) {
			return nil, response.StatusCode, 0, &provider.Error{Kind: provider.ErrTimeout, Op: "mrbilit.http", Err: context.DeadlineExceeded}
		}
		return nil, response.StatusCode, 0, &provider.Error{Kind: provider.ErrInvalidResponse, Op: "mrbilit.http", Err: errors.New("response read failed")}
	}
	if int64(len(body)) > c.maxBody {
		return nil, response.StatusCode, 0, &provider.Error{Kind: provider.ErrInvalidResponse, Op: "mrbilit.http", Err: errors.New("response exceeds limit")}
	}
	return body, response.StatusCode, 0, nil
}

func retryable(err error) bool {
	var providerErr *provider.Error
	if !errors.As(err, &providerErr) || providerErr == nil {
		return false
	}
	switch providerErr.Kind {
	case provider.ErrTimeout, provider.ErrNetwork, provider.ErrRateLimited:
		return true
	case provider.ErrHTTPStatus:
		switch providerErr.StatusCode {
		case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		}
	}
	return false
}

func contextError(err error) error {
	if errors.Is(err, context.Canceled) {
		return &provider.Error{Kind: provider.ErrCanceled, Op: "mrbilit.http", Err: context.Canceled}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &provider.Error{Kind: provider.ErrTimeout, Op: "mrbilit.http", Err: context.DeadlineExceeded}
	}
	return &provider.Error{Kind: provider.ErrCanceled, Op: "mrbilit.http", Err: err}
}

func isTimeoutError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}

func defaultJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return delay
	}
	span := int64(delay / 5)
	if span <= 0 {
		return delay
	}
	return delay + time.Duration(rand.Int63n(span+1))
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func parseRetryAfter(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil {
		if seconds <= 0 {
			return 0
		}
		delay := time.Duration(seconds) * time.Second
		if delay > maxRetryDelay {
			return maxRetryDelay
		}
		return delay
	}
	if when, err := http.ParseTime(raw); err == nil {
		delay := time.Until(when)
		if delay <= 0 {
			return 0
		}
		if delay > maxRetryDelay {
			return maxRetryDelay
		}
		return delay
	}
	return 0
}

func (c *httpClient) logAttempt(endpoint *url.URL, status int, duration time.Duration, outcome string) {
	if c.logger == nil || endpoint == nil {
		return
	}
	c.logger.Debug("mrbilit request",
		"method", http.MethodGet,
		"host", endpoint.Host,
		"path", endpoint.EscapedPath(),
		"status", status,
		"duration_ms", duration.Milliseconds(),
		"outcome", outcome,
	)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
