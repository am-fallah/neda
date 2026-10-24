package notify_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/google/uuid"
)

var testNow = time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "notify-test.db"))
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

func mustExec(t *testing.T, database interface {
	Exec(string, ...any) (sql.Result, error)
}, query string, args ...any) sql.Result {
	t.Helper()
	result, err := database.Exec(query, args...)
	if err != nil {
		t.Fatalf("exec failed: %v\nquery: %s", err, query)
	}
	return result
}

func mustQueryInt(t *testing.T, database interface {
	QueryRow(string, ...any) *sql.Row
}, query string, args ...any) int {
	t.Helper()
	var value int
	if err := database.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatalf("query failed: %v\nquery: %s", err, query)
	}
	return value
}

func insertAvailabilityFixture(t *testing.T, database *sql.DB) int64 {
	t.Helper()
	now := testNow.Format(time.RFC3339Nano)
	mustExec(t, database, `INSERT INTO monitors
		(id, label, origin_name, destination_name, departure_date, adults, children, infants,
		 enabled, expires_at, created_at, updated_at)
		VALUES ('m1', 'Test monitor', 'Tehran', 'Mashhad', '2026-12-15', 1, 0, 0, 1, ?, ?, ?)`,
		now, now, now)

	target := mustExec(t, database, `INSERT INTO monitor_providers
		(monitor_id, provider_id, origin_id, destination_id, created_at, updated_at)
		VALUES ('m1', 'fake', 'origin-fake', 'destination-fake', ?, ?)`,
		now, now)
	targetID, err := target.LastInsertId()
	if err != nil {
		t.Fatalf("target LastInsertId: %v", err)
	}

	state := mustExec(t, database, `INSERT INTO inventory_states
		(monitor_provider_id, config_version, journey_key, offering_key, state,
		 state_since, last_observed_at, updated_at)
		VALUES (?, 1, 'journey-1', 'offering-1', 'reservable', ?, ?, ?)`,
		targetID, now, now, now)
	stateID, err := state.LastInsertId()
	if err != nil {
		t.Fatalf("state LastInsertId: %v", err)
	}

	episode := mustExec(t, database, `INSERT INTO availability_episodes
		(state_id, episode_seq, opened_at) VALUES (?, 1, ?)`,
		stateID, now)
	episodeID, err := episode.LastInsertId()
	if err != nil {
		t.Fatalf("episode LastInsertId: %v", err)
	}
	return episodeID
}

func int64Ptr(value int64) *int64 { return &value }
func intPtr(value int) *int       { return &value }

func availabilityEvent(episodeID int64) notify.Event {
	return notify.Event{
		ID:              "event-availability",
		Kind:            notify.KindAvailability,
		DedupeKey:       "availability:episode:1",
		MonitorID:       "m1",
		ProviderID:      "fake",
		EpisodeID:       int64Ptr(episodeID),
		OccurredAt:      testNow,
		TrainNumber:     "TEST-1",
		Operator:        "Test operator",
		OriginName:      "Tehran",
		DestinationName: "Mashhad",
		WagonName:       "Wagon 1",
		ClassName:       "Class 1",
		PriceAmount:     int64Ptr(1500000),
		PriceCurrency:   "IRR",
		BookingURL:      "https://example.test/booking",
		CreatedAt:       testNow,
	}
}

func providerHealthEvent(kind notify.EventKind, dedupeKey string) notify.Event {
	return notify.Event{
		ID:           "event-health",
		Kind:         kind,
		DedupeKey:    dedupeKey,
		ProviderID:   "fake",
		OccurredAt:   testNow,
		ErrorKind:    "timeout",
		FailureCount: intPtr(3),
		CreatedAt:    testNow,
	}
}

func standardDeliveries() []notify.Delivery {
	return []notify.Delivery{
		{Channel: notify.ChannelTelegram, TargetKey: "telegram"},
		{Channel: notify.ChannelPush, TargetKey: "push:subscription-1"},
	}
}

func assertDelivery(t *testing.T, database *sql.DB, eventID, channel, targetKey string) {
	t.Helper()
	var status string
	var attemptCount int
	var nextAttemptAt string
	var lastAttemptAt, sentAt, lastErrorKind, leaseUntil sql.NullString
	var createdAt, updatedAt string
	if err := database.QueryRow(`SELECT status, attempt_count, next_attempt_at,
		last_attempt_at, sent_at, last_error_kind, lease_until, created_at, updated_at
		FROM notification_deliveries WHERE event_id = ? AND channel = ? AND target_key = ?`,
		eventID, channel, targetKey).Scan(
		&status, &attemptCount, &nextAttemptAt, &lastAttemptAt, &sentAt, &lastErrorKind,
		&leaseUntil, &createdAt, &updatedAt,
	); err != nil {
		t.Fatalf("query delivery %s/%s: %v", channel, targetKey, err)
	}
	if status != "pending" || attemptCount != 0 {
		t.Fatalf("delivery status/attempts = %s/%d, want pending/0", status, attemptCount)
	}
	if nextAttemptAt != testNow.Format(time.RFC3339Nano) {
		t.Fatalf("next_attempt_at = %q, want %q", nextAttemptAt, testNow.Format(time.RFC3339Nano))
	}
	if lastAttemptAt.Valid || sentAt.Valid || lastErrorKind.Valid || leaseUntil.Valid {
		t.Fatalf("new delivery has unexpected nullable values: %+v", []sql.NullString{lastAttemptAt, sentAt, lastErrorKind, leaseUntil})
	}
	if createdAt != testNow.Format(time.RFC3339Nano) || updatedAt != testNow.Format(time.RFC3339Nano) {
		t.Fatalf("created_at/updated_at = %q/%q", createdAt, updatedAt)
	}
}

func TestEnqueueAvailabilityEventAndDeliveries(t *testing.T) {
	database := newTestDB(t)
	episodeID := insertAvailabilityFixture(t, database)
	tx := begin(t, database)

	result, err := notify.Enqueue(context.Background(), tx, availabilityEvent(episodeID), standardDeliveries())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if !result.Created || result.EventID != "event-availability" {
		t.Fatalf("result = %+v, want created event-availability", result)
	}
	commit(t, tx)

	var kind, dedupeKey, monitorID, providerID, occurredAt, createdAt string
	var episodeIDValue int64
	var trainNumber, operator, originName, destinationName, wagonName, className string
	var priceCurrency, bookingURL string
	var priceAmount int64
	var errorKind sql.NullString
	var failureCount sql.NullInt64
	if err := database.QueryRow(`SELECT kind, dedupe_key, monitor_id, provider_id, episode_id,
		occurred_at, train_number, operator, origin_name, destination_name, wagon_name,
		class_name, price_amount, price_currency, booking_url, error_kind, failure_count, created_at
		FROM events WHERE id = ?`, result.EventID).Scan(
		&kind, &dedupeKey, &monitorID, &providerID, &episodeIDValue, &occurredAt,
		&trainNumber, &operator, &originName, &destinationName, &wagonName, &className,
		&priceAmount, &priceCurrency, &bookingURL, &errorKind, &failureCount, &createdAt,
	); err != nil {
		t.Fatalf("query event: %v", err)
	}
	if kind != "availability" || dedupeKey != "availability:episode:1" ||
		monitorID != "m1" || providerID != "fake" || episodeIDValue != episodeID {
		t.Fatalf("event identity = %s/%s/%s/%s/%d", kind, dedupeKey, monitorID, providerID, episodeIDValue)
	}
	if occurredAt != testNow.Format(time.RFC3339Nano) || createdAt != testNow.Format(time.RFC3339Nano) {
		t.Fatalf("event timestamps = %q/%q", occurredAt, createdAt)
	}
	if trainNumber != "TEST-1" || operator != "Test operator" || originName != "Tehran" ||
		destinationName != "Mashhad" || wagonName != "Wagon 1" || className != "Class 1" ||
		priceAmount != 1500000 || priceCurrency != "IRR" || bookingURL != "https://example.test/booking" {
		t.Fatalf("event snapshot fields did not round-trip")
	}
	if errorKind.Valid || failureCount.Valid {
		t.Fatalf("availability event has health fields: %v/%v", errorKind, failureCount)
	}

	if count := mustQueryInt(t, database, `SELECT COUNT(*) FROM notification_deliveries WHERE event_id = ?`, result.EventID); count != 2 {
		t.Fatalf("delivery count = %d, want 2", count)
	}
	assertDelivery(t, database, result.EventID, "telegram", "telegram")
	assertDelivery(t, database, result.EventID, "push", "push:subscription-1")
}

func TestEnqueueProviderHealthEvents(t *testing.T) {
	tests := []struct {
		name string
		kind notify.EventKind
		key  string
	}{
		{"degraded", notify.KindProviderDegraded, "health:fake:1:degraded"},
		{"recovered", notify.KindProviderRecovered, "health:fake:1:recovered"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := newTestDB(t)
			tx := begin(t, database)
			result, err := notify.Enqueue(context.Background(), tx, providerHealthEvent(test.kind, test.key),
				[]notify.Delivery{{Channel: notify.ChannelTelegram, TargetKey: "telegram"}})
			if err != nil {
				t.Fatalf("Enqueue: %v", err)
			}
			commit(t, tx)

			var kind, dedupeKey, providerID, occurredAt, createdAt, errorKind string
			var monitorID sql.NullString
			var episodeID sql.NullInt64
			var failureCount int
			if err := database.QueryRow(`SELECT kind, dedupe_key, monitor_id, provider_id, episode_id,
				occurred_at, error_kind, failure_count, created_at FROM events WHERE id = ?`, result.EventID).Scan(
				&kind, &dedupeKey, &monitorID, &providerID, &episodeID, &occurredAt,
				&errorKind, &failureCount, &createdAt,
			); err != nil {
				t.Fatalf("query event: %v", err)
			}
			if kind != string(test.kind) || dedupeKey != test.key || providerID != "fake" {
				t.Fatalf("event identity = %s/%s/%s", kind, dedupeKey, providerID)
			}
			if monitorID.Valid || episodeID.Valid {
				t.Fatalf("provider-health event has monitor/episode: %v/%v", monitorID, episodeID)
			}
			if occurredAt != testNow.Format(time.RFC3339Nano) || createdAt != testNow.Format(time.RFC3339Nano) {
				t.Fatalf("event timestamps = %q/%q", occurredAt, createdAt)
			}
			if errorKind != "timeout" || failureCount != 3 {
				t.Fatalf("health fields = %s/%d, want timeout/3", errorKind, failureCount)
			}
		})
	}
}

func TestEnqueueDuplicateEventReturnsExistingID(t *testing.T) {
	database := newTestDB(t)
	episodeID := insertAvailabilityFixture(t, database)

	first := begin(t, database)
	firstResult, err := notify.Enqueue(context.Background(), first, availabilityEvent(episodeID), standardDeliveries())
	if err != nil {
		t.Fatalf("first Enqueue: %v", err)
	}
	commit(t, first)

	second := begin(t, database)
	duplicate := availabilityEvent(episodeID)
	duplicate.ID = "event-duplicate"
	secondResult, err := notify.Enqueue(context.Background(), second, duplicate,
		[]notify.Delivery{{Channel: notify.ChannelPush, TargetKey: "push:subscription-2"}})
	if err != nil {
		t.Fatalf("second Enqueue: %v", err)
	}
	if secondResult.Created || secondResult.EventID != firstResult.EventID {
		t.Fatalf("duplicate result = %+v, want existing %s", secondResult, firstResult.EventID)
	}
	commit(t, second)

	if count := mustQueryInt(t, database, `SELECT COUNT(*) FROM events`); count != 1 {
		t.Fatalf("event count = %d, want 1", count)
	}
	if count := mustQueryInt(t, database, `SELECT COUNT(*) FROM notification_deliveries`); count != 2 {
		t.Fatalf("delivery count = %d, want 2", count)
	}
}

func TestEnqueueDuplicateDeliveryTargetIsNoop(t *testing.T) {
	database := newTestDB(t)
	tx := begin(t, database)
	result, err := notify.Enqueue(context.Background(), tx, providerHealthEvent(notify.KindProviderDegraded, "health:fake:1:degraded"),
		[]notify.Delivery{
			{Channel: notify.ChannelTelegram, TargetKey: "telegram"},
			{Channel: notify.ChannelTelegram, TargetKey: "telegram"},
		})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	commit(t, tx)

	if count := mustQueryInt(t, database, `SELECT COUNT(*) FROM notification_deliveries WHERE event_id = ?`, result.EventID); count != 1 {
		t.Fatalf("delivery count = %d, want 1", count)
	}
}

func TestEnqueueDoesNotCommitCallerTransaction(t *testing.T) {
	database := newTestDB(t)
	episodeID := insertAvailabilityFixture(t, database)
	tx := begin(t, database)

	if _, err := notify.Enqueue(context.Background(), tx, availabilityEvent(episodeID), standardDeliveries()); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if count := mustQueryInt(t, tx, `SELECT COUNT(*) FROM events`); count != 1 {
		t.Fatalf("event count inside tx = %d, want 1", count)
	}
	if count := mustQueryInt(t, tx, `SELECT COUNT(*) FROM notification_deliveries`); count != 2 {
		t.Fatalf("delivery count inside tx = %d, want 2", count)
	}
	rollback(t, tx)

	if count := mustQueryInt(t, database, `SELECT COUNT(*) FROM events`); count != 0 {
		t.Fatalf("event count after rollback = %d, want 0", count)
	}
	if count := mustQueryInt(t, database, `SELECT COUNT(*) FROM notification_deliveries`); count != 0 {
		t.Fatalf("delivery count after rollback = %d, want 0", count)
	}
}

func TestEnqueueDeterministicDeliveryIDsAcrossEvents(t *testing.T) {
	database := newTestDB(t)

	first := begin(t, database)
	firstEvent := providerHealthEvent(notify.KindProviderDegraded, "health:fake:1:degraded")
	firstResult, err := notify.Enqueue(context.Background(), first, firstEvent,
		[]notify.Delivery{{Channel: notify.ChannelTelegram, TargetKey: "telegram"}})
	if err != nil {
		t.Fatalf("first Enqueue: %v", err)
	}
	commit(t, first)

	second := begin(t, database)
	secondEvent := providerHealthEvent(notify.KindProviderDegraded, "health:fake:2:degraded")
	secondEvent.ID = "event-health-2"
	secondResult, err := notify.Enqueue(context.Background(), second, secondEvent,
		[]notify.Delivery{{Channel: notify.ChannelTelegram, TargetKey: "telegram"}})
	if err != nil {
		t.Fatalf("second Enqueue with same destination: %v", err)
	}
	commit(t, second)

	var firstID, secondID string
	if err := database.QueryRow(`SELECT id FROM notification_deliveries WHERE event_id = ?`, firstResult.EventID).Scan(&firstID); err != nil {
		t.Fatalf("query first delivery: %v", err)
	}
	if err := database.QueryRow(`SELECT id FROM notification_deliveries WHERE event_id = ?`, secondResult.EventID).Scan(&secondID); err != nil {
		t.Fatalf("query second delivery: %v", err)
	}
	if firstID == secondID {
		t.Fatalf("delivery IDs collide across events: %q", firstID)
	}
	if _, err := uuid.Parse(firstID); err != nil {
		t.Fatalf("first delivery ID is not a UUID: %v", err)
	}
	if _, err := uuid.Parse(secondID); err != nil {
		t.Fatalf("second delivery ID is not a UUID: %v", err)
	}

	// Re-enqueueing the same event must preserve dedupe and not add rows.
	third := begin(t, database)
	duplicate := providerHealthEvent(notify.KindProviderDegraded, "health:fake:1:degraded")
	duplicateResult, err := notify.Enqueue(context.Background(), third, duplicate,
		[]notify.Delivery{{Channel: notify.ChannelTelegram, TargetKey: "telegram"}})
	if err != nil {
		t.Fatalf("duplicate Enqueue: %v", err)
	}
	commit(t, third)
	if duplicateResult.Created || duplicateResult.EventID != firstResult.EventID {
		t.Fatalf("duplicate result = %+v, want existing %s", duplicateResult, firstResult.EventID)
	}
	if count := mustQueryInt(t, database, `SELECT COUNT(*) FROM notification_deliveries`); count != 2 {
		t.Fatalf("delivery count after duplicate = %d, want 2", count)
	}
}

func TestEnqueueDeliveryInsertFailureRollsBack(t *testing.T) {
	database := newTestDB(t)
	mustExec(t, database, `CREATE TRIGGER fail_delivery_insert
		BEFORE INSERT ON notification_deliveries
		BEGIN
			SELECT RAISE(ABORT, 'injected delivery failure');
		END`)

	tx := begin(t, database)
	if _, err := notify.Enqueue(context.Background(), tx,
		providerHealthEvent(notify.KindProviderDegraded, "health:fake:1:degraded"),
		standardDeliveries(),
	); err == nil {
		t.Fatal("Enqueue succeeded despite injected delivery failure")
	}
	rollback(t, tx)

	if count := mustQueryInt(t, database, `SELECT COUNT(*) FROM events`); count != 0 {
		t.Fatalf("event count after rollback = %d, want 0", count)
	}
	if count := mustQueryInt(t, database, `SELECT COUNT(*) FROM notification_deliveries`); count != 0 {
		t.Fatalf("delivery count after rollback = %d, want 0", count)
	}
}

func TestEnqueueEventForeignKeyFailureRollsBack(t *testing.T) {
	database := newTestDB(t)
	episodeID := insertAvailabilityFixture(t, database)
	tx := begin(t, database)

	event := availabilityEvent(episodeID)
	event.MonitorID = "missing-monitor"
	if _, err := notify.Enqueue(context.Background(), tx, event, standardDeliveries()); err == nil {
		t.Fatal("Enqueue succeeded with missing monitor foreign key")
	}
	rollback(t, tx)

	if count := mustQueryInt(t, database, `SELECT COUNT(*) FROM events`); count != 0 {
		t.Fatalf("event count = %d, want 0", count)
	}
	if count := mustQueryInt(t, database, `SELECT COUNT(*) FROM notification_deliveries`); count != 0 {
		t.Fatalf("delivery count = %d, want 0", count)
	}
}

func TestFindEventIDByDedupeKey(t *testing.T) {
	database := newTestDB(t)
	absent := begin(t, database)
	id, found, err := notify.FindEventIDByDedupeKey(context.Background(), absent, "health:fake:1:degraded")
	if err != nil {
		t.Fatalf("FindEventIDByDedupeKey absent: %v", err)
	}
	if found || id != "" {
		t.Fatalf("absent lookup = %q/%v, want empty/false", id, found)
	}
	rollback(t, absent)

	tx := begin(t, database)
	result, err := notify.Enqueue(context.Background(), tx, providerHealthEvent(notify.KindProviderDegraded, "health:fake:1:degraded"), nil)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	commit(t, tx)

	lookup := begin(t, database)
	id, found, err = notify.FindEventIDByDedupeKey(context.Background(), lookup, "health:fake:1:degraded")
	if err != nil {
		t.Fatalf("FindEventIDByDedupeKey present: %v", err)
	}
	rollback(t, lookup)
	if !found || id != result.EventID {
		t.Fatalf("present lookup = %q/%v, want %q/true", id, found, result.EventID)
	}

	if _, _, err := notify.FindEventIDByDedupeKey(context.Background(), nil, "key"); err == nil {
		t.Fatal("FindEventIDByDedupeKey(nil) succeeded")
	}
	if _, _, err := notify.FindEventIDByDedupeKey(context.Background(), absent, " "); err == nil {
		t.Fatal("FindEventIDByDedupeKey(empty) succeeded")
	}
}

func TestEnqueueRejectsInvalidEvents(t *testing.T) {
	valid := providerHealthEvent(notify.KindProviderDegraded, "health:fake:1:degraded")
	availability := availabilityEvent(1)

	tests := []struct {
		name  string
		event notify.Event
	}{
		{"empty id", func() notify.Event { e := valid; e.ID = ""; return e }()},
		{"empty dedupe key", func() notify.Event { e := valid; e.DedupeKey = ""; return e }()},
		{"invalid kind", func() notify.Event { e := valid; e.Kind = "bogus"; return e }()},
		{"empty provider", func() notify.Event { e := valid; e.ProviderID = ""; return e }()},
		{"zero occurred", func() notify.Event { e := valid; e.OccurredAt = time.Time{}; return e }()},
		{"zero created", func() notify.Event { e := valid; e.CreatedAt = time.Time{}; return e }()},
		{"availability no monitor", func() notify.Event { e := availability; e.MonitorID = ""; return e }()},
		{"availability no episode", func() notify.Event { e := availability; e.EpisodeID = nil; return e }()},
		{"availability health fields", func() notify.Event { e := availability; e.ErrorKind = "timeout"; return e }()},
		{"health monitor", func() notify.Event { e := valid; e.MonitorID = "m1"; return e }()},
		{"health episode", func() notify.Event { e := valid; e.EpisodeID = int64Ptr(1); return e }()},
		{"health no error", func() notify.Event { e := valid; e.ErrorKind = ""; return e }()},
		{"health no failure count", func() notify.Event { e := valid; e.FailureCount = nil; return e }()},
		{"health zero failure count", func() notify.Event { e := valid; e.FailureCount = intPtr(0); return e }()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := newTestDB(t)
			tx := begin(t, database)
			if _, err := notify.Enqueue(context.Background(), tx, test.event, nil); err == nil {
				t.Fatal("Enqueue succeeded, want validation error")
			}
			rollback(t, tx)
			if count := mustQueryInt(t, database, `SELECT COUNT(*) FROM events`); count != 0 {
				t.Fatalf("event count = %d, want 0", count)
			}
		})
	}
}

func TestEnqueueRejectsInvalidDeliveries(t *testing.T) {
	validEvent := providerHealthEvent(notify.KindProviderDegraded, "health:fake:1:degraded")
	tests := []struct {
		name       string
		deliveries []notify.Delivery
	}{
		{"invalid channel", []notify.Delivery{{Channel: "sms", TargetKey: "telegram"}}},
		{"telegram target", []notify.Delivery{{Channel: notify.ChannelTelegram, TargetKey: "chat-1"}}},
		{"push target", []notify.Delivery{{Channel: notify.ChannelPush, TargetKey: "subscription-1"}}},
		{"empty push target", []notify.Delivery{{Channel: notify.ChannelPush, TargetKey: "push:"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := newTestDB(t)
			tx := begin(t, database)
			if _, err := notify.Enqueue(context.Background(), tx, validEvent, test.deliveries); err == nil {
				t.Fatal("Enqueue succeeded, want validation error")
			}
			rollback(t, tx)
			if count := mustQueryInt(t, database, `SELECT COUNT(*) FROM events`); count != 0 {
				t.Fatalf("event count = %d, want 0", count)
			}
		})
	}
}

func TestEnqueueAllowsNoDeliveries(t *testing.T) {
	database := newTestDB(t)
	tx := begin(t, database)
	result, err := notify.Enqueue(context.Background(), tx, providerHealthEvent(notify.KindProviderRecovered, "health:fake:1:recovered"), nil)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	commit(t, tx)

	if count := mustQueryInt(t, database, `SELECT COUNT(*) FROM events WHERE id = ?`, result.EventID); count != 1 {
		t.Fatalf("event count = %d, want 1", count)
	}
	if count := mustQueryInt(t, database, `SELECT COUNT(*) FROM notification_deliveries`); count != 0 {
		t.Fatalf("delivery count = %d, want 0", count)
	}
}

func TestEnqueueNilTransaction(t *testing.T) {
	if _, err := notify.Enqueue(context.Background(), nil, providerHealthEvent(notify.KindProviderDegraded, "key"), nil); err == nil {
		t.Fatal("Enqueue(nil tx) succeeded")
	}
}
