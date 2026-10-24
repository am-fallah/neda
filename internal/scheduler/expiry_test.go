package scheduler_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/scheduler"
	"github.com/am-fallah/neda/internal/store"
)

func TestExpiryTimerFiresWithoutDueTarget(t *testing.T) {
	st, sqlDB := newSchedulerStore(t)
	now := schedulerNow
	expiry := now.Add(time.Hour)
	due := now.Add(2 * time.Hour)
	monitor := createSchedulerMonitor(t, st, "expiry-timer", true, expiry,
		targetSpec{providerID: "fake", active: true, due: &due})
	targetID := targetIDFor(t, monitor, "fake")
	episodeID := insertSchedulerEpisode(t, sqlDB, targetID, now)

	starts := make(chan scheduler.Target, 1)
	clock := newFakeClock(now)
	running := startScheduler(t, st, registryFor(t, "fake"), clock,
		runnerFunc(func(_ context.Context, target scheduler.Target) (scheduler.Result, error) {
			starts <- target
			return scheduler.Result{Settled: true}, nil
		}),
		1, zeroRandom(), scheduler.NewWaker(), time.Second)

	clock.WaitForTimer(t)
	assertNoStart(t, starts, 50*time.Millisecond)
	clock.Advance(time.Hour)
	waitForExpired(t, st, monitor.ID)
	assertNoStart(t, starts, 50*time.Millisecond)

	assertSchedulerEpisodeClosed(t, sqlDB, episodeID, expiry)
	reloaded, err := st.GetMonitor(context.Background(), monitor.ID)
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	if reloaded.Providers[0].NextDueAt == nil || !reloaded.Providers[0].NextDueAt.Equal(due) {
		t.Fatalf("expiry changed next_due_at to %v, want %v", reloaded.Providers[0].NextDueAt, due)
	}
	if count := schedulerCountRows(t, sqlDB, `SELECT COUNT(*) FROM events`); count != 0 {
		t.Fatalf("expiry created %d events", count)
	}
	if count := schedulerCountRows(t, sqlDB, `SELECT COUNT(*) FROM notification_deliveries`); count != 0 {
		t.Fatalf("expiry created %d deliveries", count)
	}
	running.stop(t)
}

func TestExpirySweepRunsWhileAllWorkersBusy(t *testing.T) {
	st, sqlDB := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	busy := createSchedulerMonitor(t, st, "expiry-busy", true, now.Add(24*time.Hour),
		targetSpec{providerID: "busy", active: true, due: &past})
	paused := createSchedulerMonitor(t, st, "expiry-paused-busy", false, now.Add(10*time.Second),
		targetSpec{providerID: "paused", active: true, due: nil})
	pausedTarget := targetIDFor(t, paused, "paused")
	episodeID := insertSchedulerEpisode(t, sqlDB, pausedTarget, now)

	release := make(chan struct{})
	starts := make(chan scheduler.Target, 1)
	runner := runnerFunc(func(ctx context.Context, target scheduler.Target) (scheduler.Result, error) {
		starts <- target
		select {
		case <-release:
			return scheduler.Result{Settled: true}, nil
		case <-ctx.Done():
			return scheduler.Result{}, ctx.Err()
		}
	})
	clock := newFakeClock(now)
	running := startScheduler(t, st, registryFor(t, "busy", "paused"), clock,
		runner, 1, zeroRandom(), scheduler.NewWaker(), time.Second)

	first := waitStart(t, starts)
	if first.MonitorID != busy.ID {
		t.Fatalf("dispatched monitor %s, want busy monitor %s", first.MonitorID, busy.ID)
	}
	clock.WaitForTimer(t)
	clock.Advance(10 * time.Second)
	waitForExpired(t, st, paused.ID)
	assertSchedulerEpisodeClosed(t, sqlDB, episodeID, now.Add(10*time.Second))

	busyReloaded, err := st.GetMonitor(context.Background(), busy.ID)
	if err != nil {
		t.Fatalf("GetMonitor(busy): %v", err)
	}
	if busyReloaded.ExpiredAt != nil {
		t.Fatalf("busy monitor expired at %v", busyReloaded.ExpiredAt)
	}
	close(release)
	running.stop(t)
}

func TestExpiryTimerReschedulesAfterExtension(t *testing.T) {
	st, sqlDB := newSchedulerStore(t)
	now := schedulerNow
	monitor := createSchedulerMonitor(t, st, "expiry-extension", false, now.Add(10*time.Minute),
		targetSpec{providerID: "fake", active: true, due: nil})
	episodeID := insertSchedulerEpisode(t, sqlDB, targetIDFor(t, monitor, "fake"), now)

	starts := make(chan scheduler.Target, 1)
	clock := newFakeClock(now)
	wake := scheduler.NewWaker()
	running := startScheduler(t, st, registryFor(t, "fake"), clock,
		runnerFunc(func(_ context.Context, target scheduler.Target) (scheduler.Result, error) {
			starts <- target
			return scheduler.Result{Settled: true}, nil
		}),
		1, zeroRandom(), wake, time.Second)

	clock.WaitForTimer(t)
	if _, err := sqlDB.Exec(`UPDATE monitors SET expires_at = ?, updated_at = ? WHERE id = ?`,
		schedulerTime(now.Add(time.Hour)), schedulerTime(now), monitor.ID); err != nil {
		t.Fatalf("extend expiry: %v", err)
	}
	wake.Wake()
	clock.WaitForTimer(t)
	clock.Advance(10 * time.Minute)
	time.Sleep(50 * time.Millisecond)

	reloaded, err := st.GetMonitor(context.Background(), monitor.ID)
	if err != nil {
		t.Fatalf("GetMonitor after old expiry: %v", err)
	}
	if reloaded.ExpiredAt != nil {
		t.Fatalf("monitor expired at stale timer %v", reloaded.ExpiredAt)
	}
	assertOpenSchedulerEpisode(t, sqlDB, episodeID)

	clock.Advance(50 * time.Minute)
	waitForExpired(t, st, monitor.ID)
	assertSchedulerEpisodeClosed(t, sqlDB, episodeID, now.Add(time.Hour))
	assertNoStart(t, starts, 50*time.Millisecond)
	running.stop(t)
}

func TestNilRunnerStartupReconcilesExpiryWithoutTimer(t *testing.T) {
	st, sqlDB := newSchedulerStore(t)
	now := schedulerNow
	monitor := createSchedulerMonitor(t, st, "expiry-nil-runner", true, now.Add(-time.Hour),
		targetSpec{providerID: "fake", active: true, due: timePointer(now.Add(-time.Minute))})
	episodeID := insertSchedulerEpisode(t, sqlDB, targetIDFor(t, monitor, "fake"), now)

	clock := newFakeClock(now)
	running := startScheduler(t, st, registryFor(t, "fake"), clock, nil, 1, zeroRandom(), scheduler.NewWaker(), time.Second)
	waitForExpired(t, st, monitor.ID)
	if clock.TimerCount() != 0 {
		t.Fatalf("nil runner created %d timers", clock.TimerCount())
	}
	assertSchedulerEpisodeClosed(t, sqlDB, episodeID, now)
	running.stop(t)
}

func insertSchedulerEpisode(t *testing.T, database *sql.DB, targetID int64, now time.Time) int64 {
	t.Helper()
	result, err := database.Exec(`INSERT INTO inventory_states
		(monitor_provider_id, config_version, journey_key, offering_key, state, state_since, last_observed_at, updated_at)
		VALUES (?, 1, 'journey', 'offering', 'reservable', ?, ?, ?)`,
		targetID, schedulerTime(now), schedulerTime(now), schedulerTime(now))
	if err != nil {
		t.Fatalf("insert inventory state: %v", err)
	}
	stateID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("inventory state id: %v", err)
	}
	result, err = database.Exec(`INSERT INTO availability_episodes (state_id, episode_seq, opened_at) VALUES (?, 1, ?)`,
		stateID, schedulerTime(now))
	if err != nil {
		t.Fatalf("insert availability episode: %v", err)
	}
	episodeID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("availability episode id: %v", err)
	}
	return episodeID
}

func waitForExpired(t *testing.T, st *store.Store, monitorID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		monitor, err := st.GetMonitor(context.Background(), monitorID)
		if err == nil && monitor.ExpiredAt != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("monitor %s was not expired; monitor=%+v err=%v", monitorID, monitor, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertSchedulerEpisodeClosed(t *testing.T, database *sql.DB, episodeID int64, at time.Time) {
	t.Helper()
	var closedAt, reason sql.NullString
	if err := database.QueryRow(`SELECT closed_at, close_reason FROM availability_episodes WHERE id = ?`, episodeID).
		Scan(&closedAt, &reason); err != nil {
		t.Fatalf("read episode: %v", err)
	}
	if !closedAt.Valid || closedAt.String != schedulerTime(at) || !reason.Valid || reason.String != "expired" {
		t.Fatalf("episode closed_at/reason = %v/%v, want %s/expired", closedAt, reason, schedulerTime(at))
	}
}

func assertOpenSchedulerEpisode(t *testing.T, database *sql.DB, episodeID int64) {
	t.Helper()
	var closedAt, reason sql.NullString
	if err := database.QueryRow(`SELECT closed_at, close_reason FROM availability_episodes WHERE id = ?`, episodeID).
		Scan(&closedAt, &reason); err != nil {
		t.Fatalf("read episode: %v", err)
	}
	if closedAt.Valid || reason.Valid {
		t.Fatalf("episode closed_at/reason = %v/%v, want open", closedAt, reason)
	}
}

func schedulerCountRows(t *testing.T, database *sql.DB, query string, args ...any) int {
	t.Helper()
	var count int
	if err := database.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	return count
}
