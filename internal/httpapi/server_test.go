package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type fakeReadiness struct {
	err   error
	calls int
}

func (f *fakeReadiness) Ready(context.Context, int) error {
	f.calls++
	return f.err
}

func testServer(t *testing.T, cfg Config, readiness ReadinessChecker) (*Server, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return New(cfg, logger, readiness, 3), &logs
}

func request(t *testing.T, server *Server, method, target, host string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, body)
	if host != "" {
		req.Host = host
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, req)
	return recorder
}

func TestHealthz(t *testing.T) {
	server, _ := testServer(t, Config{}, &fakeReadiness{})
	if server.cfg.MaxBodyBytes != defaultMaxBodyBytes {
		t.Fatalf("MaxBodyBytes = %d, want %d", server.cfg.MaxBodyBytes, defaultMaxBodyBytes)
	}
	if server.cfg.ReadHeaderTimeout != defaultReadHeader ||
		server.cfg.ReadTimeout != defaultReadTimeout ||
		server.cfg.WriteTimeout != defaultWriteTimeout ||
		server.cfg.IdleTimeout != defaultIdleTimeout ||
		server.cfg.ShutdownTimeout != defaultShutdownTimeout {
		t.Fatalf("server timeouts not defaulted: %+v", server.cfg)
	}
	response := request(t, server, http.MethodGet, "http://localhost/healthz", "localhost", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"status":"ok"`) {
		t.Fatalf("body = %s", response.Body.String())
	}
	if got := response.Header().Get(requestIDHeader); len(got) != 32 {
		t.Fatalf("request ID = %q, want 32 hex characters", got)
	}
	if got := response.Header().Get(headerContentTypeOption); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := response.Header().Get(headerCacheControl); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := response.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q", got)
	}
	if got := response.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("development HSTS = %q, want empty", got)
	}
}

func TestReadiness(t *testing.T) {
	t.Run("ready", func(t *testing.T) {
		readiness := &fakeReadiness{}
		server, _ := testServer(t, Config{}, readiness)
		response := request(t, server, http.MethodGet, "http://localhost/readyz", "localhost", nil)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
		}
		if readiness.calls != 1 {
			t.Fatalf("readiness calls = %d, want 1", readiness.calls)
		}
	})

	t.Run("database-error", func(t *testing.T) {
		readiness := &fakeReadiness{err: errors.New("database path /secret missing")}
		server, _ := testServer(t, Config{}, readiness)
		response := request(t, server, http.MethodGet, "http://localhost/readyz", "localhost", nil)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", response.Code)
		}
		if strings.Contains(response.Body.String(), "secret") {
			t.Fatalf("response leaked readiness detail: %s", response.Body.String())
		}
	})

	t.Run("nil-checker", func(t *testing.T) {
		server, _ := testServer(t, Config{}, nil)
		response := request(t, server, http.MethodGet, "http://localhost/readyz", "localhost", nil)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", response.Code)
		}
	})
}

func TestNotFoundAndMethodNotAllowed(t *testing.T) {
	server, _ := testServer(t, Config{}, &fakeReadiness{})
	notFound := request(t, server, http.MethodGet, "http://localhost/nope", "localhost", nil)
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("404 status = %d", notFound.Code)
	}
	if !strings.Contains(notFound.Body.String(), `"code":"not_found"`) {
		t.Fatalf("404 body = %s", notFound.Body.String())
	}

	method := request(t, server, http.MethodPost, "http://localhost/healthz", "localhost", strings.NewReader("x"))
	if method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("405 status = %d", method.Code)
	}
	if got := method.Header().Get(headerAllow); got != http.MethodGet {
		t.Fatalf("Allow = %q", got)
	}
	if !strings.Contains(method.Body.String(), `"code":"method_not_allowed"`) {
		t.Fatalf("405 body = %s", method.Body.String())
	}
}

func TestHostValidation(t *testing.T) {
	publicURL, err := url.Parse("https://neda.example.com")
	if err != nil {
		t.Fatal(err)
	}
	server, _ := testServer(t, Config{PublicURL: publicURL, Production: true}, &fakeReadiness{})

	cases := []struct {
		name   string
		host   string
		header map[string]string
		want   int
	}{
		{"matching", "neda.example.com", nil, http.StatusOK},
		{"matching-default-port", "neda.example.com:443", nil, http.StatusOK},
		{"mismatched", "evil.example.com", nil, http.StatusMisdirectedRequest},
		{"wrong-port", "neda.example.com:444", nil, http.StatusMisdirectedRequest},
		{"missing", "", nil, http.StatusMisdirectedRequest},
		{"forwarded-host-ignored", "neda.example.com", map[string]string{"X-Forwarded-Host": "evil.example.com"}, http.StatusOK},
		{"forwarded-host-cannot-rescue", "evil.example.com", map[string]string{"X-Forwarded-Host": "neda.example.com"}, http.StatusMisdirectedRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://neda.example.com/healthz", nil)
			req.Host = tc.host
			for key, value := range tc.header {
				req.Header.Set(key, value)
			}
			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder, req)
			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tc.want, recorder.Body.String())
			}
		})
	}
}

func TestDevelopmentHostValidation(t *testing.T) {
	server, _ := testServer(t, Config{}, &fakeReadiness{})
	cases := map[string]int{
		"localhost":         http.StatusOK,
		"localhost:8080":    http.StatusOK,
		"127.0.0.1:8080":    http.StatusOK,
		"evil.example.com":  http.StatusMisdirectedRequest,
		"[::1]:8080":        http.StatusMisdirectedRequest,
		"localhost.evil.co": http.StatusMisdirectedRequest,
	}
	for host, want := range cases {
		t.Run(host, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://localhost/healthz", nil)
			req.Host = host
			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder, req)
			if recorder.Code != want {
				t.Fatalf("host %q status = %d, want %d", host, recorder.Code, want)
			}
		})
	}
}

func TestProductionHSTS(t *testing.T) {
	publicURL, _ := url.Parse("https://neda.example.com")
	server, _ := testServer(t, Config{PublicURL: publicURL, Production: true}, &fakeReadiness{})
	response := request(t, server, http.MethodGet, "https://neda.example.com/healthz", "neda.example.com", nil)
	if got := response.Header().Get("Strict-Transport-Security"); got == "" {
		t.Fatal("production response lacks HSTS")
	}
}

func TestRecoveryPanic(t *testing.T) {
	server, logs := testServer(t, Config{}, &fakeReadiness{})
	panicking := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})
	handler := server.requestID(server.logging(server.recovery(panicking)))
	req := httptest.NewRequest(http.MethodGet, "http://localhost/panic", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"internal_error"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
	if !strings.Contains(logs.String(), "panic recovered") {
		t.Fatalf("panic was not logged: %s", logs.String())
	}
	if strings.Contains(logs.String(), "goroutine") {
		t.Fatalf("stack trace leaked to log: %s", logs.String())
	}
}

func TestLoggingOmitsQueryString(t *testing.T) {
	server, logs := testServer(t, Config{}, &fakeReadiness{})
	request(t, server, http.MethodGet, "http://localhost/healthz?token=query-secret", "localhost", nil)
	output := logs.String()
	if strings.Contains(output, "query-secret") {
		t.Fatalf("log leaked query string: %s", output)
	}
	if !strings.Contains(output, `"path":"/healthz"`) {
		t.Fatalf("log missing path: %s", output)
	}
	if !strings.Contains(output, `"status":200`) {
		t.Fatalf("log missing status: %s", output)
	}
}

func TestBodyLimit(t *testing.T) {
	server, _ := testServer(t, Config{MaxBodyBytes: 4}, &fakeReadiness{})
	handler := server.bodyLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	declared := httptest.NewRequest(http.MethodPost, "http://localhost/", strings.NewReader("0123456789"))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, declared)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("declared body status = %d, want 413", recorder.Code)
	}

	chunked := httptest.NewRequest(http.MethodPost, "http://localhost/", strings.NewReader("0123456789"))
	chunked.ContentLength = -1
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, chunked)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("chunked body status = %d, want 413", recorder.Code)
	}
}

func TestServeStopsOnContextCancellation(t *testing.T) {
	server, _ := testServer(t, Config{ShutdownTimeout: time.Second}, &fakeReadiness{})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener) }()

	baseURL := "http://" + listener.Addr().String()
	var response *http.Response
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		response, err = http.Get(baseURL + "/healthz")
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		cancel()
		t.Fatalf("server did not become ready: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("health status = %d", response.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not stop after context cancellation")
	}
}

func TestSplitHost(t *testing.T) {
	cases := []struct {
		input string
		name  string
		port  string
		ok    bool
	}{
		{"example.com", "example.com", "", true},
		{"example.com:443", "example.com", "443", true},
		{"[::1]:8080", "::1", "8080", true},
		{"", "", "", false},
		{"example.com:bad", "", "", false},
	}
	for _, tc := range cases {
		name, port, ok := splitHost(tc.input)
		if name != tc.name || port != tc.port || ok != tc.ok {
			t.Fatalf("splitHost(%q) = (%q,%q,%v), want (%q,%q,%v)", tc.input, name, port, ok, tc.name, tc.port, tc.ok)
		}
	}
}
