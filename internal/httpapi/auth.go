package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/auth"
)

type loginRequest struct {
	Password string `json:"password"`
}

type loginResponse struct {
	Authenticated bool      `json:"authenticated"`
	CSRFToken     string    `json:"csrfToken"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

type sessionResponse struct {
	Authenticated bool       `json:"authenticated"`
	CSRFToken     string     `json:"csrfToken,omitempty"`
	SetupRequired bool       `json:"setupRequired,omitempty"`
	ExpiresAt     *time.Time `json:"expiresAt,omitempty"`
}

type passwordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (s *Server) privateAuth(next http.Handler) http.Handler {
	if s.auth == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, r, http.StatusServiceUnavailable, "auth_unavailable", "authentication is not available")
		})
	}
	return s.auth.RequireSession(s.auth.RequireCSRF(next))
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	if s.auth == nil || s.loginGuard == nil {
		writeError(w, r, http.StatusServiceUnavailable, "auth_unavailable", "authentication is not available")
		return
	}
	if retry, err := s.loginGuard.Check(r); err != nil {
		switch {
		case errors.Is(err, auth.ErrRateLimited):
			w.Header().Set("Retry-After", strconv.Itoa(auth.RetryAfterSeconds(retry)))
			writeError(w, r, http.StatusTooManyRequests, "rate_limited", "too many login attempts")
		case errors.Is(err, auth.ErrOriginNotAllowed):
			writeError(w, r, http.StatusForbidden, "origin_not_allowed", "request origin is not allowed")
		default:
			writeError(w, r, http.StatusForbidden, "login_rejected", "login is not allowed")
		}
		return
	}

	var body loginRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid login request")
		return
	}
	result, err := s.auth.Login(r.Context(), body.Password, r.UserAgent())
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			s.loginGuard.Failure(r)
			writeError(w, r, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
		case errors.Is(err, auth.ErrSetupRequired):
			writeError(w, r, http.StatusServiceUnavailable, "setup_required", "password setup required")
		case errors.Is(err, auth.ErrPasswordChanged):
			writeError(w, r, http.StatusConflict, "password_changed", "credentials changed during login")
		default:
			writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		}
		return
	}
	s.loginGuard.Success(r)
	s.auth.SetSessionCookies(w, result)
	writeJSON(w, http.StatusOK, loginResponse{
		Authenticated: true,
		CSRFToken:     result.CSRFToken,
		ExpiresAt:     result.ExpiresAt.UTC(),
	})
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	if !requireGet(w, r) {
		return
	}
	if s.auth == nil {
		writeError(w, r, http.StatusServiceUnavailable, "auth_unavailable", "authentication is not available")
		return
	}
	info, err := s.auth.SessionInfo(r.Context(), s.auth.SessionToken(r), s.auth.CSRFToken(r))
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	response := sessionResponse{
		Authenticated: info.Authenticated,
		CSRFToken:     info.CSRFToken,
		SetupRequired: info.SetupRequired,
	}
	if info.Session != nil {
		expires := info.Session.ExpiresAt.UTC()
		response.ExpiresAt = &expires
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if err := s.auth.Logout(r.Context(), session.ID); err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	s.auth.ClearSessionCookies(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) password(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	var body passwordRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid password request")
		return
	}
	if strings.TrimSpace(body.CurrentPassword) == "" || strings.TrimSpace(body.NewPassword) == "" {
		writeError(w, r, http.StatusBadRequest, "validation_error", "current and new passwords are required")
		return
	}
	err := s.auth.ChangePassword(r.Context(), session.ID, body.CurrentPassword, body.NewPassword)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, r, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
	case errors.Is(err, auth.ErrPasswordChanged):
		writeError(w, r, http.StatusConflict, "password_changed", "password was changed by another request")
	case errors.Is(err, auth.ErrSetupRequired):
		writeError(w, r, http.StatusServiceUnavailable, "setup_required", "password setup required")
	default:
		if auth.ValidatePassword(body.NewPassword) != nil {
			writeError(w, r, http.StatusBadRequest, "validation_error", "new password does not meet the password policy")
			return
		}
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func requirePost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodPost {
		return true
	}
	w.Header().Set(headerAllow, http.MethodPost)
	writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	return false
}

// decodeJSON enforces a JSON media type, rejects unknown fields and trailing
// values, and leaves body-size limits to the outer middleware.
func decodeJSON(r *http.Request, dst any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get(headerContentType))
	if err != nil || mediaType != contentTypeJSON {
		return errors.New("httpapi: JSON content type required")
	}
	if r.Body == nil {
		return errors.New("httpapi: request body is required")
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("httpapi: trailing JSON data")
	}
	return nil
}
