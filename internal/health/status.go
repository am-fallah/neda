package health

import "time"

// StaleFloor is the minimum provider-staleness threshold.
const StaleFloor = 15 * time.Minute

// StaleThreshold returns the derived stale threshold: max(3×poll interval,
// 15 minutes). The threshold is for API presentation; it is not persisted.
func StaleThreshold(pollInterval time.Duration) time.Duration {
	if pollInterval <= 0 {
		return StaleFloor
	}
	threshold := 3 * pollInterval
	if threshold < StaleFloor {
		return StaleFloor
	}
	return threshold
}

// IsStale reports whether a provider should be presented as stale. An open
// degraded episode is always stale. Without an open episode, a provider is
// stale when its last success is older than StaleThreshold. A provider with no
// recorded success is not classified as stale here; the API can present it as
// unknown.
func IsStale(lastSuccess *time.Time, openEpisode bool, now time.Time, pollInterval time.Duration) bool {
	if openEpisode {
		return true
	}
	if lastSuccess == nil || now.IsZero() {
		return false
	}
	return now.Sub(*lastSuccess) > StaleThreshold(pollInterval)
}
