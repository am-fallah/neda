package availability_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/availability"
	"github.com/am-fallah/neda/internal/provider"
)

func unavailableInput(monitorID string, targetID int64, observedAt time.Time, state provider.State, grace, gap time.Duration) availability.Input {
	input := inputFor(monitorID, targetID, 1, observedAt, resultWith(obsSpec{
		journeyKey:  "journey-1",
		offeringKey: "offering-1",
		state:       state,
	}))
	input.UnavailableGrace = grace
	input.UnavailableGap = gap
	return input
}

func applyUnavailable(t *testing.T, database *sql.DB, input availability.Input) availability.Result {
	t.Helper()
	return applyCommit(t, database, input)
}

func unavailableColumns(t *testing.T, database *sql.DB, targetID int64) (since, last, notified *time.Time) {
	t.Helper()
	var (
		sinceValue, lastValue, notifiedValue sql.NullString
	)
	if err := database.QueryRow(`SELECT unavailable_since, last_not_reservable_at, unavailable_notified_at
		FROM inventory_states WHERE monitor_provider_id = ?`, targetID).
		Scan(&sinceValue, &lastValue, &notifiedValue); err != nil {
		t.Fatalf("load unavailable columns: %v", err)
	}
	parse := func(value sql.NullString) *time.Time {
		if !value.Valid {
			return nil
		}
		parsed, err := time.Parse(time.RFC3339Nano, value.String)
		if err != nil {
			t.Fatalf("parse unavailable timestamp %q: %v", value.String, err)
		}
		return &parsed
	}
	return parse(sinceValue), parse(lastValue), parse(notifiedValue)
}

func countEvents(t *testing.T, database *sql.DB) int {
	t.Helper()
	return mustQueryInt(t, database, `SELECT COUNT(*) FROM events`)
}

func TestGapToleranceTracksPollInterval(t *testing.T) {
	if got := availability.GapTolerance(0); got != 15*time.Minute {
		t.Fatalf("GapTolerance(0) = %v, want 15m", got)
	}
	if got := availability.GapTolerance(5 * time.Minute); got != 15*time.Minute {
		t.Fatalf("GapTolerance(5m) = %v, want 15m", got)
	}
	if got := availability.GapTolerance(30 * time.Minute); got != time.Hour {
		t.Fatalf("GapTolerance(30m) = %v, want 1h", got)
	}
	if got := availability.GapTolerance(time.Hour); got != 2*time.Hour {
		t.Fatalf("GapTolerance(1h) = %v, want 2h", got)
	}
}

func TestShortUnavailablePeriodDoesNotNotify(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "short-unavailable", 1)
	start := testNow
	grace := time.Hour
	gap := 15 * time.Minute

	for elapsed := time.Duration(0); elapsed < grace; elapsed += 10 * time.Minute {
		applyUnavailable(t, database, unavailableInput(
			"short-unavailable", targetID, start.Add(elapsed), provider.StateNotReservable, grace, gap,
		))
	}

	if events := countEvents(t, database); events != 0 {
		t.Fatalf("events = %d, want 0 before the grace period", events)
	}
	since, _, notified := unavailableColumns(t, database, targetID)
	if since == nil || !since.Equal(start) || notified != nil {
		t.Fatalf("streak = since %v notified %v, want since %v and no notice", since, notified, start)
	}
}

func TestContinuousUnavailableProducesOneSilentNotice(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "continuous-unavailable", 1)
	start := testNow
	grace := time.Hour
	gap := 15 * time.Minute

	var last availability.Result
	for elapsed := time.Duration(0); elapsed <= grace; elapsed += 10 * time.Minute {
		last = applyUnavailable(t, database, unavailableInput(
			"continuous-unavailable", targetID, start.Add(elapsed), provider.StateNotReservable, grace, gap,
		))
	}
	if len(last.EventsCreated) != 1 || last.UnavailableNotices != 1 {
		t.Fatalf("result = %+v, want one unavailable event", last)
	}
	if events := countEvents(t, database); events != 1 {
		t.Fatalf("events = %d, want 1", events)
	}

	var (
		kind, dedupeKey, monitorID string
		silent                     int
		episodeID                  sql.NullInt64
	)
	if err := database.QueryRow(`SELECT kind, dedupe_key, monitor_id, silent, episode_id FROM events`).
		Scan(&kind, &dedupeKey, &monitorID, &silent, &episodeID); err != nil {
		t.Fatalf("load unavailable event: %v", err)
	}
	if kind != "unavailable" || silent != 1 || monitorID != "continuous-unavailable" || episodeID.Valid {
		t.Fatalf("event = kind %q silent %d monitor %q episode %v", kind, silent, monitorID, episodeID)
	}
	if dedupeKey == "" {
		t.Fatal("unavailable event has no dedupe key")
	}
	deliveries := mustQueryInt(t, database, `SELECT COUNT(*) FROM notification_deliveries WHERE event_id = '`+
		last.EventsCreated[0]+`'`)
	if deliveries != 2 {
		t.Fatalf("deliveries = %d, want telegram+push", deliveries)
	}

	// Re-applying after the notice cannot create a second notice for the same
	// continuous episode.
	again := applyUnavailable(t, database, unavailableInput(
		"continuous-unavailable", targetID, start.Add(grace+10*time.Minute), provider.StateNotReservable, grace, gap,
	))
	if len(again.EventsCreated) != 0 || again.UnavailableNotices != 0 {
		t.Fatalf("duplicate notice result = %+v", again)
	}
	if events := countEvents(t, database); events != 1 {
		t.Fatalf("events after duplicate = %d, want 1", events)
	}
}

func TestLongGapResetsUnavailableStreak(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "long-gap", 1)
	start := testNow
	grace := time.Hour
	gap := 15 * time.Minute

	applyUnavailable(t, database, unavailableInput("long-gap", targetID, start, provider.StateNotReservable, grace, gap))
	resumed := start.Add(2 * time.Hour)
	applyUnavailable(t, database, unavailableInput("long-gap", targetID, resumed, provider.StateNotReservable, grace, gap))

	if events := countEvents(t, database); events != 0 {
		t.Fatalf("events = %d, want 0 after a gap longer than tolerance", events)
	}
	since, _, _ := unavailableColumns(t, database, targetID)
	if since == nil || !since.Equal(resumed) {
		t.Fatalf("streak since = %v, want reset to %v", since, resumed)
	}
}

func TestUnknownAndMissingResultsDoNotAdvanceStreak(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "unknown-gap", 1)
	start := testNow
	grace := time.Hour
	gap := 2 * time.Hour

	applyUnavailable(t, database, unavailableInput("unknown-gap", targetID, start, provider.StateNotReservable, grace, gap))
	applyUnavailable(t, database, unavailableInput("unknown-gap", targetID, start.Add(30*time.Minute), provider.StateUnknown, grace, gap))
	// A successful empty result is also inconclusive: it does not advance the
	// continuous-unavailable clock.
	empty := inputFor("unknown-gap", targetID, 1, start.Add(50*time.Minute), provider.Result{})
	empty.UnavailableGrace = grace
	empty.UnavailableGap = gap
	applyCommit(t, database, empty)

	notifiedAt := start.Add(time.Hour)
	result := applyUnavailable(t, database, unavailableInput("unknown-gap", targetID, notifiedAt, provider.StateNotReservable, grace, gap))
	if len(result.EventsCreated) != 1 || result.UnavailableNotices != 1 {
		t.Fatalf("result = %+v, want notice at the original streak start", result)
	}
	since, _, notified := unavailableColumns(t, database, targetID)
	if since == nil || !since.Equal(start) {
		t.Fatalf("streak since = %v, want original %v", since, start)
	}
	if notified == nil || !notified.Equal(notifiedAt) {
		t.Fatalf("notified = %v, want %v", notified, notifiedAt)
	}
}

func TestPersistedStreakSurvivesRestart(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "restart-streak", 1)
	start := testNow
	grace := time.Hour
	gap := 2 * time.Hour

	applyUnavailable(t, database, unavailableInput("restart-streak", targetID, start, provider.StateNotReservable, grace, gap))
	// A new Apply call uses only persisted state; no in-memory timer exists.
	result := applyUnavailable(t, database, unavailableInput(
		"restart-streak", targetID, start.Add(grace), provider.StateNotReservable, grace, gap,
	))
	if len(result.EventsCreated) != 1 {
		t.Fatalf("after restart result = %+v, want persisted notice", result)
	}
}

func TestAvailabilityReturnClosesStreakAndNewEpisodeCanNotify(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "episode-reset", 1)
	start := testNow
	grace := time.Hour
	gap := 2 * time.Hour

	applyUnavailable(t, database, unavailableInput("episode-reset", targetID, start, provider.StateNotReservable, grace, gap))
	applyUnavailable(t, database, unavailableInput("episode-reset", targetID, start.Add(grace), provider.StateNotReservable, grace, gap))
	if events := countEvents(t, database); events != 1 {
		t.Fatalf("events after first notice = %d, want 1", events)
	}

	availableAt := start.Add(grace + 10*time.Minute)
	available := applyUnavailable(t, database, unavailableInput("episode-reset", targetID, availableAt, provider.StateReservable, grace, gap))
	if len(available.EventsCreated) != 1 {
		t.Fatalf("availability result = %+v, want availability event", available)
	}
	since, last, notified := unavailableColumns(t, database, targetID)
	if since != nil || last != nil || notified != nil {
		t.Fatalf("availability return left streak columns: since=%v last=%v notified=%v", since, last, notified)
	}

	secondStart := availableAt.Add(10 * time.Minute)
	applyUnavailable(t, database, unavailableInput("episode-reset", targetID, secondStart, provider.StateNotReservable, grace, gap))
	second := applyUnavailable(t, database, unavailableInput("episode-reset", targetID, secondStart.Add(grace), provider.StateNotReservable, grace, gap))
	if len(second.EventsCreated) != 1 {
		t.Fatalf("second episode result = %+v, want a new unavailable notice", second)
	}
	if events := countEvents(t, database); events != 3 {
		t.Fatalf("events = %d, want availability + two unavailable notices", events)
	}
}

func TestPreexistingUnavailableStateStartsNewEvidenceWindow(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "preexisting", 1)
	start := testNow
	grace := time.Hour
	gap := 2 * time.Hour

	now := start.Add(-10 * time.Hour).Format(time.RFC3339Nano)
	if _, err := database.Exec(`INSERT INTO inventory_states
		(monitor_provider_id, config_version, journey_key, offering_key, state, state_since,
		 last_observed_at, updated_at)
		VALUES (?, 1, 'journey-1', 'offering-1', 'not_reservable', ?, ?, ?)`,
		targetID, now, now, now); err != nil {
		t.Fatalf("insert pre-existing state: %v", err)
	}

	first := applyUnavailable(t, database, unavailableInput("preexisting", targetID, start, provider.StateNotReservable, grace, gap))
	if len(first.EventsCreated) != 0 {
		t.Fatalf("pre-existing unavailable produced an immediate notice: %+v", first)
	}
	since, _, notified := unavailableColumns(t, database, targetID)
	if since == nil || !since.Equal(start) || notified != nil {
		t.Fatalf("new evidence window = since %v notified %v, want %v/nil", since, notified, start)
	}
	second := applyUnavailable(t, database, unavailableInput("preexisting", targetID, start.Add(grace), provider.StateNotReservable, grace, gap))
	if len(second.EventsCreated) != 1 {
		t.Fatalf("post-upgrade continuous evidence result = %+v, want notice after grace", second)
	}
}

func TestUnavailableNoticeIsNotDuplicatedByRepeatedApply(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "dedupe-unavailable", 1)
	start := testNow
	grace := time.Hour
	gap := 2 * time.Hour

	applyUnavailable(t, database, unavailableInput("dedupe-unavailable", targetID, start, provider.StateNotReservable, grace, gap))
	at := start.Add(grace)
	first := applyUnavailable(t, database, unavailableInput("dedupe-unavailable", targetID, at, provider.StateNotReservable, grace, gap))
	second := applyUnavailable(t, database, unavailableInput("dedupe-unavailable", targetID, at, provider.StateNotReservable, grace, gap))
	if len(first.EventsCreated) != 1 || len(second.EventsCreated) != 0 {
		t.Fatalf("first=%+v second=%+v, want one event", first, second)
	}
}
