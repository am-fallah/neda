package domain

import (
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

// Read-model bounds for monitor detail. The service applies these fixed limits
// so one monitor cannot cause an unbounded observation or check history query
// or response.
const (
	DefaultObservationLimit = 100
	MaxObservationLimit     = 100
	DefaultRecentCheckLimit = 10
	MaxRecentCheckLimit     = 10
)

// CheckRunOutcome is the persisted check_runs.outcome value. Running is an
// in-progress attempt; every other value is a final outcome.
type CheckRunOutcome string

const (
	CheckRunRunning   CheckRunOutcome = "running"
	CheckRunSuccess   CheckRunOutcome = "success"
	CheckRunEmpty     CheckRunOutcome = "empty"
	CheckRunError     CheckRunOutcome = "error"
	CheckRunDiscarded CheckRunOutcome = "discarded"
	CheckRunAbandoned CheckRunOutcome = "abandoned"
)

// Valid reports whether the outcome is part of the persisted schema.
func (o CheckRunOutcome) Valid() bool {
	switch o {
	case CheckRunRunning, CheckRunSuccess, CheckRunEmpty, CheckRunError,
		CheckRunDiscarded, CheckRunAbandoned:
		return true
	default:
		return false
	}
}

// InventoryObservation is the last persisted availability snapshot for one
// journey/offering at the target's current config version.
//
// State is the last confirmed state written by the availability applier. Missing
// means the latest successful check did not return this offering; in that case
// State is only the last known value and current availability is unknown. A
// provider error, timeout, malformed response, or discarded check never changes
// State, episode, or event state.
type InventoryObservation struct {
	JourneyKey  string
	OfferingKey string

	State      provider.State
	StateSince time.Time

	LastObservedAt time.Time
	LastDecisiveAt *time.Time
	LastMissingAt  *time.Time
	Missing        bool

	LastReason   string
	LastCapacity *int

	TrainNumber     string
	Operator        string
	OriginName      string
	DestinationName string
	DepartureAt     *time.Time
	ArrivalAt       *time.Time
	WagonName       string
	ClassName       string
	PriceAmount     *int64
	PriceCurrency   string
	BookingURL      string

	UpdatedAt time.Time
}

// InventoryObservationPage is a bounded observation list. Observations is never
// nil; Truncated reports that more rows exist beyond the applied limit.
type InventoryObservationPage struct {
	Observations []InventoryObservation
	Truncated    bool
}

// CheckRunDiagnostic is one bounded check_runs record. It contains sanitized
// diagnostics only: no provider response body, request URL, or credential.
type CheckRunDiagnostic struct {
	ID            int64
	ConfigVersion int64

	StartedAt  time.Time
	FinishedAt *time.Time
	Outcome    CheckRunOutcome
	ErrorKind  string
	HTTPStatus *int
	DurationMS *int64

	JourneyCount       int
	OfferingCount      int
	ReservableCount    int
	NotReservableCount int
	UnknownCount       int
	WarningCount       int

	CreatedAt time.Time
}

// CheckRunDiagnosticPage is a bounded recent-check list. Checks is never nil;
// Truncated reports that older rows exist beyond the applied limit.
type CheckRunDiagnosticPage struct {
	Checks    []CheckRunDiagnostic
	Truncated bool
}

// MonitorTargetDetail groups diagnostics for one monitor_providers row.
// Observations and RecentChecks are populated only for active targets. Inactive
// historical targets are retained in the monitor model but their stale
// diagnostics are not presented as current.
type MonitorTargetDetail struct {
	MonitorProviderID int64
	ProviderID        provider.ID
	Active            bool

	Observations          []InventoryObservation
	ObservationsTruncated bool
	RecentChecks          []CheckRunDiagnostic
	RecentChecksTruncated bool
}

// MonitorDetail is one monitor plus its provider-target diagnostics in stable
// monitor.Providers order.
type MonitorDetail struct {
	Monitor Monitor
	Targets []MonitorTargetDetail
}
