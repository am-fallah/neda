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
