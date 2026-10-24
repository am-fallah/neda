package scheduler_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/scheduler"
	"github.com/am-fallah/neda/internal/store"
)

func TestSchedulerRespectsPersistedBackoff(t *testing.T) {
	st, _ := newSchedulerStore(t)
	now := schedulerNow
	past := now.Add(-time.Minute)
	monitor := createSchedulerMonitor(t, st, "backoff-integration", true, now.Add(24*time.Hour),
		targetSpec{providerID: "fake", active: true, due: &past})
	targetID := targetIDFor(t, monitor, "fake")

	infrastructure := provider.Classification{
		Category:           provider.CategoryInfrastructure,
		Kind:               provider.ErrTimeout,
		CountsTowardOutage: true,
		TargetBackoff:      true,
	}
	var calls atomic.Int32
	starts := make(chan scheduler.Target, 2)
	applied := make(chan struct{})
	clock := newFakeClock(now)
	runner := runnerFunc(func(ctx context.Context, target scheduler.Target) (scheduler.Result, error) {
		starts <- target
		if calls.Add(1) == 1 {
			decision, err := scheduler.DecideTargetSchedule(
				now, 300*time.Second, 0, infrastructure, midpointRandom{},
			)
			if err != nil {
				return scheduler.Result{}, err
			}
			// This is the SCH-010 boundary: the policy result is persisted inside
			// the caller's outcome transaction, not by the scheduler loop.
			err = st.WithTx(ctx, func(tx *store.Store) error {
				return tx.ApplyTargetSchedule(ctx, target.ID, decision.ConsecutiveFailures, decision.NextDueAt, now)
			})
			close(applied)
			return scheduler.Result{Settled: true}, err
		}
		return scheduler.Result{Settled: true}, nil
	})
	running := startScheduler(t, st, registryFor(t, "fake"), clock,
		runner, 1, zeroRandom(), scheduler.NewWaker(), time.Second)

	first := waitStart(t, starts)
	if first.ID != targetID || first.ConfigVersion != 1 {
		t.Fatalf("first dispatch = %+v, want target %d version 1", first, targetID)
	}
	select {
	case <-applied:
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not apply the backoff schedule")
	}
	assertNoStart(t, starts, 100*time.Millisecond)

	reloaded, err := st.GetMonitor(context.Background(), monitor.ID)
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	if reloaded.Providers[0].ConsecutiveFailures != 1 {
		t.Fatalf("ConsecutiveFailures = %d, want 1", reloaded.Providers[0].ConsecutiveFailures)
	}
	wantDue := now.Add(10 * time.Minute)
	if reloaded.Providers[0].NextDueAt == nil || !reloaded.Providers[0].NextDueAt.Equal(wantDue) {
		t.Fatalf("NextDueAt = %v, want %v", reloaded.Providers[0].NextDueAt, wantDue)
	}

	clock.WaitForTimer(t)
	clock.Advance(10 * time.Minute)
	second := waitStart(t, starts)
	if second.ID != targetID {
		t.Fatalf("second dispatch target = %d, want %d", second.ID, targetID)
	}
	if calls.Load() != 2 {
		t.Fatalf("runner calls = %d, want 2", calls.Load())
	}
	running.stop(t)
}
