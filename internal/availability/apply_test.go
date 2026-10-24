package availability_test

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/availability"
	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/google/uuid"
)

var testNow = time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "availability-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	applied, err := db.Migrate(ctx, database.DB, migrations.FS)
	if err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	if err := database.Ready(ctx, applied); err != nil {
		t.Fatalf("database.Ready: %v", err)
	}
	return database.DB
}

func begin(t *testing.T, database *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	return tx
}

func commit(t *testing.T, tx *sql.Tx) {
	t.Helper()
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

func rollback(t *testing.T, tx *sql.Tx) {
	t.Helper()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
}

type execer interface {
	Exec(string, ...any) (sql.Result, error)
}

type queryer interface {
	QueryRow(string, ...any) *sql.Row
}

func mustExec(t *testing.T, database execer, query string, args ...any) sql.Result {
	t.Helper()
	result, err := database.Exec(query, args...)
	if err != nil {
		t.Fatalf("exec failed: %v\nquery: %s", err, query)
	}
	return result
}

func mustQueryInt(t *testing.T, database queryer, query string, args ...any) int {
	t.Helper()
	var value int
	if err := database.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatalf("query failed: %v\nquery: %s", err, query)
	}
	return value
}

func insertMonitorTarget(t *testing.T, database *sql.DB, monitorID string, configVersion int64) int64 {
	t.Helper()
	now := testNow.Format(time.RFC3339Nano)
	expires := testNow.Add(24 * time.Hour).Format(time.RFC3339Nano)
	mustExec(t, database, `INSERT INTO monitors
		(id, label, origin_name, destination_name, departure_date, adults, children, infants,
		 enabled, expires_at, created_at, updated_at)
		VALUES (?, ?, 'Tehran', 'Mashhad', '2026-12-15', 1, 0, 0, 1, ?, ?, ?)`,
		monitorID, "Availability "+monitorID, expires, now, now)
	target := mustExec(t, database, `INSERT INTO monitor_providers
		(monitor_id, provider_id, active, origin_id, destination_id, config_version, created_at, updated_at)
		VALUES (?, 'fake', 1, 'origin-fake', 'destination-fake', ?, ?, ?)`,
		monitorID, configVersion, now, now)
	targetID, err := target.LastInsertId()
	if err != nil {
		t.Fatalf("target LastInsertId: %v", err)
	}
	return targetID
}

func intPtr(value int) *int              { return &value }
func timePtr(value time.Time) *time.Time { return &value }

type obsSpec struct {
	journeyKey  string
	offeringKey string
	state       provider.State
	capacity    *int
	reason      string
}

func resultWith(specs ...obsSpec) provider.Result {
	order := make([]string, 0)
	byJourney := make(map[string][]obsSpec)
	for _, spec := range specs {
		if _, exists := byJourney[spec.journeyKey]; !exists {
			order = append(order, spec.journeyKey)
		}
		byJourney[spec.journeyKey] = append(byJourney[spec.journeyKey], spec)
	}
	booking, err := url.Parse("https://example.test/booking")
	if err != nil {
		panic(err)
	}
	journeys := make([]provider.JourneyObservation, 0, len(order))
	for _, journeyKey := range order {
		journey := provider.Journey{
			Key:             journeyKey,
			NativeID:        "native-" + journeyKey,
			TrainNumber:     "T-" + journeyKey,
			Operator:        "Test operator",
			OriginName:      "Tehran",
			DestinationName: "Mashhad",
			DepartureAt:     timePtr(testNow.Add(time.Hour)),
			ArrivalAt:       timePtr(testNow.Add(2 * time.Hour)),
		}
		offerings := make([]provider.OfferingObservation, 0, len(byJourney[journeyKey]))
		for _, spec := range byJourney[journeyKey] {
			offerings = append(offerings, provider.OfferingObservation{
				Offering: provider.Offering{
					Key:       spec.offeringKey,
					NativeID:  "native-" + spec.offeringKey,
					WagonName: "Wagon",
					ClassName: "Class",
					Price:     &provider.Price{Amount: 1000, Currency: "IRR"},
				},
				State:      spec.state,
				Reason:     spec.reason,
				Capacity:   spec.capacity,
				BookingURL: booking,
			})
		}
		journeys = append(journeys, provider.JourneyObservation{Journey: journey, Offerings: offerings})
	}
	return provider.Result{Journeys: journeys, ObservedAt: testNow}
}

func standardDeliveries() []notify.Delivery {
	return []notify.Delivery{
		{Channel: notify.ChannelTelegram, TargetKey: "telegram"},
		{Channel: notify.ChannelPush, TargetKey: "push:subscription-1"},
	}
}

func inputFor(monitorID string, targetID, configVersion int64, at time.Time, result provider.Result) availability.Input {
	return availability.Input{
		MonitorID:     monitorID,
		TargetID:      targetID,
		ProviderID:    "fake",
		ConfigVersion: configVersion,
		ObservedAt:    at,
		Now:           at,
		Result:        result,
		Deliveries:    standardDeliveries(),
	}
}

func applyCommit(t *testing.T, database *sql.DB, input availability.Input) availability.Result {
	t.Helper()
	tx := begin(t, database)
	result, err := availability.Apply(context.Background(), tx, input)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("Apply: %v", err)
	}
	commit(t, tx)
	return result
}

type stateSnapshot struct {
	ID             int64
	State          provider.State
	StateSince     time.Time
	LastObserved   time.Time
	LastDecisiveAt *time.Time
	LastMissingAt  *time.Time
	Reason         string
	Capacity       *int
	BookingURL     string
	UpdatedAt      time.Time
}

func loadState(t *testing.T, database *sql.DB, targetID, configVersion int64, journeyKey, offeringKey string) (stateSnapshot, bool) {
	t.Helper()
	var (
		snapshot     stateSnapshot
		state        string
		stateSince   string
		lastObserved string
		updatedAt    string
		lastDecisive sql.NullString
		lastMissing  sql.NullString
		reason       sql.NullString
		capacity     sql.NullInt64
		bookingURL   sql.NullString
	)
	err := database.QueryRow(`SELECT id, state, state_since, last_observed_at, last_decisive_at,
		last_missing_at, last_reason, last_capacity, last_booking_url, updated_at
		FROM inventory_states WHERE monitor_provider_id = ? AND config_version = ?
		AND journey_key = ? AND offering_key = ?`,
		targetID, configVersion, journeyKey, offeringKey,
	).Scan(&snapshot.ID, &state, &stateSince, &lastObserved, &lastDecisive, &lastMissing,
		&reason, &capacity, &bookingURL, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return stateSnapshot{}, false
	}
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	snapshot.State = provider.State(state)
	snapshot.StateSince = mustParseTime(t, stateSince)
	snapshot.LastObserved = mustParseTime(t, lastObserved)
	snapshot.UpdatedAt = mustParseTime(t, updatedAt)
	if lastDecisive.Valid {
		value := mustParseTime(t, lastDecisive.String)
		snapshot.LastDecisiveAt = &value
	}
	if lastMissing.Valid {
		value := mustParseTime(t, lastMissing.String)
		snapshot.LastMissingAt = &value
	}
	if reason.Valid {
		snapshot.Reason = reason.String
	}
	if capacity.Valid {
		value := int(capacity.Int64)
		snapshot.Capacity = &value
	}
	if bookingURL.Valid {
		snapshot.BookingURL = bookingURL.String
	}
	return snapshot, true
}

type episodeSnapshot struct {
	ID          int64
	Seq         int
	OpenedAt    time.Time
	ClosedAt    *time.Time
	CloseReason string
}

func loadEpisodes(t *testing.T, database *sql.DB, stateID int64) []episodeSnapshot {
	t.Helper()
	rows, err := database.Query(`SELECT id, episode_seq, opened_at, closed_at, close_reason
		FROM availability_episodes WHERE state_id = ? ORDER BY episode_seq`, stateID)
	if err != nil {
		t.Fatalf("query episodes: %v", err)
	}
	defer rows.Close()
	var episodes []episodeSnapshot
	for rows.Next() {
		var (
			episode     episodeSnapshot
			openedAt    string
			closedAt    sql.NullString
			closeReason sql.NullString
		)
		if err := rows.Scan(&episode.ID, &episode.Seq, &openedAt, &closedAt, &closeReason); err != nil {
			t.Fatalf("scan episode: %v", err)
		}
		episode.OpenedAt = mustParseTime(t, openedAt)
		if closedAt.Valid {
			value := mustParseTime(t, closedAt.String)
			episode.ClosedAt = &value
		}
		if closeReason.Valid {
			episode.CloseReason = closeReason.String
		}
		episodes = append(episodes, episode)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate episodes: %v", err)
	}
	return episodes
}

type eventSnapshot struct {
	ID              string
	Kind            string
	DedupeKey       string
	MonitorID       string
	ProviderID      string
	EpisodeID       int64
	OccurredAt      time.Time
	TrainNumber     string
	Operator        string
	OriginName      string
	DestinationName string
	WagonName       string
	ClassName       string
	PriceAmount     int64
	PriceCurrency   string
	BookingURL      string
	CreatedAt       time.Time
}

func loadEvent(t *testing.T, database *sql.DB, dedupeKey string) (eventSnapshot, bool) {
	t.Helper()
	var (
		event      eventSnapshot
		occurredAt string
		createdAt  string
	)
	err := database.QueryRow(`SELECT id, kind, dedupe_key, monitor_id, provider_id, episode_id,
		occurred_at, train_number, operator, origin_name, destination_name, wagon_name,
		class_name, price_amount, price_currency, booking_url, created_at
		FROM events WHERE dedupe_key = ?`, dedupeKey).Scan(
		&event.ID, &event.Kind, &event.DedupeKey, &event.MonitorID, &event.ProviderID,
		&event.EpisodeID, &occurredAt, &event.TrainNumber, &event.Operator,
		&event.OriginName, &event.DestinationName, &event.WagonName, &event.ClassName,
		&event.PriceAmount, &event.PriceCurrency, &event.BookingURL, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return eventSnapshot{}, false
	}
	if err != nil {
		t.Fatalf("load event: %v", err)
	}
	event.OccurredAt = mustParseTime(t, occurredAt)
	event.CreatedAt = mustParseTime(t, createdAt)
	return event, true
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return parsed.UTC()
}

func countRows(t *testing.T, database *sql.DB, query string, args ...any) int {
	t.Helper()
	return mustQueryInt(t, database, query, args...)
}

func TestApplyInitialReservableCreatesEpisodeEventAndOutbox(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "m1", 1)
	result := applyCommit(t, database, inputFor("m1", targetID, 1, testNow,
		resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3), reason: "available"})))

	if result.StatesUpserted != 1 || len(result.EpisodesOpened) != 1 ||
		len(result.EpisodesClosed) != 0 || len(result.EventsCreated) != 1 || result.MissingStates != 0 {
		t.Fatalf("apply result = %+v", result)
	}
	state, found := loadState(t, database, targetID, 1, "j1", "o1")
	if !found {
		t.Fatal("state not found")
	}
	if state.State != provider.StateReservable ||
		!state.StateSince.Equal(testNow) ||
		!state.LastObserved.Equal(testNow) ||
		state.LastDecisiveAt == nil || !state.LastDecisiveAt.Equal(testNow) ||
		state.LastMissingAt != nil ||
		state.Reason != "available" ||
		state.Capacity == nil || *state.Capacity != 3 ||
		state.BookingURL != "https://example.test/booking" ||
		!state.UpdatedAt.Equal(testNow) {
		t.Fatalf("state = %+v", state)
	}

	episodes := loadEpisodes(t, database, state.ID)
	if len(episodes) != 1 {
		t.Fatalf("episodes = %d, want 1", len(episodes))
	}
	episode := episodes[0]
	if episode.Seq != 1 || !episode.OpenedAt.Equal(testNow) || episode.ClosedAt != nil || episode.CloseReason != "" {
		t.Fatalf("episode = %+v", episode)
	}

	event, found := loadEvent(t, database, "availability:episode:1")
	if !found {
		t.Fatal("event not found")
	}
	if event.ID != "availability-episode-"+formatInt(episode.ID) ||
		event.Kind != "availability" ||
		event.MonitorID != "m1" || event.ProviderID != "fake" ||
		event.EpisodeID != episode.ID ||
		!event.OccurredAt.Equal(testNow) || !event.CreatedAt.Equal(testNow) ||
		event.TrainNumber != "T-j1" || event.Operator != "Test operator" ||
		event.OriginName != "Tehran" || event.DestinationName != "Mashhad" ||
		event.WagonName != "Wagon" || event.ClassName != "Class" ||
		event.PriceAmount != 1000 || event.PriceCurrency != "IRR" ||
		event.BookingURL != "https://example.test/booking" {
		t.Fatalf("event = %+v", event)
	}

	if count := countRows(t, database, `SELECT COUNT(*) FROM notification_deliveries WHERE event_id = ?`, event.ID); count != 2 {
		t.Fatalf("delivery count = %d, want 2", count)
	}
	assertDeliveries(t, database, event.ID)

	var nextDue, lastAttempt, lastSuccess sql.NullString
	var failures int
	if err := database.QueryRow(`SELECT next_due_at, last_attempt_at, last_success_at, consecutive_failures
		FROM monitor_providers WHERE id = ?`, targetID).Scan(&nextDue, &lastAttempt, &lastSuccess, &failures); err != nil {
		t.Fatalf("target state: %v", err)
	}
	if nextDue.Valid || lastAttempt.Valid || lastSuccess.Valid || failures != 0 {
		t.Fatalf("target state changed: %v/%v/%v/%d", nextDue, lastAttempt, lastSuccess, failures)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM provider_health`); count != 0 {
		t.Fatalf("provider_health rows = %d, want 0", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM check_runs`); count != 0 {
		t.Fatalf("check_runs rows = %d, want 0", count)
	}
}

func TestApplyInitialNonReservableAndUnknownCreateNoEpisodeOrEvent(t *testing.T) {
	tests := []struct {
		name         string
		state        provider.State
		capacity     *int
		wantDecisive bool
	}{
		{"not reservable", provider.StateNotReservable, intPtr(0), true},
		{"unknown", provider.StateUnknown, nil, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := newTestDB(t)
			targetID := insertMonitorTarget(t, database, "m1", 1)
			result := applyCommit(t, database, inputFor("m1", targetID, 1, testNow,
				resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: test.state, capacity: test.capacity, reason: "observed"})))

			state, found := loadState(t, database, targetID, 1, "j1", "o1")
			if !found {
				t.Fatal("state not found")
			}
			if state.State != test.state || state.LastMissingAt != nil || state.Reason != "observed" {
				t.Fatalf("state = %+v", state)
			}
			if test.wantDecisive != (state.LastDecisiveAt != nil) {
				t.Fatalf("decisive = %v, want %v", state.LastDecisiveAt, test.wantDecisive)
			}
			if len(loadEpisodes(t, database, state.ID)) != 0 {
				t.Fatal("episode created for non-reservable/unknown observation")
			}
			if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 0 {
				t.Fatalf("events = %d, want 0", count)
			}
			if len(result.EpisodesOpened) != 0 || len(result.EventsCreated) != 0 {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}

func TestApplyTransitions(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "m1", 1)
	t0 := testNow
	t1 := t0.Add(time.Minute)
	t2 := t1.Add(time.Minute)
	t3 := t2.Add(time.Minute)
	t4 := t3.Add(time.Minute)
	t5 := t4.Add(time.Minute)
	t6 := t5.Add(time.Minute)

	applyCommit(t, database, inputFor("m1", targetID, 1, t0,
		resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateNotReservable, capacity: intPtr(0), reason: "sold_out"})))
	state, _ := loadState(t, database, targetID, 1, "j1", "o1")
	if len(loadEpisodes(t, database, state.ID)) != 0 {
		t.Fatal("not-reservable created an episode")
	}

	res := applyCommit(t, database, inputFor("m1", targetID, 1, t1,
		resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3), reason: "available"})))
	if len(res.EpisodesOpened) != 1 || len(res.EventsCreated) != 1 {
		t.Fatalf("first reservable result = %+v", res)
	}
	state, _ = loadState(t, database, targetID, 1, "j1", "o1")
	episodes := loadEpisodes(t, database, state.ID)
	if len(episodes) != 1 || episodes[0].Seq != 1 || episodes[0].ClosedAt != nil {
		t.Fatalf("episodes after first reservable = %+v", episodes)
	}
	if !state.StateSince.Equal(t1) {
		t.Fatalf("state_since = %v, want %v", state.StateSince, t1)
	}

	res = applyCommit(t, database, inputFor("m1", targetID, 1, t2,
		resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3), reason: "available"})))
	if len(res.EpisodesOpened) != 0 || len(res.EventsCreated) != 0 {
		t.Fatalf("continuous reservable changed episodes/events: %+v", res)
	}
	state, _ = loadState(t, database, targetID, 1, "j1", "o1")
	if !state.StateSince.Equal(t1) || !state.LastObserved.Equal(t2) {
		t.Fatalf("continuous state = %+v", state)
	}

	applyCommit(t, database, inputFor("m1", targetID, 1, t3,
		resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateUnknown, capacity: nil, reason: "ambiguous"})))
	state, _ = loadState(t, database, targetID, 1, "j1", "o1")
	if state.State != provider.StateUnknown {
		t.Fatalf("state after unknown = %s", state.State)
	}
	episodes = loadEpisodes(t, database, state.ID)
	if len(episodes) != 1 || episodes[0].ClosedAt != nil {
		t.Fatal("unknown closed or removed an open episode")
	}

	applyCommit(t, database, inputFor("m1", targetID, 1, t4,
		resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3), reason: "available"})))
	state, _ = loadState(t, database, targetID, 1, "j1", "o1")
	episodes = loadEpisodes(t, database, state.ID)
	if len(episodes) != 1 || episodes[0].ClosedAt != nil {
		t.Fatalf("unknown->reservable opened/closed an episode: %+v", episodes)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 1 {
		t.Fatalf("events after unknown->reservable = %d, want 1", count)
	}

	res = applyCommit(t, database, inputFor("m1", targetID, 1, t5,
		resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateNotReservable, capacity: intPtr(0), reason: "sold_out"})))
	if len(res.EpisodesClosed) != 1 {
		t.Fatalf("close result = %+v", res)
	}
	state, _ = loadState(t, database, targetID, 1, "j1", "o1")
	episodes = loadEpisodes(t, database, state.ID)
	if len(episodes) != 1 || episodes[0].ClosedAt == nil || !episodes[0].ClosedAt.Equal(t5) ||
		episodes[0].CloseReason != "not_reservable" {
		t.Fatalf("closed episode = %+v", episodes)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 1 {
		t.Fatalf("closing created an event: %d", count)
	}

	res = applyCommit(t, database, inputFor("m1", targetID, 1, t6,
		resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3), reason: "available"})))
	if len(res.EpisodesOpened) != 1 || len(res.EventsCreated) != 1 {
		t.Fatalf("later reservable result = %+v", res)
	}
	state, _ = loadState(t, database, targetID, 1, "j1", "o1")
	episodes = loadEpisodes(t, database, state.ID)
	if len(episodes) != 2 || episodes[1].Seq != 2 || episodes[1].ClosedAt != nil {
		t.Fatalf("later episode = %+v", episodes)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 2 {
		t.Fatalf("events after later reservable = %d, want 2", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM notification_deliveries`); count != 4 {
		t.Fatalf("delivery count = %d, want 4", count)
	}
}

func TestApplyUnknownTransitions(t *testing.T) {
	t.Run("unknown to reservable without open episode", func(t *testing.T) {
		database := newTestDB(t)
		targetID := insertMonitorTarget(t, database, "m1", 1)
		applyCommit(t, database, inputFor("m1", targetID, 1, testNow,
			resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateUnknown, reason: "unknown"})))
		state, _ := loadState(t, database, targetID, 1, "j1", "o1")
		if len(loadEpisodes(t, database, state.ID)) != 0 {
			t.Fatal("initial unknown created an episode")
		}

		res := applyCommit(t, database, inputFor("m1", targetID, 1, testNow.Add(time.Minute),
			resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3), reason: "available"})))
		if len(res.EpisodesOpened) != 1 || len(res.EventsCreated) != 1 {
			t.Fatalf("unknown->reservable result = %+v", res)
		}
		state, _ = loadState(t, database, targetID, 1, "j1", "o1")
		episodes := loadEpisodes(t, database, state.ID)
		if len(episodes) != 1 || episodes[0].Seq != 1 || episodes[0].ClosedAt != nil {
			t.Fatalf("unknown->reservable episodes = %+v", episodes)
		}
	})

	t.Run("unknown to not reservable closes open episode", func(t *testing.T) {
		database := newTestDB(t)
		targetID := insertMonitorTarget(t, database, "m1", 1)
		applyCommit(t, database, inputFor("m1", targetID, 1, testNow,
			resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3), reason: "available"})))
		applyCommit(t, database, inputFor("m1", targetID, 1, testNow.Add(time.Minute),
			resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateUnknown, reason: "unknown"})))
		state, _ := loadState(t, database, targetID, 1, "j1", "o1")
		if state.State != provider.StateUnknown {
			t.Fatalf("state after unknown = %s", state.State)
		}
		if len(loadEpisodes(t, database, state.ID)) != 1 || loadEpisodes(t, database, state.ID)[0].ClosedAt != nil {
			t.Fatal("reservable->unknown closed the episode")
		}

		res := applyCommit(t, database, inputFor("m1", targetID, 1, testNow.Add(2*time.Minute),
			resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateNotReservable, capacity: intPtr(0), reason: "sold_out"})))
		if len(res.EpisodesClosed) != 1 {
			t.Fatalf("unknown->not-reservable result = %+v", res)
		}
		state, _ = loadState(t, database, targetID, 1, "j1", "o1")
		episodes := loadEpisodes(t, database, state.ID)
		if len(episodes) != 1 || episodes[0].ClosedAt == nil || episodes[0].CloseReason != "not_reservable" {
			t.Fatalf("unknown->not-reservable episodes = %+v", episodes)
		}
		if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 1 {
			t.Fatalf("unknown->not-reservable created an event: %d", count)
		}
	})
}

func TestApplyMissingAndEmptyResultsDoNotChangeStateOrEpisodes(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "m1", 1)
	t0 := testNow
	t1 := t0.Add(time.Minute)
	t2 := t1.Add(time.Minute)
	t3 := t2.Add(time.Minute)
	applyCommit(t, database, inputFor("m1", targetID, 1, t0, resultWith(
		obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3), reason: "available"},
		obsSpec{journeyKey: "j1", offeringKey: "o2", state: provider.StateNotReservable, capacity: intPtr(0), reason: "sold_out"},
	)))
	first, _ := loadState(t, database, targetID, 1, "j1", "o1")
	second, _ := loadState(t, database, targetID, 1, "j1", "o2")
	firstEpisodes := loadEpisodes(t, database, first.ID)
	secondEpisodes := loadEpisodes(t, database, second.ID)

	applyCommit(t, database, inputFor("m1", targetID, 1, t1, resultWith(
		obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3), reason: "available"},
	)))
	first, _ = loadState(t, database, targetID, 1, "j1", "o1")
	second, _ = loadState(t, database, targetID, 1, "j1", "o2")
	if first.LastMissingAt != nil {
		t.Fatalf("observed state marked missing: %v", first.LastMissingAt)
	}
	if second.LastMissingAt == nil || !second.LastMissingAt.Equal(t1) {
		t.Fatalf("missing state last_missing_at = %v, want %v", second.LastMissingAt, t1)
	}
	if second.State != provider.StateNotReservable {
		t.Fatalf("missing state changed: %s", second.State)
	}
	if len(loadEpisodes(t, database, first.ID)) != len(firstEpisodes) ||
		len(loadEpisodes(t, database, second.ID)) != len(secondEpisodes) {
		t.Fatal("missing observation changed episodes")
	}

	applyCommit(t, database, inputFor("m1", targetID, 1, t2, provider.Result{}))
	first, _ = loadState(t, database, targetID, 1, "j1", "o1")
	second, _ = loadState(t, database, targetID, 1, "j1", "o2")
	if first.LastMissingAt == nil || !first.LastMissingAt.Equal(t2) ||
		second.LastMissingAt == nil || !second.LastMissingAt.Equal(t2) {
		t.Fatalf("empty result missing timestamps = %v/%v", first.LastMissingAt, second.LastMissingAt)
	}
	if first.State != provider.StateReservable || second.State != provider.StateNotReservable {
		t.Fatalf("empty result changed states: %s/%s", first.State, second.State)
	}
	if len(loadEpisodes(t, database, first.ID)) != 1 || len(loadEpisodes(t, database, second.ID)) != 0 {
		t.Fatal("empty result changed episodes")
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 1 {
		t.Fatalf("empty/missing created events: %d", count)
	}

	applyCommit(t, database, inputFor("m1", targetID, 1, t3, resultWith(
		obsSpec{journeyKey: "j1", offeringKey: "o2", state: provider.StateReservable, capacity: intPtr(3), reason: "available"},
	)))
	first, _ = loadState(t, database, targetID, 1, "j1", "o1")
	second, _ = loadState(t, database, targetID, 1, "j1", "o2")
	if second.LastMissingAt != nil {
		t.Fatalf("observed missing state was not cleared: %v", second.LastMissingAt)
	}
	if first.LastMissingAt == nil || !first.LastMissingAt.Equal(t3) {
		t.Fatalf("absent state last_missing_at = %v, want %v", first.LastMissingAt, t3)
	}
	if second.State != provider.StateReservable {
		t.Fatalf("missing->reservable state = %s", second.State)
	}
	secondEpisodes = loadEpisodes(t, database, second.ID)
	if len(secondEpisodes) != 1 || secondEpisodes[0].ClosedAt != nil {
		t.Fatalf("missing->reservable episodes = %+v", secondEpisodes)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 2 {
		t.Fatalf("events after missing->reservable = %d, want 2", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM notification_deliveries`); count != 4 {
		t.Fatalf("deliveries after missing->reservable = %d, want 4", count)
	}
}

func TestApplyConflictingDuplicateKeysMarksUnknownWithoutEvent(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "m1", 1)
	applyCommit(t, database, inputFor("m1", targetID, 1, testNow,
		resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3), reason: "available"})))
	state, _ := loadState(t, database, targetID, 1, "j1", "o1")
	episodes := loadEpisodes(t, database, state.ID)

	res := applyCommit(t, database, inputFor("m1", targetID, 1, testNow.Add(time.Minute), resultWith(
		obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3), reason: "available"},
		obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateNotReservable, capacity: intPtr(0), reason: "sold_out"},
	)))
	if len(res.Warnings) != 1 || res.Warnings[0] != "conflicting_duplicate_keys" {
		t.Fatalf("warnings = %v", res.Warnings)
	}
	state, _ = loadState(t, database, targetID, 1, "j1", "o1")
	if state.State != provider.StateUnknown || state.Reason != "conflicting_duplicate_keys" {
		t.Fatalf("state after conflict = %+v", state)
	}
	if state.LastDecisiveAt == nil || !state.LastDecisiveAt.Equal(testNow) {
		t.Fatalf("conflict lost last decisive time: %v", state.LastDecisiveAt)
	}
	if len(loadEpisodes(t, database, state.ID)) != len(episodes) ||
		loadEpisodes(t, database, state.ID)[0].ClosedAt != nil {
		t.Fatal("conflict changed open episode")
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 1 {
		t.Fatalf("conflict created an event: %d", count)
	}

	applyCommit(t, database, inputFor("m1", targetID, 1, testNow.Add(2*time.Minute),
		resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateNotReservable, capacity: intPtr(0), reason: "sold_out"})))
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 1 {
		t.Fatalf("closing after conflict created event: %d", count)
	}
}

func TestApplyMultipleEventsUsesDistinctDeliveryIDs(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "m1", 1)
	result := applyCommit(t, database, inputFor("m1", targetID, 1, testNow, resultWith(
		obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3), reason: "available"},
		obsSpec{journeyKey: "j1", offeringKey: "o2", state: provider.StateReservable, capacity: intPtr(2), reason: "available"},
	)))
	if len(result.EventsCreated) != 2 {
		t.Fatalf("events created = %d, want 2", len(result.EventsCreated))
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM notification_deliveries`); count != 4 {
		t.Fatalf("delivery count = %d, want 4", count)
	}

	rows, err := database.Query(`SELECT id FROM notification_deliveries ORDER BY id`)
	if err != nil {
		t.Fatalf("query delivery ids: %v", err)
	}
	defer rows.Close()
	seen := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan delivery id: %v", err)
		}
		if _, err := uuid.Parse(id); err != nil {
			t.Fatalf("delivery id %q is not a UUID: %v", id, err)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate delivery id %q", id)
		}
		seen[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate delivery ids: %v", err)
	}
}

func TestApplyDuplicateSameStateCreatesOneEpisodeAndEvent(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "m1", 1)
	res := applyCommit(t, database, inputFor("m1", targetID, 1, testNow, resultWith(
		obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3), reason: "available"},
		obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3), reason: "available"},
	)))
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %v", res.Warnings)
	}
	state, _ := loadState(t, database, targetID, 1, "j1", "o1")
	if len(loadEpisodes(t, database, state.ID)) != 1 {
		t.Fatal("duplicate same state created multiple episodes")
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 1 {
		t.Fatalf("events = %d, want 1", count)
	}
}

func TestApplyReplayIsIdempotent(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "m1", 1)
	first := applyCommit(t, database, inputFor("m1", targetID, 1, testNow,
		resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3), reason: "available"})))
	if len(first.EventsCreated) != 1 {
		t.Fatalf("first result = %+v", first)
	}
	second := applyCommit(t, database, inputFor("m1", targetID, 1, testNow.Add(time.Minute),
		resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3), reason: "available"})))
	if len(second.EventsCreated) != 0 || len(second.EpisodesOpened) != 0 {
		t.Fatalf("replay result = %+v", second)
	}
	state, _ := loadState(t, database, targetID, 1, "j1", "o1")
	if !state.StateSince.Equal(testNow) || !state.LastObserved.Equal(testNow.Add(time.Minute)) {
		t.Fatalf("replay state = %+v", state)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 1 {
		t.Fatalf("events after replay = %d, want 1", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM notification_deliveries`); count != 2 {
		t.Fatalf("deliveries after replay = %d, want 2", count)
	}
	if len(loadEpisodes(t, database, state.ID)) != 1 {
		t.Fatal("replay created an episode")
	}
}

func TestApplyRollsBackWithCallerTransaction(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "m1", 1)
	tx := begin(t, database)
	if _, err := availability.Apply(context.Background(), tx, inputFor("m1", targetID, 1, testNow,
		resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3), reason: "available"}))); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if count := mustQueryInt(t, tx, `SELECT COUNT(*) FROM inventory_states`); count != 1 {
		t.Fatalf("state count inside tx = %d, want 1", count)
	}
	if count := mustQueryInt(t, tx, `SELECT COUNT(*) FROM availability_episodes`); count != 1 {
		t.Fatalf("episode count inside tx = %d, want 1", count)
	}
	if count := mustQueryInt(t, tx, `SELECT COUNT(*) FROM events`); count != 1 {
		t.Fatalf("event count inside tx = %d, want 1", count)
	}
	rollback(t, tx)

	if count := countRows(t, database, `SELECT COUNT(*) FROM inventory_states`); count != 0 {
		t.Fatalf("states after rollback = %d, want 0", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM availability_episodes`); count != 0 {
		t.Fatalf("episodes after rollback = %d, want 0", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 0 {
		t.Fatalf("events after rollback = %d, want 0", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM notification_deliveries`); count != 0 {
		t.Fatalf("deliveries after rollback = %d, want 0", count)
	}
}

func TestApplyFaultInjectionRollsBackPartialState(t *testing.T) {
	database := newTestDB(t)
	firstTarget := insertMonitorTarget(t, database, "m1", 1)
	firstInput := inputFor("m1", firstTarget, 1, testNow,
		resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3), reason: "available"}))
	firstInput.Deliveries = []notify.Delivery{{Channel: notify.ChannelTelegram, TargetKey: "telegram"}}
	applyCommit(t, database, firstInput)
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 1 {
		t.Fatalf("baseline events = %d, want 1", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM notification_deliveries`); count != 1 {
		t.Fatalf("baseline deliveries = %d, want 1", count)
	}

	mustExec(t, database, `CREATE TRIGGER fail_delivery_insert
		BEFORE INSERT ON notification_deliveries
		BEGIN
			SELECT RAISE(ABORT, 'injected delivery failure');
		END`)

	secondTarget := insertMonitorTarget(t, database, "m2", 1)
	tx := begin(t, database)
	input := inputFor("m2", secondTarget, 1, testNow.Add(time.Minute),
		resultWith(obsSpec{journeyKey: "j2", offeringKey: "o2", state: provider.StateReservable, capacity: intPtr(3), reason: "available"}))
	input.Deliveries = []notify.Delivery{{Channel: notify.ChannelTelegram, TargetKey: "telegram"}}
	if _, err := availability.Apply(context.Background(), tx, input); err == nil {
		t.Fatal("Apply succeeded despite injected delivery failure")
	}
	rollback(t, tx)

	if count := countRows(t, database, `SELECT COUNT(*) FROM inventory_states WHERE monitor_provider_id = ?`, secondTarget); count != 0 {
		t.Fatalf("second target states = %d, want 0", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM availability_episodes WHERE state_id IN
		(SELECT id FROM inventory_states WHERE monitor_provider_id = ?)`, secondTarget); count != 0 {
		t.Fatalf("second target episodes = %d, want 0", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 1 {
		t.Fatalf("events after rollback = %d, want 1", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM notification_deliveries`); count != 1 {
		t.Fatalf("deliveries after rollback = %d, want 1", count)
	}
}

func TestApplyValidatesInputBeforeWriting(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "m1", 1)
	base := inputFor("m1", targetID, 1, testNow,
		resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3), reason: "available"}))

	tests := []struct {
		name  string
		input availability.Input
	}{
		{"zero target", func() availability.Input { in := base; in.TargetID = 0; return in }()},
		{"empty monitor", func() availability.Input { in := base; in.MonitorID = ""; return in }()},
		{"empty provider", func() availability.Input { in := base; in.ProviderID = ""; return in }()},
		{"zero config", func() availability.Input { in := base; in.ConfigVersion = 0; return in }()},
		{"zero observed", func() availability.Input { in := base; in.ObservedAt = time.Time{}; return in }()},
		{"zero now", func() availability.Input { in := base; in.Now = time.Time{}; return in }()},
		{"empty journey key", func() availability.Input {
			in := base
			in.Result = resultWith(obsSpec{journeyKey: "", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3)})
			return in
		}()},
		{"empty offering key", func() availability.Input {
			in := base
			in.Result = resultWith(obsSpec{journeyKey: "j1", offeringKey: "", state: provider.StateReservable, capacity: intPtr(3)})
			return in
		}()},
		{"invalid state", func() availability.Input {
			in := base
			in.Result = resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.State("bogus"), capacity: intPtr(3)})
			return in
		}()},
		{"negative capacity", func() availability.Input {
			in := base
			in.Result = resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(-1)})
			return in
		}()},
		{"invalid delivery", func() availability.Input {
			in := base
			in.Deliveries = []notify.Delivery{{Channel: notify.ChannelTelegram, TargetKey: "chat"}}
			return in
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx := begin(t, database)
			if _, err := availability.Apply(context.Background(), tx, test.input); err == nil {
				t.Fatal("Apply succeeded, want validation error")
			}
			rollback(t, tx)
		})
	}
	if _, err := availability.Apply(context.Background(), nil, base); err == nil {
		t.Fatal("Apply(nil tx) succeeded")
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM inventory_states`); count != 0 {
		t.Fatalf("states after validation failures = %d, want 0", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM availability_episodes`); count != 0 {
		t.Fatalf("episodes after validation failures = %d, want 0", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 0 {
		t.Fatalf("events after validation failures = %d, want 0", count)
	}
}

func TestApplyConfigVersionIsolation(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "m1", 1)
	applyCommit(t, database, inputFor("m1", targetID, 1, testNow,
		resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3), reason: "available"})))
	first, _ := loadState(t, database, targetID, 1, "j1", "o1")

	mustExec(t, database, `UPDATE monitor_providers SET config_version = 2 WHERE id = ?`, targetID)
	applyCommit(t, database, inputFor("m1", targetID, 2, testNow.Add(time.Minute),
		resultWith(obsSpec{journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(3), reason: "available"})))
	second, found := loadState(t, database, targetID, 2, "j1", "o1")
	if !found {
		t.Fatal("config 2 state not found")
	}
	firstReloaded, _ := loadState(t, database, targetID, 1, "j1", "o1")
	if !firstReloaded.LastObserved.Equal(first.LastObserved) {
		t.Fatalf("config 1 state changed: %+v", firstReloaded)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM inventory_states`); count != 2 {
		t.Fatalf("state count = %d, want 2", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM availability_episodes`); count != 2 {
		t.Fatalf("episode count = %d, want 2", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 2 {
		t.Fatalf("event count = %d, want 2", count)
	}

	applyCommit(t, database, inputFor("m1", targetID, 2, testNow.Add(2*time.Minute), provider.Result{}))
	firstReloaded, _ = loadState(t, database, targetID, 1, "j1", "o1")
	second, _ = loadState(t, database, targetID, 2, "j1", "o1")
	if firstReloaded.LastMissingAt != nil {
		t.Fatalf("config 1 state marked missing by config 2 result")
	}
	if second.LastMissingAt == nil || !second.LastMissingAt.Equal(testNow.Add(2*time.Minute)) {
		t.Fatalf("config 2 missing timestamp = %v", second.LastMissingAt)
	}
}

func assertDeliveries(t *testing.T, database *sql.DB, eventID string) {
	t.Helper()
	rows, err := database.Query(`SELECT channel, target_key, status, attempt_count, next_attempt_at
		FROM notification_deliveries WHERE event_id = ? ORDER BY channel`, eventID)
	if err != nil {
		t.Fatalf("query deliveries: %v", err)
	}
	defer rows.Close()
	var count int
	for rows.Next() {
		var channel, targetKey, status, nextAttempt string
		var attemptCount int
		if err := rows.Scan(&channel, &targetKey, &status, &attemptCount, &nextAttempt); err != nil {
			t.Fatalf("scan delivery: %v", err)
		}
		if status != "pending" || attemptCount != 0 || nextAttempt != testNow.Format(time.RFC3339Nano) {
			t.Fatalf("delivery = %s/%s/%s/%d/%s", channel, targetKey, status, attemptCount, nextAttempt)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate deliveries: %v", err)
	}
	if count != 2 {
		t.Fatalf("delivery count = %d, want 2", count)
	}
}

func formatInt(value int64) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
