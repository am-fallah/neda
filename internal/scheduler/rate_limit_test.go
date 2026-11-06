package scheduler_test

import (
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/scheduler"
)

func TestApplyProviderRateLimit429EscalatesAndCaps(t *testing.T) {
	now := schedulerNow
	configured := 5 * time.Minute
	state := domain.ProviderRateLimit{ProviderID: "p"}

	state, first := scheduler.ApplyProviderRateLimit429(now, configured, 0, state, midpointRandom{}, scheduler.AdaptivePolicy{})
	if !first.Escalated || state.Consecutive429s != 1 || state.AdaptiveInterval != 10*time.Minute {
		t.Fatalf("first 429 state=%+v change=%+v", state, first)
	}
	if state.CooldownUntil == nil || !state.CooldownUntil.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("first cooldown = %v, want %v", state.CooldownUntil, now.Add(10*time.Minute))
	}
	if state.ConsecutiveSuccesses != 0 {
		t.Fatalf("success count = %d, want 0", state.ConsecutiveSuccesses)
	}

	state, second := scheduler.ApplyProviderRateLimit429(now.Add(time.Minute), configured, 0, state, midpointRandom{}, scheduler.AdaptivePolicy{})
	if state.Consecutive429s != 2 || state.AdaptiveInterval != 20*time.Minute {
		t.Fatalf("second 429 state=%+v change=%+v", state, second)
	}
	state, third := scheduler.ApplyProviderRateLimit429(now.Add(2*time.Minute), configured, 0, state, midpointRandom{}, scheduler.AdaptivePolicy{})
	if state.Consecutive429s != 3 || state.AdaptiveInterval != 30*time.Minute {
		t.Fatalf("third 429 state=%+v change=%+v", state, third)
	}
	for index := 0; index < 20; index++ {
		state, _ = scheduler.ApplyProviderRateLimit429(now, configured, 0, state, midpointRandom{}, scheduler.AdaptivePolicy{})
	}
	if state.AdaptiveInterval != 30*time.Minute {
		t.Fatalf("capped adaptive interval = %v, want 30m", state.AdaptiveInterval)
	}
	if state.Consecutive429s != 8 {
		t.Fatalf("escalation level = %d, want bounded 8", state.Consecutive429s)
	}
}

func TestApplyProviderRateLimitRetryAfterBoundsAndJitter(t *testing.T) {
	now := schedulerNow
	configured := 5 * time.Minute

	state, change := scheduler.ApplyProviderRateLimit429(now, configured, 45*time.Minute, domain.ProviderRateLimit{}, midpointRandom{}, scheduler.AdaptivePolicy{})
	if state.AdaptiveInterval != 10*time.Minute {
		t.Fatalf("adaptive interval = %v, want 10m", state.AdaptiveInterval)
	}
	if state.CooldownUntil == nil || !state.CooldownUntil.Equal(now.Add(45*time.Minute)) {
		t.Fatalf("cooldown = %v, want Retry-After lower bound", state.CooldownUntil)
	}
	if state.LastRetryAfter != 45*time.Minute || change.RetryAfter != 45*time.Minute {
		t.Fatalf("retry-after = %v / %v", state.LastRetryAfter, change.RetryAfter)
	}

	low, _ := scheduler.ApplyProviderRateLimit429(now, configured, 0, domain.ProviderRateLimit{}, &scriptedRandom{values: []int64{0}}, scheduler.AdaptivePolicy{})
	if low.AdaptiveInterval != 9*time.Minute {
		t.Fatalf("negative-jitter adaptive interval = %v, want 9m", low.AdaptiveInterval)
	}
	high, _ := scheduler.ApplyProviderRateLimit429(now, configured, 0, domain.ProviderRateLimit{}, &scriptedRandom{values: []int64{int64(120 * time.Second)}}, scheduler.AdaptivePolicy{})
	if high.AdaptiveInterval != 11*time.Minute {
		t.Fatalf("positive-jitter adaptive interval = %v, want 11m", high.AdaptiveInterval)
	}

	huge, _ := scheduler.ApplyProviderRateLimit429(now, configured, 48*time.Hour, domain.ProviderRateLimit{}, midpointRandom{}, scheduler.AdaptivePolicy{})
	if huge.LastRetryAfter != 24*time.Hour {
		t.Fatalf("absurd retry-after = %v, want 24h safety cap", huge.LastRetryAfter)
	}
	negative, _ := scheduler.ApplyProviderRateLimit429(now, configured, -time.Hour, domain.ProviderRateLimit{}, midpointRandom{}, scheduler.AdaptivePolicy{})
	if negative.LastRetryAfter != 0 {
		t.Fatalf("negative retry-after = %v, want 0", negative.LastRetryAfter)
	}

	tight, _ := scheduler.ApplyProviderRateLimit429(now, configured, 0, domain.ProviderRateLimit{}, midpointRandom{}, scheduler.AdaptivePolicy{MaxInterval: 7 * time.Minute})
	if tight.AdaptiveInterval != 7*time.Minute {
		t.Fatalf("policy max interval = %v, want 7m", tight.AdaptiveInterval)
	}
}

func TestApplyProviderRateLimitSuccessRecoversGradually(t *testing.T) {
	now := schedulerNow
	configured := 5 * time.Minute
	cooldown := now.Add(20 * time.Minute)
	state := domain.ProviderRateLimit{
		ProviderID:       "p",
		Consecutive429s:  2,
		AdaptiveInterval: 20 * time.Minute,
		CooldownUntil:    &cooldown,
	}

	state, first := scheduler.ApplyProviderRateLimitSuccess(now, configured, state, scheduler.AdaptivePolicy{})
	if first.Recovered || state.AdaptiveInterval != 20*time.Minute || state.ConsecutiveSuccesses != 1 {
		t.Fatalf("one success state=%+v change=%+v; penalty must survive", state, first)
	}
	if state.CooldownUntil == nil || !state.CooldownUntil.Equal(cooldown) {
		t.Fatalf("success cleared cooldown: %v", state.CooldownUntil)
	}
	state, _ = scheduler.ApplyProviderRateLimitSuccess(now, configured, state, scheduler.AdaptivePolicy{})
	if state.AdaptiveInterval != 20*time.Minute || state.ConsecutiveSuccesses != 2 {
		t.Fatalf("two successes state=%+v", state)
	}
	state, third := scheduler.ApplyProviderRateLimitSuccess(now, configured, state, scheduler.AdaptivePolicy{})
	if !third.Recovered || state.Consecutive429s != 1 || state.AdaptiveInterval != 10*time.Minute || state.ConsecutiveSuccesses != 0 {
		t.Fatalf("third success state=%+v change=%+v", state, third)
	}
	for index := 0; index < 3; index++ {
		state, _ = scheduler.ApplyProviderRateLimitSuccess(now, configured, state, scheduler.AdaptivePolicy{})
	}
	if state.Consecutive429s != 0 || state.AdaptiveInterval != 0 {
		t.Fatalf("fully recovered state=%+v", state)
	}

	configuredSuccess := scheduler.AdaptivePolicy{RecoverySuccesses: 1}
	one, change := scheduler.ApplyProviderRateLimitSuccess(now, configured, domain.ProviderRateLimit{
		ProviderID: "p", Consecutive429s: 1, AdaptiveInterval: 10 * time.Minute,
	}, configuredSuccess)
	if !change.Recovered || one.AdaptiveInterval != 0 || one.Consecutive429s != 0 {
		t.Fatalf("configured recovery state=%+v change=%+v", one, change)
	}
}

func TestEffectiveIntervalAndLargestQueueSpacing(t *testing.T) {
	configured := 5 * time.Minute
	if got := scheduler.EffectivePollingInterval(configured, domain.ProviderRateLimit{}); got != configured {
		t.Fatalf("effective interval without penalty = %v, want %v", got, configured)
	}
	if got := scheduler.EffectivePollingInterval(configured, domain.ProviderRateLimit{AdaptiveInterval: 10 * time.Minute}); got != 10*time.Minute {
		t.Fatalf("effective interval with penalty = %v, want 10m", got)
	}
	if got := scheduler.EffectivePollingInterval(configured, domain.ProviderRateLimit{AdaptiveInterval: time.Minute}); got != configured {
		t.Fatalf("effective interval below configured = %v, want %v", got, configured)
	}

	cases := []struct {
		name    string
		due     int
		window  time.Duration
		spacing time.Duration
		want    time.Duration
	}{
		{name: "large queue spreads across ten minutes", due: 60, window: 10 * time.Minute, spacing: 5 * time.Second, want: 10 * time.Second},
		{name: "two overdue targets keep minimum spacing", due: 2, window: 5 * time.Minute, spacing: 5 * time.Second, want: 5 * time.Second},
		{name: "medium queue spreads", due: 20, window: 5 * time.Minute, spacing: 5 * time.Second, want: 15 * time.Second},
		{name: "over capacity stays at minimum", due: 200, window: 10 * time.Minute, spacing: 5 * time.Second, want: 5 * time.Second},
		{name: "single target keeps minimum", due: 1, window: 10 * time.Minute, spacing: 5 * time.Second, want: 5 * time.Second},
		{name: "disabled spacing stays disabled", due: 60, window: 10 * time.Minute, spacing: 0, want: 0},
	}
	for _, test := range cases {
		if got := scheduler.RequestSpacing(test.due, test.window, test.spacing); got != test.want {
			t.Fatalf("%s: spacing = %v, want %v", test.name, got, test.want)
		}
	}
}

func TestDueDistributionDelaySpreadsLargeBacklog(t *testing.T) {
	window := 10 * time.Minute
	first := scheduler.DueDistributionDelay(0, 60, window, 5*time.Second, zeroRandom())
	last := scheduler.DueDistributionDelay(59, 60, window, 5*time.Second, zeroRandom())
	if first != 0 {
		t.Fatalf("first slot = %v, want 0", first)
	}
	if last != 590*time.Second {
		t.Fatalf("last slot = %v, want 590s", last)
	}
	if last >= window {
		t.Fatalf("last slot %v reached the full window %v", last, window)
	}

	// A small provider queue keeps the old near-term jitter and does not wait
	// most of an interval for no reason.
	small := scheduler.DueDistributionDelay(1, 2, 5*time.Minute, 5*time.Second, zeroRandom())
	if small > 30*time.Second {
		t.Fatalf("small-queue delay = %v, want at most 30s", small)
	}
}

func TestDecideTargetScheduleWithProviderUsesStrictestDelay(t *testing.T) {
	now := schedulerNow
	configured := 5 * time.Minute
	success := provider.Classification{Category: provider.CategorySuccess}

	decision, err := scheduler.DecideTargetScheduleWithProvider(
		now, configured, 10*time.Minute, now.Add(15*time.Minute), 4, success, midpointRandom{},
	)
	if err != nil {
		t.Fatalf("success decision: %v", err)
	}
	if decision.ConsecutiveFailures != 0 || !decision.NextDueAt.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("success decision = %+v, want cooldown lower bound", decision)
	}

	rateLimited := provider.Classification{
		Category:           provider.CategoryInfrastructure,
		Kind:               provider.ErrRateLimited,
		StatusCode:         429,
		RetryAfter:         45 * time.Minute,
		CountsTowardOutage: true,
		TargetBackoff:      true,
	}
	decision, err = scheduler.DecideTargetScheduleWithProvider(
		now, configured, 10*time.Minute, now.Add(10*time.Minute), 0, rateLimited, midpointRandom{},
	)
	if err != nil {
		t.Fatalf("rate-limited decision: %v", err)
	}
	if decision.ConsecutiveFailures != 1 || !decision.NextDueAt.Equal(now.Add(45*time.Minute)) {
		t.Fatalf("rate-limited decision = %+v, want Retry-After lower bound", decision)
	}
}
