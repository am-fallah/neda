package scheduler

import (
	"time"

	"github.com/am-fallah/neda/internal/domain"
)

const (
	// adaptiveBackoffFloor is the minimum safe adaptive polling cap. The
	// effective cap is max(4×configured interval, 30m), matching the existing
	// target backoff cap.
	adaptiveBackoffFloor = 30 * time.Minute
	// defaultAdaptiveRecoverySuccesses is the number of consecutive applicable
	// successes required before one adaptive escalation step is released.
	defaultAdaptiveRecoverySuccesses = 3
	// defaultMaxAdaptiveEscalations bounds repeated 429 escalation so a stored
	// counter cannot overflow or create an unbounded interval.
	defaultMaxAdaptiveEscalations = 8
	// maxRetryAfter is a safety bound for provider-supplied Retry-After values.
	// Adapters already cap at the same value; this protects the scheduler from
	// malformed or hostile hints while still honoring long mandatory cooldowns.
	maxRetryAfter = 24 * time.Hour
	// spreadActivationDivisor activates full-window distribution only when
	// servicing the queue at minimum spacing would consume at least 1/4 of the
	// polling window. Tiny queues keep the normal 5s spacing and do not wait
	// most of an interval for no reason.
	spreadActivationDivisor = 4
)

// AdaptivePolicy controls bounded provider-level 429 escalation and recovery.
// The zero value uses the documented production defaults. It is intentionally
// independent of the user-configured polling interval, which is never mutated.
type AdaptivePolicy struct {
	// MaxInterval overrides the computed cap max(4×configured, 30m). It is
	// mainly useful for deterministic tests; production leaves it zero.
	MaxInterval time.Duration
	// RecoverySuccesses overrides the consecutive-success threshold. Zero uses
	// defaultAdaptiveRecoverySuccesses.
	RecoverySuccesses int
	// MaxEscalations overrides the maximum 429 escalation level. Zero uses
	// defaultMaxAdaptiveEscalations.
	MaxEscalations int
}

// RateLimitChange describes one adaptive state transition for logging and
// tests. It contains no provider payload or credentials.
type RateLimitChange struct {
	Escalated        bool
	Recovered        bool
	PreviousInterval time.Duration
	NewInterval      time.Duration
	Level            int
	RetryAfter       time.Duration
	CooldownUntil    time.Time
}

// MaxAdaptiveInterval returns the configured safety cap for one polling
// interval. The result is never below the configured interval.
func (p AdaptivePolicy) MaxAdaptiveInterval(configured time.Duration) time.Duration {
	cap := 4 * configured
	if cap < adaptiveBackoffFloor {
		cap = adaptiveBackoffFloor
	}
	if p.MaxInterval > 0 {
		cap = p.MaxInterval
	}
	if cap < configured {
		cap = configured
	}
	return cap
}

func (p AdaptivePolicy) recoverySuccesses() int {
	if p.RecoverySuccesses > 0 {
		return p.RecoverySuccesses
	}
	return defaultAdaptiveRecoverySuccesses
}

func (p AdaptivePolicy) maxEscalations() int {
	if p.MaxEscalations > 0 {
		return p.MaxEscalations
	}
	return defaultMaxAdaptiveEscalations
}

// EffectivePollingInterval returns max(configured, adaptive). The hard
// cooldown is an absolute deadline handled separately by the scheduler; it is
// never folded into the configured setting.
func EffectivePollingInterval(configured time.Duration, state domain.ProviderRateLimit) time.Duration {
	if configured <= 0 {
		return configured
	}
	if state.AdaptiveInterval > configured {
		return state.AdaptiveInterval
	}
	return configured
}

// NormalizeRetryAfter safely bounds a provider hint. Missing, zero, negative,
// and absurdly large values are clamped; a valid value is always a lower bound.
func NormalizeRetryAfter(value time.Duration) time.Duration {
	if value <= 0 {
		return 0
	}
	if value > maxRetryAfter {
		return maxRetryAfter
	}
	return value
}

// ApplyProviderRateLimit429 escalates the provider-wide penalty for one 429.
// The configured interval is not read from or written to this state; it is an
// input used only to derive the bounded adaptive interval. Retry-After is a
// mandatory lower bound on the cooldown and survives jitter.
func ApplyProviderRateLimit429(
	now time.Time,
	configured time.Duration,
	retryAfter time.Duration,
	state domain.ProviderRateLimit,
	random Random,
	policy AdaptivePolicy,
) (domain.ProviderRateLimit, RateLimitChange) {
	now = now.UTC()
	change := RateLimitChange{PreviousInterval: state.AdaptiveInterval}
	if configured <= 0 || now.IsZero() {
		return state, change
	}

	level := state.Consecutive429s + 1
	if max := policy.maxEscalations(); level > max {
		level = max
	}
	cap := policy.MaxAdaptiveInterval(configured)
	base := adaptiveDelayForLevel(configured, level, cap)
	jittered := base + JitterOffset(base, random)
	if jittered < configured {
		jittered = configured
	}
	if jittered > cap {
		jittered = cap
	}

	adaptive := state.AdaptiveInterval
	if adaptive < jittered {
		adaptive = jittered
	}
	if adaptive > cap {
		adaptive = cap
	}
	if adaptive < configured {
		adaptive = configured
	}

	retryAfter = NormalizeRetryAfter(retryAfter)
	cooldown := now.Add(adaptive)
	if retryUntil := now.Add(retryAfter); retryUntil.After(cooldown) {
		cooldown = retryUntil
	}
	if state.CooldownUntil != nil && state.CooldownUntil.After(cooldown) {
		cooldown = state.CooldownUntil.UTC()
	}

	state.Consecutive429s = level
	state.AdaptiveInterval = adaptive
	state.CooldownUntil = timePointerValue(cooldown)
	state.ConsecutiveSuccesses = 0
	state.Last429At = timePointerValue(now)
	state.LastRetryAfter = retryAfter
	state.UpdatedAt = now
	if state.Scope == "" {
		state.Scope = domain.ProviderRateLimitScopeProvider
	}

	change.Escalated = true
	change.NewInterval = adaptive
	change.Level = level
	change.RetryAfter = retryAfter
	change.CooldownUntil = cooldown
	return state, change
}

// ApplyProviderRateLimitSuccess records one applicable success. It never
// clears the penalty on a single success: one escalation step is released only
// after the configured number of consecutive successes. The cooldown deadline
// is retained even when a late result succeeds, so a success cannot bypass a
// provider-wide hold.
func ApplyProviderRateLimitSuccess(
	now time.Time,
	configured time.Duration,
	state domain.ProviderRateLimit,
	policy AdaptivePolicy,
) (domain.ProviderRateLimit, RateLimitChange) {
	now = now.UTC()
	change := RateLimitChange{PreviousInterval: state.AdaptiveInterval, Level: state.Consecutive429s}
	if now.IsZero() {
		return state, change
	}
	state.LastSuccessAt = timePointerValue(now)
	state.UpdatedAt = now
	if state.Scope == "" {
		state.Scope = domain.ProviderRateLimitScopeProvider
	}
	if state.Consecutive429s <= 0 {
		state.ConsecutiveSuccesses = 0
		state.AdaptiveInterval = 0
		return state, change
	}

	state.ConsecutiveSuccesses++
	if state.ConsecutiveSuccesses < policy.recoverySuccesses() {
		return state, change
	}

	state.ConsecutiveSuccesses = 0
	state.Consecutive429s--
	if state.Consecutive429s <= 0 {
		state.Consecutive429s = 0
		state.AdaptiveInterval = 0
	} else {
		recovered := state.AdaptiveInterval / 2
		if configured > 0 && recovered <= configured {
			recovered = 0
		}
		state.AdaptiveInterval = recovered
	}
	change.Recovered = true
	change.NewInterval = state.AdaptiveInterval
	change.Level = state.Consecutive429s
	return state, change
}

// adaptiveDelayForLevel returns configured×2^level capped without overflow.
func adaptiveDelayForLevel(configured time.Duration, level int, cap time.Duration) time.Duration {
	if configured <= 0 || level <= 0 {
		return 0
	}
	delay := configured
	for step := 0; step < level; step++ {
		if delay >= cap {
			return cap
		}
		if delay > cap/2 {
			return cap
		}
		delay *= 2
	}
	if delay > cap {
		return cap
	}
	return delay
}

// RequestSpacing returns the start-to-start spacing for a provider at the
// current queue depth. Small queues retain the configured minimum spacing;
// substantial queues spread over the effective polling window. This is the
// single distribution calculation used by the scheduler loop.
func RequestSpacing(dueCount int, effectiveInterval, minSpacing time.Duration) time.Duration {
	if minSpacing <= 0 || effectiveInterval <= 0 || dueCount <= 1 {
		return minSpacing
	}
	activation := effectiveInterval / spreadActivationDivisor
	if activation <= 0 || time.Duration(dueCount) < activation/minSpacing {
		return minSpacing
	}
	spread := effectiveInterval / time.Duration(dueCount)
	if spread < minSpacing {
		return minSpacing
	}
	return spread
}

// DueDistributionDelay assigns one slot inside a polling window for startup,
// resume, and restart distribution. When the queue is too small to justify
// full-window spreading, it returns the existing bounded near-term jitter
// (0–min(10% interval, 30s)). Otherwise slots are evenly divided and receive a
// small non-negative jitter so instances and restarts do not align exactly.
func DueDistributionDelay(index, total int, effectiveInterval, minSpacing time.Duration, random Random) time.Duration {
	if effectiveInterval <= 0 || total <= 0 {
		return 0
	}
	if index < 0 {
		index = 0
	}
	if index >= total {
		index = total - 1
	}
	if RequestSpacing(total, effectiveInterval, minSpacing) <= minSpacing {
		return NearTermDelay(effectiveInterval, random)
	}
	slot := effectiveInterval / time.Duration(total)
	delay := time.Duration(index)*slot + NearTermDelay(slot, random)
	if delay > effectiveInterval {
		delay = effectiveInterval
	}
	return delay
}

func timePointerValue(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}
