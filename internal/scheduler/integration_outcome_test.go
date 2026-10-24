package scheduler_test

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/provider"
)

// TestSchedulerIntegrationSuccessAndEmptyPersist covers SCH-009 AC 1 and the
// valid-empty health addendum: a successful and a valid-empty check commit the
// check result, availability (success only), provider-health success/recovery,
// and applicable event/outbox rows, and all of it survives a database restart.
func TestSchedulerIntegrationSuccessAndEmptyPersist(t *testing.T) {
	successCalls := atomic.Int32{}
	successProvider := newRecordingProvider("success-p", nil)
	successProvider.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		if successCalls.Add(1) <= 3 {
			return timeoutError()
		}
		return reservableResult(schedulerNow, "success"), nil
	}
	emptyCalls := atomic.Int32{}
	emptyProvider := newRecordingProvider("empty-p", nil)
	emptyProvider.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		if emptyCalls.Add(1) <= 3 {
			return timeoutError()
		}
		return emptyResult(schedulerNow), nil
	}
	h := newIntegrationHarness(t, successProvider, emptyProvider)
	h.createMonitor("integration-success", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("success-p"))
	h.createMonitor("integration-empty", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("empty-p"))
	successTarget := h.target("integration-success", "success-p")
	emptyTarget := h.target("integration-empty", "empty-p")

	for i := 0; i < 3; i++ {
		if _, err := h.runFor("integration-success", "success-p"); err != nil {
			t.Fatalf("success setup failure %d: %v", i+1, err)
		}
		if _, err := h.runFor("integration-empty", "empty-p"); err != nil {
			t.Fatalf("empty setup failure %d: %v", i+1, err)
		}
	}
	for _, targetID := range []int64{successTarget.ID, emptyTarget.ID} {
		if _, err := h.sqlDB.Exec(`UPDATE monitor_providers SET next_due_at = ? WHERE id = ?`,
			schedulerTime(h.clock.Now()), targetID); err != nil {
			t.Fatalf("reset next_due_at: %v", err)
		}
	}

	running := h.startScheduler(2, 0)
	h.waitForCheckOutcome(successTarget.ID, "success")
	h.waitForCheckOutcome(emptyTarget.ID, "empty")
	running.stop(t)

	successCheck := h.latestCheck(successTarget.ID)
	if successCheck.Outcome != "success" || !successCheck.FinishedAt.Valid ||
		successCheck.Journeys != 1 || successCheck.Reservable != 1 {
		t.Fatalf("success check = %+v", successCheck)
	}
	emptyCheck := h.latestCheck(emptyTarget.ID)
	if emptyCheck.Outcome != "empty" || !emptyCheck.FinishedAt.Valid ||
		emptyCheck.Journeys != 0 || emptyCheck.Offerings != 0 {
		t.Fatalf("empty check = %+v", emptyCheck)
	}

	states, open, _, events, deliveries := h.availabilityCounts(successTarget.ID, "success-p")
	if states != 1 || open != 1 || events != 1 || deliveries != 2 {
		t.Fatalf("success availability = states=%d open=%d events=%d deliveries=%d", states, open, events, deliveries)
	}
	states, open, _, events, deliveries = h.availabilityCounts(emptyTarget.ID, "empty-p")
	if states != 0 || open != 0 || events != 0 || deliveries != 0 {
		t.Fatalf("empty availability = states=%d open=%d events=%d deliveries=%d", states, open, events, deliveries)
	}

	for _, providerID := range []provider.ID{"success-p", "empty-p"} {
		health := h.providerHealth(providerID)
		if !health.Found || health.Consecutive != 0 || !health.LastSuccessAt.Valid ||
			health.DegradedEvents != 1 || health.RecoveryEvents != 1 || health.OpenEpisodeCount != 0 {
			t.Fatalf("%s health = %+v", providerID, health)
		}
	}
	if got := h.count(`SELECT COUNT(*) FROM events`); got != 5 {
		t.Fatalf("events = %d, want 3 success-provider + 2 empty-provider", got)
	}
	if got := h.count(`SELECT COUNT(*) FROM notification_deliveries`); got != 10 {
		t.Fatalf("deliveries = %d, want 10", got)
	}
	if got := h.count(`SELECT COUNT(*) FROM check_runs`); got != 8 {
		t.Fatalf("check runs = %d, want 8", got)
	}

	successAfter := h.targetByID(successTarget.ID)
	if successAfter.LastOutcome != "success" || successAfter.ConsecutiveFailures != 0 ||
		successAfter.NextDueAt == nil || !successAfter.NextDueAt.Equal(schedulerNow.Add(5*time.Minute)) {
		t.Fatalf("success target = %+v", successAfter)
	}
	emptyAfter := h.targetByID(emptyTarget.ID)
	if emptyAfter.LastOutcome != "empty" || emptyAfter.ConsecutiveFailures != 0 ||
		emptyAfter.NextDueAt == nil || !emptyAfter.NextDueAt.Equal(schedulerNow.Add(5*time.Minute)) {
		t.Fatalf("empty target = %+v", emptyAfter)
	}

	// Restart: every committed outcome and outbox row must remain durable.
	h.reopen()
	if got := h.latestCheck(successTarget.ID).Outcome; got != "success" {
		t.Fatalf("success outcome after restart = %q", got)
	}
	if got := h.latestCheck(emptyTarget.ID).Outcome; got != "empty" {
		t.Fatalf("empty outcome after restart = %q", got)
	}
	if got := h.count(`SELECT COUNT(*) FROM events`); got != 5 {
		t.Fatalf("events after restart = %d, want 5", got)
	}
	if got := h.count(`SELECT COUNT(*) FROM notification_deliveries`); got != 10 {
		t.Fatalf("deliveries after restart = %d, want 10", got)
	}
	if got := h.providerHealth("success-p"); !got.Found || got.Consecutive != 0 || !got.LastSuccessAt.Valid {
		t.Fatalf("success health after restart = %+v", got)
	}
	if got := h.providerHealth("empty-p"); !got.Found || got.Consecutive != 0 || !got.LastSuccessAt.Valid {
		t.Fatalf("empty health after restart = %+v", got)
	}
}

// TestSchedulerIntegrationSuccessRollbackLeavesRunningRow covers SCH-009 AC 2:
// a failure during the success outcome transaction rolls back every write,
// leaves the attempt-start row running, and startup recovery abandons it
// without touching target, availability, health, or outbox state.
func TestSchedulerIntegrationSuccessRollbackLeavesRunningRow(t *testing.T) {
	providerAdapter := newRecordingProvider("rollback-p", nil)
	providerAdapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return reservableResult(schedulerNow, "rollback"), nil
	}
	h := newIntegrationHarness(t, providerAdapter)
	h.deliveries = func(context.Context, *sql.Tx) ([]notify.Delivery, error) {
		return nil, errors.New("injected delivery source failure")
	}
	h.rebuild()
	h.createMonitor("integration-rollback", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("rollback-p"))
	target := h.target("integration-rollback", "rollback-p")

	result, err := h.runFor("integration-rollback", "rollback-p")
	if err == nil {
		t.Fatal("run succeeded despite injected outcome failure")
	}
	if !result.Settled {
		t.Fatal("outcome transaction failure was not settled")
	}
	check := h.latestCheck(target.ID)
	if check.Outcome != "running" || check.FinishedAt.Valid {
		t.Fatalf("check after rollback = %+v, want running", check)
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM inventory_states`,
		`SELECT COUNT(*) FROM availability_episodes`,
		`SELECT COUNT(*) FROM provider_health`,
		`SELECT COUNT(*) FROM events`,
		`SELECT COUNT(*) FROM notification_deliveries`,
	} {
		if got := h.count(query); got != 0 {
			t.Fatalf("%s = %d, want 0", query, got)
		}
	}
	assertNoTargetOutcomeWrites(t, h.sqlDB, target.ID)

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
	if got := h.count(`SELECT COUNT(*) FROM events`); got != 0 {
		t.Fatalf("events after recovery = %d, want 0", got)
	}
	if got := h.count(`SELECT COUNT(*) FROM notification_deliveries`); got != 0 {
		t.Fatalf("deliveries after recovery = %d, want 0", got)
	}
	assertNoTargetOutcomeWrites(t, h.sqlDB, target.ID)
}

// TestSchedulerIntegrationInfrastructureErrorsPreserveAvailability covers
// SCH-009 AC 3: each applicable infrastructure error records failed check and
// target/provider diagnostics while existing availability state and episodes
// remain unchanged.
func TestSchedulerIntegrationInfrastructureErrorsPreserveAvailability(t *testing.T) {
	tests := []struct {
		name       string
		fail       func() (provider.Result, error)
		wantKind   string
		wantStatus int
	}{
		{name: "timeout", fail: timeoutError, wantKind: "timeout"},
		{name: "network", fail: networkError, wantKind: "network"},
		{name: "rate limited", fail: rateLimitedError, wantKind: "rate_limited", wantStatus: 429},
		{name: "http 500", fail: func() (provider.Result, error) { return httpStatusError(500) }, wantKind: "http_status", wantStatus: 500},
		{name: "invalid response", fail: invalidResponseError, wantKind: "invalid_response"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := atomic.Int32{}
			adapter := newRecordingProvider("infra-"+provider.ID(test.name), nil)
			adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
				if calls.Add(1) == 1 {
					return reservableResult(schedulerNow, "infra"), nil
				}
				return test.fail()
			}
			h := newIntegrationHarness(t, adapter)
			providerID := provider.ID("infra-" + test.name)
			h.createMonitor("integration-infra", true, schedulerNow.Add(24*time.Hour), harnessTargetFor(providerID))
			target := h.target("integration-infra", providerID)

			if _, err := h.runFor("integration-infra", providerID); err != nil {
				t.Fatalf("seed success: %v", err)
			}
			stateBefore := h.availabilityStateSnapshot(target.ID)
			if states, open, _, events, deliveries := h.availabilityCounts(target.ID, providerID); states != 1 || open != 1 || events != 1 || deliveries != 2 {
				t.Fatalf("seed availability = %d/%d/%d/%d", states, open, events, deliveries)
			}

			if _, err := h.runFor("integration-infra", providerID); err != nil {
				t.Fatalf("infrastructure run: %v", err)
			}
			check := h.latestCheck(target.ID)
			if check.Outcome != "error" || !check.ErrorKind.Valid || check.ErrorKind.String != test.wantKind {
				t.Fatalf("check = %+v, want error/%s", check, test.wantKind)
			}
			if !check.FinishedAt.Valid || !check.DurationMS.Valid {
				t.Fatalf("check diagnostics = finished %+v duration %+v, want both set", check.FinishedAt, check.DurationMS)
			}
			if test.wantStatus == 0 {
				if check.HTTPStatus.Valid {
					t.Fatalf("http status = %+v, want NULL", check.HTTPStatus)
				}
			} else if !check.HTTPStatus.Valid || int(check.HTTPStatus.Int64) != test.wantStatus {
				t.Fatalf("http status = %+v, want %d", check.HTTPStatus, test.wantStatus)
			}

			targetAfter := h.targetByID(target.ID)
			if targetAfter.LastOutcome != "error" || targetAfter.LastErrorKind != test.wantKind ||
				targetAfter.LastAttemptAt == nil || targetAfter.ConsecutiveFailures != 1 || targetAfter.LastSuccessAt == nil ||
				targetAfter.NextDueAt == nil || !targetAfter.NextDueAt.Equal(schedulerNow.Add(10*time.Minute)) {
				t.Fatalf("target diagnostics = %+v", targetAfter)
			}
			health := h.providerHealth(providerID)
			if !health.Found || health.Consecutive != 1 || !health.LastSuccessAt.Valid ||
				!health.LastAttemptAt.Valid || !health.LastErrorKind.Valid || health.LastErrorKind.String != test.wantKind ||
				!health.LastDurationMS.Valid || health.OpenEpisodeCount != 0 {
				t.Fatalf("provider health = %+v", health)
			}

			if stateAfter := h.availabilityStateSnapshot(target.ID); stateAfter != stateBefore {
				t.Fatalf("availability state changed: before=%+v after=%+v", stateBefore, stateAfter)
			}
			if states, open, _, events, deliveries := h.availabilityCounts(target.ID, providerID); states != 1 || open != 1 || events != 1 || deliveries != 2 {
				t.Fatalf("availability after error = %d/%d/%d/%d", states, open, events, deliveries)
			}
			if got := h.count(`SELECT COUNT(*) FROM events WHERE kind IN ('provider_degraded','provider_recovered')`); got != 0 {
				t.Fatalf("health events = %d, want 0 below threshold", got)
			}
		})
	}
}

func TestSchedulerIntegrationSuccessRollbackAtScheduleWrite(t *testing.T) {
	adapter := newRecordingProvider("schedule-rollback-p", nil)
	adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return reservableResult(schedulerNow, "schedule-rollback"), nil
	}
	h := newIntegrationHarness(t, adapter)
	h.createMonitor("integration-schedule-rollback", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("schedule-rollback-p"))
	target := h.target("integration-schedule-rollback", "schedule-rollback-p")
	// This trigger fires on the final target-schedule write, after the check
	// finalization, availability state/episode/event, provider health, and
	// outbox rows have all been written inside the outcome transaction.
	if _, err := h.sqlDB.Exec(`CREATE TRIGGER fail_schedule_write
		BEFORE UPDATE OF next_due_at ON monitor_providers
		BEGIN
			SELECT RAISE(ABORT, 'injected schedule failure');
		END`); err != nil {
		t.Fatalf("create schedule trigger: %v", err)
	}

	result, err := h.runFor("integration-schedule-rollback", "schedule-rollback-p")
	if err == nil {
		t.Fatal("run succeeded despite injected schedule failure")
	}
	if !result.Settled {
		t.Fatal("schedule-write failure was not settled")
	}
	check := h.latestCheck(target.ID)
	if check.Outcome != "running" || check.FinishedAt.Valid {
		t.Fatalf("check after schedule rollback = %+v, want running", check)
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM inventory_states`,
		`SELECT COUNT(*) FROM availability_episodes`,
		`SELECT COUNT(*) FROM provider_health`,
		`SELECT COUNT(*) FROM events`,
		`SELECT COUNT(*) FROM notification_deliveries`,
	} {
		if got := h.count(query); got != 0 {
			t.Fatalf("%s = %d, want 0 after schedule rollback", query, got)
		}
	}
	assertNoTargetOutcomeWrites(t, h.sqlDB, target.ID)

	h.reopen()
	recovery := h.startSchedulerWithRunner(nil, 1, 0)
	h.waitForCheckOutcome(target.ID, "abandoned")
	recovery.stop(t)
	if got := h.latestCheck(target.ID).Outcome; got != "abandoned" {
		t.Fatalf("startup recovery = %q, want abandoned", got)
	}
}

type availabilityState struct {
	State        string
	StateSince   string
	LastObserved string
	LastMissing  sql.NullString
}

func (h *integrationHarness) availabilityStateSnapshot(targetID int64) availabilityState {
	h.t.Helper()
	var state availabilityState
	if err := h.sqlDB.QueryRow(`SELECT state, state_since, last_observed_at, last_missing_at
		FROM inventory_states WHERE monitor_provider_id = ? ORDER BY id LIMIT 1`, targetID).Scan(
		&state.State, &state.StateSince, &state.LastObserved, &state.LastMissing,
	); err != nil {
		h.t.Fatalf("query availability state: %v", err)
	}
	return state
}

// TestSchedulerIntegrationNonCountingErrors covers SCH-009 AC 4: configuration,
// route, and ordinary 4xx errors update target/provider diagnostics without
// incrementing the A-21 outage counter or creating a health event/outbox.
func TestSchedulerIntegrationNonCountingErrors(t *testing.T) {
	tests := []struct {
		name       string
		configure  func(*recordingProvider)
		wantKind   string
		wantStatus int
	}{
		{
			name: "configuration",
			configure: func(adapter *recordingProvider) {
				adapter.validateErr = &provider.Error{Kind: provider.ErrConfiguration, Op: "ValidateConfig"}
			},
			wantKind: "configuration",
		},
		{
			name: "route",
			configure: func(adapter *recordingProvider) {
				adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
					return routeNotFoundError()
				}
			},
			wantKind: "route_not_found",
		},
		{
			name: "http 404",
			configure: func(adapter *recordingProvider) {
				adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
					return httpStatusError(404)
				}
			},
			wantKind: "http_status", wantStatus: 404,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			providerID := provider.ID("noncount-" + test.name)
			adapter := newRecordingProvider(providerID, nil)
			test.configure(adapter)
			h := newIntegrationHarness(t, adapter)
			h.createMonitor("integration-noncount", true, schedulerNow.Add(24*time.Hour), harnessTargetFor(providerID))
			target := h.target("integration-noncount", providerID)

			if _, err := h.runFor("integration-noncount", providerID); err != nil {
				t.Fatalf("run: %v", err)
			}
			check := h.latestCheck(target.ID)
			if check.Outcome != "error" || !check.ErrorKind.Valid || check.ErrorKind.String != test.wantKind {
				t.Fatalf("check = %+v, want error/%s", check, test.wantKind)
			}
			if !check.FinishedAt.Valid || !check.DurationMS.Valid {
				t.Fatalf("check diagnostics = finished %+v duration %+v, want both set", check.FinishedAt, check.DurationMS)
			}
			if test.wantStatus == 0 {
				if check.HTTPStatus.Valid {
					t.Fatalf("http status = %+v, want NULL", check.HTTPStatus)
				}
			} else if !check.HTTPStatus.Valid || int(check.HTTPStatus.Int64) != test.wantStatus {
				t.Fatalf("http status = %+v, want %d", check.HTTPStatus, test.wantStatus)
			}
			targetAfter := h.targetByID(target.ID)
			if targetAfter.LastOutcome != "error" || targetAfter.LastErrorKind != test.wantKind ||
				targetAfter.ConsecutiveFailures != 0 || targetAfter.NextDueAt == nil ||
				!targetAfter.NextDueAt.Equal(schedulerNow.Add(5*time.Minute)) {
				t.Fatalf("target diagnostics = %+v", targetAfter)
			}
			health := h.providerHealth(providerID)
			if !health.Found || health.Consecutive != 0 || !health.LastErrorKind.Valid ||
				health.LastErrorKind.String != test.wantKind || !health.LastDurationMS.Valid ||
				health.OpenEpisodeCount != 0 || health.HealthEventCount != 0 || health.HealthDeliveryCnt != 0 {
				t.Fatalf("provider health = %+v", health)
			}
			for _, query := range []string{
				`SELECT COUNT(*) FROM inventory_states`,
				`SELECT COUNT(*) FROM availability_episodes`,
				`SELECT COUNT(*) FROM events`,
				`SELECT COUNT(*) FROM notification_deliveries`,
			} {
				if got := h.count(query); got != 0 {
					t.Fatalf("%s = %d, want 0", query, got)
				}
			}
		})
	}
}

// TestSchedulerIntegrationHealthThresholdCommitAndRollback covers SCH-009 AC 5:
// the third infrastructure failure and its degraded event/outbox commit in one
// transaction; an injected failure during that transaction rolls the whole
// outcome back, leaves the running row for startup abandonment, and later
// retries do not duplicate the event.
func TestSchedulerIntegrationHealthThresholdCommitAndRollback(t *testing.T) {
	t.Run("commit and recovery", func(t *testing.T) {
		calls := atomic.Int32{}
		adapter := newRecordingProvider("threshold-p", nil)
		adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
			if calls.Add(1) <= 4 {
				return timeoutError()
			}
			return reservableResult(schedulerNow, "threshold"), nil
		}
		h := newIntegrationHarness(t, adapter)
		h.createMonitor("integration-threshold", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("threshold-p"))
		for i := 1; i <= 2; i++ {
			if _, err := h.runFor("integration-threshold", "threshold-p"); err != nil {
				t.Fatalf("failure %d: %v", i, err)
			}
		}
		if health := h.providerHealth("threshold-p"); health.Consecutive != 2 || health.DegradedEvents != 0 {
			t.Fatalf("health before threshold = %+v", health)
		}
		if _, err := h.runFor("integration-threshold", "threshold-p"); err != nil {
			t.Fatalf("third failure: %v", err)
		}
		health := h.providerHealth("threshold-p")
		if health.Consecutive != 3 || health.OpenEpisodeCount != 1 || health.DegradedEvents != 1 || health.HealthDeliveryCnt != 2 {
			t.Fatalf("health at threshold = %+v", health)
		}
		if _, err := h.runFor("integration-threshold", "threshold-p"); err != nil {
			t.Fatalf("fourth failure: %v", err)
		}
		health = h.providerHealth("threshold-p")
		if health.Consecutive != 4 || health.DegradedEvents != 1 || health.HealthDeliveryCnt != 2 {
			t.Fatalf("health above threshold = %+v", health)
		}
		if _, err := h.runFor("integration-threshold", "threshold-p"); err != nil {
			t.Fatalf("recovery run: %v", err)
		}
		health = h.providerHealth("threshold-p")
		if health.Consecutive != 0 || health.OpenEpisodeCount != 0 || health.DegradedEvents != 1 ||
			health.RecoveryEvents != 1 || health.HealthDeliveryCnt != 4 {
			t.Fatalf("health after recovery = %+v", health)
		}
		if availability := h.count(`SELECT COUNT(*) FROM events WHERE kind = 'availability' AND provider_id = 'threshold-p'`); availability != 1 {
			t.Fatalf("availability events = %d, want 1", availability)
		}
		if deliveries := h.count(`SELECT COUNT(*) FROM notification_deliveries`); deliveries != 6 {
			t.Fatalf("deliveries = %d, want 4 health + 2 availability", deliveries)
		}
	})

	t.Run("rollback at threshold", func(t *testing.T) {
		adapter := newRecordingProvider("threshold-rollback", nil)
		adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
			return timeoutError()
		}
		h := newIntegrationHarness(t, adapter)
		h.createMonitor("integration-threshold-rollback", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("threshold-rollback"))
		target := h.target("integration-threshold-rollback", "threshold-rollback")
		for i := 1; i <= 2; i++ {
			if _, err := h.runFor("integration-threshold-rollback", "threshold-rollback"); err != nil {
				t.Fatalf("failure %d: %v", i, err)
			}
		}
		if _, err := h.sqlDB.Exec(`CREATE TRIGGER fail_threshold_event
			BEFORE INSERT ON events
			BEGIN
				SELECT RAISE(ABORT, 'injected threshold event failure');
			END`); err != nil {
			t.Fatalf("create trigger: %v", err)
		}
		result, err := h.runFor("integration-threshold-rollback", "threshold-rollback")
		if err == nil {
			t.Fatal("third failure committed despite injected event failure")
		}
		if !result.Settled {
			t.Fatal("threshold rollback was not settled")
		}
		check := h.latestCheck(target.ID)
		if check.Outcome != "running" || check.FinishedAt.Valid {
			t.Fatalf("check after threshold rollback = %+v, want running", check)
		}
		health := h.providerHealth("threshold-rollback")
		if health.Consecutive != 2 || health.OpenEpisodeCount != 0 || health.DegradedEvents != 0 || health.HealthDeliveryCnt != 0 {
			t.Fatalf("health after threshold rollback = %+v", health)
		}
		targetAfter := h.targetByID(target.ID)
		if targetAfter.ConsecutiveFailures != 2 || targetAfter.NextDueAt == nil ||
			!targetAfter.NextDueAt.Equal(schedulerNow.Add(20*time.Minute)) {
			t.Fatalf("target after threshold rollback = %+v", targetAfter)
		}

		h.reopen()
		running := h.startSchedulerWithRunner(nil, 1, 0)
		h.waitForCheckOutcome(target.ID, "abandoned")
		running.stop(t)
		if got := h.count(`SELECT COUNT(*) FROM events`); got != 0 {
			t.Fatalf("events after startup recovery = %d, want 0", got)
		}
		if _, err := h.sqlDB.Exec(`DROP TRIGGER fail_threshold_event`); err != nil {
			t.Fatalf("drop trigger: %v", err)
		}
		if _, err := h.runFor("integration-threshold-rollback", "threshold-rollback"); err != nil {
			t.Fatalf("retry third failure: %v", err)
		}
		health = h.providerHealth("threshold-rollback")
		if health.Consecutive != 3 || health.OpenEpisodeCount != 1 || health.DegradedEvents != 1 || health.HealthDeliveryCnt != 2 {
			t.Fatalf("health after retry = %+v", health)
		}
		if got := h.count(`SELECT COUNT(*) FROM events WHERE kind = 'provider_degraded'`); got != 1 {
			t.Fatalf("degraded events = %d, want 1", got)
		}
	})
}

type discardCase struct {
	name    string
	outcome string
	mutate  func(h *integrationHarness, target domain.MonitorProvider) error
	reason  string
}

// TestSchedulerIntegrationDiscardedOutcomes covers SCH-009 AC 6: a successful
// result or a timeout that completes after pause, expiry, config, booking, or
// target deactivation discards only the running check row. Caller cancellation
// takes the abandoned branch. Neither branch updates target scheduling,
// availability, health, events, or outbox rows.
func TestSchedulerIntegrationDiscardedOutcomes(t *testing.T) {
	cases := []discardCase{
		{name: "pause success", outcome: "success", reason: "monitor_disabled", mutate: func(h *integrationHarness, target domain.MonitorProvider) error {
			_, err := h.sqlDB.Exec(`UPDATE monitors SET enabled = 0 WHERE id = ?`, target.MonitorID)
			return err
		}},
		{name: "pause timeout", outcome: "timeout", reason: "monitor_disabled", mutate: func(h *integrationHarness, target domain.MonitorProvider) error {
			_, err := h.sqlDB.Exec(`UPDATE monitors SET enabled = 0 WHERE id = ?`, target.MonitorID)
			return err
		}},
		{name: "pause rate limited", outcome: "rate_limited", reason: "monitor_disabled", mutate: func(h *integrationHarness, target domain.MonitorProvider) error {
			_, err := h.sqlDB.Exec(`UPDATE monitors SET enabled = 0 WHERE id = ?`, target.MonitorID)
			return err
		}},
		{name: "expiry success", outcome: "success", reason: "monitor_expired", mutate: func(h *integrationHarness, target domain.MonitorProvider) error {
			_, err := h.sqlDB.Exec(`UPDATE monitors SET expires_at = ? WHERE id = ?`,
				schedulerTime(h.clock.Now().Add(-time.Minute)), target.MonitorID)
			return err
		}},
		{name: "expiry timeout", outcome: "timeout", reason: "monitor_expired", mutate: func(h *integrationHarness, target domain.MonitorProvider) error {
			_, err := h.sqlDB.Exec(`UPDATE monitors SET expires_at = ? WHERE id = ?`,
				schedulerTime(h.clock.Now().Add(-time.Minute)), target.MonitorID)
			return err
		}},
		{name: "config success", outcome: "success", reason: "config_changed", mutate: func(h *integrationHarness, target domain.MonitorProvider) error {
			_, err := h.sqlDB.Exec(`UPDATE monitor_providers SET config_version = config_version + 1 WHERE id = ?`, target.ID)
			return err
		}},
		{name: "config timeout", outcome: "timeout", reason: "config_changed", mutate: func(h *integrationHarness, target domain.MonitorProvider) error {
			_, err := h.sqlDB.Exec(`UPDATE monitor_providers SET config_version = config_version + 1 WHERE id = ?`, target.ID)
			return err
		}},
		{name: "booking success", outcome: "success", reason: "booking_config_changed", mutate: func(h *integrationHarness, target domain.MonitorProvider) error {
			_, err := h.sqlDB.Exec(`UPDATE monitor_providers SET booking_slug = 'new-slug' WHERE id = ?`, target.ID)
			return err
		}},
		{name: "booking timeout", outcome: "timeout", reason: "booking_config_changed", mutate: func(h *integrationHarness, target domain.MonitorProvider) error {
			_, err := h.sqlDB.Exec(`UPDATE monitor_providers SET booking_slug = 'new-slug' WHERE id = ?`, target.ID)
			return err
		}},
		{name: "target inactive success", outcome: "success", reason: "target_inactive", mutate: func(h *integrationHarness, target domain.MonitorProvider) error {
			_, err := h.sqlDB.Exec(`UPDATE monitor_providers SET active = 0 WHERE id = ?`, target.ID)
			return err
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			calls := atomic.Int32{}
			adapter := newRecordingProvider("discard-p", nil)
			adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
				if calls.Add(1) == 1 {
					return reservableResult(schedulerNow, "discard"), nil
				}
				switch test.outcome {
				case "timeout":
					return timeoutError()
				case "rate_limited":
					return rateLimitedError()
				default:
					return reservableResult(schedulerNow, "discard"), nil
				}
			}
			h := newIntegrationHarness(t, adapter)
			h.createMonitor("integration-discard", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("discard-p"))
			target := h.target("integration-discard", "discard-p")
			if _, err := h.runFor("integration-discard", "discard-p"); err != nil {
				t.Fatalf("seed success: %v", err)
			}
			beforeTarget := h.targetByID(target.ID)
			before := h.outcomeCounts()

			adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
				if err := test.mutate(h, target); err != nil {
					t.Fatalf("mutate: %v", err)
				}
				switch test.outcome {
				case "timeout":
					return timeoutError()
				case "rate_limited":
					return rateLimitedError()
				default:
					return reservableResult(schedulerNow, "discard"), nil
				}
			}
			result, err := h.runFor("integration-discard", "discard-p")
			if err != nil {
				t.Fatalf("discarded run: %v", err)
			}
			if result.Settled {
				t.Fatal("discarded outcome was settled")
			}
			check := h.latestCheck(target.ID)
			if check.Outcome != "discarded" || !check.ErrorKind.Valid || check.ErrorKind.String != test.reason ||
				check.ConfigVersion != 1 {
				t.Fatalf("discard check = %+v, want discarded/%s/config 1", check, test.reason)
			}
			assertIntegrationTargetUnchanged(t, beforeTarget, h.targetByID(target.ID))
			if after := h.outcomeCounts(); after != before {
				t.Fatalf("discard changed state: before=%+v after=%+v", before, after)
			}
		})
	}

	t.Run("deleted target", func(t *testing.T) {
		adapter := newRecordingProvider("deleted-p", nil)
		h := newIntegrationHarness(t, adapter)
		h.createMonitor("integration-deleted", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("deleted-p"))
		target := h.target("integration-deleted", "deleted-p")
		adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
			if _, err := h.sqlDB.Exec(`DELETE FROM monitor_providers WHERE id = ?`, target.ID); err != nil {
				t.Fatalf("delete target: %v", err)
			}
			return reservableResult(schedulerNow, "deleted"), nil
		}
		result, err := h.runFor("integration-deleted", "deleted-p")
		if err != nil {
			t.Fatalf("deleted-target run: %v", err)
		}
		if result.Settled {
			t.Fatal("deleted-target discard was settled")
		}
		if got := h.checkCount(target.ID); got != 0 {
			t.Fatalf("check runs for deleted target = %d, want 0 after cascade", got)
		}
		if counts := h.outcomeCounts(); counts != (outcomeState{}) {
			t.Fatalf("deleted-target discard wrote state: %+v", counts)
		}
		if _, err := h.st.GetMonitor(context.Background(), "integration-deleted"); err != nil {
			t.Fatalf("deleted-target discard removed monitor: %v", err)
		}
	})

	t.Run("caller cancellation is abandoned", func(t *testing.T) {
		calls := atomic.Int32{}
		adapter := newRecordingProvider("abandon-p", nil)
		adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
			if calls.Add(1) == 1 {
				return reservableResult(schedulerNow, "abandon"), nil
			}
			return reservableResult(schedulerNow, "abandon"), nil
		}
		h := newIntegrationHarness(t, adapter)
		h.createMonitor("integration-abandon", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("abandon-p"))
		target := h.target("integration-abandon", "abandon-p")
		if _, err := h.runFor("integration-abandon", "abandon-p"); err != nil {
			t.Fatalf("seed success: %v", err)
		}
		beforeTarget := h.targetByID(target.ID)
		before := h.outcomeCounts()
		ctx, cancel := context.WithCancel(context.Background())
		adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
			cancel()
			return reservableResult(schedulerNow, "abandon"), nil
		}
		result, err := h.runner.Run(ctx, h.dispatchTarget("integration-abandon", "abandon-p"))
		if err != nil {
			t.Fatalf("abandoned run: %v", err)
		}
		if !result.Settled {
			t.Fatal("abandoned outcome was not settled")
		}
		check := h.latestCheck(target.ID)
		if check.Outcome != "abandoned" || !check.ErrorKind.Valid || check.ErrorKind.String != "canceled" {
			t.Fatalf("abandoned check = %+v", check)
		}
		assertIntegrationTargetUnchanged(t, beforeTarget, h.targetByID(target.ID))
		if after := h.outcomeCounts(); after != before {
			t.Fatalf("abandonment changed state: before=%+v after=%+v", before, after)
		}
	})
}

type outcomeState struct {
	Events        int
	Deliveries    int
	States        int
	Episodes      int
	OpenEpisodes  int
	HealthRows    int
	HealthEvents  int
	HealthDeliver int
}

func (h *integrationHarness) outcomeCounts() outcomeState {
	h.t.Helper()
	return outcomeState{
		Events:        h.count(`SELECT COUNT(*) FROM events`),
		Deliveries:    h.count(`SELECT COUNT(*) FROM notification_deliveries`),
		States:        h.count(`SELECT COUNT(*) FROM inventory_states`),
		Episodes:      h.count(`SELECT COUNT(*) FROM availability_episodes`),
		OpenEpisodes:  h.count(`SELECT COUNT(*) FROM availability_episodes WHERE closed_at IS NULL`),
		HealthRows:    h.count(`SELECT COUNT(*) FROM provider_health`),
		HealthEvents:  h.count(`SELECT COUNT(*) FROM events WHERE kind IN ('provider_degraded','provider_recovered')`),
		HealthDeliver: h.count(`SELECT COUNT(*) FROM notification_deliveries d JOIN events e ON e.id = d.event_id WHERE e.kind IN ('provider_degraded','provider_recovered')`),
	}
}

func assertIntegrationTargetUnchanged(t *testing.T, before, after domain.MonitorProvider) {
	t.Helper()
	if after.LastOutcome != before.LastOutcome ||
		after.LastErrorKind != before.LastErrorKind ||
		after.ConsecutiveFailures != before.ConsecutiveFailures ||
		!sameTimePointer(before.NextDueAt, after.NextDueAt) ||
		!sameTimePointer(before.LastAttemptAt, after.LastAttemptAt) ||
		!sameTimePointer(before.LastSuccessAt, after.LastSuccessAt) ||
		!sameTimePointer(before.LastErrorAt, after.LastErrorAt) {
		t.Fatalf("target changed: before=%+v after=%+v", before, after)
	}
}

func sameTimePointer(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

// TestSchedulerIntegrationCancellationSemantics covers SCH-009 AC 7: caller
// cancellation and shutdown are abandoned, a deadline is a timeout failure, and
// a provider-side cancellation is a failure rather than abandonment.
func TestSchedulerIntegrationCancellationSemantics(t *testing.T) {
	t.Run("caller cancellation", func(t *testing.T) {
		adapter := newRecordingProvider("cancel-p", nil)
		adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
			return reservableResult(schedulerNow, "cancel"), nil
		}
		h := newIntegrationHarness(t, adapter)
		h.createMonitor("integration-cancel", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("cancel-p"))
		target := h.target("integration-cancel", "cancel-p")
		ctx, cancel := context.WithCancel(context.Background())
		adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
			cancel()
			return reservableResult(schedulerNow, "cancel"), nil
		}
		result, err := h.runner.Run(ctx, h.dispatchTarget("integration-cancel", "cancel-p"))
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if !result.Settled {
			t.Fatal("caller cancellation was not settled as abandoned")
		}
		check := h.latestCheck(target.ID)
		if check.Outcome != "abandoned" || !check.ErrorKind.Valid || check.ErrorKind.String != "canceled" {
			t.Fatalf("check = %+v, want abandoned/canceled", check)
		}
		if counts := h.outcomeCounts(); counts != (outcomeState{}) {
			t.Fatalf("caller cancellation wrote state: %+v", counts)
		}
	})

	t.Run("deadline is timeout failure", func(t *testing.T) {
		adapter := newRecordingProvider("deadline-p", nil)
		adapter.checkFn = func(ctx context.Context, _ provider.CheckRequest) (provider.Result, error) {
			<-ctx.Done()
			return provider.Result{}, ctx.Err()
		}
		h := newIntegrationHarness(t, adapter)
		h.timeout = 40 * time.Millisecond
		h.rebuild()
		h.createMonitor("integration-deadline", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("deadline-p"))
		target := h.target("integration-deadline", "deadline-p")
		result, err := h.runFor("integration-deadline", "deadline-p")
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if !result.Settled {
			t.Fatal("deadline outcome was not settled")
		}
		check := h.latestCheck(target.ID)
		if check.Outcome != "error" || !check.ErrorKind.Valid || check.ErrorKind.String != "timeout" {
			t.Fatalf("check = %+v, want error/timeout", check)
		}
		if health := h.providerHealth("deadline-p"); health.Consecutive != 1 {
			t.Fatalf("deadline health = %+v, want failure", health)
		}
		if targetAfter := h.targetByID(target.ID); targetAfter.ConsecutiveFailures != 1 ||
			targetAfter.NextDueAt == nil || !targetAfter.NextDueAt.Equal(schedulerNow.Add(10*time.Minute)) {
			t.Fatalf("deadline target = %+v", targetAfter)
		}
	})

	t.Run("provider-side cancellation is not abandoned", func(t *testing.T) {
		adapter := newRecordingProvider("provider-cancel-p", nil)
		adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
			return provider.Result{}, &provider.Error{Kind: provider.ErrCanceled, Op: "integration.Check", Err: context.Canceled}
		}
		h := newIntegrationHarness(t, adapter)
		h.createMonitor("integration-provider-cancel", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("provider-cancel-p"))
		target := h.target("integration-provider-cancel", "provider-cancel-p")
		result, err := h.runFor("integration-provider-cancel", "provider-cancel-p")
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if !result.Settled {
			t.Fatal("provider-side cancellation was not settled")
		}
		check := h.latestCheck(target.ID)
		if check.Outcome != "error" || !check.ErrorKind.Valid || check.ErrorKind.String != "invalid_response" {
			t.Fatalf("check = %+v, want error/invalid_response", check)
		}
		if health := h.providerHealth("provider-cancel-p"); health.Consecutive != 1 {
			t.Fatalf("provider-side cancellation health = %+v", health)
		}
	})

	t.Run("scheduler shutdown abandons in-flight", func(t *testing.T) {
		started := make(chan struct{})
		adapter := newRecordingProvider("shutdown-p", nil)
		adapter.checkFn = func(ctx context.Context, _ provider.CheckRequest) (provider.Result, error) {
			close(started)
			<-ctx.Done()
			return provider.Result{}, ctx.Err()
		}
		h := newIntegrationHarness(t, adapter)
		h.createMonitor("integration-shutdown", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("shutdown-p"))
		target := h.target("integration-shutdown", "shutdown-p")
		if _, err := h.sqlDB.Exec(`UPDATE monitor_providers SET next_due_at = ? WHERE id = ?`,
			schedulerTime(h.clock.Now()), target.ID); err != nil {
			t.Fatalf("schedule shutdown target: %v", err)
		}
		beforeTarget := h.targetByID(target.ID)
		running := h.startScheduler(1, 0)
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("shutdown provider did not start")
		}
		running.stop(t)
		h.waitForCheckOutcome(target.ID, "abandoned")
		check := h.latestCheck(target.ID)
		if check.Outcome != "abandoned" || !check.FinishedAt.Valid {
			t.Fatalf("shutdown check = %+v, want abandoned", check)
		}
		if counts := h.outcomeCounts(); counts != (outcomeState{}) {
			t.Fatalf("shutdown wrote state: %+v", counts)
		}
		assertIntegrationTargetUnchanged(t, beforeTarget, h.targetByID(target.ID))
	})
}

// TestSchedulerIntegrationNoDeliveryBeforeCommit covers SCH-009 AC 8 with a
// test-only after-commit observer. It proves that the delivery source executes
// inside the outcome transaction after the check row is finalized but before
// availability/health/outbox writes, and that the committed outbox is visible
// to an independent connection only when the runner wakes after commit.
// NTF-001 is not implemented, so this exercises ordering and visibility, not an
// actual external sender.
func TestSchedulerIntegrationNoDeliveryBeforeCommit(t *testing.T) {
	adapter := newRecordingProvider("observer-p", nil)
	adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return reservableResult(schedulerNow, "observer"), nil
	}
	h := newIntegrationHarness(t, adapter)
	h.createMonitor("integration-observer", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("observer-p"))
	target := h.target("integration-observer", "observer-p")

	observer, err := db.Open(context.Background(), h.path)
	if err != nil {
		t.Fatalf("open observer database: %v", err)
	}
	defer func() {
		if err := observer.Close(); err != nil {
			t.Errorf("observer.Close: %v", err)
		}
	}()
	observe := func() observerView {
		t.Helper()
		var view observerView
		if err := observer.QueryRow(`SELECT outcome FROM check_runs
			WHERE monitor_provider_id = ? ORDER BY id DESC LIMIT 1`, target.ID).Scan(&view.Outcome); err != nil {
			t.Fatalf("observe check outcome: %v", err)
		}
		if err := observer.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&view.Events); err != nil {
			t.Fatalf("observe events: %v", err)
		}
		if err := observer.QueryRow(`SELECT COUNT(*) FROM notification_deliveries`).Scan(&view.Deliveries); err != nil {
			t.Fatalf("observe deliveries: %v", err)
		}
		if err := observer.QueryRow(`SELECT last_outcome FROM monitor_providers WHERE id = ?`, target.ID).Scan(&view.LastOutcome); err != nil {
			t.Fatalf("observe target outcome: %v", err)
		}
		return view
	}

	var beforeCommit observerView
	var insideTx observerView
	h.deliveries = func(ctx context.Context, tx *sql.Tx) ([]notify.Delivery, error) {
		beforeCommit = observe()
		if err := tx.QueryRowContext(ctx, `SELECT outcome FROM check_runs
			WHERE monitor_provider_id = ? ORDER BY id DESC LIMIT 1`, target.ID).Scan(&insideTx.Outcome); err != nil {
			t.Fatalf("observe in-transaction check outcome: %v", err)
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&insideTx.Events); err != nil {
			t.Fatalf("observe in-transaction events: %v", err)
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_deliveries`).Scan(&insideTx.Deliveries); err != nil {
			t.Fatalf("observe in-transaction deliveries: %v", err)
		}
		return telegramDeliveries(ctx, tx)
	}
	var afterCommit observerView
	h.onWake = func() {
		afterCommit = observe()
	}
	h.rebuild()

	result, err := h.runFor("integration-observer", "observer-p")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !result.Settled {
		t.Fatal("observed success was not settled")
	}
	if insideTx.Outcome != "success" || insideTx.Events != 0 || insideTx.Deliveries != 0 {
		t.Fatalf("in-transaction delivery-source view = %+v, want finalized check with no outbox yet", insideTx)
	}
	if beforeCommit.Outcome != "running" || beforeCommit.Events != 0 || beforeCommit.Deliveries != 0 || beforeCommit.LastOutcome.Valid {
		t.Fatalf("delivery source observed committed outcome state: %+v", beforeCommit)
	}
	if afterCommit.Outcome != "success" || afterCommit.Events != 1 || afterCommit.Deliveries != 2 ||
		!afterCommit.LastOutcome.Valid || afterCommit.LastOutcome.String != "success" {
		t.Fatalf("after-commit observer view = %+v", afterCommit)
	}
	if wakes := h.wakes.Load(); wakes != 1 {
		t.Fatalf("wakes = %d, want exactly one after-commit wake", wakes)
	}
}

type observerView struct {
	Outcome     string
	Events      int
	Deliveries  int
	LastOutcome sql.NullString
}
