package observability

import (
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/domain"
)

var stateNow = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

func timePtr(value time.Time) *time.Time { return &value }

func baseInput() ProviderStateInput {
	return ProviderStateInput{
		Now:                stateNow,
		ConfiguredInterval: 5 * time.Minute,
		MonitoringEnabled:  true,
		ActiveTargets:      2,
		SchedulerAvailable: true,
		Health: &HealthSnapshot{
			LastAttemptAt: timePtr(stateNow.Add(-time.Minute)),
			LastSuccessAt: timePtr(stateNow.Add(-time.Minute)),
		},
		RequiredSuccesses: 3,
	}
}

func TestDeriveProviderState(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ProviderStateInput)
		want   ProviderState
		flags  ProviderFlags
	}{
		{
			name: "healthy",
			want: StateHealthy,
		},
		{
			name: "single failure is not degraded",
			mutate: func(in *ProviderStateInput) {
				in.Health.ConsecutiveFailures = 1
				in.Health.LastErrorKind = "timeout"
				in.Health.LastFailureAt = timePtr(stateNow.Add(-time.Minute))
			},
			want: StateHealthy,
		},
		{
			name: "three failures are degraded",
			mutate: func(in *ProviderStateInput) {
				in.Health.ConsecutiveFailures = 3
				in.Health.OpenEpisode = true
			},
			want:  StateDegraded,
			flags: ProviderFlags{Degraded: true, Stale: true},
		},
		{
			name: "monitoring off is idle",
			mutate: func(in *ProviderStateInput) {
				in.MonitoringEnabled = false
			},
			want:  StateIdle,
			flags: ProviderFlags{Idle: true},
		},
		{
			name: "no active targets is idle even without scheduler",
			mutate: func(in *ProviderStateInput) {
				in.ActiveTargets = 0
				in.SchedulerAvailable = false
			},
			want:  StateIdle,
			flags: ProviderFlags{Idle: true},
		},
		{
			name: "active cooldown",
			mutate: func(in *ProviderStateInput) {
				in.RateLimit = domain.ProviderRateLimit{
					Consecutive429s:  1,
					AdaptiveInterval: 10 * time.Minute,
					CooldownUntil:    timePtr(stateNow.Add(5 * time.Minute)),
					Last429At:        timePtr(stateNow.Add(-time.Minute)),
				}
			},
			want: StateCoolingDown,
			flags: ProviderFlags{
				RateLimited: true,
				CoolingDown: true,
			},
		},
		{
			name: "penalty without a success is rate limited",
			mutate: func(in *ProviderStateInput) {
				in.Health.LastSuccessAt = timePtr(stateNow.Add(-3 * time.Minute))
				in.RateLimit = domain.ProviderRateLimit{
					Consecutive429s:  1,
					AdaptiveInterval: 10 * time.Minute,
					CooldownUntil:    timePtr(stateNow.Add(-time.Minute)),
					Last429At:        timePtr(stateNow.Add(-2 * time.Minute)),
				}
			},
			want:  StateRateLimited,
			flags: ProviderFlags{RateLimited: true},
		},
		{
			name: "success after 429 is recovering",
			mutate: func(in *ProviderStateInput) {
				in.Health.LastSuccessAt = timePtr(stateNow.Add(-time.Minute))
				in.RateLimit = domain.ProviderRateLimit{
					Consecutive429s:  1,
					AdaptiveInterval: 10 * time.Minute,
					CooldownUntil:    timePtr(stateNow.Add(-time.Minute)),
					Last429At:        timePtr(stateNow.Add(-2 * time.Minute)),
					LastSuccessAt:    timePtr(stateNow.Add(-time.Minute)),
				}
			},
			want:  StateRecovering,
			flags: ProviderFlags{RateLimited: true, Recovering: true},
		},
		{
			name: "no health data is unknown",
			mutate: func(in *ProviderStateInput) {
				in.Health = nil
			},
			want:  StateUnknown,
			flags: ProviderFlags{Unknown: true},
		},
		{
			name: "stale success is unknown",
			mutate: func(in *ProviderStateInput) {
				in.Health.LastSuccessAt = timePtr(stateNow.Add(-time.Hour))
			},
			want:  StateUnknown,
			flags: ProviderFlags{Stale: true, Unknown: true},
		},
		{
			name: "scheduler unavailable is unknown even after a success",
			mutate: func(in *ProviderStateInput) {
				in.SchedulerAvailable = false
			},
			want:  StateUnknown,
			flags: ProviderFlags{Unknown: true},
		},
		{
			name: "future target due prevents false stale",
			mutate: func(in *ProviderStateInput) {
				in.Health.LastSuccessAt = timePtr(stateNow.Add(-40 * time.Minute))
				in.NextDueAt = timePtr(stateNow.Add(30 * time.Minute))
			},
			want: StateHealthy,
		},
		{
			name: "future cooldown extends stale threshold",
			mutate: func(in *ProviderStateInput) {
				in.Health.LastSuccessAt = timePtr(stateNow.Add(-40 * time.Minute))
				in.RateLimit = domain.ProviderRateLimit{
					Consecutive429s:  1,
					AdaptiveInterval: 10 * time.Minute,
					CooldownUntil:    timePtr(stateNow.Add(30 * time.Minute)),
					Last429At:        timePtr(stateNow.Add(-40 * time.Minute)),
				}
			},
			want: StateCoolingDown,
			flags: ProviderFlags{
				CoolingDown: true, RateLimited: true, Stale: false,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := baseInput()
			if test.mutate != nil {
				test.mutate(&input)
			}
			got := DeriveProviderState(input)
			if got.State != test.want {
				t.Fatalf("state = %q, want %q (reasons=%v flags=%+v)", got.State, test.want, got.Reasons, got.Flags)
			}
			if got.Flags.Degraded != test.flags.Degraded ||
				got.Flags.Stale != test.flags.Stale ||
				got.Flags.RateLimited != test.flags.RateLimited ||
				got.Flags.CoolingDown != test.flags.CoolingDown ||
				got.Flags.Recovering != test.flags.Recovering ||
				got.Flags.Idle != test.flags.Idle ||
				got.Flags.Unknown != test.flags.Unknown {
				t.Fatalf("flags = %+v, want subset %+v", got.Flags, test.flags)
			}
		})
	}
}

func TestStaleThresholdBoundaries(t *testing.T) {
	now := stateNow
	threshold := StaleThreshold(5*time.Minute, 0, nil, nil, now)
	if threshold != 15*time.Minute {
		t.Fatalf("threshold = %v, want 15m floor", threshold)
	}
	if IsStale(timePtr(now.Add(-15*time.Minute)), false, now, threshold) {
		t.Fatal("exactly-at-threshold evidence marked stale")
	}
	if !IsStale(timePtr(now.Add(-15*time.Minute-time.Second)), false, now, threshold) {
		t.Fatal("over-threshold evidence not marked stale")
	}
	if !IsStale(nil, true, now, threshold) {
		t.Fatal("open degraded episode not marked stale")
	}
	if IsStale(nil, false, now, threshold) {
		t.Fatal("never-checked provider marked stale")
	}

	adaptive := 20 * time.Minute
	if got := StaleThreshold(5*time.Minute, adaptive, nil, nil, now); got != 60*time.Minute {
		t.Fatalf("adaptive threshold = %v, want 60m", got)
	}
	next := now.Add(30 * time.Minute)
	if got := StaleThreshold(5*time.Minute, 0, &next, nil, now); got != 45*time.Minute {
		t.Fatalf("next-due threshold = %v, want 45m", got)
	}
	cooldown := now.Add(20 * time.Minute)
	if got := StaleThreshold(5*time.Minute, 0, &next, &cooldown, now); got != 45*time.Minute {
		t.Fatalf("cooldown threshold = %v, want max(next,cooldown)+base", got)
	}
}

func TestEffectiveInterval(t *testing.T) {
	configured := 5 * time.Minute
	if got := EffectiveInterval(configured, domain.ProviderRateLimit{}); got != configured {
		t.Fatalf("no penalty effective = %v, want %v", got, configured)
	}
	if got := EffectiveInterval(configured, domain.ProviderRateLimit{AdaptiveInterval: 10 * time.Minute}); got != 10*time.Minute {
		t.Fatalf("penalty effective = %v, want 10m", got)
	}
	if got := EffectiveInterval(configured, domain.ProviderRateLimit{AdaptiveInterval: time.Minute}); got != configured {
		t.Fatalf("small penalty effective = %v, want %v", got, configured)
	}
}
