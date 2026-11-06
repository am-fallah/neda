package scheduler_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/scheduler"
)

// noStaggerRepository isolates the live queue-pacing behavior from startup
// staggering. Production uses the embedded store's real stagger.
type noStaggerRepository struct {
	scheduler.Repository
}

func (noStaggerRepository) StaggerDueTargets(context.Context, time.Time, func(domain.ScheduleTarget) time.Time) (int64, error) {
	return 0, nil
}

func (noStaggerRepository) ReconcileExpirations(context.Context, time.Time) (int64, *time.Time, error) {
	return 0, nil, nil
}

func (r noStaggerRepository) ListProviderRateLimits(ctx context.Context) ([]domain.ProviderRateLimit, error) {
	repo, ok := r.Repository.(scheduler.RateLimitRepository)
	if !ok {
		return nil, nil
	}
	return repo.ListProviderRateLimits(ctx)
}

func TestStartupSpreadsLargeBacklogAcrossPollingWindow(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	expires := now.Add(24 * time.Hour)
	const monitorCount = 30
	for index := 0; index < monitorCount; index++ {
		createSchedulerMonitor(t, st, fmt.Sprintf("spread-%02d", index), true, expires,
			targetSpec{providerID: "spread", active: true, due: &past})
	}

	starts := make(chan scheduler.Target, monitorCount)
	clock := newFakeClock(now)
	running := startSchedulerWithInterval(t, st, registryFor(t, "spread"), clock,
		runnerFunc(func(_ context.Context, target scheduler.Target) (scheduler.Result, error) {
			starts <- target
			return scheduler.Result{Settled: true}, nil
		}),
		2, zeroRandom(), scheduler.NewWaker(), time.Second, 5*time.Second, 10*time.Minute, nil)

	waitStart(t, starts)
	assertNoStart(t, starts, 100*time.Millisecond)

	monitors, err := st.ListMonitors(context.Background())
	if err != nil {
		t.Fatalf("ListMonitors: %v", err)
	}
	var earliest, latest time.Time
	assigned := 0
	for _, monitor := range monitors {
		for _, target := range monitor.Providers {
			if target.NextDueAt == nil {
				t.Fatalf("target %d was not scheduled", target.ID)
			}
			due := target.NextDueAt.UTC()
			if assigned == 0 || due.Before(earliest) {
				earliest = due
			}
			if assigned == 0 || due.After(latest) {
				latest = due
			}
			assigned++
		}
	}
	if assigned != monitorCount {
		t.Fatalf("scheduled targets = %d, want %d", assigned, monitorCount)
	}
	if latest.Sub(earliest) < 8*time.Minute {
		t.Fatalf("startup spread = %v, want most of the 10m window", latest.Sub(earliest))
	}
	if latest.After(now.Add(10 * time.Minute)) {
		t.Fatalf("latest startup schedule %v exceeds the polling window", latest)
	}
	running.stop(t)
}

func TestSchedulerPacesLargeProviderQueueWithoutBurst(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	expires := now.Add(24 * time.Hour)
	const monitorCount = 6
	for index := 0; index < monitorCount; index++ {
		createSchedulerMonitor(t, st, fmt.Sprintf("paced-%02d", index), true, expires,
			targetSpec{providerID: "paced", active: true, due: &past})
	}

	starts := make(chan scheduler.Target, monitorCount)
	clock := newFakeClock(now)
	repo := noStaggerRepository{Repository: st}
	running := startSchedulerWithInterval(t, repo, registryFor(t, "paced"), clock,
		runnerFunc(func(_ context.Context, target scheduler.Target) (scheduler.Result, error) {
			starts <- target
			return scheduler.Result{Settled: true}, nil
		}),
		2, zeroRandom(), scheduler.NewWaker(), time.Second, 5*time.Second, time.Minute, nil)

	seen := make(map[int64]bool, monitorCount)
	var previous time.Time
	for index := 0; index < monitorCount; index++ {
		if index > 0 {
			clock.WaitForTimer(t)
			clock.Advance(10 * time.Second)
		}
		target := waitStart(t, starts)
		if seen[target.ID] {
			t.Fatalf("target %d dispatched twice before all targets ran", target.ID)
		}
		seen[target.ID] = true
		at := clock.Now()
		if index > 0 && at.Sub(previous) < 10*time.Second {
			t.Fatalf("dispatch gap = %v, want 10s for a 6-target/60s window", at.Sub(previous))
		}
		previous = at
	}
	running.stop(t)
}

func TestNewMonitorDoesNotRescheduleExistingFutureTarget(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	future := now.Add(10 * time.Minute)
	expires := now.Add(24 * time.Hour)
	existing := createSchedulerMonitor(t, st, "existing-future", true, expires,
		targetSpec{providerID: "future", active: true, due: &future})
	existingTarget := existing.Providers[0]

	starts := make(chan scheduler.Target, 2)
	clock := newFakeClock(now)
	wake := scheduler.NewWaker()
	repo := noStaggerRepository{Repository: st}
	running := startSchedulerWithSpacing(t, repo, registryFor(t, "future", "new"), clock,
		runnerFunc(func(_ context.Context, target scheduler.Target) (scheduler.Result, error) {
			starts <- target
			return scheduler.Result{Settled: true}, nil
		}),
		1, zeroRandom(), wake, time.Second, 5*time.Second)

	past := now.Add(-time.Minute)
	created := createSchedulerMonitor(t, st, "new-due", true, expires,
		targetSpec{providerID: "new", active: true, due: &past})
	wake.Wake()
	target := waitStart(t, starts)
	if target.MonitorID != created.ID {
		t.Fatalf("new monitor dispatch = %s, want %s", target.MonitorID, created.ID)
	}
	reloaded := hTargetByID(t, st, existingTarget.ID)
	if reloaded.NextDueAt == nil || !reloaded.NextDueAt.Equal(future) {
		t.Fatalf("existing target next_due_at = %v, want unchanged %v", reloaded.NextDueAt, future)
	}
	running.stop(t)
}

func hTargetByID(t *testing.T, st interface {
	ListMonitors(context.Context) ([]domain.Monitor, error)
}, id int64) domain.MonitorProvider {
	t.Helper()
	monitors, err := st.ListMonitors(context.Background())
	if err != nil {
		t.Fatalf("ListMonitors: %v", err)
	}
	for _, monitor := range monitors {
		for _, target := range monitor.Providers {
			if target.ID == id {
				return target
			}
		}
	}
	t.Fatalf("target %d not found", id)
	return domain.MonitorProvider{}
}

func TestPersistedProviderCooldownBlocksDispatchUntilDeadline(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	expires := now.Add(24 * time.Hour)
	createSchedulerMonitor(t, st, "cooldown", true, expires,
		targetSpec{providerID: "cool", active: true, due: &past})
	cooldown := now.Add(90 * time.Second)
	if err := st.UpsertProviderRateLimit(context.Background(), domain.ProviderRateLimit{
		ProviderID:       "cool",
		Scope:            domain.ProviderRateLimitScopeProvider,
		Consecutive429s:  1,
		AdaptiveInterval: 10 * time.Minute,
		CooldownUntil:    &cooldown,
	}, now); err != nil {
		t.Fatalf("UpsertProviderRateLimit: %v", err)
	}

	starts := make(chan scheduler.Target, 1)
	clock := newFakeClock(now)
	repo := noStaggerRepository{Repository: st}
	running := startSchedulerWithSpacing(t, repo, registryFor(t, "cool"), clock,
		runnerFunc(func(_ context.Context, target scheduler.Target) (scheduler.Result, error) {
			starts <- target
			return scheduler.Result{Settled: true}, nil
		}),
		1, zeroRandom(), scheduler.NewWaker(), time.Second, 5*time.Second)

	assertNoStart(t, starts, 100*time.Millisecond)
	clock.WaitForTimer(t)
	clock.Advance(89 * time.Second)
	assertNoStart(t, starts, 100*time.Millisecond)
	clock.Advance(time.Second)
	target := waitStart(t, starts)
	if target.ProviderID != "cool" {
		t.Fatalf("cooldown dispatch provider = %s", target.ProviderID)
	}
	running.stop(t)
}

func TestProviderCooldownDoesNotBlockAnotherProvider(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	expires := now.Add(24 * time.Hour)
	createSchedulerMonitor(t, st, "cool-monitor", true, expires,
		targetSpec{providerID: "cool", active: true, due: &past})
	createSchedulerMonitor(t, st, "free-monitor", true, expires,
		targetSpec{providerID: "free", active: true, due: &past})
	cooldown := now.Add(time.Minute)
	if err := st.UpsertProviderRateLimit(context.Background(), domain.ProviderRateLimit{
		ProviderID:       "cool",
		Scope:            domain.ProviderRateLimitScopeProvider,
		AdaptiveInterval: 10 * time.Minute,
		CooldownUntil:    &cooldown,
	}, now); err != nil {
		t.Fatalf("UpsertProviderRateLimit: %v", err)
	}

	starts := make(chan scheduler.Target, 2)
	clock := newFakeClock(now)
	repo := noStaggerRepository{Repository: st}
	running := startSchedulerWithSpacing(t, repo, registryFor(t, "cool", "free"), clock,
		runnerFunc(func(_ context.Context, target scheduler.Target) (scheduler.Result, error) {
			starts <- target
			return scheduler.Result{Settled: true}, nil
		}),
		2, zeroRandom(), scheduler.NewWaker(), time.Second, 5*time.Second)

	first := waitStart(t, starts)
	if first.ProviderID != "free" {
		t.Fatalf("first dispatch provider = %s, want free", first.ProviderID)
	}
	assertNoStart(t, starts, 100*time.Millisecond)
	clock.Advance(time.Minute)
	second := waitStart(t, starts)
	if second.ProviderID != "cool" {
		t.Fatalf("second dispatch provider = %s, want cool after cooldown", second.ProviderID)
	}
	running.stop(t)
}

func TestMonitoringOffPreservesProviderCooldownOnResume(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	expires := now.Add(24 * time.Hour)
	createSchedulerMonitor(t, st, "off-cooldown", true, expires,
		targetSpec{providerID: "offcool", active: true, due: &past})
	cooldown := now.Add(time.Minute)
	if err := st.UpsertProviderRateLimit(context.Background(), domain.ProviderRateLimit{
		ProviderID:       "offcool",
		Scope:            domain.ProviderRateLimitScopeProvider,
		Consecutive429s:  1,
		AdaptiveInterval: 10 * time.Minute,
		CooldownUntil:    &cooldown,
	}, now); err != nil {
		t.Fatalf("UpsertProviderRateLimit: %v", err)
	}

	monitoring := &fakeMonitoring{enabled: false}
	starts := make(chan scheduler.Target, 1)
	clock := newFakeClock(now)
	wake := scheduler.NewWaker()
	repo := noStaggerRepository{Repository: st}
	running := startSchedulerWithMonitoring(t, repo, registryFor(t, "offcool"), clock,
		runnerFunc(func(_ context.Context, target scheduler.Target) (scheduler.Result, error) {
			starts <- target
			return scheduler.Result{Settled: true}, nil
		}),
		1, zeroRandom(), wake, time.Second, 5*time.Second, monitoring)

	assertNoStart(t, starts, 100*time.Millisecond)
	monitoring.setEnabled(true)
	wake.Wake()
	assertNoStart(t, starts, 100*time.Millisecond)
	if _, found, err := st.GetProviderRateLimit(context.Background(), "offcool", domain.ProviderRateLimitScopeProvider); err != nil || !found {
		t.Fatalf("monitoring toggle lost cooldown state: found=%v err=%v", found, err)
	}
	clock.WaitForTimer(t)
	clock.Advance(time.Minute)
	target := waitStart(t, starts)
	if target.ProviderID != "offcool" {
		t.Fatalf("resumed dispatch provider = %s", target.ProviderID)
	}
	running.stop(t)
}
