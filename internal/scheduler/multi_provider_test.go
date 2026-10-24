package scheduler_test

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/scheduler"
)

// TestMultiProviderOneFailsAnotherSucceeds proves that one provider's failure
// does not prevent another provider's successful check, availability state,
// health success, event, or outbox rows from committing.
func TestMultiProviderOneFailsAnotherSucceeds(t *testing.T) {
	alpha := newRecordingProvider("alpha", nil)
	alpha.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return timeoutError()
	}
	beta := newRecordingProvider("beta", nil)
	beta.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return reservableResult(schedulerNow, "beta"), nil
	}
	h := newIntegrationHarness(t, alpha, beta)
	h.createMonitor("multi-one-fails", true, schedulerNow.Add(24*time.Hour),
		harnessTargetFor("alpha"), harnessTargetFor("beta"))
	alphaTarget := h.target("multi-one-fails", "alpha")
	betaTarget := h.target("multi-one-fails", "beta")

	if _, err := h.runFor("multi-one-fails", "alpha"); err != nil {
		t.Fatalf("alpha run: %v", err)
	}
	if _, err := h.runFor("multi-one-fails", "beta"); err != nil {
		t.Fatalf("beta run: %v", err)
	}

	alphaCheck := h.latestCheck(alphaTarget.ID)
	if alphaCheck.Outcome != "error" || !alphaCheck.ErrorKind.Valid || alphaCheck.ErrorKind.String != "timeout" {
		t.Fatalf("alpha check = %+v, want error/timeout", alphaCheck)
	}
	betaCheck := h.latestCheck(betaTarget.ID)
	if betaCheck.Outcome != "success" || betaCheck.Reservable != 1 {
		t.Fatalf("beta check = %+v, want success/reservable", betaCheck)
	}

	if states := h.count(`SELECT COUNT(*) FROM inventory_states WHERE monitor_provider_id = ?`, alphaTarget.ID); states != 0 {
		t.Fatalf("alpha inventory states = %d, want 0", states)
	}
	if states := h.count(`SELECT COUNT(*) FROM inventory_states WHERE monitor_provider_id = ?`, betaTarget.ID); states != 1 {
		t.Fatalf("beta inventory states = %d, want 1", states)
	}
	if episodes := h.count(`SELECT COUNT(*) FROM availability_episodes ae JOIN inventory_states s ON s.id = ae.state_id
		WHERE s.monitor_provider_id = ? AND ae.closed_at IS NULL`, betaTarget.ID); episodes != 1 {
		t.Fatalf("beta open episodes = %d, want 1", episodes)
	}
	if events := h.count(`SELECT COUNT(*) FROM events WHERE kind = 'availability' AND provider_id = 'alpha'`); events != 0 {
		t.Fatalf("alpha availability events = %d, want 0", events)
	}
	if events := h.count(`SELECT COUNT(*) FROM events WHERE kind = 'availability' AND provider_id = 'beta'`); events != 1 {
		t.Fatalf("beta availability events = %d, want 1", events)
	}
	if deliveries := h.count(`SELECT COUNT(*) FROM notification_deliveries`); deliveries != 2 {
		t.Fatalf("deliveries = %d, want 2 for beta only", deliveries)
	}

	alphaHealth := h.providerHealth("alpha")
	if !alphaHealth.Found || alphaHealth.Consecutive != 1 {
		t.Fatalf("alpha health = %+v, want one failure", alphaHealth)
	}
	betaHealth := h.providerHealth("beta")
	if !betaHealth.Found || betaHealth.Consecutive != 0 || !betaHealth.LastSuccessAt.Valid {
		t.Fatalf("beta health = %+v, want success", betaHealth)
	}

	alphaAfter := h.targetByID(alphaTarget.ID)
	if alphaAfter.LastOutcome != "error" || alphaAfter.ConsecutiveFailures != 1 ||
		alphaAfter.NextDueAt == nil || !alphaAfter.NextDueAt.Equal(schedulerNow.Add(10*time.Minute)) {
		t.Fatalf("alpha target after failure = %+v", alphaAfter)
	}
	betaAfter := h.targetByID(betaTarget.ID)
	if betaAfter.LastOutcome != "success" || betaAfter.ConsecutiveFailures != 0 ||
		betaAfter.NextDueAt == nil || !betaAfter.NextDueAt.Equal(schedulerNow.Add(5*time.Minute)) {
		t.Fatalf("beta target after success = %+v", betaAfter)
	}
}

// TestMultiProviderIndependentAvailabilityEpisodesAndEvents proves that each
// provider owns its own inventory state, episode, event, and outbox rows.
// Closing one provider's episode must not close, merge, or suppress the other
// provider's episode or event.
func TestMultiProviderIndependentAvailabilityEpisodesAndEvents(t *testing.T) {
	alphaCalls := atomic.Int32{}
	alpha := newRecordingProvider("alpha", nil)
	alpha.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		if alphaCalls.Add(1) == 1 {
			return reservableResult(schedulerNow, "shared"), nil
		}
		return notReservableResult(schedulerNow, "shared"), nil
	}
	beta := newRecordingProvider("beta", nil)
	beta.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		// Deliberately use the same journey/offering keys as alpha. Inventory
		// rows are scoped by monitor_provider_id, so two independent states must
		// still exist even when the provider-reported keys collide.
		return reservableResult(schedulerNow, "shared"), nil
	}
	h := newIntegrationHarness(t, alpha, beta)
	h.createMonitor("multi-episodes", true, schedulerNow.Add(24*time.Hour),
		harnessTargetFor("alpha"), harnessTargetFor("beta"))
	alphaTarget := h.target("multi-episodes", "alpha")
	betaTarget := h.target("multi-episodes", "beta")

	for _, providerID := range []provider.ID{"alpha", "beta"} {
		if _, err := h.runFor("multi-episodes", providerID); err != nil {
			t.Fatalf("%s first run: %v", providerID, err)
		}
	}
	if states := h.count(`SELECT COUNT(*) FROM inventory_states`); states != 2 {
		t.Fatalf("inventory states = %d, want 2", states)
	}
	if episodes := h.count(`SELECT COUNT(*) FROM availability_episodes WHERE closed_at IS NULL`); episodes != 2 {
		t.Fatalf("open episodes = %d, want 2", episodes)
	}
	if events := h.count(`SELECT COUNT(*) FROM events WHERE kind = 'availability'`); events != 2 {
		t.Fatalf("availability events = %d, want 2", events)
	}
	if distinct := h.count(`SELECT COUNT(DISTINCT episode_id) FROM events WHERE kind = 'availability'`); distinct != 2 {
		t.Fatalf("distinct event episodes = %d, want 2", distinct)
	}
	if deliveries := h.count(`SELECT COUNT(*) FROM notification_deliveries`); deliveries != 4 {
		t.Fatalf("deliveries = %d, want 4", deliveries)
	}

	// Alpha closes; beta remains continuously reservable and must not emit a
	// duplicate event or close its episode.
	if _, err := h.runFor("multi-episodes", "alpha"); err != nil {
		t.Fatalf("alpha second run: %v", err)
	}
	if _, err := h.runFor("multi-episodes", "beta"); err != nil {
		t.Fatalf("beta second run: %v", err)
	}
	if closed := h.count(`SELECT COUNT(*) FROM availability_episodes ae JOIN inventory_states s ON s.id = ae.state_id
		WHERE s.monitor_provider_id = ? AND ae.closed_at IS NOT NULL AND ae.close_reason = 'not_reservable'`, alphaTarget.ID); closed != 1 {
		t.Fatalf("alpha closed episodes = %d, want 1", closed)
	}
	if open := h.count(`SELECT COUNT(*) FROM availability_episodes ae JOIN inventory_states s ON s.id = ae.state_id
		WHERE s.monitor_provider_id = ? AND ae.closed_at IS NULL`, betaTarget.ID); open != 1 {
		t.Fatalf("beta open episodes = %d, want 1 after alpha close", open)
	}
	if events := h.count(`SELECT COUNT(*) FROM events WHERE kind = 'availability' AND provider_id = 'beta'`); events != 1 {
		t.Fatalf("beta availability events = %d, want 1 (no duplicate for continuous state)", events)
	}
	if events := h.count(`SELECT COUNT(*) FROM events WHERE kind = 'availability'`); events != 2 {
		t.Fatalf("availability events = %d, want 2 total", events)
	}
	if deliveries := h.count(`SELECT COUNT(*) FROM notification_deliveries`); deliveries != 4 {
		t.Fatalf("deliveries = %d, want 4 after alpha close", deliveries)
	}
}

// TestMultiProviderProviderHealthIsolation verifies that provider health is
// global per provider ID and independent across provider IDs. A success for
// provider beta must not recover degraded provider alpha, while a later alpha
// success on another monitor must recover alpha's global health.
func TestMultiProviderProviderHealthIsolation(t *testing.T) {
	alphaCalls := atomic.Int32{}
	alpha := newRecordingProvider("alpha", nil)
	alpha.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		if alphaCalls.Add(1) <= 3 {
			return timeoutError()
		}
		return reservableResult(schedulerNow, "alpha"), nil
	}
	beta := newRecordingProvider("beta", nil)
	beta.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return reservableResult(schedulerNow, "beta"), nil
	}
	h := newIntegrationHarness(t, alpha, beta)
	h.createMonitor("health-degraded", true, schedulerNow.Add(24*time.Hour),
		harnessTargetFor("alpha"), harnessTargetFor("beta"))

	for i := 0; i < 3; i++ {
		if _, err := h.runFor("health-degraded", "alpha"); err != nil {
			t.Fatalf("alpha failure %d: %v", i+1, err)
		}
	}
	alphaHealth := h.providerHealth("alpha")
	if !alphaHealth.Found || alphaHealth.Consecutive != 3 || alphaHealth.OpenEpisodeCount != 1 ||
		alphaHealth.DegradedEvents != 1 || alphaHealth.RecoveryEvents != 0 {
		t.Fatalf("alpha health after 3 failures = %+v", alphaHealth)
	}
	if global := h.count(`SELECT COUNT(*) FROM events WHERE kind = 'provider_degraded'
		AND provider_id = 'alpha' AND monitor_id IS NULL`); global != 1 {
		t.Fatalf("alpha degraded events with monitor id = %d, want one global event", global)
	}

	if _, err := h.runFor("health-degraded", "beta"); err != nil {
		t.Fatalf("beta success: %v", err)
	}
	alphaHealth = h.providerHealth("alpha")
	if alphaHealth.Consecutive != 3 || alphaHealth.OpenEpisodeCount != 1 || alphaHealth.RecoveryEvents != 0 {
		t.Fatalf("beta success recovered alpha: %+v", alphaHealth)
	}
	betaHealth := h.providerHealth("beta")
	if !betaHealth.Found || betaHealth.Consecutive != 0 || betaHealth.DegradedEvents != 0 {
		t.Fatalf("beta health = %+v, want independent success", betaHealth)
	}

	// Provider health is keyed by provider ID, not by monitor. A success for
	// alpha on a second monitor must recover the same global alpha episode.
	h.createMonitor("health-recovered", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("alpha"))
	if _, err := h.runFor("health-recovered", "alpha"); err != nil {
		t.Fatalf("alpha recovery run: %v", err)
	}
	alphaHealth = h.providerHealth("alpha")
	if alphaHealth.Consecutive != 0 || alphaHealth.OpenEpisodeCount != 0 || alphaHealth.RecoveryEvents != 1 {
		t.Fatalf("alpha health after recovery = %+v", alphaHealth)
	}
	if alphaHealth.HealthDeliveryCnt != 4 {
		t.Fatalf("alpha health deliveries = %d, want 4 (degraded + recovery)", alphaHealth.HealthDeliveryCnt)
	}
	betaHealth = h.providerHealth("beta")
	if betaHealth.Consecutive != 0 || betaHealth.DegradedEvents != 0 || betaHealth.RecoveryEvents != 0 {
		t.Fatalf("alpha recovery changed beta health: %+v", betaHealth)
	}
}

// TestMultiProviderPerTargetSchedulingIsolation verifies that one target's
// exponential backoff does not change another target's due time or failure
// counter.
func TestMultiProviderPerTargetSchedulingIsolation(t *testing.T) {
	alpha := newRecordingProvider("alpha", nil)
	alpha.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return timeoutError()
	}
	beta := newRecordingProvider("beta", nil)
	beta.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return reservableResult(schedulerNow, "beta"), nil
	}
	h := newIntegrationHarness(t, alpha, beta)
	h.createMonitor("multi-schedule", true, schedulerNow.Add(24*time.Hour),
		harnessTargetFor("alpha"), harnessTargetFor("beta"))
	alphaTarget := h.target("multi-schedule", "alpha")
	betaTarget := h.target("multi-schedule", "beta")

	// Establish beta's schedule first, then prove alpha's failure writes do not
	// touch it. Running beta again after alpha would mask a cross-target write
	// because beta's success would restore its own expected values.
	if _, err := h.runFor("multi-schedule", "beta"); err != nil {
		t.Fatalf("beta success: %v", err)
	}
	betaBefore := h.targetByID(betaTarget.ID)
	if betaBefore.ConsecutiveFailures != 0 || betaBefore.NextDueAt == nil ||
		!betaBefore.NextDueAt.Equal(schedulerNow.Add(5*time.Minute)) {
		t.Fatalf("beta schedule before alpha failures = %+v", betaBefore)
	}

	for i := 0; i < 2; i++ {
		if _, err := h.runFor("multi-schedule", "alpha"); err != nil {
			t.Fatalf("alpha failure %d: %v", i+1, err)
		}
	}
	alphaAfter := h.targetByID(alphaTarget.ID)
	if alphaAfter.ConsecutiveFailures != 2 || alphaAfter.NextDueAt == nil ||
		!alphaAfter.NextDueAt.Equal(schedulerNow.Add(20*time.Minute)) {
		t.Fatalf("alpha schedule = %+v, want 2 failures and 20m backoff", alphaAfter)
	}
	betaAfter := h.targetByID(betaTarget.ID)
	if betaAfter.ConsecutiveFailures != betaBefore.ConsecutiveFailures ||
		!sameTimePointer(betaAfter.NextDueAt, betaBefore.NextDueAt) ||
		betaAfter.LastOutcome != betaBefore.LastOutcome {
		t.Fatalf("alpha failures changed beta schedule: before=%+v after=%+v", betaBefore, betaAfter)
	}
}

// TestMultiProviderSequentialWithinMonitor proves the A-33 monitor-wide guard
// still serializes different providers of one monitor while the real outcome
// runner and check stage execute. It does not treat dispatcher serialization as
// a substitute for the isolation assertions above.
func TestMultiProviderSequentialWithinMonitor(t *testing.T) {
	alphaStarted := make(chan struct{})
	betaStarted := make(chan struct{})
	releaseAlpha := make(chan struct{})

	var mu sync.Mutex
	active := 0
	maxActive := 0
	var order []provider.ID
	enter := func(id provider.ID) {
		mu.Lock()
		defer mu.Unlock()
		active++
		if active > maxActive {
			maxActive = active
		}
		order = append(order, id)
	}
	leave := func() {
		mu.Lock()
		defer mu.Unlock()
		active--
	}

	alpha := newRecordingProvider("alpha", nil)
	alpha.checkFn = func(ctx context.Context, _ provider.CheckRequest) (provider.Result, error) {
		enter("alpha")
		defer leave()
		close(alphaStarted)
		select {
		case <-releaseAlpha:
			return reservableResult(schedulerNow, "alpha"), nil
		case <-ctx.Done():
			return provider.Result{}, ctx.Err()
		}
	}
	beta := newRecordingProvider("beta", nil)
	beta.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		enter("beta")
		defer leave()
		close(betaStarted)
		return reservableResult(schedulerNow, "beta"), nil
	}
	h := newIntegrationHarness(t, alpha, beta)
	h.createMonitor("multi-sequential", true, schedulerNow.Add(24*time.Hour),
		harnessTargetFor("alpha"), harnessTargetFor("beta"))
	alphaTarget := h.target("multi-sequential", "alpha")
	betaTarget := h.target("multi-sequential", "beta")
	running := h.startScheduler(2, 0)
	defer running.stop(t)

	select {
	case <-alphaStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("alpha check did not start")
	}
	select {
	case <-betaStarted:
		t.Fatal("beta started while alpha was in flight on the same monitor")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseAlpha)
	select {
	case <-betaStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("beta did not start after alpha completed")
	}
	h.waitForCheckOutcome(alphaTarget.ID, "success")
	h.waitForCheckOutcome(betaTarget.ID, "success")

	mu.Lock()
	gotMax := maxActive
	gotOrder := append([]provider.ID(nil), order...)
	mu.Unlock()
	if gotMax != 1 {
		t.Fatalf("max concurrent providers within one monitor = %d, want 1", gotMax)
	}
	seen := make(map[provider.ID]bool, len(gotOrder))
	for _, id := range gotOrder {
		seen[id] = true
	}
	if len(gotOrder) != 2 || !seen["alpha"] || !seen["beta"] {
		t.Fatalf("provider execution set = %v, want alpha and beta exactly once", gotOrder)
	}
}

// TestMultiProviderDiscardDoesNotBlockOtherProvider proves that discarding a
// stale provider result finalizes only that check run and still allows the
// other provider of the same monitor to run and commit.
func TestMultiProviderDiscardDoesNotBlockOtherProvider(t *testing.T) {
	alphaStarted := make(chan struct{})
	releaseAlpha := make(chan struct{})
	alpha := newRecordingProvider("alpha", nil)
	alpha.checkFn = func(ctx context.Context, _ provider.CheckRequest) (provider.Result, error) {
		close(alphaStarted)
		select {
		case <-releaseAlpha:
			return reservableResult(schedulerNow, "alpha"), nil
		case <-ctx.Done():
			return provider.Result{}, ctx.Err()
		}
	}
	beta := newRecordingProvider("beta", nil)
	beta.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return reservableResult(schedulerNow, "beta"), nil
	}
	h := newIntegrationHarness(t, alpha, beta)
	h.createMonitor("multi-discard", true, schedulerNow.Add(24*time.Hour),
		harnessTargetFor("alpha"), harnessTargetFor("beta"))
	alphaTarget := h.target("multi-discard", "alpha")
	betaTarget := h.target("multi-discard", "beta")
	running := h.startScheduler(1, 0)
	defer running.stop(t)

	select {
	case <-alphaStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("alpha check did not start")
	}
	if _, err := h.sqlDB.Exec(`UPDATE monitor_providers SET config_version = config_version + 1 WHERE id = ?`, alphaTarget.ID); err != nil {
		t.Fatalf("change alpha config: %v", err)
	}
	close(releaseAlpha)
	h.waitForCheckOutcome(alphaTarget.ID, "discarded")
	h.waitForCheckOutcome(betaTarget.ID, "success")

	alphaCheck := h.latestCheck(alphaTarget.ID)
	if !alphaCheck.ErrorKind.Valid || alphaCheck.ErrorKind.String != "config_changed" || alphaCheck.ConfigVersion != 1 {
		t.Fatalf("alpha discarded check = %+v", alphaCheck)
	}
	betaCheck := h.latestCheck(betaTarget.ID)
	if betaCheck.Outcome != "success" || betaCheck.Reservable != 1 {
		t.Fatalf("beta check = %+v, want success", betaCheck)
	}
	if events := h.count(`SELECT COUNT(*) FROM events WHERE kind = 'availability' AND provider_id = 'alpha'`); events != 0 {
		t.Fatalf("alpha availability events = %d, want 0", events)
	}
	if events := h.count(`SELECT COUNT(*) FROM events WHERE kind = 'availability' AND provider_id = 'beta'`); events != 1 {
		t.Fatalf("beta availability events = %d, want 1", events)
	}
	if states := h.count(`SELECT COUNT(*) FROM inventory_states WHERE monitor_provider_id = ?`, alphaTarget.ID); states != 0 {
		t.Fatalf("alpha states = %d, want 0", states)
	}
	if states := h.count(`SELECT COUNT(*) FROM inventory_states WHERE monitor_provider_id = ?`, betaTarget.ID); states != 1 {
		t.Fatalf("beta states = %d, want 1", states)
	}
}

// TestMultiProviderIndependentEventAndOutboxCounts proves that one provider's
// event/outbox rows are not merged with or suppressed by another provider's
// empty result, and that a later event adds an independent set of rows.
func TestMultiProviderIndependentEventAndOutboxCounts(t *testing.T) {
	alpha := newRecordingProvider("alpha", nil)
	alpha.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return reservableResult(schedulerNow, "shared"), nil
	}
	betaCalls := atomic.Int32{}
	beta := newRecordingProvider("beta", nil)
	beta.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		if betaCalls.Add(1) == 1 {
			return emptyResult(schedulerNow), nil
		}
		return reservableResult(schedulerNow, "shared"), nil
	}
	h := newIntegrationHarness(t, alpha, beta)
	h.createMonitor("multi-counts", true, schedulerNow.Add(24*time.Hour),
		harnessTargetFor("alpha"), harnessTargetFor("beta"))
	betaTarget := h.target("multi-counts", "beta")

	if _, err := h.runFor("multi-counts", "alpha"); err != nil {
		t.Fatalf("alpha run: %v", err)
	}
	if _, err := h.runFor("multi-counts", "beta"); err != nil {
		t.Fatalf("beta empty run: %v", err)
	}
	if events := h.count(`SELECT COUNT(*) FROM events WHERE kind = 'availability'`); events != 1 {
		t.Fatalf("availability events = %d, want alpha's single event", events)
	}
	if events := h.count(`SELECT COUNT(*) FROM events WHERE kind = 'availability' AND provider_id = 'beta'`); events != 0 {
		t.Fatalf("beta availability events = %d, want 0 for empty result", events)
	}
	if deliveries := h.count(`SELECT COUNT(*) FROM notification_deliveries`); deliveries != 2 {
		t.Fatalf("deliveries = %d, want 2 for alpha only", deliveries)
	}
	if states := h.count(`SELECT COUNT(*) FROM inventory_states WHERE monitor_provider_id = ?`, betaTarget.ID); states != 0 {
		t.Fatalf("beta states = %d, want 0 for empty result", states)
	}

	if _, err := h.runFor("multi-counts", "beta"); err != nil {
		t.Fatalf("beta reservable run: %v", err)
	}
	if events := h.count(`SELECT COUNT(*) FROM events WHERE kind = 'availability'`); events != 2 {
		t.Fatalf("availability events = %d, want 2 independent events", events)
	}
	if distinct := h.count(`SELECT COUNT(DISTINCT episode_id) FROM events WHERE kind = 'availability'`); distinct != 2 {
		t.Fatalf("distinct event episodes = %d, want 2", distinct)
	}
	if deliveries := h.count(`SELECT COUNT(*) FROM notification_deliveries`); deliveries != 4 {
		t.Fatalf("deliveries = %d, want 4 independent rows", deliveries)
	}
	if perProvider := h.count(`SELECT COUNT(*) FROM notification_deliveries d
		JOIN events e ON e.id = d.event_id WHERE e.provider_id = 'beta'`); perProvider != 2 {
		t.Fatalf("beta deliveries = %d, want 2", perProvider)
	}
}

// TestMultiProviderExpiryClosesAllEpisodesWithoutEventsOrPolling proves that
// SCH-007 expiry reconciliation closes every open episode belonging to every
// provider of a monitor, creates no availability/health events or outbox rows,
// and never dispatches another poll for the expired monitor.
func TestMultiProviderExpiryClosesAllEpisodesWithoutEventsOrPolling(t *testing.T) {
	alpha := newRecordingProvider("alpha", nil)
	alpha.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return reservableResult(schedulerNow, "alpha"), nil
	}
	beta := newRecordingProvider("beta", nil)
	beta.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return reservableResult(schedulerNow, "beta"), nil
	}
	h := newIntegrationHarness(t, alpha, beta)
	expiresAt := schedulerNow.Add(time.Minute)
	h.createMonitor("multi-expiry", true, expiresAt,
		harnessTargetFor("alpha"), harnessTargetFor("beta"))
	for _, providerID := range []provider.ID{"alpha", "beta"} {
		if _, err := h.runFor("multi-expiry", providerID); err != nil {
			t.Fatalf("%s run: %v", providerID, err)
		}
	}
	if open := h.count(`SELECT COUNT(*) FROM availability_episodes WHERE closed_at IS NULL`); open != 2 {
		t.Fatalf("open episodes before expiry = %d, want 2", open)
	}
	checkRunsBefore := h.count(`SELECT COUNT(*) FROM check_runs`)
	eventsBefore := h.count(`SELECT COUNT(*) FROM events`)
	deliveriesBefore := h.count(`SELECT COUNT(*) FROM notification_deliveries`)

	starts := make(chan scheduler.Target, 1)
	running := h.startSchedulerWithRunner(runnerFunc(func(_ context.Context, target scheduler.Target) (scheduler.Result, error) {
		starts <- target
		return scheduler.Result{Settled: true}, nil
	}), 1, 0)
	defer running.stop(t)

	h.clock.WaitForTimer(t)
	assertNoStart(t, starts, 50*time.Millisecond)
	h.clock.Advance(time.Minute)
	waitForExpired(t, h.st, "multi-expiry")
	assertNoStart(t, starts, 100*time.Millisecond)

	if closed := h.count(`SELECT COUNT(*) FROM availability_episodes
		WHERE closed_at IS NOT NULL AND close_reason = 'expired'`); closed != 2 {
		t.Fatalf("expired episodes = %d, want 2", closed)
	}
	if open := h.count(`SELECT COUNT(*) FROM availability_episodes WHERE closed_at IS NULL`); open != 0 {
		t.Fatalf("open episodes after expiry = %d, want 0", open)
	}
	if got := h.count(`SELECT COUNT(*) FROM check_runs`); got != checkRunsBefore {
		t.Fatalf("expiry created %d new check runs, want 0", got-checkRunsBefore)
	}
	if got := h.count(`SELECT COUNT(*) FROM events`); got != eventsBefore {
		t.Fatalf("expiry changed events from %d to %d", eventsBefore, got)
	}
	if got := h.count(`SELECT COUNT(*) FROM notification_deliveries`); got != deliveriesBefore {
		t.Fatalf("expiry changed deliveries from %d to %d", deliveriesBefore, got)
	}

	// Even after the original next-due time, the expired monitor must not poll.
	h.clock.Advance(10 * time.Minute)
	assertNoStart(t, starts, 100*time.Millisecond)
	if got := h.count(`SELECT COUNT(*) FROM check_runs`); got != checkRunsBefore {
		t.Fatalf("post-expiry poll created %d check runs", got-checkRunsBefore)
	}
}

// TestMultiProviderExpiryClosesEveryOpenEpisode is a focused guard against a
// regression where expiry closed only the first provider's episode for a
// monitor. It asserts the exact closed episode set by target ID.
func TestMultiProviderExpiryClosesEveryOpenEpisode(t *testing.T) {
	alpha := newRecordingProvider("alpha", nil)
	alpha.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return reservableResult(schedulerNow, "alpha"), nil
	}
	beta := newRecordingProvider("beta", nil)
	beta.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return reservableResult(schedulerNow, "beta"), nil
	}
	h := newIntegrationHarness(t, alpha, beta)
	h.createMonitor("multi-expiry-set", true, schedulerNow.Add(time.Minute),
		harnessTargetFor("alpha"), harnessTargetFor("beta"))
	alphaTarget := h.target("multi-expiry-set", "alpha")
	betaTarget := h.target("multi-expiry-set", "beta")
	for _, providerID := range []provider.ID{"alpha", "beta"} {
		if _, err := h.runFor("multi-expiry-set", providerID); err != nil {
			t.Fatalf("%s run: %v", providerID, err)
		}
	}

	expired, _, err := h.st.ReconcileExpirations(context.Background(), schedulerNow.Add(time.Minute))
	if err != nil {
		t.Fatalf("ReconcileExpirations: %v", err)
	}
	if expired != 1 {
		t.Fatalf("expired monitors = %d, want 1", expired)
	}
	rows, err := h.sqlDB.Query(`SELECT s.monitor_provider_id, ae.close_reason
		FROM availability_episodes ae JOIN inventory_states s ON s.id = ae.state_id
		WHERE s.monitor_provider_id IN (?, ?) ORDER BY s.monitor_provider_id`, alphaTarget.ID, betaTarget.ID)
	if err != nil {
		t.Fatalf("query expired episodes: %v", err)
	}
	defer rows.Close()
	seen := make(map[int64]string)
	for rows.Next() {
		var targetID int64
		var reason sql.NullString
		if err := rows.Scan(&targetID, &reason); err != nil {
			t.Fatalf("scan expired episode: %v", err)
		}
		seen[targetID] = reason.String
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate expired episodes: %v", err)
	}
	for _, target := range []struct {
		id   int64
		name string
	}{{alphaTarget.ID, "alpha"}, {betaTarget.ID, "beta"}} {
		if reason, ok := seen[target.id]; !ok || reason != "expired" {
			t.Fatalf("%s episode close reason = %q, want expired", target.name, reason)
		}
	}
}
