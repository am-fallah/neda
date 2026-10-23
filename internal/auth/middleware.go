package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/am-fallah/neda/internal/logging"
)

// CSRFHeaderName is the header required on authenticated state-changing routes.
const CSRFHeaderName = "X-CSRF-Token"

type sessionContextKey struct{}

// SessionFromContext returns the session attached by RequireSession.
func SessionFromContext(ctx context.Context) (*Session, bool) {
	session, ok := ctx.Value(sessionContextKey{}).(*Session)
	return session, ok && session != nil
}

// RequireSession validates the session cookie and attaches the session to the
// request context. Invalid sessions clear both auth cookies.
func (m *Manager) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := m.ValidateSession(r.Context(), m.SessionToken(r))
		if err != nil {
			if errors.Is(err, ErrSessionInvalid) {
				m.ClearSessionCookies(w)
				writeAuthError(w, r, http.StatusUnauthorized, "unauthenticated", "authentication required")
				return
			}
			writeAuthError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
			return
		}
		ctx := context.WithValue(r.Context(), sessionContextKey{}, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireCSRF enforces same-origin plus the double-submit CSRF cookie and
// X-CSRF-Token header on state-changing methods. It must run after
// RequireSession.
func (m *Manager) RequireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := SessionFromContext(r.Context())
		if !ok {
			writeAuthError(w, r, http.StatusUnauthorized, "unauthenticated", "authentication required")
			return
		}
		if isSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		if err := m.origins.Check(r); err != nil {
			writeAuthError(w, r, http.StatusForbidden, "origin_not_allowed", "request origin is not allowed")
			return
		}
		cookieToken := m.CSRFToken(r)
		headerToken := r.Header.Get(CSRFHeaderName)
		if cookieToken == "" || headerToken == "" ||
			!session.VerifyCSRF(cookieToken) || !session.VerifyCSRF(headerToken) {
			writeAuthError(w, r, http.StatusForbidden, "csrf_failed", "invalid CSRF token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func writeAuthError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
		"requestId": logging.RequestID(r.Context()),
	})
}
