package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/auth"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/store"
)

// TestAPIEndToEndLifecycle exercises the real HTTP server, auth/session/CSRF,
// temp SQLite, fake provider registry, settings service, and NTF-006 read model
// as one owner workflow. It does not use a live provider or real credentials.
func TestAPIEndToEndLifecycle(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)

	for _, path := range []string{
		"/api/v1/monitors",
		"/api/v1/settings",
		"/api/v1/providers",
		"/api/v1/events",
		"/api/v1/deliveries",
		"/api/v1/overview",
	} {
		recorder := fixture.request(t, http.MethodGet, path, "", "", "", "")
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s status = %d, want 401", path, recorder.Code)
		}
	}

	session, csrf, login := fixture.login(t, authTestPass)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", login.Code, login.Body.String())
	}

	settingsRecorder := fixture.request(t, http.MethodGet, "/api/v1/settings", "", session, "", "")
	if settingsRecorder.Code != http.StatusOK {
		t.Fatalf("settings status = %d, body=%s", settingsRecorder.Code, settingsRecorder.Body.String())
	}
	settings := decodeSettings(t, settingsRecorder.Body.Bytes())
	if settings.PollIntervalSeconds != 300 || settings.PushEnabled {
		t.Fatalf("default settings = %+v", settings)
	}
	poll := 120
	push := true
	update := fixture.request(t, http.MethodPut, "/api/v1/settings", settingsUpdateBody(&poll, &push), session, csrf, authOrigin)
	if update.Code != http.StatusOK {
		t.Fatalf("settings update status = %d, body=%s", update.Code, update.Body.String())
	}
	if got := decodeSettings(t, update.Body.Bytes()); got.PollIntervalSeconds != 120 || !got.PushEnabled {
		t.Fatalf("updated settings = %+v", got)
	}
	telegram := fixture.request(t, http.MethodPut, "/api/v1/settings/telegram",
		telegramConfigBody(settingsTestToken, settingsTestChatID, false), session, csrf, authOrigin)
	if telegram.Code != http.StatusOK {
		t.Fatalf("telegram configure status = %d, body=%s", telegram.Code, telegram.Body.String())
	}
	telegramTest := fixture.request(t, http.MethodPost, "/api/v1/settings/telegram/test", "", session, csrf, authOrigin)
	if telegramTest.Code != http.StatusOK {
		t.Fatalf("telegram test status = %d, body=%s", telegramTest.Code, telegramTest.Body.String())
	}
	testedSettings := decodeSettings(t, telegramTest.Body.Bytes())
	if testedSettings.Telegram.LastTestOK == nil || !*testedSettings.Telegram.LastTestOK {
		t.Fatalf("telegram test metadata = %+v", testedSettings.Telegram)
	}

	created := createMonitor(t, fixture, session, csrf, "Integration monitor")
	list := fixture.request(t, http.MethodGet, "/api/v1/monitors", "", session, "", "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), created.ID) {
		t.Fatalf("monitor list status = %d, body=%s", list.Code, list.Body.String())
	}
	detail := fixture.request(t, http.MethodGet, "/api/v1/monitors/"+created.ID, "", session, "", "")
	if detail.Code != http.StatusOK {
		t.Fatalf("monitor detail status = %d, body=%s", detail.Code, detail.Body.String())
	}
	var detailPayload monitorDetailPayload
	if err := json.Unmarshal(detail.Body.Bytes(), &detailPayload); err != nil {
		t.Fatalf("decode monitor detail: %v", err)
	}
	if len(detailPayload.Providers) != 1 || len(detailPayload.Providers[0].Observations) != 0 {
		t.Fatalf("initial monitor diagnostics = %+v", detailPayload.Providers)
	}
	targetID, configVersion := monitorTarget(t, fixture, created.ID)
	base := fixture.clock.now
	insertHTTPObservation(t, fixture.db, targetID, httpObservationFixture{
		configVersion: configVersion, journeyKey: "integration-journey",
		offeringKey: "integration-offering", state: "reservable",
		observedAt: base.Add(-2 * time.Minute),
	})
	finished := base.Add(-time.Minute)
	insertHTTPCheck(t, fixture.db, targetID, httpCheckFixture{
		configVersion: configVersion, startedAt: base.Add(-2 * time.Minute),
		finishedAt: &finished, outcome: "success",
	})
	detail = fixture.request(t, http.MethodGet, "/api/v1/monitors/"+created.ID, "", session, "", "")
	if detail.Code != http.StatusOK {
		t.Fatalf("monitor detail after diagnostics status = %d", detail.Code)
	}
	if err := json.Unmarshal(detail.Body.Bytes(), &detailPayload); err != nil {
		t.Fatalf("decode updated monitor detail: %v", err)
	}
	if len(detailPayload.Providers[0].Observations) != 1 ||
		detailPayload.Providers[0].Observations[0].State != "reservable" ||
		len(detailPayload.Providers[0].RecentChecks) != 1 ||
		detailPayload.Providers[0].RecentChecks[0].Outcome != "success" {
		t.Fatalf("updated monitor diagnostics = %+v", detailPayload.Providers[0])
	}

	episodeID := insertHTTPEpisode(t, fixture.db, targetID, "integration-event-journey", "integration-event-offering", base.Add(-time.Minute))
	insertHTTPEvent(t, fixture.db, httpEventFixture{
		id: "evt-integration", kind: notify.KindAvailability, monitorID: created.ID,
		episodeID: &episodeID, providerID: "fake", occurredAt: base.Add(-time.Minute),
		trainNumber: "integration-train", originName: "Tehran", destinationName: "Mashhad",
	})
	insertHTTPEventDelivery(t, fixture.db, "d-integration", "evt-integration", notify.ChannelTelegram,
		"telegram", store.DeliverySent, 1, nil, &base, &base, "", base)

	events := fixture.request(t, http.MethodGet, "/api/v1/events", "", session, "", "")
	if events.Code != http.StatusOK {
		t.Fatalf("events status = %d, body=%s", events.Code, events.Body.String())
	}
	var eventList eventListPayload
	if err := json.Unmarshal(events.Body.Bytes(), &eventList); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(eventList.Events) != 1 || eventList.Events[0].ID != "evt-integration" {
		t.Fatalf("events = %+v", eventList.Events)
	}
	eventDetail := fixture.request(t, http.MethodGet, "/api/v1/events/evt-integration", "", session, "", "")
	if eventDetail.Code != http.StatusOK {
		t.Fatalf("event detail status = %d", eventDetail.Code)
	}
	var event eventPayload
	if err := json.Unmarshal(eventDetail.Body.Bytes(), &event); err != nil {
		t.Fatalf("decode event detail: %v", err)
	}
	if len(event.Deliveries) != 1 || event.Deliveries[0].Status != "sent" {
		t.Fatalf("event deliveries = %+v", event.Deliveries)
	}
	deliveries := fixture.request(t, http.MethodGet, "/api/v1/deliveries", "", session, "", "")
	if deliveries.Code != http.StatusOK {
		t.Fatalf("deliveries status = %d", deliveries.Code)
	}
	var deliveryList deliveryListPayload
	if err := json.Unmarshal(deliveries.Body.Bytes(), &deliveryList); err != nil {
		t.Fatalf("decode deliveries: %v", err)
	}
	if len(deliveryList.Deliveries) != 1 || deliveryList.Deliveries[0].Status != "sent" {
		t.Fatalf("deliveries = %+v", deliveryList.Deliveries)
	}
	overviewRecorder := fixture.request(t, http.MethodGet, "/api/v1/overview", "", session, "", "")
	if overviewRecorder.Code != http.StatusOK {
		t.Fatalf("overview status = %d", overviewRecorder.Code)
	}
	var overview overviewPayload
	if err := json.Unmarshal(overviewRecorder.Body.Bytes(), &overview); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
	if overview.Monitors.Total != 1 || overview.Monitors.Active != 1 ||
		overview.Deliveries.Total != 1 || overview.Deliveries.Sent != 1 ||
		len(overview.RecentEvents) != 1 || len(overview.RecentDeliveries) != 1 {
		t.Fatalf("overview = %+v", overview)
	}

	paused := fixture.request(t, http.MethodPost, "/api/v1/monitors/"+created.ID+"/pause", "", session, csrf, authOrigin)
	if paused.Code != http.StatusOK {
		t.Fatalf("pause status = %d", paused.Code)
	}
	overviewRecorder = fixture.request(t, http.MethodGet, "/api/v1/overview", "", session, "", "")
	if err := json.Unmarshal(overviewRecorder.Body.Bytes(), &overview); err != nil {
		t.Fatalf("decode paused overview: %v", err)
	}
	if overview.Monitors.Paused != 1 || overview.Monitors.Active != 0 {
		t.Fatalf("paused overview monitors = %+v", overview.Monitors)
	}
	resumed := fixture.request(t, http.MethodPost, "/api/v1/monitors/"+created.ID+"/resume", "", session, csrf, authOrigin)
	if resumed.Code != http.StatusOK {
		t.Fatalf("resume status = %d", resumed.Code)
	}

	deleted := fixture.request(t, http.MethodDelete, "/api/v1/monitors/"+created.ID, "", session, csrf, authOrigin)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", deleted.Code)
	}
	missingDetail := fixture.request(t, http.MethodGet, "/api/v1/monitors/"+created.ID, "", session, "", "")
	if missingDetail.Code != http.StatusNotFound {
		t.Fatalf("deleted monitor detail status = %d", missingDetail.Code)
	}
	events = fixture.request(t, http.MethodGet, "/api/v1/events", "", session, "", "")
	if err := json.Unmarshal(events.Body.Bytes(), &eventList); err != nil {
		t.Fatalf("decode events after delete: %v", err)
	}
	if eventList.Events == nil || len(eventList.Events) != 0 {
		t.Fatalf("events after delete = %+v", eventList.Events)
	}
	deliveries = fixture.request(t, http.MethodGet, "/api/v1/deliveries", "", session, "", "")
	if err := json.Unmarshal(deliveries.Body.Bytes(), &deliveryList); err != nil {
		t.Fatalf("decode deliveries after delete: %v", err)
	}
	if deliveryList.Deliveries == nil || len(deliveryList.Deliveries) != 0 {
		t.Fatalf("deliveries after delete = %+v", deliveryList.Deliveries)
	}
	overviewRecorder = fixture.request(t, http.MethodGet, "/api/v1/overview", "", session, "", "")
	if err := json.Unmarshal(overviewRecorder.Body.Bytes(), &overview); err != nil {
		t.Fatalf("decode overview after delete: %v", err)
	}
	if overview.Monitors.Total != 0 || overview.Deliveries.Total != 0 ||
		len(overview.RecentEvents) != 0 || len(overview.RecentDeliveries) != 0 {
		t.Fatalf("overview after delete = %+v", overview)
	}

	settingsAfterDelete := fixture.request(t, http.MethodGet, "/api/v1/settings", "", session, "", "")
	if settingsAfterDelete.Code != http.StatusOK {
		t.Fatalf("settings after delete status = %d", settingsAfterDelete.Code)
	}
	if got := decodeSettings(t, settingsAfterDelete.Body.Bytes()); !got.Telegram.Configured {
		t.Fatalf("telegram settings lost after monitor delete: %+v", got.Telegram)
	}

	logout := fixture.request(t, http.MethodPost, "/api/v1/auth/logout", "", session, csrf, authOrigin)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d", logout.Code)
	}
	afterLogout := fixture.request(t, http.MethodGet, "/api/v1/settings", "", session, "", "")
	if afterLogout.Code != http.StatusUnauthorized {
		t.Fatalf("settings after logout status = %d", afterLogout.Code)
	}

	logs := fixture.logs.String()
	if strings.Contains(logs, settingsTestToken) || strings.Contains(logs, settingsTestChatID) {
		t.Fatalf("integration logs leaked Telegram credentials")
	}
}
