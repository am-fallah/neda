package httpapi_test

import (
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/am-fallah/neda/internal/auth"
	"github.com/am-fallah/neda/internal/push"
)

func validPushKeys(t *testing.T) (string, string) {
	t.Helper()
	_, x, y, err := elliptic.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := elliptic.Marshal(elliptic.P256(), x, y)
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(public), base64.RawURLEncoding.EncodeToString(auth)
}

func pushRegistrationBody(t *testing.T, endpoint, label string) string {
	t.Helper()
	p256dh, auth := validPushKeys(t)
	body, err := json.Marshal(map[string]any{
		"endpoint": endpoint,
		"keys":     map[string]string{"p256dh": p256dh, "auth": auth},
		"label":    label,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestPushAPIRequiresAuthentication(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/push/config", ""},
		{http.MethodPost, "/api/v1/push/subscriptions", "{}"},
		{http.MethodPost, "/api/v1/push/test", "{}"},
	} {
		response := fixture.request(t, tc.method, tc.path, tc.body, "", "", authOrigin)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s = %d, want 401", tc.method, tc.path, response.Code)
		}
	}
}

func TestPushAPIConfigAndSubscriptionLifecycle(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	session, csrf, login := fixture.login(t, authTestPass)
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d", login.Code)
	}

	config := fixture.request(t, http.MethodGet, "/api/v1/push/config", "", session, "", "")
	if config.Code != http.StatusOK {
		t.Fatalf("config = %d %s", config.Code, config.Body.String())
	}
	var configPayload struct {
		Enabled       bool   `json:"enabled"`
		Available     bool   `json:"available"`
		PublicKey     string `json:"publicKey"`
		Subscriptions []struct {
			ID string `json:"id"`
		} `json:"subscriptions"`
	}
	if err := json.Unmarshal(config.Body.Bytes(), &configPayload); err != nil {
		t.Fatal(err)
	}
	if !configPayload.Available || configPayload.PublicKey == "" || configPayload.Enabled {
		t.Fatalf("config = %+v", configPayload)
	}
	if strings.Contains(config.Body.String(), "private") || strings.Contains(config.Body.String(), "endpoint") {
		t.Fatalf("config leaked internal fields: %s", config.Body.String())
	}

	endpoint := "https://fcm.googleapis.com/fcm/send/api-test-device"
	body := pushRegistrationBody(t, endpoint, "Test laptop")
	if response := fixture.request(t, http.MethodPost, "/api/v1/push/subscriptions", body, session, "", authOrigin); response.Code != http.StatusForbidden {
		t.Fatalf("registration without CSRF = %d", response.Code)
	}
	created := fixture.request(t, http.MethodPost, "/api/v1/push/subscriptions", body, session, csrf, authOrigin)
	if created.Code != http.StatusCreated {
		t.Fatalf("registration = %d %s", created.Code, created.Body.String())
	}
	if strings.Contains(created.Body.String(), endpoint) || strings.Contains(created.Body.String(), "p256dh") ||
		strings.Contains(created.Body.String(), "auth") || strings.Contains(created.Body.String(), "endpoint") {
		t.Fatalf("registration leaked subscription material: %s", created.Body.String())
	}
	var subscription struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &subscription); err != nil {
		t.Fatal(err)
	}
	if subscription.ID == "" || subscription.Label != "Test laptop" {
		t.Fatalf("subscription = %+v", subscription)
	}

	// Re-registering the same endpoint is idempotent.
	again := fixture.request(t, http.MethodPost, "/api/v1/push/subscriptions", body, session, csrf, authOrigin)
	if again.Code != http.StatusCreated {
		t.Fatalf("re-registration = %d", again.Code)
	}
	var againSubscription struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(again.Body.Bytes(), &againSubscription); err != nil {
		t.Fatal(err)
	}
	if againSubscription.ID != subscription.ID {
		t.Fatalf("duplicate registration ID = %q, want %q", againSubscription.ID, subscription.ID)
	}

	config = fixture.request(t, http.MethodGet, "/api/v1/push/config", "", session, "", "")
	if !strings.Contains(config.Body.String(), subscription.ID) {
		t.Fatalf("config does not list the subscription: %s", config.Body.String())
	}

	if response := fixture.request(t, http.MethodDelete, "/api/v1/push/subscriptions/"+subscription.ID, "", session, "", authOrigin); response.Code != http.StatusForbidden {
		t.Fatalf("delete without CSRF = %d", response.Code)
	}
	if response := fixture.request(t, http.MethodDelete, "/api/v1/push/subscriptions/"+subscription.ID, "", session, csrf, authOrigin); response.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", response.Code, response.Body.String())
	}
	if response := fixture.request(t, http.MethodDelete, "/api/v1/push/subscriptions/"+subscription.ID, "", session, csrf, authOrigin); response.Code != http.StatusNoContent {
		t.Fatalf("second delete = %d", response.Code)
	}
	config = fixture.request(t, http.MethodGet, "/api/v1/push/config", "", session, "", "")
	if strings.Contains(config.Body.String(), subscription.ID) {
		t.Fatalf("deleted subscription still listed: %s", config.Body.String())
	}
	if response := fixture.request(t, http.MethodDelete, "/api/v1/push/subscriptions/does-not-exist", "", session, csrf, authOrigin); response.Code != http.StatusNotFound {
		t.Fatalf("missing delete = %d", response.Code)
	}

	for _, secret := range []string{endpoint, "p256dh", "endpoint_enc"} {
		if strings.Contains(fixture.logs.String(), secret) {
			t.Fatalf("logs leaked %q: %s", secret, fixture.logs.String())
		}
	}
}

func TestPushAPIValidationErrors(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	session, csrf, _ := fixture.login(t, authTestPass)
	cases := []struct {
		name  string
		body  string
		field string
	}{
		{"http-endpoint", pushRegistrationBody(t, "http://fcm.googleapis.com/fcm/send/x", ""), "endpoint"},
		{"private-ip", pushRegistrationBody(t, "https://127.0.0.1/fcm/send/x", ""), "endpoint"},
		{"unknown-host", pushRegistrationBody(t, "https://evil.example/fcm/send/x", ""), "endpoint"},
		{"missing-keys", `{"endpoint":"https://fcm.googleapis.com/fcm/send/x"}`, "keys.p256dh"},
		{"long-label", pushRegistrationBody(t, "https://fcm.googleapis.com/fcm/send/x", strings.Repeat("a", 101)), "label"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := fixture.request(t, http.MethodPost, "/api/v1/push/subscriptions", tc.body, session, csrf, authOrigin)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
			}
			var payload struct {
				Error struct {
					Code   string            `json:"code"`
					Fields map[string]string `json:"fields"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Error.Code != "validation_error" || payload.Error.Fields[tc.field] == "" {
				t.Fatalf("payload = %+v", payload)
			}
		})
	}
}

func TestPushAPITestEndpoint(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	session, csrf, _ := fixture.login(t, authTestPass)

	if response := fixture.request(t, http.MethodPost, "/api/v1/push/test", `{}`, session, "", authOrigin); response.Code != http.StatusForbidden {
		t.Fatalf("test without CSRF = %d", response.Code)
	}
	fixture.pushTester.outcome = push.TestOutcome{Attempted: 2, Sent: 1, Failed: 1, ErrorKinds: []string{"push_gone"}}
	response := fixture.request(t, http.MethodPost, "/api/v1/push/test", `{}`, session, csrf, authOrigin)
	if response.Code != http.StatusOK {
		t.Fatalf("test = %d %s", response.Code, response.Body.String())
	}
	for _, want := range []string{`"attempted":2`, `"sent":1`, `"failed":1`, `"push_gone"`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("test response missing %q: %s", want, response.Body.String())
		}
	}
	fixture.pushTester.err = push.ErrPushDisabled
	if response := fixture.request(t, http.MethodPost, "/api/v1/push/test", `{}`, session, csrf, authOrigin); response.Code != http.StatusConflict {
		t.Fatalf("disabled test = %d", response.Code)
	}
}

func TestPushAPIUnavailableWithoutKey(t *testing.T) {
	fixture := newAuthFixtureWithTelegram(t, auth.DefaultRateLimitConfig(), true, false)
	session, csrf, _ := fixture.login(t, authTestPass)
	config := fixture.request(t, http.MethodGet, "/api/v1/push/config", "", session, "", "")
	if config.Code != http.StatusOK || !strings.Contains(config.Body.String(), `"available":false`) {
		t.Fatalf("config without key = %d %s", config.Code, config.Body.String())
	}
	body := `{"endpoint":"https://fcm.googleapis.com/fcm/send/x","keys":{"p256dh":"x","auth":"y"}}`
	if response := fixture.request(t, http.MethodPost, "/api/v1/push/subscriptions", body, session, csrf, authOrigin); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("registration without key = %d", response.Code)
	}
	if response := fixture.request(t, http.MethodPost, "/api/v1/push/test", `{}`, session, csrf, authOrigin); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("test without key = %d", response.Code)
	}
}
