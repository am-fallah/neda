package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/store"
)

type diagnosticStateFixture struct {
	configVersion int64
	journeyKey    string
	offeringKey   string
	state         string
	observedAt    time.Time
	missingAt     *time.Time
	reason        string
	capacity      *int
	trainNumber   string
	operator      string
	originName    string
	destination   string
	departureAt   *time.Time
	arrivalAt     *time.Time
	wagonName     string
	className     string
	priceAmount   *int64
	priceCurrency string
	bookingURL    string
	updatedAt     time.Time
}

func insertDiagnosticState(t *testing.T, database *sql.DB, targetID int64, fixture diagnosticStateFixture) {
	t.Helper()
	if fixture.configVersion == 0 {
		fixture.configVersion = 1
	}
	if fixture.updatedAt.IsZero() {
		fixture.updatedAt = fixture.observedAt
	}
	if _, err := database.Exec(`INSERT INTO inventory_states
		(monitor_provider_id, config_version, journey_key, offering_key, state,
		 state_since, last_observed_at, last_decisive_at, last_missing_at,
		 last_reason, last_capacity, train_number, operator, origin_name,
		 destination_name, departure_at, arrival_at, wagon_name, class_name,
		 price_amount, price_currency, last_booking_url, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		targetID, fixture.configVersion, fixture.journeyKey, fixture.offeringKey,
		fixture.state, formatTestTime(fixture.observedAt), formatTestTime(fixture.observedAt),
		formatTestTime(fixture.observedAt), readNullTime(fixture.missingAt),
		readNullString(fixture.reason), readNullInt(fixture.capacity),
		readNullString(fixture.trainNumber), readNullString(fixture.operator),
		readNullString(fixture.originName), readNullString(fixture.destination),
		readNullTime(fixture.departureAt), readNullTime(fixture.arrivalAt),
		readNullString(fixture.wagonName), readNullString(fixture.className),
		readNullInt64(fixture.priceAmount), readNullString(fixture.priceCurrency),
		readNullString(fixture.bookingURL), formatTestTime(fixture.updatedAt),
	); err != nil {
		t.Fatalf("insert diagnostic state: %v", err)
	}
}

type diagnosticCheckFixture struct {
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

func insertDiagnosticCheck(t *testing.T, database *sql.DB, targetID int64, fixture diagnosticCheckFixture) int64 {
	t.Helper()
	if fixture.configVersion == 0 {
		fixture.configVersion = 1
	}
	result, err := database.Exec(`INSERT INTO check_runs
		(monitor_provider_id, config_version, started_at, finished_at, outcome,
		 error_kind, http_status, duration_ms, journey_count, offering_count,
		 reservable_count, not_reservable_count, unknown_count, warning_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		targetID, fixture.configVersion, formatTestTime(fixture.startedAt),
		readNullTime(fixture.finishedAt), fixture.outcome,
		readNullString(fixture.errorKind), readNullInt(fixture.httpStatus),
		readNullInt64(fixture.durationMS), fixture.journeyCount,
		fixture.offeringCount, fixture.reservableCount, fixture.notReservableCount,
		fixture.unknownCount, fixture.warningCount, formatTestTime(fixture.startedAt),
	)
	if err != nil {
		t.Fatalf("insert diagnostic check: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("diagnostic check id: %v", err)
	}
	return id
}

func diagnosticsMonitor(t *testing.T) (*store.Store, *sql.DB, int64) {
	t.Helper()
	st, database := newTestStore(t)
	monitor := createScheduleMonitor(t, st, "diagnostics", true, testNow.Add(24*time.Hour), nil,
		scheduleTargetSpec{providerID: "p-diagnostics", active: true})
	return st, database, targetID(t, monitor, "p-diagnostics")
}

func TestListInventoryObservationsCurrentConfigOrderingAndMissing(t *testing.T) {
	st, database, targetID := diagnosticsMonitor(t)
	ctx := context.Background()
	base := testNow
	capacity := 4
	price := int64(125000)
	departure := base.Add(48 * time.Hour)
	missingAt := base.Add(-time.Minute)

	insertDiagnosticState(t, database, targetID, diagnosticStateFixture{
		journeyKey: "journey-1", offeringKey: "offering-1", state: "reservable",
		observedAt: base.Add(-time.Minute), reason: "", capacity: &capacity,
		trainNumber: "T1", operator: "Operator", originName: "Tehran",
		destination: "Mashhad", departureAt: &departure, wagonName: "W1",
		className: "C1", priceAmount: &price, priceCurrency: "IRR",
		bookingURL: "https://mrbilit.com/booking-1",
	})
	insertDiagnosticState(t, database, targetID, diagnosticStateFixture{
		journeyKey: "journey-2", offeringKey: "offering-2", state: "not_reservable",
		observedAt: base.Add(-2 * time.Minute), reason: "capacity_zero_unavailable",
	})
	insertDiagnosticState(t, database, targetID, diagnosticStateFixture{
		journeyKey: "journey-3", offeringKey: "offering-3", state: "unknown",
		observedAt: base.Add(-3 * time.Minute), missingAt: &missingAt,
		reason: "conflicting_duplicate_keys",
	})
	insertDiagnosticState(t, database, targetID, diagnosticStateFixture{
		configVersion: 2, journeyKey: "journey-old", offeringKey: "offering-old",
		state: "reservable", observedAt: base.Add(-time.Hour),
	})

	page, err := st.ListInventoryObservations(ctx, targetID, 1, 10)
	if err != nil {
		t.Fatalf("ListInventoryObservations: %v", err)
	}
	if page.Truncated || len(page.Observations) != 3 {
		t.Fatalf("page = %+v, want 3 untruncated observations", page)
	}
	wantOrder := []string{"reservable", "not_reservable", "unknown"}
	for index, want := range wantOrder {
		if string(page.Observations[index].State) != want {
			t.Fatalf("observation %d state = %q, want %q", index, page.Observations[index].State, want)
		}
	}
	reservable := page.Observations[0]
	if reservable.JourneyKey != "journey-1" || reservable.OfferingKey != "offering-1" {
		t.Fatalf("reservable identity = %+v", reservable)
	}
	if !reservable.StateSince.Equal(base.Add(-time.Minute)) || !reservable.LastObservedAt.Equal(base.Add(-time.Minute)) ||
		!reservable.UpdatedAt.Equal(base.Add(-time.Minute)) {
		t.Fatalf("reservable timestamps = %+v", reservable)
	}
	if reservable.LastDecisiveAt == nil || !reservable.LastDecisiveAt.Equal(base.Add(-time.Minute)) {
		t.Fatalf("last decisive = %v", reservable.LastDecisiveAt)
	}
	if reservable.Missing || reservable.LastMissingAt != nil {
		t.Fatalf("present observation marked missing: %+v", reservable)
	}
	if reservable.LastCapacity == nil || *reservable.LastCapacity != 4 ||
		reservable.PriceAmount == nil || *reservable.PriceAmount != 125000 ||
		reservable.BookingURL != "https://mrbilit.com/booking-1" {
		t.Fatalf("reservable snapshot = %+v", reservable)
	}
	if reservable.DepartureAt == nil || !reservable.DepartureAt.Equal(departure) {
		t.Fatalf("departure = %v", reservable.DepartureAt)
	}
	if page.Observations[1].LastReason != "capacity_zero_unavailable" {
		t.Fatalf("not-reservable reason = %q", page.Observations[1].LastReason)
	}
	unknown := page.Observations[2]
	if unknown.State != "unknown" || !unknown.Missing || unknown.LastMissingAt == nil ||
		!unknown.LastMissingAt.Equal(missingAt) || unknown.LastReason != "conflicting_duplicate_keys" {
		t.Fatalf("unknown/missing observation = %+v", unknown)
	}

	limited, err := st.ListInventoryObservations(ctx, targetID, 1, 2)
	if err != nil {
		t.Fatalf("ListInventoryObservations limited: %v", err)
	}
	if !limited.Truncated || len(limited.Observations) != 2 {
		t.Fatalf("limited page = %+v, want 2 truncated observations", limited)
	}
	old, err := st.ListInventoryObservations(ctx, targetID, 2, 10)
	if err != nil {
		t.Fatalf("ListInventoryObservations old config: %v", err)
	}
	if len(old.Observations) != 1 || old.Observations[0].JourneyKey != "journey-old" {
		t.Fatalf("old config observations = %+v", old.Observations)
	}
	empty, err := st.ListInventoryObservations(ctx, targetID, 99, 10)
	if err != nil {
		t.Fatalf("ListInventoryObservations empty config: %v", err)
	}
	if empty.Observations == nil || len(empty.Observations) != 0 || empty.Truncated {
		t.Fatalf("empty observation page = %+v", empty)
	}

	if _, err := st.ListInventoryObservations(ctx, 0, 1, 10); err == nil {
		t.Fatal("ListInventoryObservations accepted zero target")
	}
	if _, err := st.ListInventoryObservations(ctx, targetID, 0, 10); err == nil {
		t.Fatal("ListInventoryObservations accepted zero config version")
	}
	for _, limit := range []int{0, -1, domain.MaxObservationLimit + 1} {
		if _, err := st.ListInventoryObservations(ctx, targetID, 1, limit); err == nil {
			t.Fatalf("ListInventoryObservations accepted limit %d", limit)
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := st.ListInventoryObservations(canceled, targetID, 1, 10); err == nil {
		t.Fatal("ListInventoryObservations with canceled context succeeded")
	}
}

func TestListRecentCheckRunsOrderingBoundsAndDiagnostics(t *testing.T) {
	st, database, targetID := diagnosticsMonitor(t)
	ctx := context.Background()
	base := testNow
	httpOK := 200
	httpTimeout := 504
	durationSuccess := int64(2000)
	durationError := int64(4000)

	successAt := base.Add(-3 * time.Minute)
	successFinished := successAt.Add(2 * time.Second)
	errorAt := base.Add(-2 * time.Minute)
	errorFinished := errorAt.Add(4 * time.Second)
	runningAt := base.Add(-time.Minute)
	discardedAt := base.Add(-4 * time.Minute)
	discardedFinished := discardedAt.Add(time.Second)

	insertDiagnosticCheck(t, database, targetID, diagnosticCheckFixture{
		startedAt: successAt, finishedAt: &successFinished, outcome: "success",
		httpStatus: &httpOK, durationMS: &durationSuccess,
		journeyCount: 2, offeringCount: 7, reservableCount: 3,
		notReservableCount: 2, unknownCount: 1, warningCount: 1,
	})
	errorID := insertDiagnosticCheck(t, database, targetID, diagnosticCheckFixture{
		startedAt: errorAt, finishedAt: &errorFinished, outcome: "error",
		errorKind: "timeout", httpStatus: &httpTimeout, durationMS: &durationError,
	})
	insertDiagnosticCheck(t, database, targetID, diagnosticCheckFixture{
		startedAt: runningAt, outcome: "running",
	})
	insertDiagnosticCheck(t, database, targetID, diagnosticCheckFixture{
		startedAt: discardedAt, finishedAt: &discardedFinished, outcome: "discarded",
		errorKind: "booking_config_changed",
	})

	page, err := st.ListRecentCheckRuns(ctx, targetID, 10)
	if err != nil {
		t.Fatalf("ListRecentCheckRuns: %v", err)
	}
	if page.Truncated || len(page.Checks) != 4 {
		t.Fatalf("check page = %+v, want 4 untruncated checks", page)
	}
	wantOrder := []domain.CheckRunOutcome{
		domain.CheckRunRunning, domain.CheckRunError, domain.CheckRunSuccess, domain.CheckRunDiscarded,
	}
	for index, want := range wantOrder {
		if page.Checks[index].Outcome != want {
			t.Fatalf("check %d outcome = %q, want %q", index, page.Checks[index].Outcome, want)
		}
	}
	running := page.Checks[0]
	if running.FinishedAt != nil || running.ConfigVersion != 1 || !running.StartedAt.Equal(runningAt) {
		t.Fatalf("running check = %+v", running)
	}
	errorCheck := page.Checks[1]
	if errorCheck.ID != errorID || errorCheck.ErrorKind != "timeout" || errorCheck.HTTPStatus == nil ||
		*errorCheck.HTTPStatus != 504 || errorCheck.DurationMS == nil || *errorCheck.DurationMS != 4000 {
		t.Fatalf("error check = %+v", errorCheck)
	}
	if errorCheck.FinishedAt == nil || !errorCheck.FinishedAt.Equal(errorFinished) {
		t.Fatalf("error finished = %v", errorCheck.FinishedAt)
	}
	success := page.Checks[2]
	if success.JourneyCount != 2 || success.OfferingCount != 7 || success.ReservableCount != 3 ||
		success.NotReservableCount != 2 || success.UnknownCount != 1 || success.WarningCount != 1 {
		t.Fatalf("success counts = %+v", success)
	}
	if page.Checks[3].Outcome != domain.CheckRunDiscarded || page.Checks[3].ErrorKind != "booking_config_changed" {
		t.Fatalf("discarded check = %+v", page.Checks[3])
	}

	limited, err := st.ListRecentCheckRuns(ctx, targetID, 2)
	if err != nil {
		t.Fatalf("ListRecentCheckRuns limited: %v", err)
	}
	if !limited.Truncated || len(limited.Checks) != 2 {
		t.Fatalf("limited check page = %+v, want 2 truncated checks", limited)
	}
	empty, err := st.ListRecentCheckRuns(ctx, targetID+1, 10)
	if err != nil {
		t.Fatalf("ListRecentCheckRuns empty: %v", err)
	}
	if empty.Checks == nil || len(empty.Checks) != 0 || empty.Truncated {
		t.Fatalf("empty check page = %+v", empty)
	}

	if _, err := st.ListRecentCheckRuns(ctx, 0, 10); err == nil {
		t.Fatal("ListRecentCheckRuns accepted zero target")
	}
	for _, limit := range []int{0, -1, domain.MaxRecentCheckLimit + 1} {
		if _, err := st.ListRecentCheckRuns(ctx, targetID, limit); err == nil {
			t.Fatalf("ListRecentCheckRuns accepted limit %d", limit)
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := st.ListRecentCheckRuns(canceled, targetID, 10); err == nil {
		t.Fatal("ListRecentCheckRuns with canceled context succeeded")
	}
}
