package auth

import (
	"net/http"
	"time"
)

// SetSessionCookies writes the HttpOnly session cookie and the separate
// HttpOnly CSRF cookie. The CSRF token is also returned to the client in JSON
// so it can send X-CSRF-Token; the cookie lets /auth/session reissue it.
func (m *Manager) SetSessionCookies(w http.ResponseWriter, result LoginResult) {
	if w == nil {
		return
	}
	maxAge := m.cookieMaxAge()
	http.SetCookie(w, &http.Cookie{
		Name:     m.cfg.CookieName,
		Value:    result.SessionToken,
		Path:     "/",
		MaxAge:   maxAge,
		Expires:  result.ExpiresAt,
		HttpOnly: true,
		Secure:   m.cfg.Secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     m.cfg.CSRFCookieName,
		Value:    result.CSRFToken,
		Path:     "/",
		MaxAge:   maxAge,
		Expires:  result.ExpiresAt,
		HttpOnly: true,
		Secure:   m.cfg.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookies expires both auth cookies.
func (m *Manager) ClearSessionCookies(w http.ResponseWriter) {
	if w == nil {
		return
	}
	past := time.Unix(1, 0).UTC()
	for _, name := range []string{m.cfg.CookieName, m.cfg.CSRFCookieName} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			Expires:  past,
			HttpOnly: true,
			Secure:   m.cfg.Secure,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

// SessionToken reads the raw session token from the request cookie.
func (m *Manager) SessionToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	cookie, err := r.Cookie(m.cfg.CookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// CSRFToken reads the raw CSRF token from the request cookie.
func (m *Manager) CSRFToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	cookie, err := r.Cookie(m.cfg.CSRFCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}
