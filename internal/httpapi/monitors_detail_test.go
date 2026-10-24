package httpapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/auth"
)

type monitorDetailPayload struct {
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
	ExpiredAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Providers       []monitorProviderDetailPayload `json:"providers"`
}

type monitorProviderDetailPayload struct {
	ProviderID            string               `json:"providerId"`
	Active                bool                 `json:"active"`
	ConfigVersion         int64                `json:"configVersion"`
	NextDueAt             *time.Time           `json:"nextDueAt"`
	LastOutcome           string               `json:"lastOutcome"`
	LastErrorKind         string               `json:"lastErrorKind"`
	ConsecutiveFailures   int                  `json:"consecutiveFailures"`
	Observations          []observationPayload `json:"observations"`
	ObservationsTruncated bool                 `json:"observationsTruncated"`
	RecentChecks          []checkRunPayload    `json:"recentChecks"`
	RecentChecksTruncated bool                 `json:"recentChecksTruncated"`
}

type observationPayload struct {
	JourneyKey      string     `json:"journeyKey"`
	OfferingKey     string     `json:"offeringKey"`
	State           string     `json:"state"`
	StateSince      time.Time  `json:"stateSince"`
	LastObservedAt  time.Time  `json:"lastObservedAt"`
	LastDecisiveAt  *time.Time `json:"lastDecisiveAt"`
	LastMissingAt   *time.Time `json:"lastMissingAt"`
	Missing         bool       `json:"missing"`
	LastReason      string     `json:"lastReason"`
	LastCapacity    *int       `json:"lastCapacity"`
	TrainNumber     string     `json:"trainNumber"`
	Operator        string     `json:"operator"`
	OriginName      string     `json:"originName"`
	DestinationName string     `json:"destinationName"`
	DepartureAt     *time.Time `json:"departureAt"`
	ArrivalAt       *time.Time `json:"arrivalAt"`
	WagonName       string     `json:"wagonName"`
	ClassName       string     `json:"className"`
	PriceAmount     *int64     `json:"priceAmount"`
	PriceCurrency   string     `json:"priceCurrency"`
	BookingURL      string     `json:"bookingUrl"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

type checkRunPayload struct {
	ID                 int64      `json:"id"`
	ConfigVersion      int64      `json:"configVersion"`
	StartedAt          time.Time  `json:"startedAt"`
	FinishedAt         *time.Time `json:"finishedAt"`
	Outcome            string     `json:"outcome"`
	ErrorKind          string     `json:"errorKind"`
	HTTPStatus         *int       `json:"httpStatus"`
	DurationMS         *int64     `json:"durationMs"`
	JourneyCount       int        `json:"journeyCount"`
	OfferingCount      int        `json:"offeringCount"`
	ReservableCount    int        `json:"reservableCount"`
	NotReservableCount int        `json:"notReservableCount"`
	UnknownCount       int        `json:"unknownCount"`
	WarningCount       int        `json:"warningCount"`
	CreatedAt          time.Time  `json:"createdAt"`
}

type httpObservationFixture struct {
	configVersion int64
	journeyKey    string
	offeringKey   string
	state         string
	observedAt    time.Time
	missingAt     *time.Time
}

func insertHTTPObservation(t *testing.T, database *sql.DB, targetID int64, fixture httpObservationFixture) {
	t.Helper()
	if fixture.configVersion == 0 {
		fixture.configVersion = 1
	}
	departure := fixture.observedAt.Add(48 * time.Hour)
	price := int64(125000)
	capacity := 4
	bookingURL := "https://mrbilit.com/" + fixture.journeyKey
	reason := ""
	switch fixture.state {
	case "not_reservable":
		reason = "capacity_zero_unavailable"
	case "unknown":
		reason = "conflicting_duplicate_keys"
	}
	if _, err := database.Exec(`INSERT INTO inventory_states
		(monitor_provider_id, config_version, journey_key, offering_key, state,
		 state_since, last_observed_at, last_decisive_at, last_missing_at,
		 last_reason, last_capacity, train_number, operator, origin_name,
		 destination_name, departure_at, arrival_at, wagon_name, class_name,
		 price_amount, price_currency, last_booking_url, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		targetID, fixture.configVersion, fixture.journeyKey, fixture.offeringKey,
		fixture.state, httpTime(fixture.observedAt), httpTime(fixture.observedAt),
		httpTime(fixture.observedAt), httpNullTime(fixture.missingAt),
		httpNullString(reason), capacity,
		"train-"+fixture.journeyKey, "Operator", "Tehran", "Mashhad",
		httpTime(departure), nil, "wagon-"+fixture.journeyKey,
		"class-"+fixture.journeyKey, price, "IRR", bookingURL, httpTime(fixture.observedAt),
	); err != nil {
		t.Fatalf("insert HTTP observation: %v", err)
	}
}

type httpCheckFixture struct {
	configVersion      int64
	startedAt          time.Time
	finishedAt         *time.Time
	outcome            string
	errorKind          string
	httpStatus         *int
	durationMS         *int64
	journeyCount       int
	offeringCount      int
	reservableCount    int
	notReservableCount int
	unknownCount       int
	warningCount       int
}

func insertHTTPCheck(t *testing.T, database *sql.DB, targetID int64, fixture httpCheckFixture) {
	t.Helper()
	if fixture.configVersion == 0 {
		fixture.configVersion = 1
	}
	if _, err := database.Exec(`INSERT INTO check_runs
		(monitor_provider_id, config_version, started_at, finished_at, outcome,
		 error_kind, http_status, duration_ms, journey_count, offering_count,
		 reservable_count, not_reservable_count, unknown_count, warning_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		targetID, fixture.configVersion, httpTime(fixture.startedAt),
		httpNullTime(fixture.finishedAt), fixture.outcome,
		httpNullString(fixture.errorKind), httpNullInt(fixture.httpStatus),
		httpNullInt64(fixture.durationMS), fixture.journeyCount, fixture.offeringCount,
		fixture.reservableCount, fixture.notReservableCount, fixture.unknownCount,
		fixture.warningCount, httpTime(fixture.startedAt),
	); err != nil {
		t.Fatalf("insert HTTP check: %v", err)
	}
}

func httpTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func httpNullTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return httpTime(*value)
}

func httpNullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func httpNullInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func httpNullInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func monitorTarget(t *testing.T, fixture *authFixture, monitorID string) (int64, int64) {
	t.Helper()
	monitor, err := fixture.monitors.Get(context.Background(), monitorID)
	if err != nil {
		t.Fatalf("Get monitor: %v", err)
	}
	if len(monitor.Providers) != 1 {
		t.Fatalf("providers = %d, want 1", len(monitor.Providers))
	}
	return monitor.Providers[0].ID, monitor.Providers[0].ConfigVersion
}

func TestMonitorDetailIncludesObservationsAndRecentChecks(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	session, csrf, login := fixture.login(t, authTestPass)
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d", login.Code)
	}
	created := createMonitor(t, fixture, session, csrf, "Detail monitor")
	targetID, configVersion := monitorTarget(t, fixture, created.ID)
	base := fixture.clock.now

	insertHTTPObservation(t, fixture.db, targetID, httpObservationFixture{
		configVersion: configVersion,
		journeyKey:    "journey-a",
		offeringKey:   "offering-a",
		state:         "reservable",
		observedAt:    base.Add(-2 * time.Minute),
	})
	insertHTTPObservation(t, fixture.db, targetID, httpObservationFixture{
		configVersion: configVersion,
		journeyKey:    "journey-b",
		offeringKey:   "offering-b",
		state:         "not_reservable",
		observedAt:    base.Add(-3 * time.Minute),
	})
	missingAt := base.Add(-time.Minute)
	insertHTTPObservation(t, fixture.db, targetID, httpObservationFixture{
		configVersion: configVersion,
		journeyKey:    "journey-c",
		offeringKey:   "offering-c",
		state:         "unknown",
		observedAt:    base.Add(-4 * time.Minute),
		missingAt:     &missingAt,
	})
	insertHTTPObservation(t, fixture.db, targetID, httpObservationFixture{
		configVersion: configVersion + 1,
		journeyKey:    "journey-old",
		offeringKey:   "offering-old",
		state:         "reservable",
		observedAt:    base.Add(-5 * time.Minute),
	})

	httpOK := 200
	httpTimeout := 504
	durationSuccess := int64(2000)
	durationError := int64(4000)
	successAt := base.Add(-5 * time.Minute)
	successFinished := successAt.Add(2 * time.Second)
	errorAt := base.Add(-2 * time.Minute)
	errorFinished := errorAt.Add(4 * time.Second)
	discardedAt := base.Add(-30 * time.Second)
	discardedFinished := discardedAt.Add(time.Second)
	runningAt := base.Add(-10 * time.Second)
	insertHTTPCheck(t, fixture.db, targetID, httpCheckFixture{
		configVersion: configVersion, startedAt: successAt, finishedAt: &successFinished,
		outcome: "success", httpStatus: &httpOK, durationMS: &durationSuccess,
		journeyCount: 2, offeringCount: 7, reservableCount: 3,
		notReservableCount: 2, unknownCount: 1, warningCount: 1,
	})
	insertHTTPCheck(t, fixture.db, targetID, httpCheckFixture{
		configVersion: configVersion, startedAt: errorAt, finishedAt: &errorFinished,
		outcome: "error", errorKind: "timeout", httpStatus: &httpTimeout,
		durationMS: &durationError,
	})
	insertHTTPCheck(t, fixture.db, targetID, httpCheckFixture{
		configVersion: configVersion, startedAt: discardedAt, finishedAt: &discardedFinished,
		outcome: "discarded", errorKind: "booking_config_changed",
	})
	insertHTTPCheck(t, fixture.db, targetID, httpCheckFixture{
		configVersion: configVersion, startedAt: runningAt, outcome: "running",
	})

	recorder := fixture.request(t, http.MethodGet, "/api/v1/monitors/"+created.ID, "", session, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "targetKey") ||
		strings.Contains(recorder.Body.String(), "leaseUntil") ||
		strings.Contains(recorder.Body.String(), "endpoint") ||
		strings.Contains(recorder.Body.String(), "token") {
		t.Fatalf("detail exposed internal fields: %s", recorder.Body.String())
	}
	var detail monitorDetailPayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.ID != created.ID || detail.Status != "active" || len(detail.Providers) != 1 {
		t.Fatalf("detail = %+v", detail)
	}
	provider := detail.Providers[0]
	if provider.ProviderID == "" || !provider.Active || provider.ConfigVersion != configVersion {
		t.Fatalf("provider detail = %+v", provider)
	}
	if provider.ObservationsTruncated || provider.RecentChecksTruncated {
		t.Fatalf("unexpected truncation flags = %+v", provider)
	}
	if len(provider.Observations) != 3 {
		t.Fatalf("observations = %d, want 3 current-config rows: %+v", len(provider.Observations), provider.Observations)
	}
	wantStates := []string{"reservable", "not_reservable", "unknown"}
	for index, want := range wantStates {
		if provider.Observations[index].State != want {
			t.Fatalf("observation %d state = %q, want %q", index, provider.Observations[index].State, want)
		}
	}
	reservable := provider.Observations[0]
	if reservable.JourneyKey != "journey-a" || reservable.OfferingKey != "offering-a" ||
		!reservable.LastObservedAt.Equal(base.Add(-2*time.Minute)) ||
		reservable.Missing || reservable.LastMissingAt != nil {
		t.Fatalf("reservable observation = %+v", reservable)
	}
	if reservable.BookingURL != "https://mrbilit.com/journey-a" ||
		reservable.PriceAmount == nil || *reservable.PriceAmount != 125000 ||
		reservable.DepartureAt == nil || !reservable.DepartureAt.Equal(base.Add(-2*time.Minute).Add(48*time.Hour)) {
		t.Fatalf("reservable snapshot = %+v", reservable)
	}
	if provider.Observations[1].State != "not_reservable" || provider.Observations[1].LastReason != "capacity_zero_unavailable" {
		t.Fatalf("not-reservable observation = %+v", provider.Observations[1])
	}
	unknown := provider.Observations[2]
	if unknown.State != "unknown" || !unknown.Missing || unknown.LastMissingAt == nil ||
		!unknown.LastMissingAt.Equal(missingAt) {
		t.Fatalf("unknown observation = %+v", unknown)
	}

	if len(provider.RecentChecks) != 4 {
		t.Fatalf("recent checks = %d, want 4: %+v", len(provider.RecentChecks), provider.RecentChecks)
	}
	wantOutcomes := []string{"running", "discarded", "error", "success"}
	for index, want := range wantOutcomes {
		if provider.RecentChecks[index].Outcome != want {
			t.Fatalf("check %d outcome = %q, want %q", index, provider.RecentChecks[index].Outcome, want)
		}
	}
	if provider.RecentChecks[0].FinishedAt != nil {
		t.Fatalf("running check finished = %v", provider.RecentChecks[0].FinishedAt)
	}
	if provider.RecentChecks[1].ErrorKind != "booking_config_changed" {
		t.Fatalf("discarded check = %+v", provider.RecentChecks[1])
	}
	errorCheck := provider.RecentChecks[2]
	if errorCheck.ErrorKind != "timeout" || errorCheck.HTTPStatus == nil || *errorCheck.HTTPStatus != 504 ||
		errorCheck.DurationMS == nil || *errorCheck.DurationMS != 4000 {
		t.Fatalf("error check = %+v", errorCheck)
	}
	successCheck := provider.RecentChecks[3]
	if successCheck.ReservableCount != 3 || successCheck.NotReservableCount != 2 ||
		successCheck.UnknownCount != 1 || successCheck.WarningCount != 1 {
		t.Fatalf("success check = %+v", successCheck)
	}

	list := fixture.request(t, http.MethodGet, "/api/v1/monitors", "", session, "", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d", list.Code)
	}
	if strings.Contains(list.Body.String(), "observations") || strings.Contains(list.Body.String(), "recentChecks") {
		t.Fatalf("list response grew detail diagnostics: %s", list.Body.String())
	}
}

func TestMonitorDetailEmptyArraysMissingAndAuth(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	session, csrf, _ := fixture.login(t, authTestPass)
	created := createMonitor(t, fixture, session, csrf, "Empty detail")

	recorder := fixture.request(t, http.MethodGet, "/api/v1/monitors/"+created.ID, "", session, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var detail monitorDetailPayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if len(detail.Providers) != 1 {
		t.Fatalf("providers = %d, want 1", len(detail.Providers))
	}
	provider := detail.Providers[0]
	if provider.Observations == nil || len(provider.Observations) != 0 ||
		provider.RecentChecks == nil || len(provider.RecentChecks) != 0 {
		t.Fatalf("empty diagnostics = %+v", provider)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"observations":[]`) || !strings.Contains(body, `"recentChecks":[]`) {
		t.Fatalf("empty arrays not explicit: %s", body)
	}
	if !strings.Contains(body, `"observationsTruncated":false`) || !strings.Contains(body, `"recentChecksTruncated":false`) {
		t.Fatalf("truncation flags missing: %s", body)
	}

	missing := fixture.request(t, http.MethodGet, "/api/v1/monitors/missing", "", session, "", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing monitor status = %d", missing.Code)
	}
	unauthenticated := fixture.request(t, http.MethodGet, "/api/v1/monitors/"+created.ID, "", "", "", "")
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated detail status = %d", unauthenticated.Code)
	}
}

func TestMonitorDetailPausedExpiredAndStaleData(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	session, csrf, _ := fixture.login(t, authTestPass)
	created := createMonitor(t, fixture, session, csrf, "Lifecycle detail")
	targetID, configVersion := monitorTarget(t, fixture, created.ID)
	base := fixture.clock.now
	oldObserved := base.Add(-2 * time.Hour)
	oldFinished := oldObserved.Add(time.Second)
	insertHTTPObservation(t, fixture.db, targetID, httpObservationFixture{
		configVersion: configVersion, journeyKey: "journey-stale", offeringKey: "offering-stale",
		state: "reservable", observedAt: oldObserved,
	})
	insertHTTPCheck(t, fixture.db, targetID, httpCheckFixture{
		configVersion: configVersion, startedAt: oldObserved, finishedAt: &oldFinished,
		outcome: "success",
	})

	paused := fixture.request(t, http.MethodPost, "/api/v1/monitors/"+created.ID+"/pause", "", session, csrf, authOrigin)
	if paused.Code != http.StatusOK {
		t.Fatalf("pause status = %d, body=%s", paused.Code, paused.Body.String())
	}
	recorder := fixture.request(t, http.MethodGet, "/api/v1/monitors/"+created.ID, "", session, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("paused detail status = %d", recorder.Code)
	}
	var pausedDetail monitorDetailPayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &pausedDetail); err != nil {
		t.Fatalf("decode paused detail: %v", err)
	}
	if pausedDetail.Status != "paused" || pausedDetail.Enabled {
		t.Fatalf("paused detail = %+v", pausedDetail)
	}
	if len(pausedDetail.Providers[0].Observations) != 1 ||
		!pausedDetail.Providers[0].Observations[0].LastObservedAt.Equal(oldObserved) {
		t.Fatalf("paused observation was fabricated or lost: %+v", pausedDetail.Providers[0].Observations)
	}
	if len(pausedDetail.Providers[0].RecentChecks) != 1 ||
		!pausedDetail.Providers[0].RecentChecks[0].StartedAt.Equal(oldObserved) {
		t.Fatalf("paused checks = %+v", pausedDetail.Providers[0].RecentChecks)
	}

	resumed := fixture.request(t, http.MethodPost, "/api/v1/monitors/"+created.ID+"/resume", "", session, csrf, authOrigin)
	if resumed.Code != http.StatusOK {
		t.Fatalf("resume status = %d", resumed.Code)
	}
	if _, err := fixture.db.Exec(`UPDATE monitors SET expires_at = ?, expired_at = ? WHERE id = ?`,
		httpTime(base.Add(-time.Minute)), httpTime(base.Add(-time.Minute)), created.ID); err != nil {
		t.Fatalf("expire monitor: %v", err)
	}
	expired := fixture.request(t, http.MethodGet, "/api/v1/monitors/"+created.ID, "", session, "", "")
	if expired.Code != http.StatusOK {
		t.Fatalf("expired detail status = %d", expired.Code)
	}
	var expiredDetail monitorDetailPayload
	if err := json.Unmarshal(expired.Body.Bytes(), &expiredDetail); err != nil {
		t.Fatalf("decode expired detail: %v", err)
	}
	if expiredDetail.Status != "expired" || expiredDetail.ExpiredAt == nil {
		t.Fatalf("expired detail = %+v", expiredDetail)
	}
	if len(expiredDetail.Providers[0].Observations) != 1 ||
		!expiredDetail.Providers[0].Observations[0].LastObservedAt.Equal(oldObserved) {
		t.Fatalf("expired observations = %+v", expiredDetail.Providers[0].Observations)
	}
}

func TestMonitorDetailProviderErrorDoesNotChangeAvailability(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	session, csrf, _ := fixture.login(t, authTestPass)
	created := createMonitor(t, fixture, session, csrf, "Failure detail")
	targetID, configVersion := monitorTarget(t, fixture, created.ID)
	base := fixture.clock.now
	observedAt := base.Add(-5 * time.Minute)
	insertHTTPObservation(t, fixture.db, targetID, httpObservationFixture{
		configVersion: configVersion, journeyKey: "journey-failure", offeringKey: "offering-failure",
		state: "reservable", observedAt: observedAt,
	})
	errorAt := base.Add(-time.Minute)
	errorFinished := errorAt.Add(2 * time.Second)
	httpStatus := 504
	duration := int64(2000)
	insertHTTPCheck(t, fixture.db, targetID, httpCheckFixture{
		configVersion: configVersion, startedAt: errorAt, finishedAt: &errorFinished,
		outcome: "error", errorKind: "timeout", httpStatus: &httpStatus, durationMS: &duration,
	})
	if _, err := fixture.db.Exec(`UPDATE monitor_providers
		SET last_outcome = 'error', last_error_kind = 'timeout', last_error_at = ?,
		    last_attempt_at = ?, consecutive_failures = 1
		WHERE id = ?`, httpTime(errorFinished), httpTime(errorFinished), targetID); err != nil {
		t.Fatalf("update target diagnostics: %v", err)
	}

	recorder := fixture.request(t, http.MethodGet, "/api/v1/monitors/"+created.ID, "", session, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var detail monitorDetailPayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	provider := detail.Providers[0]
	if len(provider.Observations) != 1 || provider.Observations[0].State != "reservable" {
		t.Fatalf("provider error changed availability: %+v", provider.Observations)
	}
	if len(provider.RecentChecks) != 1 || provider.RecentChecks[0].Outcome != "error" ||
		provider.RecentChecks[0].ErrorKind != "timeout" {
		t.Fatalf("provider error diagnostics = %+v", provider.RecentChecks)
	}
	if provider.LastOutcome != "error" || provider.LastErrorKind != "timeout" || provider.ConsecutiveFailures != 1 {
		t.Fatalf("target diagnostics = %+v", provider)
	}
}
