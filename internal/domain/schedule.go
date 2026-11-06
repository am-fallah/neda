package domain

import (
	"fmt"
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

// Global polling interval bounds from docs/PRODUCT_SPEC.md §3.2. The value is
// stored in SQLite settings so the dashboard can change it without a restart.
const (
	DefaultPollInterval = 300 * time.Second
	MinPollInterval     = 60 * time.Second
	MaxPollInterval     = 3600 * time.Second
)

// ScheduleTarget is the minimal projection the scheduler needs for due
// selection and restart staggering. Provider-specific route fields are loaded
// later by the provider-check stage; they are not duplicated here.
type ScheduleTarget struct {
	ID            int64
	MonitorID     string
	ProviderID    provider.ID
	ConfigVersion int64
	NextDueAt     *time.Time
}

// ValidatePollInterval enforces the product's inclusive 60–3600 second bounds.
func ValidatePollInterval(interval time.Duration) error {
	if interval < MinPollInterval || interval > MaxPollInterval {
		return fmt.Errorf("poll interval must be between %s and %s", MinPollInterval, MaxPollInterval)
	}
	return nil
}

// ProviderRateLimit is the durable adaptive throttling state for one provider
// rate-limit scope. The configured polling interval is never stored here; the
// effective interval is always max(configured, AdaptiveInterval). A missing row
// is equivalent to the zero value.
type ProviderRateLimit struct {
	ProviderID           provider.ID
	Scope                string
	Consecutive429s      int
	AdaptiveInterval     time.Duration
	CooldownUntil        *time.Time
	ConsecutiveSuccesses int
	Last429At            *time.Time
	LastSuccessAt        *time.Time
	LastRetryAfter       time.Duration
	UpdatedAt            time.Time
}

// ProviderRateLimitScopeProvider is the only scope currently exposed by the
// provider boundary. Adapters do not report endpoint, account, or IP scopes,
// so Neda applies the safest identifiable scope: the whole provider.
const ProviderRateLimitScopeProvider = "provider"
