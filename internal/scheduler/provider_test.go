package scheduler_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/scheduler"
)

func TestProviderConcurrencyOneAcrossMonitors(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	expires := now.Add(24 * time.Hour)
	createSchedulerMonitor(t, st, "m1", true, expires,
		targetSpec{providerID: "fake", active: true, due: &past})
	createSchedulerMonitor(t, st, "m2", true, expires,
		targetSpec{providerID: "fake", active: true, due: &past})

	release := make(chan struct{}, 2)
	starts := make(chan scheduler.Target, 2)
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
	running := startSchedulerWithSpacing(t, st, registryFor(t, "fake"), newFakeClock(now),
		runner, 2, zeroRandom(), scheduler.NewWaker(), time.Second, 0)

	first := waitStart(t, starts)
	assertNoStart(t, starts, 100*time.Millisecond)
	release <- struct{}{}
	second := waitStart(t, starts)
	if first.ID == second.ID || first.MonitorID == second.MonitorID {
		t.Fatalf("provider concurrency test reused monitor/target: %+v / %+v", first, second)
	}
	if first.ProviderID != "fake" || second.ProviderID != "fake" {
		t.Fatalf("providers = %s/%s, want fake/fake", first.ProviderID, second.ProviderID)
	}
	release <- struct{}{}
	running.stop(t)
}

func TestProviderSpacingIsStartToStart(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	expires := now.Add(24 * time.Hour)
	createSchedulerMonitor(t, st, "m1", true, expires,
		targetSpec{providerID: "fake", active: true, due: &past})
	createSchedulerMonitor(t, st, "m2", true, expires,
		targetSpec{providerID: "fake", active: true, due: &past})

	starts := make(chan scheduler.Target, 2)
	clock := newFakeClock(now)
	running := startSchedulerWithSpacing(t, st, registryFor(t, "fake"), clock,
		runnerFunc(func(_ context.Context, target scheduler.Target) (scheduler.Result, error) {
			starts <- target
			return scheduler.Result{Settled: true}, nil
		}),
		2, zeroRandom(), scheduler.NewWaker(), time.Second, 5*time.Second)

	first := waitStart(t, starts)
	firstAt := clock.Now()
	assertNoStart(t, starts, 100*time.Millisecond)
	clock.WaitForTimer(t)
	clock.Advance(5 * time.Second)
	second := waitStart(t, starts)
	secondAt := clock.Now()
	if secondAt.Sub(firstAt) < 5*time.Second {
		t.Fatalf("provider dispatch gap = %v, want at least 5s", secondAt.Sub(firstAt))
	}
	if first.MonitorID == second.MonitorID {
		t.Fatalf("spacing test reused monitor %s", first.MonitorID)
	}
	running.stop(t)
}

func TestDifferentProvidersRunConcurrently(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	expires := now.Add(24 * time.Hour)
	createSchedulerMonitor(t, st, "alpha-monitor", true, expires,
		targetSpec{providerID: "alpha", active: true, due: &past})
	createSchedulerMonitor(t, st, "beta-monitor", true, expires,
		targetSpec{providerID: "beta", active: true, due: &past})

	release := make(chan struct{}, 2)
	starts := make(chan scheduler.Target, 2)
	runner := runnerFunc(func(ctx context.Context, target scheduler.Target) (scheduler.Result, error) {
		starts <- target
		select {
		case <-release:
			return scheduler.Result{Settled: true}, nil
		case <-ctx.Done():
			return scheduler.Result{}, ctx.Err()
		}
	})
	running := startSchedulerWithSpacing(t, st, registryFor(t, "alpha", "beta"), newFakeClock(now),
		runner, 2, zeroRandom(), scheduler.NewWaker(), time.Second, 5*time.Second)

	first := waitStart(t, starts)
	second := waitStart(t, starts)
	if first.ProviderID == second.ProviderID {
		t.Fatalf("different-provider test dispatched provider %s twice", first.ProviderID)
	}
	release <- struct{}{}
	release <- struct{}{}
	running.stop(t)
}

func TestProviderInFlightBlocksEvenAfterSpacingElapses(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	expires := now.Add(24 * time.Hour)
	createSchedulerMonitor(t, st, "m1", true, expires,
		targetSpec{providerID: "fake", active: true, due: &past})
	createSchedulerMonitor(t, st, "m2", true, expires,
		targetSpec{providerID: "fake", active: true, due: &past})

	release := make(chan struct{}, 2)
	starts := make(chan scheduler.Target, 2)
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
	running := startSchedulerWithSpacing(t, st, registryFor(t, "fake"), clock,
		runner, 2, zeroRandom(), scheduler.NewWaker(), time.Second, 5*time.Second)

	waitStart(t, starts)
	clock.Advance(10 * time.Second)
	assertNoStart(t, starts, 100*time.Millisecond)
	release <- struct{}{}
	second := waitStart(t, starts)
	if second.MonitorID == "" {
		t.Fatal("second dispatch has no monitor")
	}
	release <- struct{}{}
	running.stop(t)
}

func TestProviderSpacingCombinesWithUnsettledHold(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	createSchedulerMonitor(t, st, "m1", true, now.Add(24*time.Hour),
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
	running := startSchedulerWithSpacing(t, st, registryFor(t, "fake"), clock,
		runner, 1, zeroRandom(), scheduler.NewWaker(), time.Second, 5*time.Second)

	waitStart(t, starts)
	clock.WaitForTimer(t) // unsettled hold: 1s
	clock.Advance(time.Second)
	assertNoStart(t, starts, 50*time.Millisecond)
	clock.WaitForTimer(t) // remaining provider slot: 4s
	clock.Advance(4 * time.Second)
	second := waitStart(t, starts)
	if second.MonitorID != "m1" {
		t.Fatalf("retry dispatched monitor %s, want m1", second.MonitorID)
	}
	if calls.Load() != 2 {
		t.Fatalf("runner calls = %d, want 2", calls.Load())
	}
	running.stop(t)
}

func TestProviderSpacingResetsOnRestart(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	createSchedulerMonitor(t, st, "restart", true, now.Add(24*time.Hour),
		targetSpec{providerID: "fake", active: true, due: &past})

	firstStarts := make(chan scheduler.Target, 1)
	first := startSchedulerWithSpacing(t, st, registryFor(t, "fake"), newFakeClock(now),
		runnerFunc(func(_ context.Context, target scheduler.Target) (scheduler.Result, error) {
			firstStarts <- target
			return scheduler.Result{Settled: true}, nil
		}),
		1, zeroRandom(), scheduler.NewWaker(), time.Second, 5*time.Second)
	waitStart(t, firstStarts)
	first.stop(t)

	secondStarts := make(chan scheduler.Target, 1)
	second := startSchedulerWithSpacing(t, st, registryFor(t, "fake"), newFakeClock(now),
		runnerFunc(func(_ context.Context, target scheduler.Target) (scheduler.Result, error) {
			secondStarts <- target
			return scheduler.Result{Settled: true}, nil
		}),
		1, zeroRandom(), scheduler.NewWaker(), time.Second, 5*time.Second)
	waitStart(t, secondStarts)
	second.stop(t)
}

func TestProviderSpacingDefaultsAndRejectsNegative(t *testing.T) {
	if scheduler.DefaultProviderSpacing != 5*time.Second {
		t.Fatalf("DefaultProviderSpacing = %v, want 5s", scheduler.DefaultProviderSpacing)
	}
	st, _ := newSchedulerStore(t)
	registry := registryFor(t, "fake")
	negative := -time.Second
	if _, err := scheduler.New(scheduler.Config{
		Repository:      st,
		Providers:       registry,
		Interval:        fixedInterval(300 * time.Second),
		ProviderSpacing: &negative,
	}); err == nil {
		t.Fatal("scheduler.New accepted negative provider spacing")
	}
	if _, err := scheduler.New(scheduler.Config{
		Repository: st,
		Providers:  registry,
		Interval:   fixedInterval(300 * time.Second),
	}); err != nil {
		t.Fatalf("scheduler.New rejected nil spacing default: %v", err)
	}
}
