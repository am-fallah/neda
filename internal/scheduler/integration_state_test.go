package scheduler_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/scheduler"
)

// TestSchedulerIntegrationMaterialEditDiscardsStaleAttempt covers SCH-009 AC 9
// and the stale in-flight addendum: an attempt starts with the old config,
// blocks while a material edit commits a new config and near-term schedule,
// then completes. The SCH-010 validity gate must discard it so it cannot
// overwrite the edited target's scheduling or provider health. A subsequent
// check uses the new config.
func TestSchedulerIntegrationMaterialEditDiscardsStaleAttempt(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	calls := atomic.Int32{}
	adapter := newRecordingProvider("material-p", nil)
	adapter.checkFn = func(ctx context.Context, _ provider.CheckRequest) (provider.Result, error) {
		switch calls.Add(1) {
		case 1:
			return reservableResult(schedulerNow, "material"), nil
		case 2:
			close(started)
			select {
			case <-release:
				// A timeout would increment provider/target health and apply
				// backoff if the validity gate failed; the gate must discard it.
				return timeoutError()
			case <-ctx.Done():
				return provider.Result{}, ctx.Err()
			}
		default:
			return reservableResult(schedulerNow, "material"), nil
		}
	}
	h := newIntegrationHarness(t, adapter)
	h.createMonitor("integration-material", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("material-p"))
	target := h.target("integration-material", "material-p")

	// Seed an old-config availability state/episode and success health so the
	// stale timeout has something it could incorrectly overwrite.
	if _, err := h.runFor("integration-material", "material-p"); err != nil {
		t.Fatalf("seed success: %v", err)
	}
	healthBefore := h.providerHealth("material-p")
	beforeEvents := h.count(`SELECT COUNT(*) FROM events`)
	beforeDeliveries := h.count(`SELECT COUNT(*) FROM notification_deliveries`)

	type runResult struct {
		result scheduler.Result
		err    error
	}
	results := make(chan runResult, 1)
	go func() {
		result, err := h.runner.Run(context.Background(), h.dispatchTarget("integration-material", "material-p"))
		results <- runResult{result: result, err: err}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("stale attempt did not start")
	}

	nearTerm := schedulerNow.Add(2 * time.Minute)
	updated := h.updateMonitor("integration-material", 2*time.Minute, func(input *domain.MonitorInput) {
		input.OriginName = "Tehran changed"
	})
	if updated.Providers[0].ConfigVersion != 2 {
		t.Fatalf("material edit config version = %d, want 2", updated.Providers[0].ConfigVersion)
	}
	if updated.Providers[0].NextDueAt == nil || !updated.Providers[0].NextDueAt.Equal(nearTerm) {
		t.Fatalf("material edit next_due_at = %v, want %v", updated.Providers[0].NextDueAt, nearTerm)
	}

	close(release)
	var got runResult
	select {
	case got = <-results:
	case <-time.After(2 * time.Second):
		t.Fatal("stale attempt did not finish")
	}
	if got.err != nil {
		t.Fatalf("stale attempt error: %v", got.err)
	}
	if got.result.Settled {
		t.Fatal("stale attempt was settled")
	}
	check := h.latestCheck(target.ID)
	if check.Outcome != "discarded" || !check.ErrorKind.Valid || check.ErrorKind.String != "config_changed" ||
		check.ConfigVersion != 1 {
		t.Fatalf("stale check = %+v, want discarded/config_changed/config 1", check)
	}

	after := h.targetByID(target.ID)
	if after.ConfigVersion != 2 || after.NextDueAt == nil || !after.NextDueAt.Equal(nearTerm) ||
		after.LastOutcome != "success" || after.LastSuccessAt == nil ||
		after.ConsecutiveFailures != 0 {
		t.Fatalf("stale attempt overwrote target scheduling/health: %+v", after)
	}
	healthAfter := h.providerHealth("material-p")
	if healthAfter.Consecutive != 0 ||
		healthAfter.LastSuccessAt.String != healthBefore.LastSuccessAt.String ||
		healthAfter.LastDurationMS.Int64 != healthBefore.LastDurationMS.Int64 {
		t.Fatalf("stale attempt overwrote provider health: before=%+v after=%+v", healthBefore, healthAfter)
	}
	if events := h.count(`SELECT COUNT(*) FROM events`); events != beforeEvents {
		t.Fatalf("stale attempt changed events from %d to %d", beforeEvents, events)
	}
	if deliveries := h.count(`SELECT COUNT(*) FROM notification_deliveries`); deliveries != beforeDeliveries {
		t.Fatalf("stale attempt changed deliveries from %d to %d", beforeDeliveries, deliveries)
	}
	if open := h.count(`SELECT COUNT(*) FROM availability_episodes WHERE closed_at IS NULL`); open != 0 {
		t.Fatalf("open old-config episodes = %d, want 0 after material edit", open)
	}
	if closed := h.count(`SELECT COUNT(*) FROM availability_episodes WHERE close_reason = 'config_changed'`); closed != 1 {
		t.Fatalf("config-changed episodes = %d, want 1", closed)
	}
	if states := h.count(`SELECT COUNT(*) FROM inventory_states WHERE monitor_provider_id = ? AND config_version = 2`, target.ID); states != 0 {
		t.Fatalf("stale attempt wrote new-config states = %d, want 0", states)
	}

	// The next check must use the edited config and commit normally.
	if _, err := h.runFor("integration-material", "material-p"); err != nil {
		t.Fatalf("new-config check: %v", err)
	}
	check = h.latestCheck(target.ID)
	if check.Outcome != "success" || check.ConfigVersion != 2 {
		t.Fatalf("new-config check = %+v, want success/config 2", check)
	}
	if states := h.count(`SELECT COUNT(*) FROM inventory_states WHERE monitor_provider_id = ? AND config_version = 2`, target.ID); states != 1 {
		t.Fatalf("new-config states = %d, want 1", states)
	}
	after = h.targetByID(target.ID)
	if after.NextDueAt == nil || !after.NextDueAt.Equal(schedulerNow.Add(5*time.Minute)) {
		t.Fatalf("new-config schedule = %+v", after)
	}
	if events := h.count(`SELECT COUNT(*) FROM events`); events != beforeEvents+1 {
		t.Fatalf("events after new config = %d, want %d", events, beforeEvents+1)
	}
}

// TestSchedulerIntegrationStartAndCrashRecovery covers SCH-009 AC 10.
func TestSchedulerIntegrationStartAndCrashRecovery(t *testing.T) {
	var observedRuns int
	var observedOutcome string
	var observedErr error
	adapter := newRecordingProvider("start-p", nil)
	h := newIntegrationHarness(t, adapter)
	h.createMonitor("integration-start", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("start-p"))
	target := h.target("integration-start", "start-p")

	adapter.onCheck = func(context.Context, provider.CheckRequest) {
		observedErr = h.sqlDB.QueryRow(`SELECT COUNT(*) FROM check_runs WHERE monitor_provider_id = ?`, target.ID).Scan(&observedRuns)
		if observedErr == nil {
			observedErr = h.sqlDB.QueryRow(`SELECT outcome FROM check_runs WHERE monitor_provider_id = ? ORDER BY id DESC LIMIT 1`, target.ID).Scan(&observedOutcome)
		}
	}
	attempt, err := h.stage.RunCheck(context.Background(), h.dispatchTarget("integration-start", "start-p"))
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if observedErr != nil {
		t.Fatalf("observe running row: %v", observedErr)
	}
	if observedRuns != 1 || observedOutcome != "running" {
		t.Fatalf("during I/O runs=%d outcome=%q, want one running row", observedRuns, observedOutcome)
	}
	if attempt.CheckRunID <= 0 {
		t.Fatalf("CheckRunID = %d, want positive", attempt.CheckRunID)
	}
	if got := h.checkCount(target.ID); got != 1 {
		t.Fatalf("check runs = %d, want 1", got)
	}
	check := h.latestCheck(target.ID)
	if check.Outcome != "running" || check.FinishedAt.Valid {
		t.Fatalf("simulated crash row = %+v, want running", check)
	}
	assertNoTargetOutcomeWrites(t, h.sqlDB, target.ID)
	if counts := h.outcomeCounts(); counts != (outcomeState{}) {
		t.Fatalf("attempt start wrote unrelated state: %+v", counts)
	}

	h.reopen()
	if got := h.latestCheck(target.ID).Outcome; got != "running" {
		t.Fatalf("running row after restart = %q, want running", got)
	}
	running := h.startSchedulerWithRunner(nil, 1, 0)
	h.waitForCheckOutcome(target.ID, "abandoned")
	running.stop(t)
	check = h.latestCheck(target.ID)
	if check.Outcome != "abandoned" || !check.FinishedAt.Valid {
		t.Fatalf("startup recovery = %+v, want abandoned", check)
	}
	assertNoTargetOutcomeWrites(t, h.sqlDB, target.ID)
	if counts := h.outcomeCounts(); counts != (outcomeState{}) {
		t.Fatalf("startup recovery wrote unrelated state: %+v", counts)
	}
}

// TestSchedulerIntegrationOutcomeCrashRecovery covers SCH-009 AC 11: a crash
// during the outcome transaction rolls back and leaves the running row; startup
// abandons it; a later retry creates exactly one event/outbox set, with no
// duplicates from the failed attempt.
func TestSchedulerIntegrationOutcomeCrashRecovery(t *testing.T) {
	adapter := newRecordingProvider("txcrash-p", nil)
	adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return reservableResult(schedulerNow, "txcrash"), nil
	}
	h := newIntegrationHarness(t, adapter)
	h.createMonitor("integration-txcrash", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("txcrash-p"))
	target := h.target("integration-txcrash", "txcrash-p")
	if _, err := h.sqlDB.Exec(`CREATE TRIGGER fail_outcome_event
		BEFORE INSERT ON events
		BEGIN
			SELECT RAISE(ABORT, 'injected outcome crash');
		END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	result, err := h.runFor("integration-txcrash", "txcrash-p")
	if err == nil {
		t.Fatal("crashed outcome committed")
	}
	if !result.Settled {
		t.Fatal("crashed outcome was not settled")
	}
	check := h.latestCheck(target.ID)
	if check.Outcome != "running" || check.FinishedAt.Valid {
		t.Fatalf("check after crash = %+v, want running", check)
	}
	if counts := h.outcomeCounts(); counts != (outcomeState{}) {
		t.Fatalf("crashed outcome wrote state: %+v", counts)
	}
	assertNoTargetOutcomeWrites(t, h.sqlDB, target.ID)

	h.reopen()
	running := h.startSchedulerWithRunner(nil, 1, 0)
	h.waitForCheckOutcome(target.ID, "abandoned")
	running.stop(t)
	if counts := h.outcomeCounts(); counts != (outcomeState{}) {
		t.Fatalf("startup after crash wrote state: %+v", counts)
	}
	if _, err := h.sqlDB.Exec(`DROP TRIGGER fail_outcome_event`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}

	if _, err := h.runFor("integration-txcrash", "txcrash-p"); err != nil {
		t.Fatalf("retry success: %v", err)
	}
	check = h.latestCheck(target.ID)
	if check.Outcome != "success" {
		t.Fatalf("retry check = %+v, want success", check)
	}
	states, open, _, events, deliveries := h.availabilityCounts(target.ID, "txcrash-p")
	if states != 1 || open != 1 || events != 1 || deliveries != 2 {
		t.Fatalf("retry availability = %d/%d/%d/%d, want no duplicates", states, open, events, deliveries)
	}
	if got := h.count(`SELECT COUNT(*) FROM provider_health`); got != 1 {
		t.Fatalf("health rows = %d, want 1", got)
	}
	if got := h.count(`SELECT COUNT(*) FROM check_runs`); got != 2 {
		t.Fatalf("check runs = %d, want abandoned plus success", got)
	}
}

// TestSchedulerIntegrationSlowRetriesStopAtDeadline covers SCH-009 AC 12: a
// provider that keeps retrying is bounded by the single overall check deadline
// and classified as a timeout failure, not abandonment.
func TestSchedulerIntegrationSlowRetriesStopAtDeadline(t *testing.T) {
	if scheduler.DefaultCheckTimeout != 20*time.Second {
		t.Fatalf("DefaultCheckTimeout = %v, want 20s", scheduler.DefaultCheckTimeout)
	}
	t.Run("default overall deadline is 20s", func(t *testing.T) {
		var remaining time.Duration
		var hasDeadline bool
		adapter := newRecordingProvider("deadline-default-p", nil)
		adapter.checkFn = func(ctx context.Context, _ provider.CheckRequest) (provider.Result, error) {
			deadline, ok := ctx.Deadline()
			hasDeadline = ok
			if ok {
				remaining = time.Until(deadline)
			}
			return reservableResult(schedulerNow, "deadline-default"), nil
		}
		h := newIntegrationHarness(t, adapter)
		h.timeout = 0
		h.rebuild()
		h.createMonitor("integration-deadline-default", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("deadline-default-p"))
		if _, err := h.runFor("integration-deadline-default", "deadline-default-p"); err != nil {
			t.Fatalf("run: %v", err)
		}
		if !hasDeadline {
			t.Fatal("provider context has no overall deadline")
		}
		if remaining <= 19*time.Second || remaining > 20*time.Second {
			t.Fatalf("remaining deadline = %v, want about 20s", remaining)
		}
	})

	attempts := atomic.Int32{}
	adapter := newRecordingProvider("slow-p", nil)
	adapter.checkFn = func(ctx context.Context, _ provider.CheckRequest) (provider.Result, error) {
		for index := 0; ; index++ {
			attempts.Add(1)
			select {
			case <-ctx.Done():
				return provider.Result{}, ctx.Err()
			case <-time.After(time.Duration(index+1) * time.Millisecond):
			}
		}
	}
	h := newIntegrationHarness(t, adapter)
	h.timeout = 120 * time.Millisecond
	h.rebuild()
	h.createMonitor("integration-slow", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("slow-p"))
	target := h.target("integration-slow", "slow-p")

	start := time.Now()
	result, err := h.runFor("integration-slow", "slow-p")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !result.Settled {
		t.Fatal("timeout outcome was not settled")
	}
	if elapsed < 100*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("elapsed = %v, want bounded by the injected overall deadline", elapsed)
	}
	if attempts.Load() < 2 {
		t.Fatalf("provider attempts = %d, want repeated retries before the deadline", attempts.Load())
	}
	check := h.latestCheck(target.ID)
	if check.Outcome != "error" || !check.ErrorKind.Valid || check.ErrorKind.String != "timeout" {
		t.Fatalf("check = %+v, want error/timeout", check)
	}
	if check.Outcome == "abandoned" {
		t.Fatal("deadline was classified as abandoned")
	}
	if health := h.providerHealth("slow-p"); health.Consecutive != 1 {
		t.Fatalf("health = %+v, want one timeout failure", health)
	}
	targetAfter := h.targetByID(target.ID)
	if targetAfter.ConsecutiveFailures != 1 || targetAfter.NextDueAt == nil ||
		!targetAfter.NextDueAt.Equal(schedulerNow.Add(10*time.Minute)) {
		t.Fatalf("timeout target = %+v", targetAfter)
	}
	if states := h.count(`SELECT COUNT(*) FROM inventory_states`); states != 0 {
		t.Fatalf("timeout wrote %d availability states", states)
	}
	if events := h.count(`SELECT COUNT(*) FROM events`); events != 0 {
		t.Fatalf("timeout wrote %d events", events)
	}
}

// TestSchedulerIntegrationBookingSlugEditDuringFlight covers SCH-009 AC 13: a
// booking-slug-only edit during I/O discards the attempt without an old-URL
// event, and the next check uses the new slug.
func TestSchedulerIntegrationBookingSlugEditDuringFlight(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	calls := atomic.Int32{}
	adapter := newRecordingProvider("booking-p", nil)
	adapter.checkFn = func(ctx context.Context, request provider.CheckRequest) (provider.Result, error) {
		if calls.Add(1) == 1 {
			close(started)
			select {
			case <-release:
				return reservableResultWithURL(schedulerNow, "booking",
					"https://example.test/"+request.Config.BookingSlug), nil
			case <-ctx.Done():
				return provider.Result{}, ctx.Err()
			}
		}
		return reservableResultWithURL(schedulerNow, "booking",
			"https://example.test/"+request.Config.BookingSlug), nil
	}
	h := newIntegrationHarness(t, adapter)
	h.createMonitor("integration-booking", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("booking-p"))
	if _, err := h.sqlDB.Exec(`UPDATE monitor_providers SET booking_slug = 'old-slug' WHERE monitor_id = ?`, "integration-booking"); err != nil {
		t.Fatalf("seed booking slug: %v", err)
	}
	target := h.target("integration-booking", "booking-p")

	type runResult struct {
		result scheduler.Result
		err    error
	}
	results := make(chan runResult, 1)
	go func() {
		result, err := h.runner.Run(context.Background(), h.dispatchTarget("integration-booking", "booking-p"))
		results <- runResult{result: result, err: err}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("booking attempt did not start")
	}

	updated := h.updateMonitor("integration-booking", 0, func(input *domain.MonitorInput) {
		input.Providers[0].BookingSlug = "new-slug"
	})
	if updated.Providers[0].ConfigVersion != 1 {
		t.Fatalf("booking-only edit config version = %d, want 1", updated.Providers[0].ConfigVersion)
	}
	if updated.Providers[0].NextDueAt != nil {
		t.Fatalf("booking-only edit changed next_due_at to %v", updated.Providers[0].NextDueAt)
	}

	close(release)
	var got runResult
	select {
	case got = <-results:
	case <-time.After(2 * time.Second):
		t.Fatal("booking attempt did not finish")
	}
	if got.err != nil {
		t.Fatalf("booking attempt error: %v", got.err)
	}
	if got.result.Settled {
		t.Fatal("booking-changed attempt was settled")
	}
	check := h.latestCheck(target.ID)
	if check.Outcome != "discarded" || !check.ErrorKind.Valid ||
		check.ErrorKind.String != "booking_config_changed" || check.ConfigVersion != 1 {
		t.Fatalf("booking discard = %+v", check)
	}
	if counts := h.outcomeCounts(); counts != (outcomeState{}) {
		t.Fatalf("booking discard wrote state: %+v", counts)
	}
	if oldURL := h.count(`SELECT COUNT(*) FROM events WHERE booking_url = ?`, "https://example.test/old-slug"); oldURL != 0 {
		t.Fatalf("events with old booking URL = %d, want 0", oldURL)
	}

	if _, err := h.runFor("integration-booking", "booking-p"); err != nil {
		t.Fatalf("new-slug run: %v", err)
	}
	if source := adapter.request(); source.Config.BookingSlug != "new-slug" {
		t.Fatalf("new check booking slug = %q, want new-slug", source.Config.BookingSlug)
	}
	check = h.latestCheck(target.ID)
	if check.Outcome != "success" {
		t.Fatalf("new-slug check = %+v, want success", check)
	}
	if newURL := h.count(`SELECT COUNT(*) FROM events WHERE booking_url = ?`, "https://example.test/new-slug"); newURL != 1 {
		t.Fatalf("events with new booking URL = %d, want 1", newURL)
	}
	if oldURL := h.count(`SELECT COUNT(*) FROM events WHERE booking_url = ?`, "https://example.test/old-slug"); oldURL != 0 {
		t.Fatalf("events with old booking URL after success = %d, want 0", oldURL)
	}
}

// TestSchedulerIntegrationExpiryEditResume covers SCH-009 AC 14: an active
// monitor that expires and a monitor paused across expiry both reject resume,
// then recover consistently after a future departure-date edit and resume.
func TestSchedulerIntegrationExpiryEditResume(t *testing.T) {
	t.Run("active monitor", func(t *testing.T) {
		adapter := newRecordingProvider("expiry-active-p", nil)
		adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
			return reservableResult(schedulerNow, "expiry-active"), nil
		}
		h := newIntegrationHarness(t, adapter)
		h.createMonitor("integration-expiry-active", true, schedulerNow.Add(time.Hour), harnessTargetFor("expiry-active-p"))
		target := h.target("integration-expiry-active", "expiry-active-p")
		if _, err := h.runFor("integration-expiry-active", "expiry-active-p"); err != nil {
			t.Fatalf("seed success: %v", err)
		}
		if open := h.count(`SELECT COUNT(*) FROM availability_episodes WHERE closed_at IS NULL`); open != 1 {
			t.Fatalf("open episodes = %d, want 1", open)
		}

		h.clock.Advance(time.Hour)
		running := h.startSchedulerWithRunner(nil, 1, 0)
		waitForExpired(t, h.st, "integration-expiry-active")
		running.stop(t)
		expired := h.monitor("integration-expiry-active")
		if expired.ExpiredAt == nil {
			t.Fatal("active monitor was not marked expired")
		}
		if closed := h.count(`SELECT COUNT(*) FROM availability_episodes WHERE close_reason = 'expired' AND closed_at IS NOT NULL`); closed != 1 {
			t.Fatalf("expired episodes = %d, want 1", closed)
		}

		service := h.monitorService(2 * time.Minute)
		if _, err := service.Resume(context.Background(), "integration-expiry-active"); !errors.Is(err, domain.ErrMonitorExpired) {
			t.Fatalf("resume expired monitor error = %v, want ErrMonitorExpired", err)
		}
		updated := h.updateMonitor("integration-expiry-active", 2*time.Minute, func(input *domain.MonitorInput) {
			input.DepartureDate = "2027-02-01"
		})
		if updated.ExpiredAt != nil || !updated.ExpiresAt.After(h.clock.Now()) {
			t.Fatalf("date edit did not clear expiry: expired_at=%v expires_at=%v", updated.ExpiredAt, updated.ExpiresAt)
		}
		if updated.Providers[0].ConfigVersion != 2 {
			t.Fatalf("date edit config version = %d, want 2", updated.Providers[0].ConfigVersion)
		}
		if _, err := service.Resume(context.Background(), "integration-expiry-active"); err != nil {
			t.Fatalf("resume after date edit: %v", err)
		}
		resumed := h.monitor("integration-expiry-active")
		if !resumed.Enabled || resumed.ExpiredAt != nil || resumed.Providers[0].NextDueAt == nil ||
			!resumed.Providers[0].NextDueAt.Equal(h.clock.Now().Add(2*time.Minute)) {
			t.Fatalf("resumed monitor = %+v", resumed)
		}

		h.clock.Advance(2 * time.Minute)
		checksBefore := h.checkCount(target.ID)
		running = h.startScheduler(1, 0)
		h.waitForCheckCount(target.ID, checksBefore+1)
		h.waitForCheckOutcome(target.ID, "success")
		running.stop(t)
		check := h.latestCheck(target.ID)
		if check.Outcome != "success" || check.ConfigVersion != 2 {
			t.Fatalf("post-resume check = %+v, want success/config 2", check)
		}
		if states := h.count(`SELECT COUNT(*) FROM inventory_states WHERE monitor_provider_id = ? AND config_version = 2`, target.ID); states != 1 {
			t.Fatalf("post-resume states = %d, want 1", states)
		}
	})

	t.Run("paused across expiry", func(t *testing.T) {
		adapter := newRecordingProvider("expiry-paused-p", nil)
		adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
			return reservableResult(schedulerNow, "expiry-paused"), nil
		}
		h := newIntegrationHarness(t, adapter)
		h.createMonitor("integration-expiry-paused", true, schedulerNow.Add(time.Hour), harnessTargetFor("expiry-paused-p"))
		target := h.target("integration-expiry-paused", "expiry-paused-p")
		if _, err := h.runFor("integration-expiry-paused", "expiry-paused-p"); err != nil {
			t.Fatalf("seed success: %v", err)
		}
		service := h.monitorService(2 * time.Minute)
		if _, err := service.Pause(context.Background(), "integration-expiry-paused"); err != nil {
			t.Fatalf("pause: %v", err)
		}
		paused := h.monitor("integration-expiry-paused")
		if paused.Enabled || paused.Providers[0].NextDueAt != nil {
			t.Fatalf("paused monitor = %+v", paused)
		}

		h.clock.Advance(time.Hour)
		running := h.startSchedulerWithRunner(nil, 1, 0)
		waitForExpired(t, h.st, "integration-expiry-paused")
		running.stop(t)
		expired := h.monitor("integration-expiry-paused")
		if expired.ExpiredAt == nil || expired.Enabled {
			t.Fatalf("paused monitor after expiry = %+v", expired)
		}
		if closed := h.count(`SELECT COUNT(*) FROM availability_episodes WHERE close_reason = 'expired' AND closed_at IS NOT NULL`); closed != 1 {
			t.Fatalf("paused expired episodes = %d, want 1", closed)
		}
		if _, err := service.Resume(context.Background(), "integration-expiry-paused"); !errors.Is(err, domain.ErrMonitorExpired) {
			t.Fatalf("resume paused expired monitor error = %v, want ErrMonitorExpired", err)
		}
		updated := h.updateMonitor("integration-expiry-paused", 2*time.Minute, func(input *domain.MonitorInput) {
			input.DepartureDate = "2027-02-01"
		})
		if updated.ExpiredAt != nil || updated.Enabled {
			t.Fatalf("paused date edit = %+v, want unexpired and still paused", updated)
		}
		if updated.Providers[0].NextDueAt != nil {
			t.Fatalf("paused date edit scheduled target at %v", updated.Providers[0].NextDueAt)
		}
		if _, err := service.Resume(context.Background(), "integration-expiry-paused"); err != nil {
			t.Fatalf("resume after paused date edit: %v", err)
		}
		resumed := h.monitor("integration-expiry-paused")
		if !resumed.Enabled || resumed.Providers[0].NextDueAt == nil ||
			!resumed.Providers[0].NextDueAt.Equal(h.clock.Now().Add(2*time.Minute)) {
			t.Fatalf("paused resumed monitor = %+v", resumed)
		}

		h.clock.Advance(2 * time.Minute)
		checksBefore := h.checkCount(target.ID)
		running = h.startScheduler(1, 0)
		h.waitForCheckCount(target.ID, checksBefore+1)
		h.waitForCheckOutcome(target.ID, "success")
		running.stop(t)
		check := h.latestCheck(target.ID)
		if check.Outcome != "success" || check.ConfigVersion != 2 {
			t.Fatalf("paused post-resume check = %+v, want success/config 2", check)
		}
	})
}

// TestSchedulerIntegrationHealthTransitionTable covers SCH-009 AC 15: provider
// and target counters, last-attempt/error/duration diagnostics, and interleaved
// non-counting outcomes follow the Architecture §12 transition table.
func TestSchedulerIntegrationHealthTransitionTable(t *testing.T) {
	step := atomic.Int32{}
	adapter := newRecordingProvider("table-p", nil)
	adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		switch step.Add(1) {
		case 1:
			return timeoutError()
		case 2:
			return configurationError()
		case 3:
			return routeNotFoundError()
		case 4:
			return httpStatusError(404)
		case 5:
			return reservableResult(schedulerNow, "table"), nil
		case 6:
			return timeoutError()
		case 7:
			return rateLimitedError()
		default:
			return emptyResult(schedulerNow), nil
		}
	}
	h := newIntegrationHarness(t, adapter)
	h.createMonitor("integration-table", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("table-p"))
	target := h.target("integration-table", "table-p")

	run := func() {
		h.clock.Advance(time.Minute)
		if _, err := h.runFor("integration-table", "table-p"); err != nil {
			t.Fatalf("table step: %v", err)
		}
	}
	nowString := func() string { return schedulerTime(h.clock.Now()) }

	// 1. Infrastructure timeout: counter +1 and full failure diagnostics.
	run()
	health := h.providerHealth("table-p")
	if !health.Found || health.Consecutive != 1 || !health.LastAttemptAt.Valid ||
		health.LastAttemptAt.String != nowString() || !health.LastFailureAt.Valid ||
		health.LastFailureAt.String != nowString() || !health.LastErrorKind.Valid ||
		health.LastErrorKind.String != "timeout" || !health.LastDurationMS.Valid ||
		health.LastSuccessAt.Valid || health.HealthEventCount != 0 {
		t.Fatalf("step 1 health = %+v", health)
	}
	targetAfter := h.targetByID(target.ID)
	if targetAfter.ConsecutiveFailures != 1 || targetAfter.LastOutcome != "error" ||
		targetAfter.LastErrorKind != "timeout" || targetAfter.NextDueAt == nil ||
		!targetAfter.NextDueAt.Equal(h.clock.Now().Add(10*time.Minute)) {
		t.Fatalf("step 1 target = %+v", targetAfter)
	}

	// 2-4. Configuration, route, and ordinary 4xx errors update diagnostics but
	// leave the provider and target counters unchanged.
	for index, wantKind := range []string{"configuration", "route_not_found", "http_status"} {
		run()
		health = h.providerHealth("table-p")
		if health.Consecutive != 1 || health.LastErrorKind.String != wantKind ||
			health.LastAttemptAt.String != nowString() || health.LastFailureAt.String != nowString() ||
			health.HealthEventCount != 0 {
			t.Fatalf("non-counting step %d health = %+v", index+2, health)
		}
		targetAfter = h.targetByID(target.ID)
		if targetAfter.ConsecutiveFailures != 1 || targetAfter.LastOutcome != "error" ||
			targetAfter.LastErrorKind != wantKind || targetAfter.NextDueAt == nil ||
			!targetAfter.NextDueAt.Equal(h.clock.Now().Add(5*time.Minute)) {
			t.Fatalf("non-counting step %d target = %+v", index+2, targetAfter)
		}
	}

	// 5. Applicable success resets counters and records success diagnostics
	// while retaining the last error as history.
	run()
	successAt := nowString()
	health = h.providerHealth("table-p")
	if health.Consecutive != 0 || !health.LastSuccessAt.Valid || health.LastSuccessAt.String != successAt ||
		!health.LastErrorKind.Valid || health.LastErrorKind.String != "http_status" ||
		!health.LastDurationMS.Valid || health.HealthEventCount != 0 {
		t.Fatalf("success health = %+v", health)
	}
	targetAfter = h.targetByID(target.ID)
	if targetAfter.ConsecutiveFailures != 0 || targetAfter.LastOutcome != "success" ||
		targetAfter.LastSuccessAt == nil || targetAfter.NextDueAt == nil ||
		!targetAfter.NextDueAt.Equal(h.clock.Now().Add(5*time.Minute)) {
		t.Fatalf("success target = %+v", targetAfter)
	}

	// 6-7. A new infrastructure failure sequence increments normally; the
	// second 429 applies the two-failure backoff.
	run()
	health = h.providerHealth("table-p")
	if health.Consecutive != 1 || health.LastErrorKind.String != "timeout" ||
		health.LastSuccessAt.String != successAt {
		t.Fatalf("second timeout health = %+v", health)
	}
	targetAfter = h.targetByID(target.ID)
	if targetAfter.ConsecutiveFailures != 1 || targetAfter.LastErrorKind != "timeout" ||
		targetAfter.LastSuccessAt == nil || targetAfter.NextDueAt == nil ||
		!targetAfter.NextDueAt.Equal(h.clock.Now().Add(10*time.Minute)) {
		t.Fatalf("second timeout target = %+v", targetAfter)
	}
	run()
	health = h.providerHealth("table-p")
	if health.Consecutive != 2 || health.LastErrorKind.String != "rate_limited" ||
		!health.LastDurationMS.Valid || health.HealthEventCount != 0 {
		t.Fatalf("rate-limited health = %+v", health)
	}
	targetAfter = h.targetByID(target.ID)
	if targetAfter.ConsecutiveFailures != 2 || targetAfter.LastErrorKind != "rate_limited" ||
		targetAfter.NextDueAt == nil || !targetAfter.NextDueAt.Equal(h.clock.Now().Add(20*time.Minute)) {
		t.Fatalf("rate-limited target = %+v", targetAfter)
	}

	// 8. Valid empty is an applicable health success and does not change the
	// existing availability episode/event.
	run()
	health = h.providerHealth("table-p")
	if health.Consecutive != 0 || !health.LastSuccessAt.Valid ||
		health.LastSuccessAt.String != nowString() || health.LastErrorKind.String != "rate_limited" ||
		health.HealthEventCount != 0 {
		t.Fatalf("empty-success health = %+v", health)
	}
	targetAfter = h.targetByID(target.ID)
	if targetAfter.ConsecutiveFailures != 0 || targetAfter.LastOutcome != "empty" ||
		targetAfter.NextDueAt == nil || !targetAfter.NextDueAt.Equal(h.clock.Now().Add(5*time.Minute)) {
		t.Fatalf("empty-success target = %+v", targetAfter)
	}
	if states, open, _, events, deliveries := h.availabilityCounts(target.ID, "table-p"); states != 1 || open != 1 || events != 1 || deliveries != 2 {
		t.Fatalf("availability after empty = %d/%d/%d/%d, want unchanged success state", states, open, events, deliveries)
	}
}
