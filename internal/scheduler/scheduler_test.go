package scheduler_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/provider/fake"
	"github.com/am-fallah/neda/internal/scheduler"
	"github.com/am-fallah/neda/internal/store"
)

var schedulerNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

type stubProvider struct {
	id provider.ID
}

func (p stubProvider) Metadata() provider.Metadata {
	return provider.Metadata{ID: p.id, DisplayName: "stub " + string(p.id)}
}

func (p stubProvider) ValidateConfig(provider.MonitorProviderConfig) error { return nil }

func (p stubProvider) Check(context.Context, provider.CheckRequest) (provider.Result, error) {
	return provider.Result{}, nil
}

func registryFor(t *testing.T, ids ...provider.ID) *provider.Registry {
	t.Helper()
	providers := make([]provider.Provider, 0, len(ids))
	for _, id := range ids {
		providers = append(providers, stubProvider{id: id})
	}
	registry, err := provider.NewRegistry(providers...)
	if err != nil {
		t.Fatalf("provider.NewRegistry: %v", err)
	}
	return registry
}

type fixedInterval time.Duration

func (f fixedInterval) PollInterval(context.Context) (time.Duration, error) {
	return time.Duration(f), nil
}

type scriptedRandom struct {
	mu     sync.Mutex
	values []int64
	index  int
}

func (r *scriptedRandom) Int64N(n int64) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n <= 0 {
		panic("scriptedRandom: n must be positive")
	}
	if len(r.values) == 0 {
		return 0
	}
	value := r.values[r.index%len(r.values)]
	r.index++
	if value < 0 {
		value = -value
	}
	return value % n
}

func zeroRandom() scheduler.Random {
	return &scriptedRandom{values: []int64{0}}
}

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
	added  chan struct{}
}

func newFakeClock(now time.Time) *fakeClock {
	return &fakeClock{now: now, added: make(chan struct{}, 32)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) NewTimer(duration time.Duration) scheduler.Timer {
	c.mu.Lock()
	timer := &fakeTimer{at: c.now.Add(duration), ch: make(chan time.Time, 1)}
	c.timers = append(c.timers, timer)
	c.mu.Unlock()
	select {
	case c.added <- struct{}{}:
	default:
	}
	return timer
}

func (c *fakeClock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	now := c.now
	remaining := make([]*fakeTimer, 0, len(c.timers))
	due := make([]*fakeTimer, 0, len(c.timers))
	for _, timer := range c.timers {
		timer.mu.Lock()
		if timer.stopped || timer.fired {
			timer.mu.Unlock()
			continue
		}
		if !timer.at.After(now) {
			timer.fired = true
			timer.mu.Unlock()
			due = append(due, timer)
			continue
		}
		timer.mu.Unlock()
		remaining = append(remaining, timer)
	}
	c.timers = remaining
	c.mu.Unlock()

	for _, timer := range due {
		select {
		case timer.ch <- now:
		default:
		}
	}
}

func (c *fakeClock) WaitForTimer(t *testing.T) {
	t.Helper()
	select {
	case <-c.added:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the scheduler timer")
	}
}

func (c *fakeClock) TimerCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

type fakeTimer struct {
	mu      sync.Mutex
	at      time.Time
	ch      chan time.Time
	stopped bool
	fired   bool
}

func (t *fakeTimer) C() <-chan time.Time { return t.ch }

func (t *fakeTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped || t.fired {
		return false
	}
	t.stopped = true
	return true
}

type runnerFunc func(context.Context, scheduler.Target) (scheduler.Result, error)

func (f runnerFunc) Run(ctx context.Context, target scheduler.Target) (scheduler.Result, error) {
	return f(ctx, target)
}

type targetSpec struct {
	providerID provider.ID
	active     bool
	due        *time.Time
}

func newSchedulerStore(t *testing.T) (*store.Store, *sql.DB) {
	t.Helper()
	database, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "scheduler-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("database.Close: %v", err)
		}
	})
	applied, err := db.Migrate(context.Background(), database.DB, migrations.FS)
	if err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	if err := database.Ready(context.Background(), applied); err != nil {
		t.Fatalf("database.Ready: %v", err)
	}
	return store.New(database.DB), database.DB
}

func createSchedulerMonitor(t *testing.T, st *store.Store, id string, enabled bool, expiresAt time.Time, specs ...targetSpec) domain.Monitor {
	t.Helper()
	targets := make([]domain.MonitorProvider, 0, len(specs))
	for _, spec := range specs {
		targets = append(targets, domain.MonitorProvider{
			MonitorID:     id,
			ProviderID:    spec.providerID,
			Active:        spec.active,
			OriginID:      "origin-" + string(spec.providerID),
			DestinationID: "destination-" + string(spec.providerID),
			ConfigVersion: 1,
			NextDueAt:     spec.due,
			CreatedAt:     schedulerNow,
			UpdatedAt:     schedulerNow.Add(-time.Hour),
		})
	}
	monitor, err := st.CreateMonitor(context.Background(), domain.Monitor{
		ID:              id,
		Label:           "Scheduler " + id,
		OriginName:      "Tehran",
		DestinationName: "Mashhad",
		DepartureDate:   "2026-10-30",
		Adults:          1,
		Enabled:         enabled,
		ExpiresAt:       expiresAt,
		CreatedAt:       schedulerNow,
		UpdatedAt:       schedulerNow,
		Providers:       targets,
	})
	if err != nil {
		t.Fatalf("CreateMonitor(%s): %v", id, err)
	}
	return monitor
}

func targetIDFor(t *testing.T, monitor domain.Monitor, providerID provider.ID) int64 {
	t.Helper()
	for _, target := range monitor.Providers {
		if target.ProviderID == providerID {
			return target.ID
		}
	}
	t.Fatalf("monitor %s has no target for provider %s", monitor.ID, providerID)
	return 0
}

func timePointer(value time.Time) *time.Time { return &value }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

type runningScheduler struct {
	cancel context.CancelFunc
	done   <-chan error
}

func startScheduler(
	t *testing.T,
	st *store.Store,
	registry *provider.Registry,
	clock *fakeClock,
	runner scheduler.Runner,
	workers int,
	random scheduler.Random,
	wake *scheduler.Waker,
	grace time.Duration,
) *runningScheduler {
	t.Helper()
	if random == nil {
		random = zeroRandom()
	}
	sched, err := scheduler.New(scheduler.Config{
		Repository:    st,
		Providers:     registry,
		Interval:      fixedInterval(300 * time.Second),
		Runner:        runner,
		Workers:       workers,
		Wake:          wake.C(),
		Clock:         clock,
		Random:        random,
		Logger:        discardLogger(),
		ShutdownGrace: grace,
	})
	if err != nil {
		t.Fatalf("scheduler.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sched.Run(ctx) }()
	t.Cleanup(cancel)
	return &runningScheduler{cancel: cancel, done: done}
}

func (r *runningScheduler) stop(t *testing.T) {
	t.Helper()
	r.cancel()
	select {
	case err := <-r.done:
		if err != nil {
			t.Fatalf("scheduler.Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop")
	}
}

func waitStart(t *testing.T, starts <-chan scheduler.Target) scheduler.Target {
	t.Helper()
	select {
	case target := <-starts:
		return target
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a scheduler dispatch")
		return scheduler.Target{}
	}
}

func assertNoStart(t *testing.T, starts <-chan scheduler.Target, duration time.Duration) {
	t.Helper()
	select {
	case target := <-starts:
		t.Fatalf("unexpected dispatch: monitor=%s target=%d", target.MonitorID, target.ID)
	case <-time.After(duration):
	}
}

func TestJitterUsesTenPercentAndClamps(t *testing.T) {
	low := &scriptedRandom{values: []int64{0}}
	high := &scriptedRandom{values: []int64{int64(60 * time.Second)}}
	mid := &scriptedRandom{values: []int64{int64(30 * time.Second)}}

	if got := scheduler.JitterOffset(300*time.Second, low); got != -30*time.Second {
		t.Fatalf("JitterOffset(low) = %v, want -30s", got)
	}
	if got := scheduler.JitterOffset(300*time.Second, high); got != 30*time.Second {
		t.Fatalf("JitterOffset(high) = %v, want +30s", got)
	}
	if got := scheduler.JitterOffset(300*time.Second, mid); got != 0 {
		t.Fatalf("JitterOffset(mid) = %v, want 0", got)
	}
	if got := scheduler.NextInterval(60*time.Second, low); got != 60*time.Second {
		t.Fatalf("NextInterval(min clamp) = %v, want 60s", got)
	}
	if got := scheduler.NextInterval(3600*time.Second, &scriptedRandom{values: []int64{int64(720 * time.Second)}}); got != 3600*time.Second {
		t.Fatalf("NextInterval(max clamp) = %v, want 3600s", got)
	}
	if got := scheduler.NextInterval(300*time.Second, high); got != 330*time.Second {
		t.Fatalf("NextInterval(300s) = %v, want 330s", got)
	}

	if got := scheduler.NearTermDelay(300*time.Second, low); got != 0 {
		t.Fatalf("NearTermDelay(low) = %v, want 0", got)
	}
	if got := scheduler.NearTermDelay(300*time.Second, &scriptedRandom{values: []int64{int64(30 * time.Second)}}); got != 30*time.Second {
		t.Fatalf("NearTermDelay(high) = %v, want 30s", got)
	}
	if got := scheduler.NearTermDelay(60*time.Second, &scriptedRandom{values: []int64{int64(6 * time.Second)}}); got != 6*time.Second {
		t.Fatalf("NearTermDelay(60s) = %v, want 6s", got)
	}
	if got := scheduler.NearTermDelay(3600*time.Second, &scriptedRandom{values: []int64{int64(30 * time.Second)}}); got != 30*time.Second {
		t.Fatalf("NearTermDelay(3600s cap) = %v, want 30s", got)
	}
	if got := scheduler.NearTermDelay(0, low); got != 0 {
		t.Fatalf("NearTermDelay(0) = %v, want 0", got)
	}
}

func TestDueSelectionHonorsEligibilityAndRegistry(t *testing.T) {
	st, sqlDB := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)
	expires := now.Add(24 * time.Hour)

	eligible := createSchedulerMonitor(t, st, "eligible", true, expires,
		targetSpec{providerID: "fake", active: true, due: &past})
	createSchedulerMonitor(t, st, "future", true, expires,
		targetSpec{providerID: "fake", active: true, due: &future})
	createSchedulerMonitor(t, st, "paused", false, expires,
		targetSpec{providerID: "fake", active: true, due: &past})
	createSchedulerMonitor(t, st, "expired-at", true, expires,
		targetSpec{providerID: "fake", active: true, due: &past})
	if _, err := sqlDB.Exec(`UPDATE monitors SET expired_at = ? WHERE id = 'expired-at'`, schedulerTime(now)); err != nil {
		t.Fatalf("expire monitor: %v", err)
	}
	createSchedulerMonitor(t, st, "expired-by", true, now.Add(-time.Minute),
		targetSpec{providerID: "fake", active: true, due: &past})
	createSchedulerMonitor(t, st, "inactive", true, expires,
		targetSpec{providerID: "fake", active: false, due: &past})
	createSchedulerMonitor(t, st, "unregistered", true, expires,
		targetSpec{providerID: "missing", active: true, due: &past})

	recorded := make(chan scheduler.Target, 8)
	wake := scheduler.NewWaker()
	clock := newFakeClock(now)
	running := startScheduler(t, st, registryFor(t, "fake"), clock,
		runnerFunc(func(_ context.Context, target scheduler.Target) (scheduler.Result, error) {
			recorded <- target
			return scheduler.Result{Settled: true}, nil
		}),
		4, zeroRandom(), wake, time.Second)

	target := waitStart(t, recorded)
	if target.MonitorID != eligible.ID || target.ID != targetIDFor(t, eligible, "fake") {
		t.Fatalf("dispatched %+v, want eligible target %d", target, targetIDFor(t, eligible, "fake"))
	}
	assertNoStart(t, recorded, 100*time.Millisecond)
	wake.Wake()
	assertNoStart(t, recorded, 50*time.Millisecond)
	running.stop(t)
}

func schedulerTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func TestMonitorWideSerialization(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	monitor := createSchedulerMonitor(t, st, "one-monitor", true, now.Add(24*time.Hour),
		targetSpec{providerID: "alpha", active: true, due: &past},
		targetSpec{providerID: "beta", active: true, due: &past})

	release := make(chan struct{}, 2)
	starts := make(chan scheduler.Target, 4)
	runner := runnerFunc(func(ctx context.Context, target scheduler.Target) (scheduler.Result, error) {
		select {
		case starts <- target:
		case <-ctx.Done():
			return scheduler.Result{}, ctx.Err()
		}
		select {
		case <-release:
			return scheduler.Result{Settled: true}, nil
		case <-ctx.Done():
			return scheduler.Result{}, ctx.Err()
		}
	})
	running := startScheduler(t, st, registryFor(t, "alpha", "beta"), newFakeClock(now),
		runner, 2, zeroRandom(), scheduler.NewWaker(), time.Second)

	first := waitStart(t, starts)
	if first.MonitorID != monitor.ID {
		t.Fatalf("first monitor = %s, want %s", first.MonitorID, monitor.ID)
	}
	assertNoStart(t, starts, 100*time.Millisecond)
	release <- struct{}{}

	second := waitStart(t, starts)
	if second.MonitorID != monitor.ID || second.ID == first.ID {
		t.Fatalf("second target = %+v, want a different target of monitor %s", second, monitor.ID)
	}
	release <- struct{}{}
	running.stop(t)
}

func TestWorkerPoolIsBounded(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	expires := now.Add(24 * time.Hour)
	for _, id := range []string{"m1", "m2", "m3"} {
		createSchedulerMonitor(t, st, id, true, expires,
			targetSpec{providerID: "fake", active: true, due: &past})
	}

	release := make(chan struct{}, 3)
	starts := make(chan scheduler.Target, 4)
	runner := runnerFunc(func(ctx context.Context, target scheduler.Target) (scheduler.Result, error) {
		select {
		case starts <- target:
		case <-ctx.Done():
			return scheduler.Result{}, ctx.Err()
		}
		select {
		case <-release:
			return scheduler.Result{Settled: true}, nil
		case <-ctx.Done():
			return scheduler.Result{}, ctx.Err()
		}
	})
	running := startScheduler(t, st, registryFor(t, "fake"), newFakeClock(now),
		runner, 2, zeroRandom(), scheduler.NewWaker(), time.Second)

	first := waitStart(t, starts)
	second := waitStart(t, starts)
	if first.MonitorID == second.MonitorID {
		t.Fatalf("two concurrent dispatches for monitor %s", first.MonitorID)
	}
	assertNoStart(t, starts, 100*time.Millisecond)

	release <- struct{}{}
	third := waitStart(t, starts)
	if third.MonitorID == first.MonitorID || third.MonitorID == second.MonitorID {
		t.Fatalf("third dispatch reused monitor %s", third.MonitorID)
	}
	release <- struct{}{}
	release <- struct{}{}
	running.stop(t)
}

func TestTimerFiresForFutureDueTime(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	future := now.Add(time.Hour)
	createSchedulerMonitor(t, st, "timer", true, now.Add(24*time.Hour),
		targetSpec{providerID: "fake", active: true, due: &future})

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
	target := waitStart(t, starts)
	if target.MonitorID != "timer" {
		t.Fatalf("timer dispatched monitor %s", target.MonitorID)
	}
	running.stop(t)
}

func TestWakeReevaluatesWithoutAdvancingClock(t *testing.T) {
	st, sqlDB := newSchedulerStore(t)
	now := schedulerNow
	future := now.Add(time.Hour)
	monitor := createSchedulerMonitor(t, st, "wake", true, now.Add(24*time.Hour),
		targetSpec{providerID: "fake", active: true, due: &future})
	targetID := targetIDFor(t, monitor, "fake")

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
	if _, err := sqlDB.Exec(`UPDATE monitor_providers SET next_due_at = ? WHERE id = ?`,
		schedulerTime(now), targetID); err != nil {
		t.Fatalf("advance next_due_at: %v", err)
	}
	wake.Wake()
	target := waitStart(t, starts)
	if target.ID != targetID {
		t.Fatalf("wake dispatched target %d, want %d", target.ID, targetID)
	}
	if !clock.Now().Equal(now) {
		t.Fatalf("clock advanced to %v; wake must not depend on the timer", clock.Now())
	}
	running.stop(t)
}

func TestRestartStaggerPreservesFutureAndSkipsIneligibleTargets(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)
	expires := now.Add(24 * time.Hour)

	due := createSchedulerMonitor(t, st, "due", true, expires,
		targetSpec{providerID: "p-due", active: true, due: &past})
	null := createSchedulerMonitor(t, st, "null", true, expires,
		targetSpec{providerID: "p-null", active: true, due: nil})
	futureMonitor := createSchedulerMonitor(t, st, "future", true, expires,
		targetSpec{providerID: "p-future", active: true, due: &future})
	paused := createSchedulerMonitor(t, st, "paused", false, expires,
		targetSpec{providerID: "p-paused", active: true, due: &past})
	expired := createSchedulerMonitor(t, st, "expired", true, now.Add(-time.Minute),
		targetSpec{providerID: "p-expired", active: true, due: &past})

	starts := make(chan scheduler.Target, 4)
	running := startScheduler(t, st, registryFor(t, "p-due", "p-null", "p-future", "p-paused", "p-expired"),
		newFakeClock(now),
		runnerFunc(func(_ context.Context, target scheduler.Target) (scheduler.Result, error) {
			starts <- target
			return scheduler.Result{Settled: true}, nil
		}),
		1, &scriptedRandom{values: []int64{0, int64(30 * time.Second)}}, scheduler.NewWaker(), time.Second)

	// The due target is staggered to now and dispatched; that start confirms
	// startup staggering completed.
	waitStart(t, starts)

	assertDue := func(monitor domain.Monitor, providerID provider.ID, want time.Time) {
		t.Helper()
		reloaded, err := st.GetMonitor(context.Background(), monitor.ID)
		if err != nil {
			t.Fatalf("GetMonitor(%s): %v", monitor.ID, err)
		}
		target := reloaded.Providers[0]
		if target.ProviderID != providerID {
			t.Fatalf("target provider = %s, want %s", target.ProviderID, providerID)
		}
		if target.NextDueAt == nil || !target.NextDueAt.Equal(want) {
			t.Fatalf("%s next_due_at = %v, want %v", providerID, target.NextDueAt, want)
		}
	}
	assertDue(due, "p-due", now)
	assertDue(null, "p-null", now.Add(30*time.Second))
	assertDue(futureMonitor, "p-future", future)
	assertDue(paused, "p-paused", past)
	assertDue(expired, "p-expired", past)
	running.stop(t)
}

func TestStartupRecoveryWithNilRunnerIsIdle(t *testing.T) {
	st, sqlDB := newSchedulerStore(t)
	now := schedulerNow
	future := now.Add(time.Hour)
	monitor := createSchedulerMonitor(t, st, "recovery", true, now.Add(24*time.Hour),
		targetSpec{providerID: "fake", active: true, due: &future})
	targetID := targetIDFor(t, monitor, "fake")

	result, err := sqlDB.Exec(`INSERT INTO check_runs
		(monitor_provider_id, config_version, started_at, outcome, created_at)
		VALUES (?, 1, ?, 'running', ?)`,
		targetID, schedulerTime(now.Add(-time.Minute)), schedulerTime(now.Add(-time.Minute)),
	)
	if err != nil {
		t.Fatalf("insert running check: %v", err)
	}
	runningID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("running check id: %v", err)
	}

	clock := newFakeClock(now)
	running := startScheduler(t, st, registryFor(t, "fake"), clock, nil, 1, zeroRandom(), scheduler.NewWaker(), time.Second)

	deadline := time.Now().Add(2 * time.Second)
	for {
		var outcome string
		var finishedAt sql.NullString
		err := sqlDB.QueryRow(`SELECT outcome, finished_at FROM check_runs WHERE id = ?`, runningID).
			Scan(&outcome, &finishedAt)
		if err == nil && outcome == "abandoned" {
			if !finishedAt.Valid {
				t.Fatal("abandoned check has no finished_at")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("running check was not recovered; outcome=%q err=%v", outcome, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	reloaded, err := st.GetMonitor(context.Background(), monitor.ID)
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	if reloaded.Providers[0].NextDueAt == nil || !reloaded.Providers[0].NextDueAt.Equal(future) {
		t.Fatalf("nil-runner recovery changed next_due_at: %v", reloaded.Providers[0].NextDueAt)
	}
	if clock.TimerCount() != 0 {
		t.Fatalf("nil runner created %d timers", clock.TimerCount())
	}
	running.stop(t)
}

func TestCancellationStopsRunnerAndReturnsCleanly(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	createSchedulerMonitor(t, st, "cancel", true, now.Add(24*time.Hour),
		targetSpec{providerID: "fake", active: true, due: &past})

	started := make(chan struct{})
	runner := runnerFunc(func(ctx context.Context, _ scheduler.Target) (scheduler.Result, error) {
		close(started)
		<-ctx.Done()
		return scheduler.Result{}, ctx.Err()
	})
	running := startScheduler(t, st, registryFor(t, "fake"), newFakeClock(now),
		runner, 1, zeroRandom(), scheduler.NewWaker(), time.Second)

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not start")
	}
	running.stop(t)
}

func TestShutdownGraceBoundsRunnerThatIgnoresContext(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	createSchedulerMonitor(t, st, "grace", true, now.Add(24*time.Hour),
		targetSpec{providerID: "fake", active: true, due: &past})

	started := make(chan struct{})
	release := make(chan struct{})
	runner := runnerFunc(func(context.Context, scheduler.Target) (scheduler.Result, error) {
		close(started)
		<-release
		return scheduler.Result{Settled: true}, nil
	})
	running := startScheduler(t, st, registryFor(t, "fake"), newFakeClock(now),
		runner, 1, zeroRandom(), scheduler.NewWaker(), 50*time.Millisecond)

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not start")
	}

	start := time.Now()
	running.cancel()
	select {
	case err := <-running.done:
		if err != nil {
			t.Fatalf("scheduler.Run: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop within the shutdown grace")
	}
	if elapsed := time.Since(start); elapsed > 900*time.Millisecond {
		t.Fatalf("shutdown took %v, want bounded by the configured grace", elapsed)
	}
	close(release)
}

func TestNoRedispatchWithoutReschedule(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	monitor := createSchedulerMonitor(t, st, "settled", true, now.Add(24*time.Hour),
		targetSpec{providerID: "fake", active: true, due: &past})

	var calls atomic.Int32
	starts := make(chan scheduler.Target, 2)
	runner := runnerFunc(func(_ context.Context, target scheduler.Target) (scheduler.Result, error) {
		calls.Add(1)
		starts <- target
		return scheduler.Result{Settled: true}, nil
	})
	wake := scheduler.NewWaker()
	clock := newFakeClock(now)
	running := startScheduler(t, st, registryFor(t, "fake"), clock,
		runner, 1, zeroRandom(), wake, time.Second)

	target := waitStart(t, starts)
	if target.MonitorID != monitor.ID {
		t.Fatalf("dispatched %s, want %s", target.MonitorID, monitor.ID)
	}
	for i := 0; i < 3; i++ {
		wake.Wake()
		time.Sleep(20 * time.Millisecond)
	}
	assertNoStart(t, starts, 50*time.Millisecond)
	clock.Advance(time.Hour)
	assertNoStart(t, starts, 50*time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("runner calls = %d, want 1 without a reschedule", got)
	}
	running.stop(t)
}

func TestMaterialConfigVersionChangeRedispatchesSameDue(t *testing.T) {
	st, sqlDB := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	monitor := createSchedulerMonitor(t, st, "material", true, now.Add(24*time.Hour),
		targetSpec{providerID: "fake", active: true, due: &past})
	targetID := targetIDFor(t, monitor, "fake")

	starts := make(chan scheduler.Target, 2)
	wake := scheduler.NewWaker()
	running := startScheduler(t, st, registryFor(t, "fake"), newFakeClock(now),
		runnerFunc(func(_ context.Context, target scheduler.Target) (scheduler.Result, error) {
			starts <- target
			return scheduler.Result{Settled: true}, nil
		}),
		1, zeroRandom(), wake, time.Second)

	first := waitStart(t, starts)
	if first.ConfigVersion != 1 {
		t.Fatalf("first config version = %d, want 1", first.ConfigVersion)
	}
	if _, err := sqlDB.Exec(`UPDATE monitor_providers SET config_version = 2, next_due_at = ? WHERE id = ?`,
		schedulerTime(now), targetID); err != nil {
		t.Fatalf("apply material edit: %v", err)
	}
	wake.Wake()
	second := waitStart(t, starts)
	if second.ConfigVersion != 2 {
		t.Fatalf("second config version = %d, want 2", second.ConfigVersion)
	}
	if !second.NextDueAt.Equal(first.NextDueAt) {
		t.Fatalf("second due = %v, want the same due %v", second.NextDueAt, first.NextDueAt)
	}
	running.stop(t)
}

func TestBookingOnlyChangeDoesNotRedispatchSameDue(t *testing.T) {
	st, sqlDB := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	monitor := createSchedulerMonitor(t, st, "booking", true, now.Add(24*time.Hour),
		targetSpec{providerID: "fake", active: true, due: &past})
	targetID := targetIDFor(t, monitor, "fake")

	starts := make(chan scheduler.Target, 2)
	wake := scheduler.NewWaker()
	running := startScheduler(t, st, registryFor(t, "fake"), newFakeClock(now),
		runnerFunc(func(_ context.Context, target scheduler.Target) (scheduler.Result, error) {
			starts <- target
			return scheduler.Result{Settled: true}, nil
		}),
		1, zeroRandom(), wake, time.Second)

	waitStart(t, starts)
	if _, err := sqlDB.Exec(`UPDATE monitor_providers SET booking_slug = 'new-slug', next_due_at = ? WHERE id = ?`,
		schedulerTime(now), targetID); err != nil {
		t.Fatalf("apply booking edit: %v", err)
	}
	wake.Wake()
	assertNoStart(t, starts, 100*time.Millisecond)
	running.stop(t)
}

func TestRunnerPanicDoesNotStopTheLoop(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	expires := now.Add(24 * time.Hour)
	createSchedulerMonitor(t, st, "panic", true, expires,
		targetSpec{providerID: "fake", active: true, due: &past})
	createSchedulerMonitor(t, st, "after", true, expires,
		targetSpec{providerID: "fake", active: true, due: &past})

	var calls atomic.Int32
	starts := make(chan scheduler.Target, 2)
	runner := runnerFunc(func(_ context.Context, target scheduler.Target) (scheduler.Result, error) {
		starts <- target
		if calls.Add(1) == 1 {
			panic("boom")
		}
		return scheduler.Result{Settled: true}, nil
	})
	running := startScheduler(t, st, registryFor(t, "fake"), newFakeClock(now),
		runner, 1, zeroRandom(), scheduler.NewWaker(), time.Second)

	first := waitStart(t, starts)
	second := waitStart(t, starts)
	if first.MonitorID == second.MonitorID {
		t.Fatalf("panic test reused monitor %s", first.MonitorID)
	}
	running.stop(t)
}

func TestUnsettledResultRetriesAfterProtectiveHold(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	monitor := createSchedulerMonitor(t, st, "unsettled", true, now.Add(24*time.Hour),
		targetSpec{providerID: "fake", active: true, due: &past})

	var calls atomic.Int32
	starts := make(chan scheduler.Target, 2)
	clock := newFakeClock(now)
	runner := runnerFunc(func(_ context.Context, target scheduler.Target) (scheduler.Result, error) {
		starts <- target
		if calls.Add(1) == 1 {
			return scheduler.Result{Settled: false}, nil
		}
		return scheduler.Result{Settled: true}, nil
	})
	running := startScheduler(t, st, registryFor(t, "fake"), clock,
		runner, 1, zeroRandom(), scheduler.NewWaker(), time.Second)

	waitStart(t, starts)
	clock.WaitForTimer(t)
	assertNoStart(t, starts, 50*time.Millisecond)
	clock.Advance(time.Second)
	second := waitStart(t, starts)
	if second.MonitorID != monitor.ID {
		t.Fatalf("retry dispatched monitor %s, want %s", second.MonitorID, monitor.ID)
	}
	if calls.Load() != 2 {
		t.Fatalf("runner calls = %d, want 2", calls.Load())
	}
	running.stop(t)
}

func TestMaterialEditWakeReachesScheduler(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	registry, err := provider.NewRegistry(fake.New())
	if err != nil {
		t.Fatalf("provider.NewRegistry: %v", err)
	}
	wake := scheduler.NewWaker()
	nearTerm := func(base time.Time) time.Time { return base.Add(time.Hour) }
	service, err := domain.NewMonitorService(st, registry, domain.MonitorServiceConfig{
		Now: func() time.Time { return now },
		NearTermSchedule: func(base time.Time) time.Time {
			return nearTerm(base)
		},
		NewID: func() (string, error) { return "wake-monitor", nil },
		Wake:  wake.Wake,
	})
	if err != nil {
		t.Fatalf("NewMonitorService: %v", err)
	}
	input := domain.MonitorInput{
		Label:           "Wake monitor",
		OriginName:      "Tehran",
		DestinationName: "Mashhad",
		DepartureDate:   "2026-10-30",
		Adults:          1,
		Providers: []domain.ProviderInput{{
			ProviderID:    fake.ID,
			OriginID:      fake.OriginAvailable,
			DestinationID: fake.DestinationDefault,
		}},
	}
	created, err := service.Create(context.Background(), input)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Providers[0].NextDueAt == nil || !created.Providers[0].NextDueAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("created next_due_at = %v, want %v", created.Providers[0].NextDueAt, now.Add(time.Hour))
	}

	starts := make(chan scheduler.Target, 1)
	clock := newFakeClock(now)
	running := startScheduler(t, st, registry, clock,
		runnerFunc(func(_ context.Context, target scheduler.Target) (scheduler.Result, error) {
			starts <- target
			return scheduler.Result{Settled: true}, nil
		}),
		1, zeroRandom(), wake, time.Second)

	clock.WaitForTimer(t)
	assertNoStart(t, starts, 50*time.Millisecond)

	nearTerm = func(base time.Time) time.Time { return base }
	changed := input
	changed.OriginName = "Tehran changed"
	updated, err := service.Update(context.Background(), created.ID, changed)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Providers[0].ConfigVersion != 2 {
		t.Fatalf("updated config version = %d, want 2", updated.Providers[0].ConfigVersion)
	}

	target := waitStart(t, starts)
	if target.MonitorID != created.ID {
		t.Fatalf("wake dispatched monitor %s, want %s", target.MonitorID, created.ID)
	}
	if target.ConfigVersion != 2 {
		t.Fatalf("dispatched config version = %d, want 2", target.ConfigVersion)
	}
	if !target.NextDueAt.Equal(now) {
		t.Fatalf("dispatched due = %v, want %v", target.NextDueAt, now)
	}
	running.stop(t)
}

func TestNewRejectsMissingDependenciesAndBadBounds(t *testing.T) {
	st, _ := newSchedulerStore(t)
	registry := registryFor(t, "fake")
	base := scheduler.Config{
		Repository: st,
		Providers:  registry,
		Interval:   fixedInterval(300 * time.Second),
	}
	if _, err := scheduler.New(scheduler.Config{}); err == nil {
		t.Fatal("New accepted a nil repository")
	}
	if _, err := scheduler.New(scheduler.Config{Repository: st}); err == nil {
		t.Fatal("New accepted nil providers")
	}
	if _, err := scheduler.New(scheduler.Config{Repository: st, Providers: registry}); err == nil {
		t.Fatal("New accepted a nil interval source")
	}
	base.Workers = scheduler.MaxWorkers + 1
	if _, err := scheduler.New(base); err == nil {
		t.Fatal("New accepted too many workers")
	}
	base.Workers = 1
	base.ShutdownGrace = -time.Second
	if _, err := scheduler.New(base); err == nil {
		t.Fatal("New accepted a negative shutdown grace")
	}
}
