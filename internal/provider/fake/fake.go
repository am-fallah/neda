// Package fake provides a deterministic, test-only provider implementation.
//
// It is never registered by default. Callers must explicitly construct it, and
// its metadata marks it TestOnly so the API/UI can label it as a test source.
package fake

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

const (
	// ID is the explicit fake-provider identifier.
	ID provider.ID = "fake"
	// DisplayName is deliberately not a real ticket source.
	DisplayName = "Test provider — not a real ticket source"

	// OriginAvailable is the default deterministic available scenario.
	OriginAvailable = "test-origin-available"
	// OriginEmpty returns a successful empty result.
	OriginEmpty = "test-origin-empty"
	// OriginSoldOut returns a NotReservable observation.
	OriginSoldOut = "test-origin-soldout"
	// OriginUnknown returns an Unknown observation.
	OriginUnknown = "test-origin-unknown"
	// OriginTimeout returns a timeout error.
	OriginTimeout = "test-origin-timeout"
	// OriginNetwork returns a network error.
	OriginNetwork = "test-origin-network"
	// OriginRateLimited returns a 429/rate-limit error.
	OriginRateLimited = "test-origin-rate-limited"
	// OriginHTTP500 returns an HTTP 500 error.
	OriginHTTP500 = "test-origin-http-500"
	// OriginHTTP404 returns an ordinary HTTP 404 error.
	OriginHTTP404 = "test-origin-http-404"
	// OriginInvalidResponse returns an invalid-response error.
	OriginInvalidResponse = "test-origin-invalid-response"
	// OriginRouteNotFound returns a route-not-found error.
	OriginRouteNotFound = "test-origin-route-not-found"
	// OriginProviderCanceled simulates provider-side cancellation without a
	// canceled caller context.
	OriginProviderCanceled = "test-origin-provider-canceled"
	// DestinationDefault is the standard fake destination ID.
	DestinationDefault = "test-destination-default"

	fakeOriginPrefix      = "test-origin-"
	fakeDestinationPrefix = "test-destination-"
	maxPassengers         = 9
	maxRouteIDLength      = 64
)

// Provider is the deterministic fake provider.
type Provider struct {
	now func() time.Time
}

var _ provider.Provider = (*Provider)(nil)

// New constructs a fake provider using time.Now.
func New() *Provider {
	return NewWithClock(time.Now)
}

// NewWithClock constructs a fake provider with an injected clock for
// deterministic Result.ObservedAt values.
func NewWithClock(now func() time.Time) *Provider {
	if now == nil {
		now = time.Now
	}
	return &Provider{now: now}
}

func (p *Provider) nowTime() time.Time {
	if p == nil || p.now == nil {
		return time.Now()
	}
	return p.now()
}

// Metadata returns the test-only provider metadata.
func (p *Provider) Metadata() provider.Metadata {
	return provider.Metadata{
		ID:          ID,
		DisplayName: DisplayName,
		TestOnly:    true,
		Capabilities: provider.Capabilities{
			RequiresOriginID:      true,
			RequiresDestinationID: true,
			SupportsBookingURL:    false,
			SupportsGender:        false,
			MaxPassengers:         maxPassengers,
		},
	}
}

// ValidateConfig enforces fake-only route IDs and rejects unsupported fields.
func (p *Provider) ValidateConfig(cfg provider.MonitorProviderConfig) error {
	if !validRouteID(cfg.OriginID, fakeOriginPrefix) {
		return configurationError("origin id must use the test-origin- prefix")
	}
	if !validRouteID(cfg.DestinationID, fakeDestinationPrefix) {
		return configurationError("destination id must use the test-destination- prefix")
	}
	if cfg.OriginID == cfg.DestinationID {
		return configurationError("origin and destination ids must differ")
	}
	if strings.TrimSpace(cfg.BookingSlug) != "" {
		return configurationError("fake provider does not support booking slugs")
	}
	if strings.TrimSpace(cfg.GenderCode) != "" {
		return configurationError("fake provider does not support gender codes")
	}
	return nil
}

// Check returns deterministic results based on fake route IDs. It performs no
// network access.
func (p *Provider) Check(ctx context.Context, req provider.CheckRequest) (provider.Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return provider.Result{}, &provider.Error{Kind: provider.ErrCanceled, Op: "fake.Check", Err: err}
		}
		return provider.Result{}, &provider.Error{Kind: provider.ErrTimeout, Op: "fake.Check", Err: err}
	}
	if err := p.ValidateConfig(req.Config); err != nil {
		return provider.Result{}, err
	}
	if err := validateRequest(req); err != nil {
		return provider.Result{}, err
	}

	observedAt := p.nowTime().UTC()
	switch req.Config.OriginID {
	case OriginEmpty:
		return provider.Result{Journeys: []provider.JourneyObservation{}, ObservedAt: observedAt}, nil
	case OriginSoldOut:
		return resultFor(req, provider.StateNotReservable, "fake_sold_out", intPointer(0), observedAt), nil
	case OriginUnknown:
		return resultFor(req, provider.StateUnknown, "fake_ambiguous", nil, observedAt), nil
	case OriginTimeout:
		return provider.Result{}, &provider.Error{Kind: provider.ErrTimeout, Op: "fake.Check"}
	case OriginNetwork:
		return provider.Result{}, &provider.Error{Kind: provider.ErrNetwork, Op: "fake.Check"}
	case OriginRateLimited:
		return provider.Result{}, &provider.Error{
			Kind:       provider.ErrRateLimited,
			Op:         "fake.Check",
			StatusCode: 429,
			RetryAfter: 2 * time.Second,
		}
	case OriginHTTP500:
		return provider.Result{}, &provider.Error{Kind: provider.ErrHTTPStatus, Op: "fake.Check", StatusCode: 500}
	case OriginHTTP404:
		return provider.Result{}, &provider.Error{Kind: provider.ErrHTTPStatus, Op: "fake.Check", StatusCode: 404}
	case OriginInvalidResponse:
		return provider.Result{}, &provider.Error{Kind: provider.ErrInvalidResponse, Op: "fake.Check"}
	case OriginRouteNotFound:
		return provider.Result{}, &provider.Error{Kind: provider.ErrRouteNotFound, Op: "fake.Check"}
	case OriginProviderCanceled:
		return provider.Result{}, &provider.Error{
			Kind: provider.ErrCanceled,
			Op:   "fake.Check",
			Err:  context.Canceled,
		}
	default:
		required := req.Passengers.Adults + req.Passengers.Children + req.Passengers.Infants
		return resultFor(req, provider.StateReservable, "fake_available", intPointer(required+1), observedAt), nil
	}
}

func validateRequest(req provider.CheckRequest) error {
	if strings.TrimSpace(req.MonitorID) == "" {
		return configurationError("monitor id is required")
	}
	parsed, err := time.Parse("2006-01-02", req.DepartureDate)
	if err != nil || parsed.Format("2006-01-02") != req.DepartureDate {
		return configurationError("departure date must be a Gregorian YYYY-MM-DD value")
	}
	if req.Passengers.Adults < 1 {
		return configurationError("at least one adult is required")
	}
	if req.Passengers.Children < 0 || req.Passengers.Infants < 0 {
		return configurationError("child and infant counts must not be negative")
	}
	total := req.Passengers.Adults + req.Passengers.Children + req.Passengers.Infants
	if total > maxPassengers {
		return configurationError("fake provider supports at most nine passengers")
	}
	if req.Route.OriginID != "" && req.Route.OriginID != req.Config.OriginID {
		return configurationError("route and config origin ids differ")
	}
	if req.Route.DestinationID != "" && req.Route.DestinationID != req.Config.DestinationID {
		return configurationError("route and config destination ids differ")
	}
	return nil
}

func resultFor(req provider.CheckRequest, state provider.State, reason string, capacity *int, observedAt time.Time) provider.Result {
	originName := req.Route.OriginName
	if strings.TrimSpace(originName) == "" {
		originName = req.Config.OriginID
	}
	destinationName := req.Route.DestinationName
	if strings.TrimSpace(destinationName) == "" {
		destinationName = req.Config.DestinationID
	}
	journey := provider.Journey{
		Key:             fmt.Sprintf("fake:%s:%s:%s", req.Config.OriginID, req.Config.DestinationID, req.DepartureDate),
		NativeID:        "fake-journey-1",
		TrainNumber:     "TEST-001",
		Operator:        "Neda Test Provider",
		OriginName:      originName,
		DestinationName: destinationName,
	}
	offering := provider.Offering{
		Key:       "fake:offering:1",
		NativeID:  "fake-offering-1",
		WagonName: "Test Wagon",
		ClassName: "Test Class",
	}
	return provider.Result{
		Journeys: []provider.JourneyObservation{{
			Journey: journey,
			Offerings: []provider.OfferingObservation{{
				Offering: offering,
				State:    state,
				Reason:   reason,
				Capacity: capacity,
			}},
		}},
		ObservedAt: observedAt,
	}
}

func validRouteID(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) <= len(prefix) || len(value) > maxRouteIDLength {
		return false
	}
	if strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func configurationError(message string) error {
	return &provider.Error{
		Kind: provider.ErrConfiguration,
		Op:   "fake.ValidateConfig",
		Err:  errors.New(message),
	}
}

func intPointer(value int) *int {
	return &value
}
