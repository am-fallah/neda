package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/auth"
	"github.com/am-fallah/neda/internal/provider/fake"
)

type monitorPayload struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	OriginName      string `json:"originName"`
	DestinationName string `json:"destinationName"`
	DepartureDate   string `json:"departureDate"`
	Adults          int    `json:"adults"`
	Children        int    `json:"children"`
	Infants         int    `json:"infants"`
	Enabled         bool   `json:"enabled"`
	Status          string `json:"status"`
	ExpiresAt       time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Providers       []struct {
		ProviderID          string     `json:"providerId"`
		Active              bool       `json:"active"`
		ConfigVersion       int64      `json:"configVersion"`
		NextDueAt           *time.Time `json:"nextDueAt"`
		ConsecutiveFailures int        `json:"consecutiveFailures"`
	} `json:"providers"`
}

type monitorListPayload struct {
	Monitors []monitorPayload `json:"monitors"`
}

type errorPayload struct {
	Error struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Fields  map[string]string `json:"fields"`
	} `json:"error"`
}

func monitorRequestBody(label string) map[string]any {
	return map[string]any{
		"label":           label,
		"originName":      "Tehran",
		"destinationName": "Mashhad",
		"departureDate":   "2099-12-15",
		"adults":          1,
		"children":        0,
		"infants":         0,
		"providers": []map[string]any{{
			"providerId":    string(fake.ID),
			"originId":      fake.OriginAvailable,
			"destinationId": fake.DestinationDefault,
		}},
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return string(encoded)
}

func createMonitor(t *testing.T, fixture *authFixture, session, csrf, label string) monitorPayload {
	t.Helper()
	recorder := fixture.request(t, http.MethodPost, "/api/v1/monitors",
		mustJSON(t, monitorRequestBody(label)), session, csrf, authOrigin)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create monitor status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var payload monitorPayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode monitor: %v", err)
	}
	if payload.ID == "" {
		t.Fatal("monitor id is empty")
	}
	return payload
}

func TestMonitorsCRUDAndPauseResume(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	session, csrf, login := fixture.login(t, authTestPass)
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d", login.Code)
	}

	created := createMonitor(t, fixture, session, csrf, "Morning train")
	if created.Status != "active" || !created.Enabled || created.Label != "Morning train" {
		t.Fatalf("created monitor = %+v", created)
	}
	if len(created.Providers) != 1 || created.Providers[0].ProviderID != string(fake.ID) ||
		!created.Providers[0].Active || created.Providers[0].ConfigVersion != 1 ||
		created.Providers[0].NextDueAt == nil {
		t.Fatalf("created providers = %+v", created.Providers)
	}

	list := fixture.request(t, http.MethodGet, "/api/v1/monitors", "", session, "", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%s", list.Code, list.Body.String())
	}
	var listed monitorListPayload
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Monitors) != 1 || listed.Monitors[0].ID != created.ID {
		t.Fatalf("listed monitors = %+v", listed.Monitors)
	}

	detail := fixture.request(t, http.MethodGet, "/api/v1/monitors/"+created.ID, "", session, "", "")
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status = %d", detail.Code)
	}

	patchedBody := monitorRequestBody("Evening train")
	patched := fixture.request(t, http.MethodPatch, "/api/v1/monitors/"+created.ID,
		mustJSON(t, patchedBody), session, csrf, authOrigin)
	if patched.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body=%s", patched.Code, patched.Body.String())
	}
	var updated monitorPayload
	if err := json.Unmarshal(patched.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if updated.Label != "Evening train" || updated.Providers[0].ConfigVersion != 1 {
		t.Fatalf("label-only patch = %+v", updated)
	}

	paused := fixture.request(t, http.MethodPost, "/api/v1/monitors/"+created.ID+"/pause", "", session, csrf, authOrigin)
	if paused.Code != http.StatusOK {
		t.Fatalf("pause status = %d, body=%s", paused.Code, paused.Body.String())
	}
	var pausedPayload monitorPayload
	if err := json.Unmarshal(paused.Body.Bytes(), &pausedPayload); err != nil {
		t.Fatalf("decode pause: %v", err)
	}
	if pausedPayload.Enabled || pausedPayload.Status != "paused" || pausedPayload.Providers[0].NextDueAt != nil {
		t.Fatalf("paused monitor = %+v", pausedPayload)
	}

	pausedAgain := fixture.request(t, http.MethodPost, "/api/v1/monitors/"+created.ID+"/pause", "", session, csrf, authOrigin)
	if pausedAgain.Code != http.StatusOK {
		t.Fatalf("idempotent pause status = %d", pausedAgain.Code)
	}

	resumed := fixture.request(t, http.MethodPost, "/api/v1/monitors/"+created.ID+"/resume", "", session, csrf, authOrigin)
	if resumed.Code != http.StatusOK {
		t.Fatalf("resume status = %d, body=%s", resumed.Code, resumed.Body.String())
	}
	var resumedPayload monitorPayload
	if err := json.Unmarshal(resumed.Body.Bytes(), &resumedPayload); err != nil {
		t.Fatalf("decode resume: %v", err)
	}
	if !resumedPayload.Enabled || resumedPayload.Status != "active" || resumedPayload.Providers[0].NextDueAt == nil {
		t.Fatalf("resumed monitor = %+v", resumedPayload)
	}

	deleted := fixture.request(t, http.MethodDelete, "/api/v1/monitors/"+created.ID, "", session, csrf, authOrigin)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body=%s", deleted.Code, deleted.Body.String())
	}
	missing := fixture.request(t, http.MethodGet, "/api/v1/monitors/"+created.ID, "", session, "", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("deleted monitor status = %d", missing.Code)
	}
}

func TestMonitorsValidationAndExpiredResume(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	session, csrf, _ := fixture.login(t, authTestPass)

	missingLabel := monitorRequestBody("")
	delete(missingLabel, "label")
	recorder := fixture.request(t, http.MethodPost, "/api/v1/monitors", mustJSON(t, missingLabel), session, csrf, authOrigin)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("missing label status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var validation errorPayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &validation); err != nil {
		t.Fatalf("decode validation: %v", err)
	}
	if validation.Error.Code != "validation_error" || validation.Error.Fields["label"] == "" {
		t.Fatalf("validation payload = %+v", validation)
	}

	body := monitorRequestBody("Duplicate")
	providers := body["providers"].([]map[string]any)
	body["providers"] = append(providers, providers[0])
	recorder = fixture.request(t, http.MethodPost, "/api/v1/monitors", mustJSON(t, body), session, csrf, authOrigin)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("duplicate provider status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &validation); err != nil {
		t.Fatalf("decode duplicate validation: %v", err)
	}
	if validation.Error.Fields["providers[1].providerId"] == "" {
		t.Fatalf("duplicate provider fields = %+v", validation.Error.Fields)
	}

	body = monitorRequestBody("Unknown provider")
	body["providers"] = []map[string]any{{
		"providerId":    "missing",
		"originId":      "x",
		"destinationId": "y",
	}}
	recorder = fixture.request(t, http.MethodPost, "/api/v1/monitors", mustJSON(t, body), session, csrf, authOrigin)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unregistered provider status = %d", recorder.Code)
	}

	body = monitorRequestBody("Unknown field")
	body["unexpected"] = true
	recorder = fixture.request(t, http.MethodPost, "/api/v1/monitors", mustJSON(t, body), session, csrf, authOrigin)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d", recorder.Code)
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &validation); err != nil {
		t.Fatalf("decode unknown field: %v", err)
	}
	if validation.Error.Code != "invalid_request" {
		t.Fatalf("unknown field code = %q", validation.Error.Code)
	}

	created := createMonitor(t, fixture, session, csrf, "Expiring")
	past := fixture.clock.now.Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err := fixture.db.Exec(`UPDATE monitors SET expires_at = ? WHERE id = ?`, past, created.ID); err != nil {
		t.Fatalf("expire monitor: %v", err)
	}
	resume := fixture.request(t, http.MethodPost, "/api/v1/monitors/"+created.ID+"/resume", "", session, csrf, authOrigin)
	if resume.Code != http.StatusConflict {
		t.Fatalf("expired resume status = %d, body=%s", resume.Code, resume.Body.String())
	}
	if err := json.Unmarshal(resume.Body.Bytes(), &validation); err != nil {
		t.Fatalf("decode expired resume: %v", err)
	}
	if validation.Error.Code != "monitor_expired" {
		t.Fatalf("expired resume code = %q", validation.Error.Code)
	}
}

func TestMonitorsAuthAndMethodHandling(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	session, csrf, _ := fixture.login(t, authTestPass)

	unauth := fixture.request(t, http.MethodGet, "/api/v1/monitors", "", "", "", "")
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list status = %d", unauth.Code)
	}

	// GET is a safe method and does not require CSRF once authenticated.
	get := fixture.request(t, http.MethodGet, "/api/v1/monitors", "", session, "", "")
	if get.Code != http.StatusOK {
		t.Fatalf("authenticated GET status = %d", get.Code)
	}

	noCSRF := fixture.request(t, http.MethodPost, "/api/v1/monitors",
		mustJSON(t, monitorRequestBody("No CSRF")), session, "", authOrigin)
	if noCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, body=%s", noCSRF.Code, noCSRF.Body.String())
	}

	method := fixture.request(t, http.MethodPut, "/api/v1/monitors", "{}", session, csrf, authOrigin)
	if method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT monitors status = %d", method.Code)
	}
	if allow := method.Header().Get("Allow"); allow != "GET, POST" {
		t.Fatalf("Allow = %q, want GET, POST", allow)
	}

	badPause := fixture.request(t, http.MethodDelete, "/api/v1/monitors/missing/pause", "", session, csrf, authOrigin)
	if badPause.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE pause status = %d", badPause.Code)
	}
	if allow := badPause.Header().Get("Allow"); allow != "POST" {
		t.Fatalf("pause Allow = %q, want POST", allow)
	}

	missing := fixture.request(t, http.MethodGet, "/api/v1/monitors/missing", "", session, "", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing monitor status = %d", missing.Code)
	}
	extra := fixture.request(t, http.MethodGet, "/api/v1/monitors/missing/extra", "", session, "", "")
	if extra.Code != http.StatusNotFound {
		t.Fatalf("extra path status = %d", extra.Code)
	}
}
