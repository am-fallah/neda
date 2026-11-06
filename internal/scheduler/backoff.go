package scheduler

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/provider"
)

// maxBackoffFloor is the minimum cap for target exponential backoff. The
// effective cap is max(4×interval, 30m) per Architecture §10.1.
const maxBackoffFloor = 30 * time.Minute

// ScheduleDecision is the target scheduling result of one applicable outcome.
// Apply is false for abandoned/caller-cancelled outcomes, which must not update
// target scheduling. Discarded outcomes are handled before this policy is
// called and also never update target scheduling.
type ScheduleDecision struct {
	Apply               bool
	ConsecutiveFailures int
	NextDueAt           time.Time
}

// DecideTargetSchedule maps a PRV-010 classification to the approved target
// scheduling policy:
//
//   - applicable success/valid empty: reset the counter and use the normal
//     jittered interval;
//   - applicable infrastructure failure (TargetBackoff): increment the counter,
//     apply interval×2^failures capped at max(4×interval, 30m), add ±10%
//     jitter after the cap, then honor RetryAfter as a lower bound;
//   - config/route/ordinary 4xx: leave the counter unchanged and use the normal
//     interval;
//   - abandoned/caller cancellation: do not apply a target update.
//
// It is pure and performs no I/O or HTTP work. The outcome transaction owner
// (SCH-010) calls it after the post-I/O validity gate and persists the result
// through Store.ApplyTargetSchedule inside that transaction.
func DecideTargetSchedule(
	now time.Time,
	interval time.Duration,
	currentFailures int,
	classification provider.Classification,
	random Random,
) (ScheduleDecision, error) {
	return decideTargetSchedule(now, interval, 0, time.Time{}, currentFailures, classification, random)
}

// DecideTargetScheduleWithProvider applies the provider-level adaptive polling
// interval and cooldown as strict lower bounds on top of the target policy.
// effectiveInterval is max(configured, adaptive); cooldownUntil is the absolute
// Retry-After/adaptive hold. Using both here keeps the persisted target schedule
// and the scheduler guard aligned around one next-eligible calculation.
func DecideTargetScheduleWithProvider(
	now time.Time,
	configuredInterval time.Duration,
	effectiveInterval time.Duration,
	cooldownUntil time.Time,
	currentFailures int,
	classification provider.Classification,
	random Random,
) (ScheduleDecision, error) {
	return decideTargetSchedule(now, configuredInterval, effectiveInterval, cooldownUntil, currentFailures, classification, random)
}

func decideTargetSchedule(
	now time.Time,
	interval time.Duration,
	effectiveInterval time.Duration,
	cooldownUntil time.Time,
	currentFailures int,
	classification provider.Classification,
	random Random,
) (ScheduleDecision, error) {
	if now.IsZero() {
		return ScheduleDecision{}, errors.New("scheduler: schedule time must not be zero")
	}
	if err := domain.ValidatePollInterval(interval); err != nil {
		return ScheduleDecision{}, fmt.Errorf("scheduler: %w", err)
	}
	if effectiveInterval < 0 {
		return ScheduleDecision{}, errors.New("scheduler: effective polling interval must not be negative")
	}
	if effectiveInterval < interval {
		effectiveInterval = interval
	}
	if currentFailures < 0 {
		return ScheduleDecision{}, errors.New("scheduler: consecutive failures must not be negative")
	}
	if classification.Category == "" {
		return ScheduleDecision{}, errors.New("scheduler: classification category is required")
	}

	now = now.UTC()
	if classification.IsAbandoned() {
		return ScheduleDecision{Apply: false}, nil
	}
	retryAfter := NormalizeRetryAfter(classification.RetryAfter)

	nextFor := func(base time.Duration) time.Time {
		next := now.Add(base)
		if effectiveInterval > 0 {
			if effective := now.Add(effectiveInterval); effective.After(next) {
				next = effective
			}
		}
		if retryAfter > 0 {
			if retry := now.Add(retryAfter); retry.After(next) {
				next = retry
			}
		}
		if !cooldownUntil.IsZero() {
			cooldown := cooldownUntil.UTC()
			if cooldown.After(next) {
				next = cooldown
			}
		}
		return next
	}

	if classification.IsSuccess() {
		return ScheduleDecision{
			Apply:               true,
			ConsecutiveFailures: 0,
			NextDueAt:           nextFor(NextInterval(interval, random)),
		}, nil
	}
	if !classification.TargetBackoff {
		return ScheduleDecision{
			Apply:               true,
			ConsecutiveFailures: currentFailures,
			NextDueAt:           nextFor(NextInterval(interval, random)),
		}, nil
	}

	if currentFailures == math.MaxInt {
		return ScheduleDecision{}, errors.New("scheduler: consecutive failures overflow")
	}
	failures := currentFailures + 1
	delay := backoffDelay(interval, failures)
	delay += JitterOffset(delay, random)
	if retryAfter > delay {
		delay = retryAfter
	}
	return ScheduleDecision{
		Apply:               true,
		ConsecutiveFailures: failures,
		NextDueAt:           nextFor(delay),
	}, nil
}

// backoffDelay computes interval×2^failures capped at max(4×interval, 30m).
// The loop stops as soon as the cap is reached, so a very large stored failure
// count cannot cause a long loop or overflow.
func backoffDelay(interval time.Duration, failures int) time.Duration {
	cap := 4 * interval
	if cap < maxBackoffFloor {
		cap = maxBackoffFloor
	}
	delay := interval
	for attempt := 0; attempt < failures; attempt++ {
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
