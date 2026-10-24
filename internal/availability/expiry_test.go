package availability_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/availability"
	"github.com/am-fallah/neda/internal/provider"
)

func TestCloseExpiredClosesAllMonitorEpisodesWithoutEvents(t *testing.T) {
	database := newTestDB(t)
	targetA := insertMonitorTarget(t, database, "expire-a", 1)
	targetA2 := insertMonitorProvider(t, database, "expire-a", "other", 1)
	targetB := insertMonitorTarget(t, database, "expire-b", 1)

	// Three open episodes for monitor A (one target has two config versions and
	// another target has one) and one for B.
	for _, input := range []availability.Input{
		inputFor("expire-a", targetA, 1, testNow, resultWith(obsSpec{
			journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(2), reason: "available",
		})),
		inputFor("expire-a", targetA, 2, testNow, resultWith(obsSpec{
			journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(2), reason: "available",
		})),
		inputFor("expire-a", targetA2, 1, testNow, resultWith(obsSpec{
			journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(2), reason: "available",
		})),
		inputFor("expire-b", targetB, 1, testNow, resultWith(obsSpec{
			journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(2), reason: "available",
		})),
	} {
		applyCommit(t, database, input)
	}

	eventsBefore := countRows(t, database, `SELECT COUNT(*) FROM events`)
	deliveriesBefore := countRows(t, database, `SELECT COUNT(*) FROM notification_deliveries`)
	expiredAt := testNow.Add(time.Hour)
	tx := begin(t, database)
	closed, err := availability.CloseExpired(context.Background(), tx, "expire-a", expiredAt)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("CloseExpired: %v", err)
	}
	commit(t, tx)
	if closed != 3 {
		t.Fatalf("closed episodes = %d, want 3", closed)
	}

	if count := countRows(t, database, `SELECT COUNT(*) FROM availability_episodes e
		JOIN inventory_states s ON s.id = e.state_id
		JOIN monitor_providers mp ON mp.id = s.monitor_provider_id
		WHERE mp.monitor_id = 'expire-a' AND e.closed_at = ? AND e.close_reason = 'expired'`,
		formatTestTime(expiredAt)); count != 3 {
		t.Fatalf("monitor A expired episodes = %d, want 3", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM availability_episodes e
		JOIN inventory_states s ON s.id = e.state_id
		WHERE s.monitor_provider_id = ? AND e.closed_at IS NULL`, targetB); count != 1 {
		t.Fatalf("monitor B open episodes = %d, want 1", count)
	}
	if events := countRows(t, database, `SELECT COUNT(*) FROM events`); events != eventsBefore {
		t.Fatalf("events = %d, want unchanged %d", events, eventsBefore)
	}
	if deliveries := countRows(t, database, `SELECT COUNT(*) FROM notification_deliveries`); deliveries != deliveriesBefore {
		t.Fatalf("deliveries = %d, want unchanged %d", deliveries, deliveriesBefore)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM provider_health`); count != 0 {
		t.Fatalf("provider_health rows = %d, want 0", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM check_runs`); count != 0 {
		t.Fatalf("check_runs rows = %d, want 0", count)
	}

	tx = begin(t, database)
	again, err := availability.CloseExpired(context.Background(), tx, "expire-a", expiredAt.Add(time.Minute))
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("second CloseExpired: %v", err)
	}
	commit(t, tx)
	if again != 0 {
		t.Fatalf("second close = %d, want 0", again)
	}
}

func TestCloseExpiredRollsBackWithCallerTransactionAndValidatesInput(t *testing.T) {
	database := newTestDB(t)
	target := insertMonitorTarget(t, database, "expire-rollback", 1)
	applyCommit(t, database, inputFor("expire-rollback", target, 1, testNow, resultWith(obsSpec{
		journeyKey: "j1", offeringKey: "o1", state: provider.StateReservable, capacity: intPtr(1), reason: "available",
	})))

	tx := begin(t, database)
	if _, err := availability.CloseExpired(context.Background(), tx, "expire-rollback", testNow.Add(time.Hour)); err != nil {
		_ = tx.Rollback()
		t.Fatalf("CloseExpired: %v", err)
	}
	rollback(t, tx)
	if count := countRows(t, database, `SELECT COUNT(*) FROM availability_episodes WHERE closed_at IS NULL`); count != 1 {
		t.Fatalf("open episodes after rollback = %d, want 1", count)
	}

	if _, err := availability.CloseExpired(context.Background(), nil, "expire-rollback", testNow); err == nil {
		t.Fatal("CloseExpired accepted a nil transaction")
	}
	tx = begin(t, database)
	defer rollback(t, tx)
	if _, err := availability.CloseExpired(context.Background(), tx, " ", testNow); err == nil {
		t.Fatal("CloseExpired accepted an empty monitor id")
	}
	if _, err := availability.CloseExpired(context.Background(), tx, "expire-rollback", time.Time{}); err == nil {
		t.Fatal("CloseExpired accepted a zero time")
	}
}

func insertMonitorProvider(t *testing.T, database *sql.DB, monitorID, providerID string, configVersion int64) int64 {
	t.Helper()
	now := testNow.Format(time.RFC3339Nano)
	result, err := database.Exec(`INSERT INTO monitor_providers
		(monitor_id, provider_id, active, origin_id, destination_id, config_version, created_at, updated_at)
		VALUES (?, ?, 1, ?, ?, ?, ?, ?)`,
		monitorID, providerID, "origin-"+providerID, "destination-"+providerID, configVersion, now, now)
	if err != nil {
		t.Fatalf("insert monitor provider: %v", err)
	}
	targetID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("monitor provider id: %v", err)
	}
	return targetID
}

func formatTestTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
