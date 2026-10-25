// Package provider defines Neda's normalized provider boundary.
//
// Core packages consume these types and the Provider interface. Provider
// adapters own request/response parsing, availability rules, and stable keys;
// no adapter-specific JSON or business rule belongs here.
package provider

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ID is an explicitly registered provider identifier.
type ID string

// State is a normalized offering availability state.
type State string

const (
	StateReservable    State = "reservable"
	StateNotReservable State = "not_reservable"
	StateUnknown       State = "unknown"
)

// Valid reports whether s is one of the normalized states.
func (s State) Valid() bool {
	switch s {
	case StateReservable, StateNotReservable, StateUnknown:
		return true
	default:
		return false
	}
}

// ErrorKind is a stable provider error category.
type ErrorKind string

const (
	ErrTimeout         ErrorKind = "timeout"
	ErrNetwork         ErrorKind = "network"
	ErrHTTPStatus      ErrorKind = "http_status"
	ErrRateLimited     ErrorKind = "rate_limited"
	ErrInvalidResponse ErrorKind = "invalid_response"
	ErrRouteNotFound   ErrorKind = "route_not_found"
	ErrConfiguration   ErrorKind = "configuration"
	ErrCanceled        ErrorKind = "canceled"
)

// Valid reports whether k is a known provider error category.
func (k ErrorKind) Valid() bool {
	switch k {
	case ErrTimeout, ErrNetwork, ErrHTTPStatus, ErrRateLimited,
		ErrInvalidResponse, ErrRouteNotFound, ErrConfiguration, ErrCanceled:
		return true
	default:
		return false
	}
}

// Error is a safe, typed provider error. The underlying Err is available to
// errors.Is/As callers but is deliberately omitted from Error() so URLs,
// query strings, response bodies, and credentials cannot leak into logs.
type Error struct {
	Kind       ErrorKind
	Op         string // short, non-sensitive operation name
	StatusCode int
	RetryAfter time.Duration
	Err        error
}

// Error implements error with safe metadata only.
func (e *Error) Error() string {
	if e == nil {
		return "provider error"
	}
	parts := []string{"provider error"}
	if e.Op != "" {
		parts = append(parts, "op="+e.Op)
	}
	kind := e.Kind
	if !kind.Valid() {
		kind = "unknown"
	}
	parts = append(parts, "kind="+string(kind))
	if e.StatusCode != 0 {
		parts = append(parts, "status="+strconv.Itoa(e.StatusCode))
	}
	if e.RetryAfter > 0 {
		parts = append(parts, "retry_after="+e.RetryAfter.String())
	}
	return strings.Join(parts, " ")
}

// Unwrap exposes the underlying cause for errors.Is/As.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Capabilities describes provider-specific configuration and UI needs.
// Generic validation and presentation consult these flags; adapters remain
// responsible for their own final validation and fail-closed behavior.
type Capabilities struct {
	RequiresOriginID        bool
	RequiresDestinationID   bool
	SupportsURLSearch       bool
	SupportsPrice           bool
	SupportsBookingURL      bool
	SupportsGender          bool
	RequiresOptionSelection bool
	MaxPassengers           int // 0 means no local bound is advertised
}

// Metadata is code-owned provider metadata. TestOnly providers must never be
// presented as a real ticket source.
type Metadata struct {
	ID           ID
	DisplayName  string
	TestOnly     bool
	Capabilities Capabilities
}

// MonitorProviderConfig is the per-monitor provider configuration.
type MonitorProviderConfig struct {
	OriginID      string
	DestinationID string
	BookingSlug   string
	GenderCode    string
}

// Route carries display names and provider-native route identifiers.
type Route struct {
	OriginName      string
	DestinationName string
	OriginID        string
	DestinationID   string
}

// Passengers is the requested passenger mix.
type Passengers struct {
	Adults   int
	Children int
	Infants  int
}

// CheckRequest is the normalized provider input. It intentionally omits
// internal target IDs and config versions; the scheduler retains those for its
// post-I/O validity gate.
type CheckRequest struct {
	MonitorID     string
	DepartureDate string // Gregorian YYYY-MM-DD
	Route         Route
	Passengers    Passengers
	Config        MonitorProviderConfig
}

// Price is a provider-reported amount. Amount is never scaled or converted.
// Currency is empty until the provider's unit semantics are verified; callers
// must not infer a unit from a bare amount.
type Price struct {
	Amount   int64
	Currency string
}

// Journey is a normalized journey identity and snapshot.
type Journey struct {
	Key             string
	NativeID        string
	TrainNumber     string
	Operator        string
	OriginName      string
	DestinationName string
	DepartureAt     *time.Time
	ArrivalAt       *time.Time
}

// Offering is a normalized offering identity and snapshot.
type Offering struct {
	Key             string
	NativeID        string
	WagonID         string
	WagonName       string
	ClassName       string
	CompartmentType string
	Price           *Price
}

// SelectedOption is the durable identity and metadata of one selected
// journey/class pair. Keys are provider-normalized; the remaining fields are
// matching guardrails and display metadata.
type SelectedOption struct {
	JourneyKey       string
	OfferingKey      string
	JourneyNativeID  string
	OfferingNativeID string
	TrainNumber      string
	Operator         string
	OriginName       string
	DestinationName  string
	DepartureAt      *time.Time
	ArrivalAt        *time.Time
	WagonID          string
	WagonName        string
	ClassName        string
	CompartmentType  string
}

// SearchRequest asks a URL-capable provider to resolve and fetch an initial
// train search. It never mutates monitor state.
type SearchRequest struct {
	URL        string
	Passengers Passengers
}

// SearchResult is the normalized initial search response.
type SearchResult struct {
	URL           string
	Slug          string
	JalaliDate    string
	DepartureDate string
	Route         Route
	Passengers    Passengers
	Result        Result
}

// URLSearcher is implemented by providers that can resolve a public search
// URL and fetch an initial result without creating a monitor.
type URLSearcher interface {
	SearchURL(ctx context.Context, req SearchRequest) (SearchResult, error)
}

// InputError is a user-correctable search/configuration error. Code is a
// stable machine-readable value; Message is safe for display.
type InputError struct {
	Field   string
	Code    string
	Message string
	Cause   error
}

func (e *InputError) Error() string {
	if e == nil {
		return "provider input error"
	}
	if e.Field != "" {
		return e.Field + ": " + e.Message
	}
	return e.Message
}

func (e *InputError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// OfferingObservation is one normalized offering state.
type OfferingObservation struct {
	Offering   Offering
	State      State
	Reason     string // stable reason code, not user-facing prose
	Capacity   *int
	BookingURL *url.URL // validated only
}

// JourneyObservation groups offerings under one journey.
type JourneyObservation struct {
	Journey   Journey
	Offerings []OfferingObservation
}

// Result is a successful provider result. Warnings are bounded, sanitized
// categories, never raw payloads.
type Result struct {
	Journeys   []JourneyObservation
	Warnings   []string
	ObservedAt time.Time
}

// Provider is implemented by every explicit provider adapter.
type Provider interface {
	Metadata() Metadata
	ValidateConfig(cfg MonitorProviderConfig) error
	Check(ctx context.Context, req CheckRequest) (Result, error)
}
