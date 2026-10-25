package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/am-fallah/neda/internal/migrations"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "neda-test.db")
	database, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func migrateReal(t *testing.T, database *DB) int {
	t.Helper()
	count, err := Migrate(context.Background(), database.DB, migrations.FS)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return count
}

func TestOpenAppliesPragmasOnInitialAndReplacementConnections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pragmas.db")
	database, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	assertPragmas(t, database.DB)
	if got := database.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1", got)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	replacement, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = replacement.Close() })
	assertPragmas(t, replacement.DB)
}

func assertPragmas(t *testing.T, database *sql.DB) {
	t.Helper()
	var foreignKeys int
	if err := database.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
	var journalMode string
	if err := database.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}
	var synchronous int
	if err := database.QueryRow(`PRAGMA synchronous`).Scan(&synchronous); err != nil {
		t.Fatalf("synchronous: %v", err)
	}
	if synchronous != 2 {
		t.Fatalf("synchronous = %d, want 2 (FULL)", synchronous)
	}
	var busyTimeout int
	if err := database.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatalf("busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", busyTimeout)
	}
	var autoCheckpoint int
	if err := database.QueryRow(`PRAGMA wal_autocheckpoint`).Scan(&autoCheckpoint); err != nil {
		t.Fatalf("wal_autocheckpoint: %v", err)
	}
	if autoCheckpoint != 1000 {
		t.Fatalf("wal_autocheckpoint = %d, want 1000", autoCheckpoint)
	}
	var journalLimit int
	if err := database.QueryRow(`PRAGMA journal_size_limit`).Scan(&journalLimit); err != nil {
		t.Fatalf("journal_size_limit: %v", err)
	}
	if journalLimit != 67108864 {
		t.Fatalf("journal_size_limit = %d, want 67108864", journalLimit)
	}
}

func TestMigrateAppliesAllAndIsIdempotent(t *testing.T) {
	database := openTestDB(t)
	if count := migrateReal(t, database); count != 5 {
		t.Fatalf("migration count = %d, want 5", count)
	}
	if err := database.Ready(context.Background(), 5); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if err := database.Ready(context.Background(), 6); err == nil {
		t.Fatal("Ready accepted an unexpected migration count")
	}

	var migrationsApplied int
	if err := database.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationsApplied); err != nil {
		t.Fatal(err)
	}
	if migrationsApplied != 5 {
		t.Fatalf("schema_migrations count = %d, want 5", migrationsApplied)
	}

	if count := migrateReal(t, database); count != 5 {
		t.Fatalf("second migration count = %d, want 5", count)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationsApplied); err != nil {
		t.Fatal(err)
	}
	if migrationsApplied != 5 {
		t.Fatalf("schema_migrations count after rerun = %d, want 5", migrationsApplied)
	}

	var integrity string
	if err := database.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatal(err)
	}
	if integrity != "ok" {
		t.Fatalf("integrity_check = %q, want ok", integrity)
	}
}

func TestMigrateRejectsChecksumMismatch(t *testing.T) {
	database := openTestDB(t)
	first := fstest.MapFS{
		"0001_first.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE a (id INTEGER);`)},
	}
	if _, err := Migrate(context.Background(), database.DB, first); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	changed := fstest.MapFS{
		"0001_first.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE a (id INTEGER, changed TEXT);`)},
	}
	_, err := Migrate(context.Background(), database.DB, changed)
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("error = %v, want checksum mismatch", err)
	}
}

func TestMigrateRollsBackFailingMigration(t *testing.T) {
	database := openTestDB(t)
	fsys := fstest.MapFS{
		"0001_ok.sql":  &fstest.MapFile{Data: []byte(`CREATE TABLE a (id INTEGER);`)},
		"0002_bad.sql": &fstest.MapFile{Data: []byte("CREATE TABLE b (id INTEGER);\nTHIS IS NOT SQL;")},
	}
	_, err := Migrate(context.Background(), database.DB, fsys)
	if err == nil {
		t.Fatal("Migrate succeeded with invalid SQL")
	}
	var applied int
	if err := database.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("schema_migrations count = %d, want 1", applied)
	}
	var exists int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='b'`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists != 0 {
		t.Fatal("table b exists after failed migration rollback")
	}
}

func TestMigrateRejectsDatabaseNewerThanBinary(t *testing.T) {
	database := openTestDB(t)
	first := fstest.MapFS{
		"0001_first.sql":  &fstest.MapFile{Data: []byte(`CREATE TABLE a (id INTEGER);`)},
		"0002_second.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE b (id INTEGER);`)},
	}
	if _, err := Migrate(context.Background(), database.DB, first); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	older := fstest.MapFS{
		"0001_first.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE a (id INTEGER);`)},
	}
	_, err := Migrate(context.Background(), database.DB, older)
	if err == nil || !strings.Contains(err.Error(), "not present") {
		t.Fatalf("error = %v, want newer-database error", err)
	}
}

func TestMigrateRejectsInvalidFilesystems(t *testing.T) {
	database := openTestDB(t)
	if _, err := Migrate(context.Background(), database.DB, nil); err == nil {
		t.Fatal("Migrate(nil) succeeded")
	}
	empty := fstest.MapFS{}
	if _, err := Migrate(context.Background(), database.DB, empty); err == nil {
		t.Fatal("Migrate(empty) succeeded")
	}
	invalid := fstest.MapFS{
		"bad.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE a (id INTEGER);`)},
	}
	if _, err := Migrate(context.Background(), database.DB, invalid); err == nil {
		t.Fatal("Migrate(invalid name) succeeded")
	}
}

func TestSchemaConstraints(t *testing.T) {
	database := openTestDB(t)
	migrateReal(t, database)

	mustExec(t, database.DB, `INSERT INTO monitors
		(id, label, origin_name, destination_name, departure_date, adults, children, infants, enabled, expires_at, created_at, updated_at)
		VALUES ('m1', 'Tehran to Mashhad', 'Tehran', 'Mashhad', '2026-10-30', 1, 0, 0, 1, '2026-10-30T20:30:00Z', '2026-10-23T00:00:00Z', '2026-10-23T00:00:00Z')`)
	mustExec(t, database.DB, `INSERT INTO monitor_providers
		(monitor_id, provider_id, origin_id, destination_id, created_at, updated_at)
		VALUES ('m1', 'fake', 'origin-1', 'destination-1', '2026-10-23T00:00:00Z', '2026-10-23T00:00:00Z')`)

	mustFail(t, database.DB, `INSERT INTO settings(key, value, is_secret, updated_at) VALUES ('x', 'y', 2, '2026-10-23T00:00:00Z')`)
	mustFail(t, database.DB, `UPDATE monitors SET enabled = 2 WHERE id = 'm1'`)
	mustFail(t, database.DB, `INSERT INTO monitors
		(id, label, origin_name, destination_name, departure_date, adults, children, infants, enabled, expires_at, created_at, updated_at)
		VALUES ('m2', 'bad date', 'A', 'B', '30-10-2026', 1, 0, 0, 1, '2026-10-30T20:30:00Z', 'now', 'now')`)
	mustFail(t, database.DB, `INSERT INTO monitor_providers
		(monitor_id, provider_id, origin_id, destination_id, created_at, updated_at)
		VALUES ('missing-monitor', 'fake', 'a', 'b', 'now', 'now')`)
	mustFail(t, database.DB, `UPDATE monitor_providers SET consecutive_failures = -1 WHERE monitor_id = 'm1'`)
	mustFail(t, database.DB, `INSERT INTO check_runs
		(monitor_provider_id, config_version, started_at, outcome, created_at)
		VALUES (1, 1, 'now', 'success', 'now')`)
	mustFail(t, database.DB, `INSERT INTO check_runs
		(monitor_provider_id, config_version, started_at, finished_at, outcome, created_at)
		VALUES (1, 1, 'now', 'now', 'running', 'now')`)

	mustExec(t, database.DB, `INSERT INTO check_runs
		(monitor_provider_id, config_version, started_at, outcome, created_at)
		VALUES (1, 1, 'now', 'running', 'now')`)
	mustExec(t, database.DB, `UPDATE check_runs SET outcome='success', finished_at='now' WHERE id=1`)

	mustExec(t, database.DB, `INSERT INTO inventory_states
		(monitor_provider_id, config_version, journey_key, offering_key, state, state_since, last_observed_at, updated_at)
		VALUES (1, 1, 'journey', 'offering', 'reservable', 'now', 'now', 'now')`)
	mustExec(t, database.DB, `INSERT INTO availability_episodes
		(state_id, episode_seq, opened_at) VALUES (1, 1, 'now')`)
	mustFail(t, database.DB, `INSERT INTO availability_episodes
		(state_id, episode_seq, opened_at) VALUES (1, 2, 'now')`)
	mustFail(t, database.DB, `UPDATE availability_episodes SET close_reason='bad' WHERE id=1`)

	mustExec(t, database.DB, `INSERT INTO events
		(id, kind, dedupe_key, monitor_id, provider_id, episode_id, occurred_at, created_at)
		VALUES ('e1', 'availability', 'availability:episode:1', 'm1', 'fake', 1, 'now', 'now')`)
	mustFail(t, database.DB, `INSERT INTO events
		(id, kind, dedupe_key, provider_id, occurred_at, created_at)
		VALUES ('e2', 'availability', 'availability:episode:2', 'fake', 'now', 'now')`)
	mustFail(t, database.DB, `INSERT INTO events
		(id, kind, dedupe_key, provider_id, occurred_at, created_at)
		VALUES ('e3', 'provider_degraded', 'health:fake:1:degraded', 'fake', 'now', 'now')`)
	mustExec(t, database.DB, `INSERT INTO events
		(id, kind, dedupe_key, provider_id, occurred_at, error_kind, failure_count, created_at)
		VALUES ('e4', 'provider_degraded', 'health:fake:1:degraded', 'fake', 'now', 'timeout', 3, 'now')`)

	var violations int
	if err := database.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil {
		t.Fatal(err)
	}
	if violations != 0 {
		t.Fatalf("foreign key violations = %d, want 0", violations)
	}
}

func TestNotificationSchemaConstraints(t *testing.T) {
	database := openTestDB(t)
	migrateReal(t, database)
	const now = "2026-10-24T10:00:00Z"

	mustExec(t, database.DB, `INSERT INTO provider_health
		(provider_id, consecutive_failures, updated_at) VALUES ('fake', 0, ?)`, now)
	mustFail(t, database.DB, `UPDATE provider_health SET consecutive_failures = -1 WHERE provider_id = 'fake'`)
	mustExec(t, database.DB, `UPDATE provider_health SET last_attempt_at = ?, last_success_at = ?,
		last_failure_at = ?, last_error_kind = 'timeout', last_duration_ms = 25,
		open_episode_id = NULL, updated_at = ? WHERE provider_id = 'fake'`, now, now, now, now)

	mustExec(t, database.DB, `INSERT INTO provider_health_episodes
		(provider_id, opened_at, failure_count_at_open) VALUES ('fake', ?, 3)`, now)
	mustFail(t, database.DB, `INSERT INTO provider_health_episodes
		(provider_id, opened_at, failure_count_at_open) VALUES ('fake', ?, 0)`, now)
	mustFail(t, database.DB, `INSERT INTO provider_health_episodes
		(provider_id, opened_at, failure_count_at_open) VALUES ('fake', ?, 3)`, now)
	mustExec(t, database.DB, `UPDATE provider_health_episodes SET closed_at = ?, last_error_kind = 'timeout'
		WHERE provider_id = 'fake' AND closed_at IS NULL`, now)
	mustExec(t, database.DB, `INSERT INTO provider_health_episodes
		(provider_id, opened_at, failure_count_at_open) VALUES ('fake', ?, 3)`, now)

	mustExec(t, database.DB, `INSERT INTO events
		(id, kind, dedupe_key, provider_id, occurred_at, error_kind, failure_count, created_at)
		VALUES ('e-health', 'provider_degraded', 'health:fake:1:degraded', 'fake', ?, 'timeout', 3, ?)`,
		now, now)
	mustExec(t, database.DB, `INSERT INTO notification_deliveries
		(id, event_id, channel, target_key, status, next_attempt_at, created_at, updated_at)
		VALUES ('d1', 'e-health', 'telegram', 'telegram', 'pending', ?, ?, ?)`, now, now, now)
	mustFail(t, database.DB, `INSERT INTO notification_deliveries
		(id, event_id, channel, target_key, status, next_attempt_at, created_at, updated_at)
		VALUES ('d-bad-channel', 'e-health', 'sms', 'telegram', 'pending', ?, ?, ?)`, now, now, now)
	mustFail(t, database.DB, `INSERT INTO notification_deliveries
		(id, event_id, channel, target_key, status, next_attempt_at, created_at, updated_at)
		VALUES ('d-bad-status', 'e-health', 'push', 'push:sub', 'queued', ?, ?, ?)`, now, now, now)
	mustFail(t, database.DB, `INSERT INTO notification_deliveries
		(id, event_id, channel, target_key, status, attempt_count, next_attempt_at, created_at, updated_at)
		VALUES ('d-bad-count', 'e-health', 'push', 'push:sub-count', 'pending', -1, ?, ?, ?)`, now, now, now)
	mustFail(t, database.DB, `INSERT INTO notification_deliveries
		(id, event_id, channel, target_key, status, next_attempt_at, created_at, updated_at)
		VALUES ('d-duplicate-target', 'e-health', 'telegram', 'telegram', 'pending', ?, ?, ?)`, now, now, now)
	mustFail(t, database.DB, `INSERT INTO notification_deliveries
		(id, event_id, channel, target_key, status, next_attempt_at, created_at, updated_at)
		VALUES ('d-missing-event', 'missing-event', 'push', 'push:sub-missing', 'pending', ?, ?, ?)`, now, now, now)

	mustExec(t, database.DB, `INSERT INTO notification_attempts
		(delivery_id, attempt_no, started_at, status) VALUES ('d1', 1, ?, 'sent')`, now)
	mustFail(t, database.DB, `INSERT INTO notification_attempts
		(delivery_id, attempt_no, started_at, status) VALUES ('d1', 0, ?, 'sent')`, now)
	mustFail(t, database.DB, `INSERT INTO notification_attempts
		(delivery_id, attempt_no, started_at, status) VALUES ('d1', 2, ?, 'pending')`, now)
	mustFail(t, database.DB, `INSERT INTO notification_attempts
		(delivery_id, attempt_no, started_at, status) VALUES ('d1', 1, ?, 'failed')`, now)
	mustFail(t, database.DB, `INSERT INTO notification_attempts
		(delivery_id, attempt_no, started_at, status) VALUES ('missing-delivery', 1, ?, 'sent')`, now)

	mustExec(t, database.DB, `INSERT INTO push_subscriptions
		(id, endpoint_hash, endpoint_enc, p256dh_enc, auth_enc, created_at)
		VALUES ('p1', 'hash-1', X'01', X'02', X'03', ?)`, now)
	mustFail(t, database.DB, `INSERT INTO push_subscriptions
		(id, endpoint_hash, endpoint_enc, p256dh_enc, auth_enc, created_at)
		VALUES ('p-duplicate', 'hash-1', X'01', X'02', X'03', ?)`, now)
	mustFail(t, database.DB, `INSERT INTO push_subscriptions
		(id, endpoint_hash, endpoint_enc, p256dh_enc, auth_enc, failure_count, created_at)
		VALUES ('p-bad-count', 'hash-2', X'01', X'02', X'03', -1, ?)`, now)
	mustFail(t, database.DB, `INSERT INTO push_subscriptions
		(id, endpoint_hash, endpoint_enc, p256dh_enc, auth_enc, created_at)
		VALUES ('p-null-blob', 'hash-3', NULL, X'02', X'03', ?)`, now)

	mustExec(t, database.DB, `DELETE FROM notification_deliveries WHERE id = 'd1'`)
	var attempts int
	if err := database.QueryRow(`SELECT COUNT(*) FROM notification_attempts`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 0 {
		t.Fatalf("attempts after delivery delete = %d, want 0", attempts)
	}

	mustExec(t, database.DB, `INSERT INTO notification_deliveries
		(id, event_id, channel, target_key, status, next_attempt_at, created_at, updated_at)
		VALUES ('d2', 'e-health', 'push', 'push:sub-2', 'pending', ?, ?, ?)`, now, now, now)
	mustExec(t, database.DB, `DELETE FROM events WHERE id = 'e-health'`)
	var deliveries int
	if err := database.QueryRow(`SELECT COUNT(*) FROM notification_deliveries`).Scan(&deliveries); err != nil {
		t.Fatal(err)
	}
	if deliveries != 0 {
		t.Fatalf("deliveries after event delete = %d, want 0", deliveries)
	}

	var violations int
	if err := database.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil {
		t.Fatal(err)
	}
	if violations != 0 {
		t.Fatalf("foreign key violations = %d, want 0", violations)
	}
}

func mustExec(t *testing.T, database *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := database.Exec(query, args...); err != nil {
		t.Fatalf("exec failed: %v\nquery: %s", err, query)
	}
}

func mustFail(t *testing.T, database *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := database.Exec(query, args...)
	if err == nil {
		t.Fatalf("exec succeeded, want failure\nquery: %s", query)
	}
	if errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unexpected ErrNoRows: %v", err)
	}
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	if _, err := Open(context.Background(), " "); err == nil {
		t.Fatal("Open(empty) succeeded")
	}
}

func TestOpenAcceptsRelativePath(t *testing.T) {
	t.Chdir(t.TempDir())
	database, err := Open(context.Background(), "./data/neda.db")
	if err != nil {
		t.Fatalf("Open(relative): %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := Migrate(context.Background(), database.DB, migrations.FS); err != nil {
		t.Fatalf("Migrate(relative): %v", err)
	}
	if err := database.Ready(context.Background(), 5); err != nil {
		t.Fatalf("Ready(relative): %v", err)
	}
}
