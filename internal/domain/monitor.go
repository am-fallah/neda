// Package domain contains Neda's monitor model, validation, lifecycle rules,
// and the CRUD service. Provider-specific request/response rules remain in the
// provider adapters.
package domain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

var (
	// ErrMonitorNotFound reports a missing monitor.
	ErrMonitorNotFound = errors.New("domain: monitor not found")
	// ErrMonitorExpired reports a monitor that cannot be resumed until its
	// departure date is moved into the future.
	ErrMonitorExpired = errors.New("domain: monitor expired")
	// ErrMonitorInvalid reports input validation failure.
	ErrMonitorInvalid = errors.New("domain: invalid monitor")
)

// ValidationError identifies one monitor input field.
type ValidationError struct {
	Field   string
	Message string
	Cause   error
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ErrMonitorInvalid.Error()
	}
	if e.Field == "" {
		return fmt.Sprintf("%s: %s", ErrMonitorInvalid, e.Message)
	}
	return fmt.Sprintf("%s: %s: %s", ErrMonitorInvalid, e.Field, e.Message)
}

// Unwrap makes errors.Is(err, ErrMonitorInvalid) work for validation failures.
func (e *ValidationError) Unwrap() error {
	return ErrMonitorInvalid
}

// MonitorStatus is the derived dashboard status.
type MonitorStatus string

const (
	StatusActive  MonitorStatus = "active"
	StatusPaused  MonitorStatus = "paused"
	StatusExpired MonitorStatus = "expired"
)

// Monitor is a durable monitor with its provider target rows.
type Monitor struct {
	ID              string
	Label           string
	OriginName      string
	DestinationName string
	DepartureDate   string
	Adults          int
	Children        int
	Infants         int
	Enabled         bool
	ExpiresAt       time.Time
	ExpiredAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Providers       []MonitorProvider
}

// MonitorProvider is one provider-specific target row. Inactive rows are
// retained for history.
type MonitorProvider struct {
	ID                  int64
	MonitorID           string
	ProviderID          provider.ID
	Active              bool
	OriginID            string
	DestinationID       string
	BookingSlug         string
	GenderCode          string
	ConfigVersion       int64
	NextDueAt           *time.Time
	LastAttemptAt       *time.Time
	LastSuccessAt       *time.Time
	LastOutcome         string
	LastErrorKind       string
	LastErrorAt         *time.Time
	ConsecutiveFailures int
	Selections          []MonitorProviderSelection
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// MonitorProviderSelection is one persisted train/class option for a provider
// target and configuration version.
type MonitorProviderSelection struct {
	ID                int64
	MonitorProviderID int64
	ConfigVersion     int64
	Option            provider.SelectedOption
	CreatedAt         time.Time
}

// ProviderInput is one selected provider and its route configuration.
type ProviderInput struct {
	ProviderID    provider.ID
	OriginID      string
	DestinationID string
	BookingSlug   string
	GenderCode    string
	Selections    []provider.SelectedOption
}

// MonitorInput is the create/edit payload after normalization.
type MonitorInput struct {
	Label           string
	OriginName      string
	DestinationName string
	DepartureDate   string
	Adults          int
	Children        int
	Infants         int
	Providers       []ProviderInput
}

// MonitorUpdateResult reports whether an edit was material and whether the
// scheduler should be woken after commit.
type MonitorUpdateResult struct {
	Monitor  Monitor
	Material bool
}

// MonitorRepository is the persistence boundary used by MonitorService.
type MonitorRepository interface {
	CreateMonitor(ctx context.Context, monitor Monitor) (Monitor, error)
	GetMonitor(ctx context.Context, id string) (Monitor, error)
	ListMonitors(ctx context.Context) ([]Monitor, error)
	UpdateMonitor(ctx context.Context, id string, input MonitorInput, now, scheduleAt time.Time, loc *time.Location) (MonitorUpdateResult, error)
	PauseMonitor(ctx context.Context, id string, now time.Time) (Monitor, error)
	ResumeMonitor(ctx context.Context, id string, now, scheduleAt time.Time) (Monitor, error)
	DeleteMonitor(ctx context.Context, id string) error
	ListInventoryObservations(ctx context.Context, targetID, configVersion int64, limit int) (InventoryObservationPage, error)
	ListRecentCheckRuns(ctx context.Context, targetID int64, limit int) (CheckRunDiagnosticPage, error)
}

// ProviderResolver looks up explicitly registered provider adapters.
type ProviderResolver interface {
	Lookup(id provider.ID) (provider.Provider, bool)
}

// IsExpired reports whether the monitor is expired at now.
func (m Monitor) IsExpired(now time.Time) bool {
	return m.ExpiredAt != nil || !m.ExpiresAt.After(now)
}

// Status derives active/paused/expired from persisted fields.
func (m Monitor) Status(now time.Time) MonitorStatus {
	if m.IsExpired(now) {
		return StatusExpired
	}
	if !m.Enabled {
		return StatusPaused
	}
	return StatusActive
}

// ActiveProviders returns active target rows in stable order.
func (m Monitor) ActiveProviders() []MonitorProvider {
	providers := make([]MonitorProvider, 0, len(m.Providers))
	for _, target := range m.Providers {
		if target.Active {
			providers = append(providers, target)
		}
	}
	return providers
}
