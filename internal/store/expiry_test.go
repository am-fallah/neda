package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/domain"
)

func TestReconcileExpirationsMarksDueAndClosesEpisodes(t *testing.T) {
	st, sqlDB := newTestStore(t)
	ctx := context.Background()
	now := testNow
	past := now.Add(-time.Minute)

	exact := createScheduleMonitor(t, st, "expiry-exact", true, now, nil,
		scheduleTargetSpec{providerID: "p-exact", active: true, due: &past})
	paused := createScheduleMonitor(t, st, "expiry-paused", false, now.Add(-time.Hour), nil,
		scheduleTargetSpec{providerID: "p-paused", active: true, due: &past})
	subsecond := createScheduleMonitor(t, st, "expiry-subsecond", true, now.Add(500*time.Millisecond), nil,
		scheduleTargetSpec{providerID: "p-subsecond", active: true, due: &past})
	future := createScheduleMonitor(t, st, "expiry-future", true, now.Add(time.Hour), nil,
		scheduleTargetSpec{providerID: "p-future", active: true, due: &past})

	exactEpisode := insertOpenEpisode(t, sqlDB, targetID(t, exact, "p-exact"), 1)
	pausedEpisode := insertOpenEpisode(t, sqlDB, targetID(t, paused, "p-paused"), 1)
	subsecondEpisode := insertOpenEpisode(t, sqlDB, targetID(t, subsecond, "p-subsecond"), 1)
	futureEpisode := insertOpenEpisode(t, sqlDB, targetID(t, future, "p-future"), 1)

	exactTargetID := targetID(t, exact, "p-exact")
	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO check_runs
		(monitor_provider_id, config_version, started_at, outcome, created_at)
		VALUES (?, 1, ?, 'running', ?)`, exactTargetID, formatTestTime(now.Add(-time.Minute)), formatTestTime(now.Add(-time.Minute))); err != nil {
		t.Fatalf("insert running check: %v", err)
	}

	expired, next, err := st.ReconcileExpirations(ctx, now)
	if err != nil {
		t.Fatalf("ReconcileExpirations: %v", err)
	}
	if expired != 2 {
		t.Fatalf("expired = %d, want 2", expired)
	}
	if next == nil || !next.Equal(now.Add(500*time.Millisecond)) {
		t.Fatalf("next expiry = %v, want %v", next, now.Add(500*time.Millisecond))
	}

	for _, monitor := range []domain.Monitor{exact, paused} {
		reloaded, err := st.GetMonitor(ctx, monitor.ID)
		if err != nil {
			t.Fatalf("GetMonitor(%s): %v", monitor.ID, err)
		}
		if reloaded.ExpiredAt == nil || !reloaded.ExpiredAt.Equal(now) {
			t.Fatalf("%s ExpiredAt = %v, want %v", monitor.ID, reloaded.ExpiredAt, now)
		}
		if reloaded.Status(now) != domain.StatusExpired {
			t.Fatalf("%s status = %s, want expired", monitor.ID, reloaded.Status(now))
		}
		if reloaded.Providers[0].NextDueAt == nil || !reloaded.Providers[0].NextDueAt.Equal(past) {
			t.Fatalf("%s next_due_at changed to %v", monitor.ID, reloaded.Providers[0].NextDueAt)
		}
		if !reloaded.Providers[0].UpdatedAt.Equal(monitor.Providers[0].UpdatedAt) {
			t.Fatalf("%s target updated_at changed from %v to %v", monitor.ID, monitor.Providers[0].UpdatedAt, reloaded.Providers[0].UpdatedAt)
		}
	}
	assertClosedEpisode(t, sqlDB, exactEpisode, "expired")
	assertClosedEpisode(t, sqlDB, pausedEpisode, "expired")
	var closedAt sql.NullString
	if err := sqlDB.QueryRowContext(ctx, `SELECT closed_at FROM availability_episodes WHERE id = ?`, exactEpisode).Scan(&closedAt); err != nil {
		t.Fatalf("read exact episode close time: %v", err)
	}
	if !closedAt.Valid || closedAt.String != formatTestTime(now) {
		t.Fatalf("exact episode closed_at = %v, want %s", closedAt, formatTestTime(now))
	}

	for _, monitor := range []domain.Monitor{subsecond, future} {
		reloaded, err := st.GetMonitor(ctx, monitor.ID)
		if err != nil {
			t.Fatalf("GetMonitor(%s): %v", monitor.ID, err)
		}
		if reloaded.ExpiredAt != nil {
			t.Fatalf("%s ExpiredAt = %v, want nil", monitor.ID, reloaded.ExpiredAt)
		}
	}
	assertOpenEpisode(t, sqlDB, subsecondEpisode)
	assertOpenEpisode(t, sqlDB, futureEpisode)

	var checkOutcome string
	if err := sqlDB.QueryRowContext(ctx, `SELECT outcome FROM check_runs`).Scan(&checkOutcome); err != nil {
		t.Fatalf("read running check: %v", err)
	}
	if checkOutcome != "running" {
		t.Fatalf("check outcome = %q, want running", checkOutcome)
	}
	if count := countStoreRows(t, sqlDB, `SELECT COUNT(*) FROM provider_health`); count != 0 {
		t.Fatalf("provider_health rows = %d, want 0", count)
	}
	if count := countStoreRows(t, sqlDB, `SELECT COUNT(*) FROM events`); count != 0 {
		t.Fatalf("events = %d, want 0", count)
	}
	if count := countStoreRows(t, sqlDB, `SELECT COUNT(*) FROM notification_deliveries`); count != 0 {
		t.Fatalf("deliveries = %d, want 0", count)
	}

	expired, next, err = st.ReconcileExpirations(ctx, now.Add(500*time.Millisecond))
	if err != nil {
		t.Fatalf("ReconcileExpirations(subsecond): %v", err)
	}
	if expired != 1 {
		t.Fatalf("subsecond expired = %d, want 1", expired)
	}
	if next == nil || !next.Equal(now.Add(time.Hour)) {
		t.Fatalf("next expiry after subsecond = %v, want %v", next, now.Add(time.Hour))
	}
	assertClosedEpisode(t, sqlDB, subsecondEpisode, "expired")
	assertOpenEpisode(t, sqlDB, futureEpisode)

	expired, _, err = st.ReconcileExpirations(ctx, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ReconcileExpirations(idempotent): %v", err)
	}
	if expired != 0 {
		t.Fatalf("repeated expired = %d, want 0", expired)
	}

	expired, next, err = st.ReconcileExpirations(ctx, now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("ReconcileExpirations(final): %v", err)
	}
	if expired != 1 || next != nil {
		t.Fatalf("final expiry = %d/%v, want 1/nil", expired, next)
	}
	assertClosedEpisode(t, sqlDB, futureEpisode, "expired")
}

func TestReconcileExpirationsRollsBackOnEpisodeFailure(t *testing.T) {
	st, sqlDB := newTestStore(t)
	ctx := context.Background()
	now := testNow
	monitor := createScheduleMonitor(t, st, "expiry-rollback", true, now, nil,
		scheduleTargetSpec{providerID: "p-rollback", active: true, due: timePtr(now)})
	episodeID := insertOpenEpisode(t, sqlDB, targetID(t, monitor, "p-rollback"), 1)

	if _, err := sqlDB.ExecContext(ctx, `CREATE TRIGGER fail_expired_episode
		BEFORE UPDATE OF closed_at ON availability_episodes
		WHEN NEW.close_reason = 'expired'
		BEGIN
			SELECT RAISE(ABORT, 'injected expiry failure');
		END`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	if _, _, err := st.ReconcileExpirations(ctx, now); err == nil {
		t.Fatal("ReconcileExpirations succeeded despite injected episode failure")
	}
	reloaded, err := st.GetMonitor(ctx, monitor.ID)
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	if reloaded.ExpiredAt != nil {
		t.Fatalf("ExpiredAt = %v after rollback, want nil", reloaded.ExpiredAt)
	}
	assertOpenEpisode(t, sqlDB, episodeID)

	if _, _, err := st.ReconcileExpirations(ctx, time.Time{}); err == nil {
		t.Fatal("ReconcileExpirations accepted zero time")
	}
}

func TestReconciledExpirySupportsFutureDateEditAndResume(t *testing.T) {
	st, sqlDB := newTestStore(t)
	ctx := context.Background()
	now := testNow
	monitor := createScheduleMonitor(t, st, "expiry-recovery", false, now.Add(-time.Hour), nil,
		scheduleTargetSpec{providerID: "p-recovery", active: true, due: timePtr(now.Add(-time.Minute))})
	if _, _, err := st.ReconcileExpirations(ctx, now); err != nil {
		t.Fatalf("ReconcileExpirations: %v", err)
	}
	expired, err := st.GetMonitor(ctx, monitor.ID)
	if err != nil {
		t.Fatalf("GetMonitor expired: %v", err)
	}
	if expired.ExpiredAt == nil || expired.Enabled {
		t.Fatalf("expired monitor = %+v", expired)
	}
	if _, err := st.ResumeMonitor(ctx, monitor.ID, now, now.Add(time.Minute)); !errors.Is(err, domain.ErrMonitorExpired) {
		t.Fatalf("Resume expired = %v, want ErrMonitorExpired", err)
	}

	location, err := domain.TehranLocation()
	if err != nil {
		t.Fatalf("TehranLocation: %v", err)
	}
	input := storeInput(expired)
	input.DepartureDate = "2026-11-05"
	updated, err := st.UpdateMonitor(ctx, monitor.ID, input, now, now.Add(2*time.Second), location)
	if err != nil {
		t.Fatalf("UpdateMonitor future date: %v", err)
	}
	if updated.Monitor.ExpiredAt != nil {
		t.Fatalf("ExpiredAt = %v, want nil", updated.Monitor.ExpiredAt)
	}
	if updated.Monitor.Enabled {
		t.Fatal("paused monitor became enabled")
	}
	for _, target := range updated.Monitor.Providers {
		if target.NextDueAt != nil {
			t.Fatalf("paused target next_due_at = %v, want nil", target.NextDueAt)
		}
	}

	resumed, err := st.ResumeMonitor(ctx, monitor.ID, now.Add(3*time.Second), now.Add(5*time.Second))
	if err != nil {
		t.Fatalf("ResumeMonitor after future edit: %v", err)
	}
	if !resumed.Enabled || resumed.Providers[0].NextDueAt == nil || !resumed.Providers[0].NextDueAt.Equal(now.Add(5*time.Second)) {
		t.Fatalf("resumed monitor = %+v", resumed)
	}
	if count := countStoreRows(t, sqlDB, `SELECT COUNT(*) FROM events`); count != 0 {
		t.Fatalf("events = %d, want 0", count)
	}
}

func assertOpenEpisode(t *testing.T, database *sql.DB, episodeID int64) {
	t.Helper()
	var closed sql.NullString
	var reason sql.NullString
	if err := database.QueryRow(`SELECT closed_at, close_reason FROM availability_episodes WHERE id = ?`, episodeID).
		Scan(&closed, &reason); err != nil {
		t.Fatalf("read open episode: %v", err)
	}
	if closed.Valid || reason.Valid {
		t.Fatalf("episode closed_at/reason = %v/%v, want open", closed, reason)
	}
}

func countStoreRows(t *testing.T, database *sql.DB, query string, args ...any) int {
	t.Helper()
	var count int
	if err := database.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	return count
}
