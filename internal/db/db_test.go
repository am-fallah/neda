package db

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
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

func TestForeignKeyRebuildMigrationPreservesChildRows(t *testing.T) {
	database := openTestDB(t)
	fsys := fstest.MapFS{
		"0001_parent.sql": &fstest.MapFile{Data: []byte(`
CREATE TABLE events (id TEXT PRIMARY KEY, kind TEXT NOT NULL);
CREATE TABLE notification_deliveries (
	id TEXT PRIMARY KEY,
	event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE
);
INSERT INTO events VALUES ('e1', 'availability');
INSERT INTO notification_deliveries VALUES ('d1', 'e1');
`)},
		"0002_rebuild.sql": &fstest.MapFile{Data: []byte(`
-- neda:foreign-keys-off
PRAGMA legacy_alter_table = ON;
ALTER TABLE events RENAME TO events_old;
CREATE TABLE events (id TEXT PRIMARY KEY, kind TEXT NOT NULL, silent INTEGER NOT NULL DEFAULT 0);
INSERT INTO events (id, kind) SELECT id, kind FROM events_old;
DROP TABLE events_old;
PRAGMA legacy_alter_table = OFF;
`)},
	}
	if _, err := Migrate(context.Background(), database.DB, fsys); err != nil {
		t.Fatalf("Migrate rebuild: %v", err)
	}
	var children int
	if err := database.QueryRow(`SELECT COUNT(*) FROM notification_deliveries`).Scan(&children); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if children != 1 {
		t.Fatalf("delivery rows = %d, want 1 preserved through rebuild", children)
	}
	var foreignKeys int
	if err := database.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d after rebuild migration, want 1", foreignKeys)
	}
	if _, err := database.Exec(`INSERT INTO events VALUES ('e2', 'unavailable', 1)`); err != nil {
		t.Fatalf("insert rebuilt event: %v", err)
	}
}

func TestMigrateAppliesAllAndIsIdempotent(t *testing.T) {
	database := openTestDB(t)
	if count := migrateReal(t, database); count != 11 {
		t.Fatalf("migration count = %d, want 11", count)
	}
	if err := database.Ready(context.Background(), 11); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if err := database.Ready(context.Background(), 7); err == nil {
		t.Fatal("Ready accepted an unexpected migration count")
	}

	var migrationsApplied int
	if err := database.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationsApplied); err != nil {
		t.Fatal(err)
	}
	if migrationsApplied != 11 {
		t.Fatalf("schema_migrations count = %d, want 11", migrationsApplied)
	}

	if count := migrateReal(t, database); count != 11 {
		t.Fatalf("second migration count = %d, want 11", count)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationsApplied); err != nil {
		t.Fatal(err)
	}
	if migrationsApplied != 11 {
		t.Fatalf("schema_migrations count after rerun = %d, want 11", migrationsApplied)
	}

	var integrity string
	if err := database.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatal(err)
	}
	if integrity != "ok" {
		t.Fatalf("integrity_check = %q, want ok", integrity)
	}
}

func TestProviderRateLimitMigrationPreservesExistingData(t *testing.T) {
	database := openTestDB(t)
	providerRateLimitSQL, err := fs.ReadFile(migrations.FS, "0008_provider_rate_limits.sql")
	if err != nil {
		t.Fatalf("read 0008 migration: %v", err)
	}
	fsys := fstest.MapFS{
		"0001_existing.sql": &fstest.MapFile{Data: []byte(`
CREATE TABLE settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	is_secret INTEGER NOT NULL DEFAULT 0,
	updated_at TEXT NOT NULL
);
INSERT INTO settings VALUES ('poll_interval_seconds', '300', 0, '2026-01-01T00:00:00Z');
`)},
		"0008_provider_rate_limits.sql": &fstest.MapFile{Data: providerRateLimitSQL},
	}
	if count, err := Migrate(context.Background(), database.DB, fsys); err != nil || count != 2 {
		t.Fatalf("Migrate = %d, %v; want 2, nil", count, err)
	}
	var interval string
	if err := database.QueryRow(`SELECT value FROM settings WHERE key = 'poll_interval_seconds'`).Scan(&interval); err != nil {
		t.Fatalf("existing setting after migration: %v", err)
	}
	if interval != "300" {
		t.Fatalf("existing setting = %q, want 300", interval)
	}
	if _, err := database.Exec(`INSERT INTO provider_rate_limits
		(provider_id, scope, adaptive_interval_ms, updated_at)
		VALUES ('p', 'provider', 600000, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert migrated rate limit: %v", err)
	}
}

func migrationsThrough(t *testing.T, last string) fstest.MapFS {
	t.Helper()
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	result := fstest.MapFS{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") || name > last {
			continue
		}
		data, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		result[name] = &fstest.MapFile{Data: data}
	}
	return result
}

func TestObservabilityMigrationsPreserveExistingCheckRuns(t *testing.T) {
	database := openTestDB(t)
	if count, err := Migrate(context.Background(), database.DB, migrationsThrough(t, "0008_provider_rate_limits.sql")); err != nil || count != 8 {
		t.Fatalf("migrate to 0008 = %d, %v", count, err)
	}
	mustExec(t, database.DB, `INSERT INTO monitors
		(id, label, origin_name, destination_name, departure_date, adults, children, infants, enabled, expires_at, created_at, updated_at)
		VALUES ('m1', 'Existing', 'Tehran', 'Mashhad', '2026-12-15', 1, 0, 0, 1, '2026-12-16T00:00:00Z', '2026-11-06T00:00:00Z', '2026-11-06T00:00:00Z')`)
	mustExec(t, database.DB, `INSERT INTO monitor_providers
		(monitor_id, provider_id, active, origin_id, destination_id, config_version, next_due_at, created_at, updated_at)
		VALUES ('m1', 'fake', 1, 'o', 'd', 1, '2026-11-06T00:00:00Z', '2026-11-06T00:00:00Z', '2026-11-06T00:00:00Z')`)
	mustExec(t, database.DB, `INSERT INTO check_runs
		(monitor_provider_id, config_version, started_at, finished_at, outcome, duration_ms, created_at)
		VALUES (1, 1, '2026-11-06T00:00:00Z', '2026-11-06T00:00:01Z', 'success', 1000, '2026-11-06T00:00:00Z')`)

	if count, err := Migrate(context.Background(), database.DB, migrations.FS); err != nil || count != 11 {
		t.Fatalf("migrate to current = %d, %v", count, err)
	}
	var providerRequest int
	var providerStarted, duration, errorKind, queueWait sql.NullString
	if err := database.QueryRow(`SELECT provider_request, provider_started_at, provider_duration_ms,
		provider_error_kind, queue_wait_ms FROM check_runs WHERE id = 1`).
		Scan(&providerRequest, &providerStarted, &duration, &errorKind, &queueWait); err != nil {
		t.Fatalf("read migrated check: %v", err)
	}
	if providerRequest != 0 || providerStarted.Valid || duration.Valid || errorKind.Valid || queueWait.Valid {
		t.Fatalf("existing check gained provider metrics: %d/%v/%v/%v/%v",
			providerRequest, providerStarted, duration, errorKind, queueWait)
	}
	var outcome string
	if err := database.QueryRow(`SELECT outcome FROM check_runs WHERE id = 1`).Scan(&outcome); err != nil || outcome != "success" {
		t.Fatalf("existing outcome = %q, %v", outcome, err)
	}

	for _, table := range []string{"provider_minute_metrics", "provider_queue_samples", "provider_deferral_episodes", "provider_rate_limit_events"} {
		var exists int
		if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists != 1 {
			t.Fatalf("table %s missing", table)
		}
	}
	for _, index := range []string{
		"idx_check_runs_provider_started",
		"idx_check_runs_provider_target_started",
		"idx_check_runs_provider_errors",
		"idx_check_runs_incomplete",
		"idx_provider_minute_metrics_provider",
		"idx_provider_minute_metrics_time",
		"idx_provider_queue_samples_minute",
		"idx_provider_queue_samples_provider",
		"idx_provider_deferral_episodes_provider_time",
		"idx_provider_deferral_episodes_started",
		"idx_provider_deferral_episodes_active",
		"idx_provider_rate_limit_events_provider",
		"idx_provider_rate_limit_events_time",
	} {
		var exists int
		if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, index).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists != 1 {
			t.Fatalf("index %s missing", index)
		}
	}

	now := "2026-11-06T00:00:00Z"
	mustExec(t, database.DB, `INSERT INTO provider_rate_limit_events
		(provider_id, occurred_at, kind, previous_level, escalation_level, previous_adaptive_ms,
		 adaptive_ms, retry_after_ms, check_run_id, created_at)
		VALUES ('fake', ?, 'rate_limited', 1, 2, 0, 600000, 0, 1, ?)`, now, now)
	mustExec(t, database.DB, `DELETE FROM check_runs WHERE id = 1`)
	var checkRunID sql.NullInt64
	if err := database.QueryRow(`SELECT check_run_id FROM provider_rate_limit_events`).Scan(&checkRunID); err != nil {
		t.Fatal(err)
	}
	if checkRunID.Valid {
		t.Fatalf("event check_run_id = %d after delete, want NULL", checkRunID.Int64)
	}

	mustExec(t, database.DB, `INSERT INTO provider_deferral_episodes
		(id, provider_id, target_id, reason, started_at, last_seen_at, created_at)
		VALUES ('e1', 'fake', 1, 'cooldown', ?, ?, ?)`, now, now, now)
	mustExec(t, database.DB, `DELETE FROM monitors WHERE id = 'm1'`)
	var targetID sql.NullInt64
	if err := database.QueryRow(`SELECT target_id FROM provider_deferral_episodes WHERE id='e1'`).Scan(&targetID); err != nil {
		t.Fatal(err)
	}
	if targetID.Valid {
		t.Fatalf("episode target_id = %d after delete, want NULL", targetID.Int64)
	}

	mustFail(t, database.DB, `INSERT INTO provider_rate_limit_events
		(provider_id, occurred_at, kind, escalation_level, previous_adaptive_ms, adaptive_ms, created_at)
		VALUES ('fake', ?, 'escalated', 1, 0, 0, ?)`, now, now)
	mustFail(t, database.DB, `INSERT INTO provider_queue_samples
		(id, provider_id, sampled_at, minute_start, due, created_at)
		VALUES ('bad', 'fake', ?, ?, -1, ?)`, now, now, now)
	mustFail(t, database.DB, `INSERT INTO provider_deferral_episodes
		(id, provider_id, reason, started_at, last_seen_at, created_at)
		VALUES ('bad', 'fake', 'unknown', ?, ?, ?)`, now, now, now)
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
	if err := database.Ready(context.Background(), 11); err != nil {
		t.Fatalf("Ready(relative): %v", err)
	}
}

func TestOpenReadOnlyRejectsWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "readonly.db")
	database, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := Migrate(context.Background(), database.DB, migrations.FS); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	readOnly, err := OpenReadOnly(context.Background(), path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	t.Cleanup(func() { _ = readOnly.Close() })

	var queryOnly int
	if err := readOnly.QueryRow(`PRAGMA query_only`).Scan(&queryOnly); err != nil {
		t.Fatalf("PRAGMA query_only: %v", err)
	}
	if queryOnly != 1 {
		t.Fatalf("query_only = %d, want 1", queryOnly)
	}
	var count int
	if err := readOnly.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("read query: %v", err)
	}
	if count != 11 {
		t.Fatalf("migration count via read handle = %d, want 11", count)
	}
	if _, err := readOnly.Exec(`INSERT INTO settings(key, value, is_secret, updated_at) VALUES ('x','y',0,'2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("read-only connection accepted a write")
	}
}
