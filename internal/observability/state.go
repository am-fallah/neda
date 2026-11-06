// Package observability owns scheduler/provider runtime snapshots, health-state
// derivation, and bounded provider rollup data. It is intentionally separate
// from authoritative scheduling, availability, and notification state.
package observability

import (
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/health"
)

const (
	// DefaultQueueSampleInterval bounds durable scheduler sampling to at most
	// four point-in-time samples per provider per minute.
	DefaultQueueSampleInterval = 15 * time.Second
	// SnapshotStaleAfter is the maximum age of a scheduler heartbeat before the
	// runtime snapshot is presented as stale. The scheduler heartbeats every
	// DefaultQueueSampleInterval even when no target is due.
	SnapshotStaleAfter = 2 * time.Minute
	// QueueStaleAfter is the maximum age of a provider queue sample before the
	// queue block is marked stale.
	QueueStaleAfter = 2 * DefaultQueueSampleInterval
	// QueueSampleRetention matches the provider-minute rollup retention.
	QueueSampleRetention = 7 * 24 * time.Hour
)

// ProviderState is the primary operational state shown for one provider.
type ProviderState string

const (
	StateHealthy     ProviderState = "healthy"
	StateDegraded    ProviderState = "degraded"
	StateRateLimited ProviderState = "rate_limited"
	StateCoolingDown ProviderState = "cooling_down"
	StateRecovering  ProviderState = "recovering"
	StateIdle        ProviderState = "idle"
	StateUnknown     ProviderState = "unknown"
)

// HealthSnapshot is the provider_health subset used for state derivation.
// Missing health data is represented by a nil pointer, not a zero record.
type HealthSnapshot struct {
	LastAttemptAt       *time.Time
	LastSuccessAt       *time.Time
	LastFailureAt       *time.Time
	LastErrorKind       string
	ConsecutiveFailures int
	OpenEpisode         bool
}

// ProviderStateInput is the complete input to the pure health-state function.
// ConfiguredInterval is the single global polling interval; there is no
// per-provider configured interval in the current product. NextDueAt is the
// earliest active target deadline and lets the stale calculation include
// target-level backoff instead of declaring a provider stale while no check is
// expected.
type ProviderStateInput struct {
	Now                time.Time
	ConfiguredInterval time.Duration
	MonitoringEnabled  bool
	ActiveTargets      int
	NextDueAt          *time.Time
	Health             *HealthSnapshot
	RateLimit          domain.ProviderRateLimit
	RequiredSuccesses  int
	SchedulerAvailable bool
}

// ProviderFlags are secondary qualifiers. They let the UI show, for example,
// an active cooldown while also marking the provider as degraded.
type ProviderFlags struct {
	Degraded    bool `json:"degraded"`
	Stale       bool `json:"stale"`
	RateLimited bool `json:"rateLimited"`
	CoolingDown bool `json:"coolingDown"`
	Recovering  bool `json:"recovering"`
	Idle        bool `json:"idle"`
	Unknown     bool `json:"unknown"`
}

// ProviderStateResult is the derived state plus stable reason codes.
type ProviderStateResult struct {
	State   ProviderState `json:"state"`
	Reasons []string      `json:"reasons"`
	Flags   ProviderFlags `json:"flags"`
}

// StaleThreshold returns the exact observability staleness threshold:
//
//	base = max(3 × max(configured, adaptive), 15 minutes)
//	wait = max(0, earliestNextDueAt - now, activeCooldownUntil - now)
//	threshold = base + wait
//
// The extra wait prevents a provider from being labeled stale while every
// active target is intentionally backed off or the provider is in a mandatory
// cooldown. A provider that has never succeeded is Unknown, not stale.
func StaleThreshold(configured, adaptive time.Duration, nextDueAt, cooldownUntil *time.Time, now time.Time) time.Duration {
	effective := configured
	if adaptive > effective {
		effective = adaptive
	}
	if effective <= 0 {
		effective = domain.DefaultPollInterval
	}
	threshold := 3 * effective
	if threshold < health.StaleFloor {
		threshold = health.StaleFloor
	}
	var wait time.Duration
	now = now.UTC()
	if nextDueAt != nil && nextDueAt.After(now) {
		wait = nextDueAt.Sub(now)
	}
	if cooldownUntil != nil && cooldownUntil.After(now) && cooldownUntil.Sub(now) > wait {
		wait = cooldownUntil.Sub(now)
	}
	return threshold + wait
}

// IsStale reports whether a provider's last successful evidence is older than
// threshold. An open degraded episode is always stale evidence.
func IsStale(lastSuccess *time.Time, openEpisode bool, now time.Time, threshold time.Duration) bool {
	if openEpisode {
		return true
	}
	if lastSuccess == nil || now.IsZero() {
		return false
	}
	return now.UTC().Sub(lastSuccess.UTC()) > threshold
}

// DeriveProviderState applies the documented precedence. It never mutates the
// input and does not perform I/O.
func DeriveProviderState(input ProviderStateInput) ProviderStateResult {
	now := input.Now.UTC()
	reasons := make([]string, 0, 3)
	result := ProviderStateResult{
		Reasons: reasons,
	}

	cooldownActive := input.RateLimit.CooldownUntil != nil &&
		input.RateLimit.CooldownUntil.After(now)
	penaltyActive := input.RateLimit.Consecutive429s > 0 ||
		(input.ConfiguredInterval > 0 && input.RateLimit.AdaptiveInterval > input.ConfiguredInterval)

	lastSuccess := (*time.Time)(nil)
	if input.Health != nil {
		lastSuccess = input.Health.LastSuccessAt
	}
	if lastSuccess == nil {
		lastSuccess = input.RateLimit.LastSuccessAt
	}
	last429 := input.RateLimit.Last429At
	successSince429 := lastSuccess != nil && last429 != nil && lastSuccess.After(*last429)

	result.Flags.CoolingDown = cooldownActive
	result.Flags.RateLimited = penaltyActive
	result.Flags.Recovering = penaltyActive && successSince429 && !cooldownActive
	if cooldownActive {
		result.Flags.Recovering = false
	}

	openEpisode := input.Health != nil && input.Health.OpenEpisode
	failures := 0
	if input.Health != nil {
		failures = input.Health.ConsecutiveFailures
	}
	result.Flags.Degraded = openEpisode || failures >= health.DegradedFailureThreshold

	threshold := StaleThreshold(
		input.ConfiguredInterval,
		input.RateLimit.AdaptiveInterval,
		input.NextDueAt,
		input.RateLimit.CooldownUntil,
		now,
	)
	result.Flags.Stale = IsStale(lastSuccess, openEpisode, now, threshold)

	if !input.MonitoringEnabled {
		result.State = StateIdle
		result.Reasons = append(result.Reasons, "monitoring_off")
		result.Flags.Idle = true
		return result
	}
	if input.ActiveTargets <= 0 {
		result.State = StateIdle
		result.Reasons = append(result.Reasons, "no_active_targets")
		result.Flags.Idle = true
		return result
	}

	if cooldownActive {
		result.State = StateCoolingDown
		result.Reasons = append(result.Reasons, "cooldown_active")
		if penaltyActive {
			result.Reasons = append(result.Reasons, "rate_limit_penalty")
		}
		return result
	}

	if penaltyActive {
		if successSince429 {
			result.State = StateRecovering
			result.Reasons = append(result.Reasons, "rate_limit_recovery")
		} else {
			result.State = StateRateLimited
			result.Reasons = append(result.Reasons, "rate_limit_penalty")
		}
		if result.Flags.Degraded {
			result.Reasons = append(result.Reasons, "degraded_episode")
		}
		return result
	}

	if result.Flags.Degraded {
		result.State = StateDegraded
		result.Reasons = append(result.Reasons, "degraded_episode")
		return result
	}

	if !input.SchedulerAvailable {
		result.State = StateUnknown
		result.Flags.Unknown = true
		result.Reasons = append(result.Reasons, "scheduler_unavailable")
		return result
	}

	if lastSuccess != nil && !result.Flags.Stale {
		result.State = StateHealthy
		if failures > 0 {
			result.Reasons = append(result.Reasons, "recent_failures")
		}
		return result
	}

	result.State = StateUnknown
	result.Flags.Unknown = true
	switch {
	case input.Health == nil:
		result.Reasons = append(result.Reasons, "no_health_data")
	case input.Health.LastSuccessAt == nil && lastSuccess == nil:
		result.Reasons = append(result.Reasons, "no_successful_check")
	case result.Flags.Stale:
		result.Reasons = append(result.Reasons, "stale_success")
	default:
		result.Reasons = append(result.Reasons, "insufficient_data")
	}
	return result
}

// CooldownRemaining returns the positive remaining duration.
func CooldownRemaining(now time.Time, until *time.Time) time.Duration {
	if until == nil {
		return 0
	}
	remaining := until.UTC().Sub(now.UTC())
	if remaining < 0 {
		return 0
	}
	return remaining
}

// EffectiveInterval mirrors the scheduling package's max(configured, adaptive)
// rule without changing the configured setting.
func EffectiveInterval(configured time.Duration, state domain.ProviderRateLimit) time.Duration {
	if configured <= 0 {
		return configured
	}
	if state.AdaptiveInterval > configured {
		return state.AdaptiveInterval
	}
	return configured
}
