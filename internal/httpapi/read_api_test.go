package httpapi_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/auth"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/store"
)

type providerListPayload struct {
	Providers []struct {
		ID           string `json:"id"`
		DisplayName  string `json:"displayName"`
		TestOnly     bool   `json:"testOnly"`
		Capabilities struct {
			RequiresOriginID      bool `json:"requiresOriginId"`
			RequiresDestinationID bool `json:"requiresDestinationId"`
			SupportsBookingURL    bool `json:"supportsBookingUrl"`
			SupportsGender        bool `json:"supportsGender"`
			MaxPassengers         int  `json:"maxPassengers"`
		} `json:"capabilities"`
		Health *struct {
			LastAttemptAt         *time.Time `json:"lastAttemptAt"`
			LastSuccessAt         *time.Time `json:"lastSuccessAt"`
			LastFailureAt         *time.Time `json:"lastFailureAt"`
			LastErrorKind         string     `json:"lastErrorKind"`
			LastDurationMS        *int64     `json:"lastDurationMs"`
			ConsecutiveFailures   int        `json:"consecutiveFailures"`
			Stale                 bool       `json:"stale"`
			StaleThresholdSeconds int        `json:"staleThresholdSeconds"`
			OpenEpisode           *struct {
				ID                 int64     `json:"id"`
				OpenedAt           time.Time `json:"openedAt"`
				FailureCountAtOpen int       `json:"failureCountAtOpen"`
				LastErrorKind      string    `json:"lastErrorKind"`
			} `json:"openEpisode"`
		} `json:"health"`
	} `json:"providers"`
}

type eventListPayload struct {
	Events     []eventPayload `json:"events"`
	NextCursor string         `json:"nextCursor"`
}

type eventPayload struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	ProviderID   string    `json:"providerId"`
	MonitorID    string    `json:"monitorId"`
	MonitorLabel string    `json:"monitorLabel"`
	EpisodeID    *int64    `json:"episodeId"`
	OccurredAt   time.Time `json:"occurredAt"`
	State        string    `json:"state"`
	BookingURL   string    `json:"bookingUrl"`
	ErrorKind    string    `json:"errorKind"`
	FailureCount *int      `json:"failureCount"`
	Deliveries   []struct {
		ID            string     `json:"id"`
		Channel       string     `json:"channel"`
		Status        string     `json:"status"`
		AttemptCount  int        `json:"attemptCount"`
		NextAttemptAt *time.Time `json:"nextAttemptAt"`
		LastErrorKind string     `json:"lastErrorKind"`
	} `json:"deliveries"`
	DeliverySummary struct {
		Total          int `json:"total"`
		Pending        int `json:"pending"`
		Sent           int `json:"sent"`
		RetryScheduled int `json:"retryScheduled"`
		Failed         int `json:"failed"`
		Skipped        int `json:"skipped"`
		Attempted      int `json:"attempted"`
		Terminal       int `json:"terminal"`
	} `json:"deliverySummary"`
}

type deliveryListPayload struct {
	Deliveries []struct {
		ID              string     `json:"id"`
		EventID         string     `json:"eventId"`
		EventKind       string     `json:"eventKind"`
		EventOccurredAt time.Time  `json:"eventOccurredAt"`
		ProviderID      string     `json:"providerId"`
		MonitorID       string     `json:"monitorId"`
		Channel         string     `json:"channel"`
		Status          string     `json:"status"`
		AttemptCount    int        `json:"attemptCount"`
		NextAttemptAt   *time.Time `json:"nextAttemptAt"`
		LastErrorKind   string     `json:"lastErrorKind"`
		CreatedAt       time.Time  `json:"createdAt"`
	} `json:"deliveries"`
	NextCursor string `json:"nextCursor"`
}

type overviewPayload struct {
	GeneratedAt time.Time `json:"generatedAt"`
	Monitors    struct {
		Total   int `json:"total"`
		Active  int `json:"active"`
		Paused  int `json:"paused"`
		Expired int `json:"expired"`
	} `json:"monitors"`
	Providers struct {
		Total    int `json:"total"`
		Degraded int `json:"degraded"`
		Stale    int `json:"stale"`
	} `json:"providers"`
	Deliveries struct {
		Total          int `json:"total"`
		Pending        int `json:"pending"`
		Sending        int `json:"sending"`
		Sent           int `json:"sent"`
		RetryScheduled int `json:"retryScheduled"`
		Failed         int `json:"failed"`
		Skipped        int `json:"skipped"`
	} `json:"deliveries"`
	RecentEvents     []eventPayload `json:"recentEvents"`
	RecentDeliveries []struct {
		ID      string `json:"id"`
		Channel string `json:"channel"`
		Status  string `json:"status"`
	} `json:"recentDeliveries"`
}

func insertHTTPEpisode(t *testing.T, database *sql.DB, targetID int64, journeyKey, offeringKey string, observedAt time.Time) int64 {
	t.Helper()
	result, err := database.Exec(`INSERT INTO inventory_states
		(monitor_provider_id, config_version, journey_key, offering_key, state,
		 state_since, last_observed_at, last_decisive_at, updated_at)
		VALUES (?, 1, ?, ?, 'reservable', ?, ?, ?, ?)`,
		targetID, journeyKey, offeringKey, httpTime(observedAt),
		httpTime(observedAt), httpTime(observedAt), httpTime(observedAt),
	)
	if err != nil {
		t.Fatalf("insert HTTP episode state: %v", err)
	}
	stateID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("HTTP episode state id: %v", err)
	}
	result, err = database.Exec(`INSERT INTO availability_episodes (state_id, episode_seq, opened_at)
		VALUES (?, 1, ?)`, stateID, httpTime(observedAt))
	if err != nil {
		t.Fatalf("insert HTTP availability episode: %v", err)
	}
	episodeID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("HTTP availability episode id: %v", err)
	}
	return episodeID
}

type httpEventFixture struct {
	id              string
	kind            notify.EventKind
	monitorID       string
	episodeID       *int64
	providerID      string
	occurredAt      time.Time
	trainNumber     string
	originName      string
	destinationName string
	wagonName       string
	className       string
	priceAmount     *int64
	priceCurrency   string
	bookingURL      string
	errorKind       string
	failureCount    *int
	createdAt       time.Time
}

func insertHTTPEvent(t *testing.T, database *sql.DB, fixture httpEventFixture) {
	t.Helper()
	if fixture.createdAt.IsZero() {
		fixture.createdAt = fixture.occurredAt
	}
	if _, err := database.Exec(`INSERT INTO events
		(id, kind, dedupe_key, monitor_id, provider_id, episode_id, occurred_at,
		 train_number, operator, origin_name, destination_name, departure_at, arrival_at,
		 wagon_name, class_name, price_amount, price_currency, booking_url,
		 error_kind, failure_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, NULL, NULL, ?, ?, ?, ?, ?, ?, ?, ?)`,
		fixture.id, string(fixture.kind), "dedupe:"+fixture.id,
		httpNullString(fixture.monitorID), fixture.providerID, httpNullInt64(fixture.episodeID),
		httpTime(fixture.occurredAt), httpNullString(fixture.trainNumber),
		httpNullString(fixture.originName), httpNullString(fixture.destinationName),
		httpNullString(fixture.wagonName), httpNullString(fixture.className),
		httpNullInt64(fixture.priceAmount), httpNullString(fixture.priceCurrency),
		httpNullString(fixture.bookingURL), httpNullString(fixture.errorKind),
		httpNullInt(fixture.failureCount), httpTime(fixture.createdAt),
	); err != nil {
		t.Fatalf("insert HTTP event: %v", err)
	}
}

func insertHTTPEventDelivery(t *testing.T, database *sql.DB, id, eventID string, channel notify.Channel, targetKey string, status store.DeliveryStatus, attempts int, next, last, sent *time.Time, errorKind string, createdAt time.Time) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO notification_deliveries
		(id, event_id, channel, target_key, status, attempt_count, next_attempt_at,
		 last_attempt_at, sent_at, last_error_kind, lease_until, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?)`,
		id, eventID, string(channel), targetKey, string(status), attempts,
		httpNullTime(next), httpNullTime(last), httpNullTime(sent),
		httpNullString(errorKind), httpTime(createdAt), httpTime(createdAt),
	); err != nil {
		t.Fatalf("insert HTTP delivery: %v", err)
	}
}

func insertHTTPHealthEpisode(t *testing.T, database *sql.DB, providerID string, openedAt time.Time, failureCount int, errorKind string) int64 {
	t.Helper()
	result, err := database.Exec(`INSERT INTO provider_health_episodes
		(provider_id, opened_at, failure_count_at_open, last_error_kind)
		VALUES (?, ?, ?, ?)`,
		providerID, httpTime(openedAt), failureCount, httpNullString(errorKind),
	)
	if err != nil {
		t.Fatalf("insert HTTP health episode: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("HTTP health episode id: %v", err)
	}
	return id
}

func insertHTTPProviderHealth(t *testing.T, database *sql.DB, providerID string, lastAttempt, lastSuccess, lastFailure *time.Time, errorKind string, duration *int64, failures int, openEpisodeID *int64, updatedAt time.Time) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO provider_health
		(provider_id, last_attempt_at, last_success_at, last_failure_at,
		 last_error_kind, last_duration_ms, consecutive_failures,
		 open_episode_id, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		providerID, httpNullTime(lastAttempt), httpNullTime(lastSuccess),
		httpNullTime(lastFailure), httpNullString(errorKind), httpNullInt64(duration),
		failures, httpNullInt64(openEpisodeID), httpTime(updatedAt),
	); err != nil {
		t.Fatalf("insert HTTP provider health: %v", err)
	}
}

func TestProvidersEndpoint(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	session, csrf, _ := fixture.login(t, authTestPass)

	unauthenticated := fixture.request(t, http.MethodGet, "/api/v1/providers", "", "", "", "")
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated providers status = %d", unauthenticated.Code)
	}
	recorder := fixture.request(t, http.MethodGet, "/api/v1/providers", "", session, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("providers status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var providers providerListPayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &providers); err != nil {
		t.Fatalf("decode providers: %v", err)
	}
	if len(providers.Providers) != 2 {
		t.Fatalf("providers = %d, want 2: %+v", len(providers.Providers), providers.Providers)
	}
	fakeProvider := providers.Providers[0]
	if fakeProvider.ID != "fake" || !fakeProvider.TestOnly || fakeProvider.Health != nil {
		t.Fatalf("fake provider = %+v", fakeProvider)
	}
	if !fakeProvider.Capabilities.RequiresOriginID || !fakeProvider.Capabilities.RequiresDestinationID {
		t.Fatalf("fake capabilities = %+v", fakeProvider.Capabilities)
	}

	oldSuccess := fixture.clock.now.Add(-2 * time.Hour)
	newer := fixture.clock.now.Add(-time.Minute)
	duration := int64(1500)
	insertHTTPProviderHealth(t, fixture.db, "fake", &newer, &oldSuccess, &newer, "timeout", &duration, 0, nil, newer)
	episodeID := insertHTTPHealthEpisode(t, fixture.db, "alt", oldSuccess, 3, "network")
	insertHTTPProviderHealth(t, fixture.db, "alt", &newer, nil, &newer, "network", nil, 3, &episodeID, newer)

	recorder = fixture.request(t, http.MethodGet, "/api/v1/providers", "", session, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("providers with health status = %d", recorder.Code)
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &providers); err != nil {
		t.Fatalf("decode providers with health: %v", err)
	}
	fakeHealth := providers.Providers[0].Health
	if fakeHealth == nil || !fakeHealth.Stale || fakeHealth.StaleThresholdSeconds != 900 ||
		fakeHealth.LastDurationMS == nil || *fakeHealth.LastDurationMS != 1500 ||
		fakeHealth.LastErrorKind != "timeout" {
		t.Fatalf("fake health = %+v", fakeHealth)
	}
	altHealth := providers.Providers[1].Health
	if altHealth == nil || !altHealth.Stale || altHealth.OpenEpisode == nil ||
		altHealth.OpenEpisode.ID != episodeID || altHealth.OpenEpisode.FailureCountAtOpen != 3 ||
		altHealth.ConsecutiveFailures != 3 {
		t.Fatalf("alt health = %+v", altHealth)
	}
	if strings.Contains(recorder.Body.String(), "last_duration_ms") || strings.Contains(recorder.Body.String(), "open_episode_id") {
		t.Fatalf("provider response leaked internal columns: %s", recorder.Body.String())
	}
	method := fixture.request(t, http.MethodPost, "/api/v1/providers", "", session, csrf, authOrigin)
	if method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST providers status = %d", method.Code)
	}
}

func TestEventsAndDeliveriesEndpoints(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	session, csrf, _ := fixture.login(t, authTestPass)
	created := createMonitor(t, fixture, session, csrf, "Read API monitor")
	targetID, _ := monitorTarget(t, fixture, created.ID)
	base := fixture.clock.now

	episode1 := insertHTTPEpisode(t, fixture.db, targetID, "journey-1", "offering-1", base.Add(-3*time.Minute))
	episode2 := insertHTTPEpisode(t, fixture.db, targetID, "journey-2", "offering-2", base.Add(-2*time.Minute))
	episode3 := insertHTTPEpisode(t, fixture.db, targetID, "journey-3", "offering-3", base.Add(-time.Minute))
	booked := "https://mrbilit.com/journey-1"
	price := int64(125000)
	for index, event := range []struct {
		id        string
		episodeID int64
		at        time.Time
	}{
		{"evt-1", episode1, base.Add(-3 * time.Minute)},
		{"evt-2", episode2, base.Add(-2 * time.Minute)},
		{"evt-3", episode3, base.Add(-time.Minute)},
	} {
		insertHTTPEvent(t, fixture.db, httpEventFixture{
			id: event.id, kind: notify.KindAvailability, monitorID: created.ID,
			episodeID: &event.episodeID, providerID: "fake", occurredAt: event.at,
			trainNumber: "train-" + string(rune('1'+index)),
			originName:  "Tehran", destinationName: "Mashhad",
			wagonName: "wagon", className: "class", priceAmount: &price,
			priceCurrency: "IRR", bookingURL: booked,
		})
	}
	failureCount := 3
	insertHTTPEvent(t, fixture.db, httpEventFixture{
		id: "evt-health", kind: notify.KindProviderDegraded, providerID: "fake",
		occurredAt: base, errorKind: "timeout", failureCount: &failureCount,
	})

	next := base.Add(time.Minute)
	sentAt := base.Add(-2 * time.Minute)
	insertHTTPEventDelivery(t, fixture.db, "d-1-telegram", "evt-1", notify.ChannelTelegram, "telegram", store.DeliverySent, 1, nil, &sentAt, &sentAt, "", base.Add(-3*time.Minute))
	insertHTTPEventDelivery(t, fixture.db, "d-1-push", "evt-1", notify.ChannelPush, "push:one", store.DeliveryRetryScheduled, 2, &next, &sentAt, nil, "telegram_transient", base.Add(-3*time.Minute))
	insertHTTPEventDelivery(t, fixture.db, "d-2-telegram", "evt-2", notify.ChannelTelegram, "telegram", store.DeliverySent, 1, nil, &sentAt, &sentAt, "", base.Add(-2*time.Minute))
	insertHTTPEventDelivery(t, fixture.db, "d-3-push", "evt-3", notify.ChannelPush, "push:two", store.DeliveryFailed, 5, nil, &sentAt, nil, "permanent", base.Add(-time.Minute))
	insertHTTPEventDelivery(t, fixture.db, "d-health", "evt-health", notify.ChannelTelegram, "telegram", store.DeliverySkipped, 0, nil, nil, nil, "unconfigured", base)

	unauthenticated := fixture.request(t, http.MethodGet, "/api/v1/events", "", "", "", "")
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated events status = %d", unauthenticated.Code)
	}
	recorder := fixture.request(t, http.MethodGet, "/api/v1/events", "", session, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("events status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var events eventListPayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &events); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	wantOrder := []string{"evt-health", "evt-3", "evt-2", "evt-1"}
	if len(events.Events) != len(wantOrder) {
		t.Fatalf("events = %d, want %d", len(events.Events), len(wantOrder))
	}
	for index, want := range wantOrder {
		if events.Events[index].ID != want {
			t.Fatalf("event %d = %q, want %q", index, events.Events[index].ID, want)
		}
	}
	healthEvent := events.Events[0]
	if healthEvent.MonitorID != "" || healthEvent.State != "" || healthEvent.ErrorKind != "timeout" ||
		healthEvent.FailureCount == nil || *healthEvent.FailureCount != 3 {
		t.Fatalf("health event = %+v", healthEvent)
	}
	availabilityEvent := events.Events[3]
	if availabilityEvent.State != "reservable" || availabilityEvent.MonitorID != created.ID ||
		availabilityEvent.BookingURL != booked || len(availabilityEvent.Deliveries) != 2 ||
		availabilityEvent.DeliverySummary.Total != 2 || availabilityEvent.DeliverySummary.Sent != 1 ||
		availabilityEvent.DeliverySummary.RetryScheduled != 1 || availabilityEvent.DeliverySummary.Attempted != 2 ||
		availabilityEvent.DeliverySummary.Terminal != 1 {
		t.Fatalf("availability event = %+v", availabilityEvent)
	}
	if strings.Contains(recorder.Body.String(), "targetKey") || strings.Contains(recorder.Body.String(), "dedupeKey") ||
		strings.Contains(recorder.Body.String(), "leaseUntil") {
		t.Fatalf("events response leaked internal fields: %s", recorder.Body.String())
	}

	for _, filter := range []struct {
		path  string
		count int
	}{
		{"/api/v1/events?kind=availability", 3},
		{"/api/v1/events?monitorId=" + created.ID, 3},
		{"/api/v1/events?providerId=fake", 4},
		{"/api/v1/events?kind=provider_recovered", 0},
	} {
		filtered := fixture.request(t, http.MethodGet, filter.path, "", session, "", "")
		if filtered.Code != http.StatusOK {
			t.Fatalf("%s status = %d", filter.path, filtered.Code)
		}
		var filteredEvents eventListPayload
		if err := json.Unmarshal(filtered.Body.Bytes(), &filteredEvents); err != nil {
			t.Fatalf("decode %s: %v", filter.path, err)
		}
		if len(filteredEvents.Events) != filter.count {
			t.Fatalf("%s events = %d, want %d", filter.path, len(filteredEvents.Events), filter.count)
		}
	}

	limited := fixture.request(t, http.MethodGet, "/api/v1/events?limit=2", "", session, "", "")
	if limited.Code != http.StatusOK {
		t.Fatalf("limited events status = %d", limited.Code)
	}
	var limitedPayload eventListPayload
	if err := json.Unmarshal(limited.Body.Bytes(), &limitedPayload); err != nil {
		t.Fatalf("decode limited events: %v", err)
	}
	if len(limitedPayload.Events) != 2 || limitedPayload.NextCursor == "" {
		t.Fatalf("limited events = %+v", limitedPayload)
	}
	nextPage := fixture.request(t, http.MethodGet, "/api/v1/events?limit=2&cursor="+limitedPayload.NextCursor, "", session, "", "")
	if nextPage.Code != http.StatusOK {
		t.Fatalf("next events page status = %d", nextPage.Code)
	}
	var nextPayload eventListPayload
	if err := json.Unmarshal(nextPage.Body.Bytes(), &nextPayload); err != nil {
		t.Fatalf("decode next events: %v", err)
	}
	if len(nextPayload.Events) != 2 || nextPayload.Events[0].ID != "evt-2" || nextPayload.Events[1].ID != "evt-1" ||
		nextPayload.NextCursor != "" {
		t.Fatalf("next events page = %+v", nextPayload)
	}

	for _, path := range []string{
		"/api/v1/events?kind=other",
		"/api/v1/events?limit=101",
		"/api/v1/events?limit=nope",
		"/api/v1/events?cursor=nope",
		"/api/v1/deliveries?channel=email",
		"/api/v1/deliveries?status=nope",
		"/api/v1/deliveries?kind=nope",
		"/api/v1/deliveries?limit=101",
		"/api/v1/deliveries?cursor=nope",
	} {
		response := fixture.request(t, http.MethodGet, path, "", session, "", "")
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400; body=%s", path, response.Code, response.Body.String())
		}
	}

	detail := fixture.request(t, http.MethodGet, "/api/v1/events/evt-1", "", session, "", "")
	if detail.Code != http.StatusOK {
		t.Fatalf("event detail status = %d, body=%s", detail.Code, detail.Body.String())
	}
	var detailEvent eventPayload
	if err := json.Unmarshal(detail.Body.Bytes(), &detailEvent); err != nil {
		t.Fatalf("decode event detail: %v", err)
	}
	if detailEvent.ID != "evt-1" || len(detailEvent.Deliveries) != 2 {
		t.Fatalf("event detail = %+v", detailEvent)
	}
	missing := fixture.request(t, http.MethodGet, "/api/v1/events/missing", "", session, "", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing event status = %d", missing.Code)
	}
	method := fixture.request(t, http.MethodPost, "/api/v1/events", "", session, csrf, authOrigin)
	if method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST events status = %d", method.Code)
	}

	deliveries := fixture.request(t, http.MethodGet, "/api/v1/deliveries", "", session, "", "")
	if deliveries.Code != http.StatusOK {
		t.Fatalf("deliveries status = %d, body=%s", deliveries.Code, deliveries.Body.String())
	}
	var deliveryList deliveryListPayload
	if err := json.Unmarshal(deliveries.Body.Bytes(), &deliveryList); err != nil {
		t.Fatalf("decode deliveries: %v", err)
	}
	if len(deliveryList.Deliveries) != 5 {
		t.Fatalf("deliveries = %d, want 5", len(deliveryList.Deliveries))
	}
	if strings.Contains(deliveries.Body.String(), "targetKey") || strings.Contains(deliveries.Body.String(), "leaseUntil") {
		t.Fatalf("delivery response leaked internal fields: %s", deliveries.Body.String())
	}
	filtered := fixture.request(t, http.MethodGet, "/api/v1/deliveries?channel=push&status=failed", "", session, "", "")
	if filtered.Code != http.StatusOK {
		t.Fatalf("filtered deliveries status = %d", filtered.Code)
	}
	if err := json.Unmarshal(filtered.Body.Bytes(), &deliveryList); err != nil {
		t.Fatalf("decode filtered deliveries: %v", err)
	}
	if len(deliveryList.Deliveries) != 1 || deliveryList.Deliveries[0].ID != "d-3-push" {
		t.Fatalf("filtered deliveries = %+v", deliveryList.Deliveries)
	}
	eventFiltered := fixture.request(t, http.MethodGet, "/api/v1/deliveries?eventId=evt-1", "", session, "", "")
	if eventFiltered.Code != http.StatusOK {
		t.Fatalf("event deliveries status = %d", eventFiltered.Code)
	}
	if err := json.Unmarshal(eventFiltered.Body.Bytes(), &deliveryList); err != nil {
		t.Fatalf("decode event deliveries: %v", err)
	}
	if len(deliveryList.Deliveries) != 2 {
		t.Fatalf("event deliveries = %+v", deliveryList.Deliveries)
	}
	deliveryPage := fixture.request(t, http.MethodGet, "/api/v1/deliveries?limit=2", "", session, "", "")
	if deliveryPage.Code != http.StatusOK {
		t.Fatalf("delivery page status = %d", deliveryPage.Code)
	}
	if err := json.Unmarshal(deliveryPage.Body.Bytes(), &deliveryList); err != nil {
		t.Fatalf("decode delivery page: %v", err)
	}
	if len(deliveryList.Deliveries) != 2 || deliveryList.NextCursor == "" {
		t.Fatalf("delivery page = %+v", deliveryList)
	}
	empty := fixture.request(t, http.MethodGet, "/api/v1/deliveries?eventId=missing", "", session, "", "")
	if empty.Code != http.StatusOK {
		t.Fatalf("empty deliveries status = %d", empty.Code)
	}
	if err := json.Unmarshal(empty.Body.Bytes(), &deliveryList); err != nil {
		t.Fatalf("decode empty deliveries: %v", err)
	}
	if deliveryList.Deliveries == nil || len(deliveryList.Deliveries) != 0 {
		t.Fatalf("empty deliveries = %+v", deliveryList.Deliveries)
	}
}

func TestOverviewEndpoint(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	session, csrf, _ := fixture.login(t, authTestPass)
	base := fixture.clock.now

	emptyRecorder := fixture.request(t, http.MethodGet, "/api/v1/overview", "", session, "", "")
	if emptyRecorder.Code != http.StatusOK {
		t.Fatalf("empty overview status = %d, body=%s", emptyRecorder.Code, emptyRecorder.Body.String())
	}
	var overview overviewPayload
	if err := json.Unmarshal(emptyRecorder.Body.Bytes(), &overview); err != nil {
		t.Fatalf("decode empty overview: %v", err)
	}
	if overview.Monitors.Total != 0 || overview.Providers.Total != 2 ||
		overview.Deliveries.Total != 0 || overview.RecentEvents == nil || overview.RecentDeliveries == nil {
		t.Fatalf("empty overview = %+v", overview)
	}

	active := createMonitor(t, fixture, session, csrf, "Active")
	paused := createMonitor(t, fixture, session, csrf, "Paused")
	expired := createMonitor(t, fixture, session, csrf, "Expired")
	pausedResponse := fixture.request(t, http.MethodPost, "/api/v1/monitors/"+paused.ID+"/pause", "", session, csrf, authOrigin)
	if pausedResponse.Code != http.StatusOK {
		t.Fatalf("pause status = %d", pausedResponse.Code)
	}
	if _, err := fixture.db.Exec(`UPDATE monitors SET expires_at = ?, expired_at = ? WHERE id = ?`,
		httpTime(base.Add(-time.Minute)), httpTime(base.Add(-time.Minute)), expired.ID); err != nil {
		t.Fatalf("expire monitor: %v", err)
	}

	targetID, _ := monitorTarget(t, fixture, active.ID)
	episodeID := insertHTTPEpisode(t, fixture.db, targetID, "overview-journey", "overview-offering", base.Add(-time.Minute))
	insertHTTPEvent(t, fixture.db, httpEventFixture{
		id: "evt-overview", kind: notify.KindAvailability, monitorID: active.ID,
		episodeID: &episodeID, providerID: "fake", occurredAt: base.Add(-time.Minute),
		originName: "Tehran", destinationName: "Mashhad",
	})
	insertHTTPEventDelivery(t, fixture.db, "d-overview-sent", "evt-overview", notify.ChannelTelegram, "telegram",
		store.DeliverySent, 1, nil, &base, &base, "", base)
	insertHTTPEventDelivery(t, fixture.db, "d-overview-pending", "evt-overview", notify.ChannelPush, "push:pending",
		store.DeliveryPending, 0, &base, nil, nil, "", base)
	insertHTTPEvent(t, fixture.db, httpEventFixture{
		id: "evt-overview-health", kind: notify.KindProviderDegraded, providerID: "fake",
		occurredAt: base, errorKind: "timeout", failureCount: intPtr(3),
	})
	insertHTTPEventDelivery(t, fixture.db, "d-overview-failed", "evt-overview-health", notify.ChannelTelegram, "telegram",
		store.DeliveryFailed, 5, nil, &base, nil, "permanent", base)

	oldSuccess := base.Add(-2 * time.Hour)
	episodeHealthID := insertHTTPHealthEpisode(t, fixture.db, "alt", oldSuccess, 3, "network")
	insertHTTPProviderHealth(t, fixture.db, "fake", &base, &oldSuccess, &base, "timeout", nil, 0, nil, base)
	insertHTTPProviderHealth(t, fixture.db, "alt", &base, nil, &base, "network", nil, 3, &episodeHealthID, base)

	recorder := fixture.request(t, http.MethodGet, "/api/v1/overview", "", session, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("overview status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &overview); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
	if !overview.GeneratedAt.Equal(base) {
		t.Fatalf("generatedAt = %v, want %v", overview.GeneratedAt, base)
	}
	if overview.Monitors.Total != 3 || overview.Monitors.Active != 1 ||
		overview.Monitors.Paused != 1 || overview.Monitors.Expired != 1 {
		t.Fatalf("overview monitors = %+v", overview.Monitors)
	}
	if overview.Providers.Total != 2 || overview.Providers.Degraded != 1 || overview.Providers.Stale != 2 {
		t.Fatalf("overview providers = %+v", overview.Providers)
	}
	if overview.Deliveries.Total != 3 || overview.Deliveries.Sent != 1 ||
		overview.Deliveries.Pending != 1 || overview.Deliveries.Failed != 1 {
		t.Fatalf("overview deliveries = %+v", overview.Deliveries)
	}
	if len(overview.RecentEvents) != 1 || overview.RecentEvents[0].ID != "evt-overview" {
		t.Fatalf("recent events = %+v", overview.RecentEvents)
	}
	if len(overview.RecentDeliveries) != 3 {
		t.Fatalf("recent deliveries = %+v", overview.RecentDeliveries)
	}
	if strings.Contains(recorder.Body.String(), "targetKey") || strings.Contains(recorder.Body.String(), "leaseUntil") {
		t.Fatalf("overview leaked internal fields: %s", recorder.Body.String())
	}
	unauthenticated := fixture.request(t, http.MethodGet, "/api/v1/overview", "", "", "", "")
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated overview status = %d", unauthenticated.Code)
	}
	method := fixture.request(t, http.MethodPost, "/api/v1/overview", "", session, csrf, authOrigin)
	if method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST overview status = %d", method.Code)
	}
}
