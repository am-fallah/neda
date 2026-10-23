package scheduler

import (
	"math/rand/v2"
	"time"

	"github.com/am-fallah/neda/internal/domain"
)

const maxNearTermJitter = 30 * time.Second

type defaultRandom struct{}

func (defaultRandom) Int64N(n int64) int64 { return rand.Int64N(n) }

// JitterOffset returns a random offset of at most ±10% of interval, using the
// same formula for every scheduling decision. Callers apply it through
// NextInterval or NearTermDelay rather than inventing a second jitter formula.
func JitterOffset(interval time.Duration, random Random) time.Duration {
	if interval <= 0 {
		return 0
	}
	if random == nil {
		random = defaultRandom{}
	}
	half := interval / 10
	if half <= 0 {
		return 0
	}
	span := int64(2*half) + 1
	return time.Duration(random.Int64N(span) - int64(half))
}

// NextInterval applies ±10% jitter and clamps the effective interval to the
// product bounds of 60–3600 seconds.
func NextInterval(interval time.Duration, random Random) time.Duration {
	if interval <= 0 {
		return interval
	}
	next := interval + JitterOffset(interval, random)
	if next < domain.MinPollInterval {
		return domain.MinPollInterval
	}
	if next > domain.MaxPollInterval {
		return domain.MaxPollInterval
	}
	return next
}

// NearTermDelay returns the "small jitter" used for create/resume and restart
// staggering: a delay of 0–10% of the interval, capped at 30 seconds. It uses
// the same random source and 10% basis as the per-check jitter, but a
// non-negative delay because a near-term check must not be scheduled in the
// past. This is the single near-term convention used by SCH-001/SCH-002.
func NearTermDelay(interval time.Duration, random Random) time.Duration {
	if interval <= 0 {
		return 0
	}
	if random == nil {
		random = defaultRandom{}
	}
	bound := interval / 10
	if bound > maxNearTermJitter {
		bound = maxNearTermJitter
	}
	if bound <= 0 {
		return 0
	}
	return time.Duration(random.Int64N(int64(bound) + 1))
}
