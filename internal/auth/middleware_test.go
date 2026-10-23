package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func loginForTest(t *testing.T, manager *Manager) LoginResult {
	t.Helper()
	if err := manager.SetPassword(context.Background(), testPassword); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	result, err := manager.Login(context.Background(), testPassword, "test-agent")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	return result
}

func TestSessionCookieFlags(t *testing.T) {
	manager, _, _, _ := newAuthManager(t, true)
	result := loginForTest(t, manager)
	recorder := httptest.NewRecorder()
	manager.SetSessionCookies(recorder, result)

	cookies := recorder.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("cookies = %d, want 2", len(cookies))
	}
	for _, cookie := range cookies {
		if cookie.Name != manager.cfg.CookieName && cookie.Name != manager.cfg.CSRFCookieName {
			t.Fatalf("unexpected cookie name %q", cookie.Name)
		}
		if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
			t.Fatalf("cookie %q flags = HttpOnly:%v Secure:%v SameSite:%v", cookie.Name, cookie.HttpOnly, cookie.Secure, cookie.SameSite)
		}
		if cookie.Path != "/" || cookie.Domain != "" {
			t.Fatalf("cookie %q path/domain = %q/%q", cookie.Name, cookie.Path, cookie.Domain)
		}
		if cookie.MaxAge != int((2 * time.Hour).Seconds()) {
			t.Fatalf("cookie %q MaxAge = %d", cookie.Name, cookie.MaxAge)
		}
		if cookie.Value == "" {
			t.Fatalf("cookie %q has empty value", cookie.Name)
		}
	}

	devManager, _, _, _ := newAuthManager(t, false)
	devResult := loginForTest(t, devManager)
	devRecorder := httptest.NewRecorder()
	devManager.SetSessionCookies(devRecorder, devResult)
	for _, cookie := range devRecorder.Result().Cookies() {
		if cookie.Secure {
			t.Fatalf("development cookie %q is Secure", cookie.Name)
		}
	}
}

func TestClearSessionCookies(t *testing.T) {
	manager, _, _, _ := newAuthManager(t, true)
	recorder := httptest.NewRecorder()
	manager.ClearSessionCookies(recorder)
	cookies := recorder.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("cleared cookies = %d, want 2", len(cookies))
	}
	for _, cookie := range cookies {
		if cookie.MaxAge >= 0 {
			t.Fatalf("cookie %q MaxAge = %d, want negative", cookie.Name, cookie.MaxAge)
		}
		if !cookie.Expires.Before(time.Now()) {
			t.Fatalf("cookie %q Expires = %v, want past", cookie.Name, cookie.Expires)
		}
		if cookie.Value != "" || !cookie.HttpOnly || !cookie.Secure {
			t.Fatalf("cookie %q not cleared correctly", cookie.Name)
		}
	}
}

func TestCookieReaders(t *testing.T) {
	manager, _, _, _ := newAuthManager(t, true)
	result := loginForTest(t, manager)
	req := httptest.NewRequest(http.MethodGet, "https://neda.example.com/", nil)
	req.AddCookie(&http.Cookie{Name: manager.cfg.CookieName, Value: result.SessionToken})
	req.AddCookie(&http.Cookie{Name: manager.cfg.CSRFCookieName, Value: result.CSRFToken})
	if got := manager.SessionToken(req); got != result.SessionToken {
		t.Fatalf("SessionToken = %q", got)
	}
	if got := manager.CSRFToken(req); got != result.CSRFToken {
		t.Fatalf("CSRFToken = %q", got)
	}
	if got := manager.SessionToken(nil); got != "" {
		t.Fatalf("nil request SessionToken = %q", got)
	}
}

func TestRequireSessionMiddleware(t *testing.T) {
	manager, _, _, _ := newAuthManager(t, true)
	result := loginForTest(t, manager)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := SessionFromContext(r.Context()); !ok {
			t.Fatal("session missing from context")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	handler := manager.RequireSession(next)

	request := httptest.NewRequest(http.MethodGet, "https://neda.example.com/api/v1/monitors", nil)
	request.AddCookie(&http.Cookie{Name: manager.cfg.CookieName, Value: result.SessionToken})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("valid session status = %d", recorder.Code)
	}

	missing := httptest.NewRequest(http.MethodGet, "https://neda.example.com/api/v1/monitors", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, missing)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d", recorder.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body["error"].(map[string]any)["code"] != "unauthenticated" {
		t.Fatalf("error body = %s", recorder.Body.String())
	}
	if len(recorder.Result().Cookies()) != 2 {
		t.Fatalf("missing session did not clear both cookies")
	}
}

func TestRequireCSRFMiddleware(t *testing.T) {
	manager, _, _, _ := newAuthManager(t, true)
	result := loginForTest(t, manager)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := manager.RequireSession(manager.RequireCSRF(next))

	newRequest := func(method string) *http.Request {
		req := httptest.NewRequest(method, "https://neda.example.com/api/v1/monitors", nil)
		req.AddCookie(&http.Cookie{Name: manager.cfg.CookieName, Value: result.SessionToken})
		req.AddCookie(&http.Cookie{Name: manager.cfg.CSRFCookieName, Value: result.CSRFToken})
		req.Header.Set("Origin", "https://neda.example.com")
		req.Header.Set(CSRFHeaderName, result.CSRFToken)
		return req
	}

	tests := []struct {
		name   string
		mutate func(*http.Request)
		status int
		code   string
	}{
		{"valid", func(*http.Request) {}, http.StatusNoContent, ""},
		{"missing header", func(r *http.Request) { r.Header.Del(CSRFHeaderName) }, http.StatusForbidden, "csrf_failed"},
		{"wrong header", func(r *http.Request) { r.Header.Set(CSRFHeaderName, "wrong") }, http.StatusForbidden, "csrf_failed"},
		{"missing cookie", func(r *http.Request) {
			r.Header.Del("Cookie")
			r.AddCookie(&http.Cookie{Name: manager.cfg.CookieName, Value: result.SessionToken})
		}, http.StatusForbidden, "csrf_failed"},
		{"wrong origin", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example.com") }, http.StatusForbidden, "origin_not_allowed"},
		{"missing origin", func(r *http.Request) {
			r.Header.Del("Origin")
			r.Header.Del("Referer")
		}, http.StatusForbidden, "origin_not_allowed"},
		{"safe method", func(r *http.Request) {
			r.Method = http.MethodGet
			r.Header.Del(CSRFHeaderName)
			r.Header.Del("Origin")
			r.Header.Del("Cookie")
			r.AddCookie(&http.Cookie{Name: manager.cfg.CookieName, Value: result.SessionToken})
		}, http.StatusNoContent, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := newRequest(http.MethodPost)
			test.mutate(req)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.status, recorder.Body.String())
			}
			if test.code != "" {
				var body map[string]any
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode error body: %v", err)
				}
				if body["error"].(map[string]any)["code"] != test.code {
					t.Fatalf("error body = %s", recorder.Body.String())
				}
			}
		})
	}
}

func TestRequireCSRFWithoutSession(t *testing.T) {
	manager, _, _, _ := newAuthManager(t, true)
	handler := manager.RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not run")
	}))
	req := httptest.NewRequest(http.MethodPost, "https://neda.example.com/api/v1/monitors", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}
