package scheduler_test

import (
	"context"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/provider"
)

func TestAdaptive429EscalatesPersistsAndRecovers(t *testing.T) {
	adapter := newRecordingProvider("adaptive-p", nil)
	calls := 0
	adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		calls++
		if calls <= 2 {
			return provider.Result{}, &provider.Error{
				Kind:       provider.ErrRateLimited,
				Op:         "test.Check",
				StatusCode: 429,
				RetryAfter: 45 * time.Minute,
			}
		}
		return emptyResult(schedulerNow), nil
	}
	h := newIntegrationHarness(t, adapter)
	h.createMonitor("adaptive", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("adaptive-p"))
	target := h.target("adaptive", "adaptive-p")

	if _, err := h.runFor("adaptive", "adaptive-p"); err != nil {
		t.Fatalf("first 429: %v", err)
	}
	state, found, err := h.st.GetProviderRateLimit(context.Background(), "adaptive-p", domain.ProviderRateLimitScopeProvider)
	if err != nil || !found {
		t.Fatalf("rate limit after first 429 = %+v, found=%v, err=%v", state, found, err)
	}
	if state.Consecutive429s != 1 || state.AdaptiveInterval != 10*time.Minute {
		t.Fatalf("first 429 state = %+v, want level 1 / 10m", state)
	}
	if state.CooldownUntil == nil || !state.CooldownUntil.Equal(h.clock.Now().Add(45*time.Minute)) {
		t.Fatalf("first cooldown = %v, want Retry-After 45m", state.CooldownUntil)
	}
	afterFirst := h.targetByID(target.ID)
	if afterFirst.ConsecutiveFailures != 1 || afterFirst.NextDueAt == nil ||
		!afterFirst.NextDueAt.Equal(h.clock.Now().Add(45*time.Minute)) {
		t.Fatalf("first target = %+v, want Retry-After lower bound", afterFirst)
	}

	// The configured interval is never overwritten by adaptive state.
	if interval, err := h.st.PollInterval(context.Background()); err != nil || interval != 5*time.Minute {
		t.Fatalf("configured interval after 429 = %v, %v; want 5m", interval, err)
	}

	// Restart durability: a new store/runner reads the same committed penalty.
	h.reopen()
	state, found, err = h.st.GetProviderRateLimit(context.Background(), "adaptive-p", domain.ProviderRateLimitScopeProvider)
	if err != nil || !found || state.Consecutive429s != 1 || state.AdaptiveInterval != 10*time.Minute {
		t.Fatalf("rate limit after restart = %+v, found=%v, err=%v", state, found, err)
	}

	h.clock.Advance(45 * time.Minute)
	if _, err := h.runFor("adaptive", "adaptive-p"); err != nil {
		t.Fatalf("second 429: %v", err)
	}
	state, found, err = h.st.GetProviderRateLimit(context.Background(), "adaptive-p", domain.ProviderRateLimitScopeProvider)
	if err != nil || !found {
		t.Fatalf("rate limit after second 429 = %+v, found=%v, err=%v", state, found, err)
	}
	if state.Consecutive429s != 2 || state.AdaptiveInterval != 20*time.Minute {
		t.Fatalf("second 429 state = %+v, want level 2 / 20m", state)
	}
	// Let the mandatory cooldown expire before exercising recovery. A success
	// that somehow completed earlier must not shorten the hold (covered by the
	// policy test).
	h.clock.Advance(45 * time.Minute)

	// One or two successes must not erase the penalty.
	for index := 0; index < 2; index++ {
		h.clock.Advance(time.Minute)
		if _, err := h.runFor("adaptive", "adaptive-p"); err != nil {
			t.Fatalf("success %d: %v", index+1, err)
		}
		state, found, err = h.st.GetProviderRateLimit(context.Background(), "adaptive-p", domain.ProviderRateLimitScopeProvider)
		if err != nil || !found || state.Consecutive429s != 2 || state.AdaptiveInterval != 20*time.Minute {
			t.Fatalf("after success %d state = %+v, found=%v, err=%v", index+1, state, found, err)
		}
		targetAfter := h.targetByID(target.ID)
		if targetAfter.NextDueAt == nil || !targetAfter.NextDueAt.Equal(h.clock.Now().Add(20*time.Minute)) {
			t.Fatalf("target after success %d = %+v, want adaptive interval", index+1, targetAfter)
		}
	}

	// The third consecutive success releases one escalation step.
	h.clock.Advance(time.Minute)
	if _, err := h.runFor("adaptive", "adaptive-p"); err != nil {
		t.Fatalf("third success: %v", err)
	}
	state, found, err = h.st.GetProviderRateLimit(context.Background(), "adaptive-p", domain.ProviderRateLimitScopeProvider)
	if err != nil || !found || state.Consecutive429s != 1 || state.AdaptiveInterval != 10*time.Minute {
		t.Fatalf("after third success state = %+v, found=%v, err=%v", state, found, err)
	}
	targetAfter := h.targetByID(target.ID)
	if targetAfter.NextDueAt == nil || !targetAfter.NextDueAt.Equal(h.clock.Now().Add(10*time.Minute)) {
		t.Fatalf("target after recovery step = %+v, want 10m", targetAfter)
	}

	// Nine successes eventually restore the configured interval.
	for state.Consecutive429s > 0 {
		h.clock.Advance(time.Minute)
		if _, err := h.runFor("adaptive", "adaptive-p"); err != nil {
			t.Fatalf("recovery success: %v", err)
		}
		state, found, err = h.st.GetProviderRateLimit(context.Background(), "adaptive-p", domain.ProviderRateLimitScopeProvider)
		if err != nil || !found {
			t.Fatalf("recovery state = %+v, found=%v, err=%v", state, found, err)
		}
	}
	if state.AdaptiveInterval != 0 {
		t.Fatalf("fully recovered adaptive interval = %v, want 0", state.AdaptiveInterval)
	}
	targetAfter = h.targetByID(target.ID)
	if targetAfter.NextDueAt == nil || !targetAfter.NextDueAt.Equal(h.clock.Now().Add(5*time.Minute)) {
		t.Fatalf("target after full recovery = %+v, want configured 5m", targetAfter)
	}
}

func TestDiscarded429StillAppliesProviderPenalty(t *testing.T) {
	adapter := newRecordingProvider("discard-rate", nil)
	adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return provider.Result{}, &provider.Error{
			Kind:       provider.ErrRateLimited,
			Op:         "test.Check",
			StatusCode: 429,
			RetryAfter: 2 * time.Second,
		}
	}
	h := newIntegrationHarness(t, adapter)
	monitor := h.createMonitor("discard-rate-monitor", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("discard-rate"))
	target := h.target(monitor.ID, "discard-rate")
	beforeTarget := h.targetByID(target.ID)
	if _, err := h.sqlDB.Exec(`UPDATE monitors SET enabled = 0 WHERE id = ?`, monitor.ID); err != nil {
		t.Fatalf("pause monitor: %v", err)
	}

	result, err := h.runFor(monitor.ID, "discard-rate")
	if err != nil {
		t.Fatalf("discarded 429: %v", err)
	}
	if result.Settled {
		t.Fatal("discarded 429 was settled")
	}
	check := h.latestCheck(target.ID)
	if check.Outcome != "discarded" || check.ErrorKind.String != "monitor_disabled" {
		t.Fatalf("discard check = %+v", check)
	}
	state, found, err := h.st.GetProviderRateLimit(context.Background(), "discard-rate", domain.ProviderRateLimitScopeProvider)
	if err != nil || !found || state.Consecutive429s != 1 {
		t.Fatalf("discarded 429 rate limit = %+v, found=%v, err=%v", state, found, err)
	}
	assertIntegrationTargetUnchanged(t, beforeTarget, h.targetByID(target.ID))
}

func TestAdaptive429DoesNotPenalizeUnrelatedProvider(t *testing.T) {
	alpha := newRecordingProvider("alpha-rate", nil)
	alpha.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return provider.Result{}, &provider.Error{
			Kind:       provider.ErrRateLimited,
			Op:         "test.Check",
			StatusCode: 429,
			RetryAfter: 2 * time.Second,
		}
	}
	beta := newRecordingProvider("beta-rate", nil)
	beta.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return emptyResult(schedulerNow), nil
	}
	h := newIntegrationHarness(t, alpha, beta)
	h.createMonitor("alpha-monitor", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("alpha-rate"))
	h.createMonitor("beta-monitor", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("beta-rate"))

	if _, err := h.runFor("alpha-monitor", "alpha-rate"); err != nil {
		t.Fatalf("alpha 429: %v", err)
	}
	if _, err := h.runFor("beta-monitor", "beta-rate"); err != nil {
		t.Fatalf("beta success: %v", err)
	}
	if _, found, err := h.st.GetProviderRateLimit(context.Background(), "beta-rate", domain.ProviderRateLimitScopeProvider); err != nil || found {
		t.Fatalf("beta acquired a rate-limit penalty: found=%v err=%v", found, err)
	}
	betaTarget := h.target("beta-monitor", "beta-rate")
	if betaTarget.NextDueAt == nil || !betaTarget.NextDueAt.Equal(h.clock.Now().Add(5*time.Minute)) {
		t.Fatalf("beta target = %+v, want configured interval", betaTarget)
	}
}
