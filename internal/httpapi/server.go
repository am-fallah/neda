// Package httpapi provides Neda's HTTP transport, health endpoints, and
// security middleware.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/auth"
	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/logging"
	"github.com/am-fallah/neda/internal/provider"
	appsettings "github.com/am-fallah/neda/internal/settings"
	"github.com/am-fallah/neda/internal/store"
)

const (
	defaultMaxBodyBytes     = 1 << 20
	defaultShutdownTimeout  = 15 * time.Second
	defaultReadHeader       = 5 * time.Second
	defaultReadTimeout      = 15 * time.Second
	defaultWriteTimeout     = 30 * time.Second
	defaultIdleTimeout      = 60 * time.Second
	readinessCheckTimeout   = 2 * time.Second
	contentTypeJSON         = "application/json"
	requestIDHeader         = "X-Request-ID"
	headerContentType       = "Content-Type"
	headerAllow             = "Allow"
	headerCacheControl      = "Cache-Control"
	headerContentTypeOption = "X-Content-Type-Options"
)

// Config controls the HTTP server. Zero values receive safe defaults.
type Config struct {
	Addr              string
	Production        bool
	PublicURL         *url.URL
	MaxBodyBytes      int64
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	// Auth and LoginGuard compose the authentication endpoints. They are
	// optional for health-only tests; private routes fail closed when nil.
	Auth       *auth.Manager
	LoginGuard *auth.LoginGuard
	Monitors   *domain.MonitorService
	Settings   *appsettings.Service
	Store      *store.Store
	Providers  *provider.Registry
	// WebUI serves the embedded SPA for non-API routes. It is optional so
	// health-only tests and API-only deployments remain valid.
	WebUI http.Handler
	Now   func() time.Time
}

// ReadinessChecker verifies that the database and migrations are usable.
type ReadinessChecker interface {
	Ready(ctx context.Context, expectedMigrations int) error
}

// Server owns the HTTP handler and lifecycle.
type Server struct {
	cfg                Config
	logger             *slog.Logger
	readiness          ReadinessChecker
	expectedMigrations int
	handler            http.Handler
	auth               *auth.Manager
	loginGuard         *auth.LoginGuard
	monitors           *domain.MonitorService
	settings           *appsettings.Service
	store              *store.Store
	providers          *provider.Registry
	webUI              http.Handler
	now                func() time.Time
}

// New constructs a Server. A nil logger falls back to slog.Default.
func New(cfg Config, logger *slog.Logger, readiness ReadinessChecker, expectedMigrations int) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = defaultMaxBodyBytes
	}
	if cfg.ReadHeaderTimeout <= 0 {
		cfg.ReadHeaderTimeout = defaultReadHeader
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = defaultReadTimeout
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = defaultWriteTimeout
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = defaultIdleTimeout
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = defaultShutdownTimeout
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	s := &Server{
		cfg:                cfg,
		logger:             logger,
		readiness:          readiness,
		expectedMigrations: expectedMigrations,
		auth:               cfg.Auth,
		loginGuard:         cfg.LoginGuard,
		monitors:           cfg.Monitors,
		settings:           cfg.Settings,
		store:              cfg.Store,
		providers:          cfg.Providers,
		webUI:              cfg.WebUI,
		now:                cfg.Now,
	}
	s.handler = s.buildHandler()
	return s
}

// Handler returns the composed HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// Run listens on the configured address and serves until ctx is canceled.
func (s *Server) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("http: listen: %w", err)
	}
	s.logger.Info("http server listening", "addr", listener.Addr().String())
	return s.Serve(ctx, listener)
}

// Serve serves on listener and shuts down gracefully when ctx is canceled.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	server := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: s.cfg.ReadHeaderTimeout,
		ReadTimeout:       s.cfg.ReadTimeout,
		WriteTimeout:      s.cfg.WriteTimeout,
		IdleTimeout:       s.cfg.IdleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("http: shutdown: %w", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http: serve: %w", err)
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("http: serve: %w", err)
	}
}

func (s *Server) buildHandler() http.Handler {
	var handler http.Handler = http.HandlerFunc(s.route)
	handler = s.bodyLimit(handler)
	handler = s.hostValidation(handler)
	handler = s.securityHeaders(handler)
	handler = s.recovery(handler)
	handler = s.logging(handler)
	handler = s.requestID(handler)
	return handler
}

func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v1/monitors" || strings.HasPrefix(r.URL.Path, "/api/v1/monitors/") {
		s.privateAuth(http.HandlerFunc(s.handleMonitors)).ServeHTTP(w, r)
		return
	}
	if r.URL.Path == "/api/v1/settings" || strings.HasPrefix(r.URL.Path, "/api/v1/settings/") {
		s.privateAuth(http.HandlerFunc(s.handleSettings)).ServeHTTP(w, r)
		return
	}
	if r.URL.Path == "/api/v1/providers" {
		s.privateAuth(http.HandlerFunc(s.handleProviders)).ServeHTTP(w, r)
		return
	}
	if r.URL.Path == "/api/v1/events" || strings.HasPrefix(r.URL.Path, "/api/v1/events/") {
		s.privateAuth(http.HandlerFunc(s.handleEvents)).ServeHTTP(w, r)
		return
	}
	if r.URL.Path == "/api/v1/deliveries" {
		s.privateAuth(http.HandlerFunc(s.handleDeliveries)).ServeHTTP(w, r)
		return
	}
	if r.URL.Path == "/api/v1/overview" {
		s.privateAuth(http.HandlerFunc(s.handleOverview)).ServeHTTP(w, r)
		return
	}
	switch r.URL.Path {
	case "/healthz":
		if !requireGet(w, r) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case "/readyz":
		if !requireGet(w, r) {
			return
		}
		s.ready(w, r)
	case "/api/v1/auth/login":
		s.login(w, r)
	case "/api/v1/auth/session":
		s.session(w, r)
	case "/api/v1/auth/logout":
		s.privateAuth(http.HandlerFunc(s.logout)).ServeHTTP(w, r)
	case "/api/v1/auth/password":
		s.privateAuth(http.HandlerFunc(s.password)).ServeHTTP(w, r)
	default:
		if isReservedBackendPath(r.URL.Path) {
			writeError(w, r, http.StatusNotFound, "not_found", "not found")
			return
		}
		if s.webUI != nil {
			s.webUI.ServeHTTP(w, r)
			return
		}
		writeError(w, r, http.StatusNotFound, "not_found", "not found")
	}
}

// isReservedBackendPath keeps unknown API and health routes JSON and prevents
// the SPA fallback from swallowing backend paths.
func isReservedBackendPath(requestPath string) bool {
	return requestPath == "/api" ||
		strings.HasPrefix(requestPath, "/api/") ||
		strings.HasPrefix(requestPath, "/healthz/") ||
		strings.HasPrefix(requestPath, "/readyz/")
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if s.readiness == nil {
		writeError(w, r, http.StatusServiceUnavailable, "not_ready", "service is not ready")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), readinessCheckTimeout)
	defer cancel()
	if err := s.readiness.Ready(ctx, s.expectedMigrations); err != nil {
		s.logger.Warn("readiness check failed",
			"request_id", logging.RequestID(r.Context()),
			"error", err,
		)
		writeError(w, r, http.StatusServiceUnavailable, "not_ready", "service is not ready")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func requireGet(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet {
		return true
	}
	w.Header().Set(headerAllow, http.MethodGet)
	writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	return false
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set(headerContentType, contentTypeJSON)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
		"requestId": logging.RequestID(r.Context()),
	})
}

func (s *Server) requestHostMatches(requestHost string) bool {
	if s.cfg.PublicURL != nil {
		return sameHost(requestHost, s.cfg.PublicURL)
	}
	if s.cfg.Production {
		return false
	}
	return isLocalHost(requestHost)
}

func sameHost(requestHost string, publicURL *url.URL) bool {
	requestName, requestPort, ok := splitHost(requestHost)
	if !ok {
		return false
	}
	publicName, publicPort, ok := splitHost(publicURL.Host)
	if !ok {
		return false
	}
	if !strings.EqualFold(requestName, publicName) {
		return false
	}
	if publicPort == "" {
		publicPort = defaultPort(publicURL.Scheme)
	}
	if requestPort == "" {
		requestPort = defaultPort(publicURL.Scheme)
	}
	return requestPort == publicPort
}

func isLocalHost(hostport string) bool {
	name, _, ok := splitHost(hostport)
	if !ok {
		return false
	}
	return strings.EqualFold(name, "localhost") || name == "127.0.0.1"
}

func splitHost(hostport string) (string, string, bool) {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return "", "", false
	}
	name, port, err := net.SplitHostPort(hostport)
	if err != nil {
		if strings.Contains(err.Error(), "missing port in address") {
			return strings.Trim(hostport, "[]"), "", true
		}
		return "", "", false
	}
	name = strings.Trim(name, "[]")
	if name == "" {
		return "", "", false
	}
	if port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return "", "", false
		}
	}
	return name, port, true
}

func defaultPort(scheme string) string {
	if strings.EqualFold(scheme, "http") {
		return "80"
	}
	return "443"
}
