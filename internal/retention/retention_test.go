package retention

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/migrations"
)

var testNow = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

type testEnv struct {
	db     *sql.DB
	worker *Worker
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "retention.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if _, err := db.Migrate(ctx, database.DB, migrations.FS); err != nil {
		_ = database.Close()
		t.Fatalf("db.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	worker, err := New(database.DB, Config{
		Now:          func() time.Time { return testNow },
		InitialDelay: 0,
		Interval:     24 * time.Hour,
		Logger:       testLogger(t),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &testEnv{db: database.DB, worker: worker}
}

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func seedRetentionFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	now := testNow
	old := now.Add(-200 * 24 * time.Hour)
	oldCheck := now.Add(-15 * 24 * time.Hour)
	oldAttempt := now.Add(-31 * 24 * time.Hour)
	oldDisabled := now.Add(-31 * 24 * time.Hour)
	recent := now.Add(-24 * time.Hour)
	expired := now.Add(-time.Hour)
	active := now.Add(24 * time.Hour)
	revokedOld := now.Add(-31 * 24 * time.Hour)
	revokedRecent := now.Add(-24 * time.Hour)

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	}
	exec(`INSERT INTO monitors
		(id, label, origin_name, destination_name, departure_date, adults, children, infants, enabled, expires_at, created_at, updated_at)
		VALUES ('m1', 'Test', 'A', 'B', '2027-01-01', 1, 0, 0, 1, ?, ?, ?)`, format(active), format(old), format(old))
	exec(`INSERT INTO monitor_providers
		(id, monitor_id, provider_id, active, origin_id, destination_id, config_version, created_at, updated_at)
		VALUES (1, 'm1', 'fake', 1, '1', '2', 1, ?, ?)`, format(old), format(old))

	// States/episodes: open, old-with-terminal-event, old-with-pending-event, new.
	exec(`INSERT INTO inventory_states
		(id, monitor_provider_id, config_version, journey_key, offering_key, state, state_since, last_observed_at, updated_at)
		VALUES
		(1, 1, 1, 'j1', 'o1', 'reservable', ?, ?, ?),
		(2, 1, 1, 'j2', 'o2', 'reservable', ?, ?, ?),
		(3, 1, 1, 'j3', 'o3', 'reservable', ?, ?, ?),
		(4, 1, 1, 'j4', 'o4', 'reservable', ?, ?, ?)`,
		format(old), format(old), format(old),
		format(old), format(old), format(old),
		format(old), format(old), format(old),
		format(recent), format(recent), format(recent))
	exec(`INSERT INTO availability_episodes (id, state_id, episode_seq, opened_at, closed_at, close_reason) VALUES
		(1, 1, 1, ?, NULL, NULL),
		(2, 2, 1, ?, ?, 'not_reservable'),
		(3, 3, 1, ?, ?, 'not_reservable'),
		(4, 4, 1, ?, ?, 'not_reservable')`,
		format(old), format(old), format(old), format(old), format(old), format(old), format(recent), format(recent))

	exec(`INSERT INTO events (id, kind, dedupe_key, monitor_id, provider_id, episode_id, occurred_at, created_at) VALUES
		('ev-old', 'availability', 'dedupe-old', 'm1', 'fake', 2, ?, ?),
		('ev-pending', 'availability', 'dedupe-pending', 'm1', 'fake', 3, ?, ?)`,
		format(old), format(old), format(old), format(old))
	exec(`INSERT INTO notification_deliveries
		(id, event_id, channel, target_key, status, attempt_count, next_attempt_at, created_at, updated_at)
		VALUES
		('d-old', 'ev-old', 'telegram', 'telegram', 'sent', 1, NULL, ?, ?),
		('d-pending', 'ev-pending', 'telegram', 'telegram', 'pending', 0, ?, ?, ?)`,
		format(old), format(old), format(recent), format(old), format(old))
	exec(`INSERT INTO notification_attempts (delivery_id, attempt_no, started_at, finished_at, status) VALUES
		('d-old', 1, ?, ?, 'sent'),
		('d-pending', 1, ?, ?, 'sent')`,
		format(oldAttempt), format(oldAttempt), format(recent), format(recent))

	exec(`INSERT INTO check_runs
		(id, monitor_provider_id, config_version, started_at, finished_at, outcome, created_at)
		VALUES
		(1, 1, 1, ?, ?, 'success', ?),
		(2, 1, 1, ?, NULL, 'running', ?),
		(3, 1, 1, ?, ?, 'success', ?)`,
		format(oldCheck), format(oldCheck), format(oldCheck),
		format(oldCheck), format(oldCheck),
		format(recent), format(recent), format(recent))

	exec(`INSERT INTO provider_health_episodes (id, provider_id, opened_at, closed_at, failure_count_at_open) VALUES
		(1, 'fake', ?, ?, 3),
		(2, 'fake', ?, NULL, 3),
		(3, 'fake', ?, ?, 3)`,
		format(old), format(old), format(old), format(old), format(old))
	exec(`INSERT INTO provider_health (provider_id, last_attempt_at, consecutive_failures, open_episode_id, updated_at) VALUES
		('fake', ?, 0, 2, ?)`, format(recent), format(recent))

	exec(`INSERT INTO sessions (id, token_hash, csrf_hash, created_at, last_seen_at, expires_at, revoked_at) VALUES
		('s-expired', X'01', X'02', ?, ?, ?, NULL),
		('s-revoked-old', X'03', X'04', ?, ?, ?, ?),
		('s-revoked-recent', X'05', X'06', ?, ?, ?, ?),
		('s-active', X'07', X'08', ?, ?, ?, NULL)`,
		format(old), format(old), format(expired),
		format(old), format(old), format(active), format(revokedOld),
		format(old), format(old), format(active), format(revokedRecent),
		format(old), format(old), format(active))

	exec(`INSERT INTO push_subscriptions (id, endpoint_hash, endpoint_enc, p256dh_enc, auth_enc, created_at, failure_count, disabled_at) VALUES
		('p-old', 'hash-old', X'01', X'02', X'03', ?, 1, ?),
		('p-recent', 'hash-recent', X'04', X'05', X'06', ?, 1, ?),
		('p-active', 'hash-active', X'07', X'08', X'09', ?, 0, NULL)`,
		format(old), format(oldDisabled), format(old), format(recent), format(old), format(old))
}

func TestRunOncePrunesInDeterministicOrderAndPreservesOpenOrPendingData(t *testing.T) {
	env := newTestEnv(t)
	seedRetentionFixture(t, env.db)
	ctx := context.Background()

	summary, err := env.worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	want := Summary{
		Attempts:             1,
		CheckRuns:            1,
		Events:               1,
		HealthEpisodes:       2,
		AvailabilityEpisodes: 1,
		InventoryStates:      1,
		Sessions:             2,
		PushSubscriptions:    1,
	}
	if summary != want {
		t.Fatalf("summary = %+v, want %+v", summary, want)
	}

	assertCount(t, env.db, `SELECT COUNT(*) FROM check_runs`, 2) // running + recent
	assertCount(t, env.db, `SELECT COUNT(*) FROM check_runs WHERE id = 2 AND outcome = 'running'`, 1)
	assertCount(t, env.db, `SELECT COUNT(*) FROM events`, 1)
	assertCount(t, env.db, `SELECT COUNT(*) FROM events WHERE id = 'ev-pending'`, 1)
	assertCount(t, env.db, `SELECT COUNT(*) FROM notification_deliveries`, 1)
	assertCount(t, env.db, `SELECT COUNT(*) FROM notification_deliveries WHERE id = 'd-pending' AND status = 'pending'`, 1)
	assertCount(t, env.db, `SELECT COUNT(*) FROM notification_attempts`, 1)
	assertCount(t, env.db, `SELECT COUNT(*) FROM notification_attempts WHERE delivery_id = 'd-pending'`, 1)
	assertCount(t, env.db, `SELECT COUNT(*) FROM availability_episodes`, 3)
	assertCount(t, env.db, `SELECT COUNT(*) FROM availability_episodes WHERE id = 1 AND closed_at IS NULL`, 1)
	assertCount(t, env.db, `SELECT COUNT(*) FROM availability_episodes WHERE id = 3`, 1)
	assertCount(t, env.db, `SELECT COUNT(*) FROM inventory_states`, 3)
	assertCount(t, env.db, `SELECT COUNT(*) FROM inventory_states WHERE id IN (1, 3, 4)`, 3)
	assertCount(t, env.db, `SELECT COUNT(*) FROM provider_health_episodes`, 1)
	assertCount(t, env.db, `SELECT COUNT(*) FROM provider_health_episodes WHERE closed_at IS NULL`, 1)
	assertCount(t, env.db, `SELECT COUNT(*) FROM provider_health WHERE open_episode_id = 2`, 1)
	assertCount(t, env.db, `SELECT COUNT(*) FROM sessions`, 2)
	assertCount(t, env.db, `SELECT COUNT(*) FROM sessions WHERE id IN ('s-revoked-recent', 's-active')`, 2)
	assertCount(t, env.db, `SELECT COUNT(*) FROM push_subscriptions`, 2)
	assertCount(t, env.db, `SELECT COUNT(*) FROM push_subscriptions WHERE id IN ('p-recent', 'p-active')`, 2)

	assertForeignKeyCheck(t, env.db)

	// A second run is idempotent and removes nothing.
	second, err := env.worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	if second != (Summary{}) {
		t.Fatalf("second summary = %+v, want zero", second)
	}
}

func TestRunOnceRollsBackWhenForeignKeyCheckFails(t *testing.T) {
	env := newTestEnv(t)
	seedRetentionFixture(t, env.db)
	ctx := context.Background()

	conn, err := env.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO notification_attempts
		(delivery_id, attempt_no, started_at, status) VALUES ('missing-delivery', 1, ?, 'failed')`, format(testNow.Add(-time.Hour))); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := env.worker.RunOnce(ctx); err == nil {
		t.Fatal("RunOnce accepted a foreign-key violation")
	}
	// Nothing from the would-have-been cleanup may be committed.
	assertCount(t, env.db, `SELECT COUNT(*) FROM events`, 2)
	assertCount(t, env.db, `SELECT COUNT(*) FROM check_runs`, 3)
	assertCount(t, env.db, `SELECT COUNT(*) FROM sessions`, 4)
}

func TestRunOnceRejectsCanceledContext(t *testing.T) {
	env := newTestEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := env.worker.RunOnce(ctx); err == nil {
		t.Fatal("canceled context accepted")
	}
}

func assertCount(t *testing.T, db *sql.DB, query string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("%q = %d, want %d", query, got, want)
	}
}

func assertForeignKeyCheck(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key violation after retention")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
