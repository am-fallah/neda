package fake_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/provider/fake"
)

var fakeNow = time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)

func newProvider() *fake.Provider {
	return fake.NewWithClock(func() time.Time { return fakeNow })
}

func testRequest(origin string) provider.CheckRequest {
	return provider.CheckRequest{
		MonitorID:     "monitor-1",
		DepartureDate: "2026-11-01",
		Route: provider.Route{
			OriginName:      "Tehran",
			DestinationName: "Mashhad",
			OriginID:        origin,
			DestinationID:   fake.DestinationDefault,
		},
		Passengers: provider.Passengers{Adults: 1, Children: 1, Infants: 0},
		Config: provider.MonitorProviderConfig{
			OriginID:      origin,
			DestinationID: fake.DestinationDefault,
		},
	}
}

func TestMetadataIsTestOnly(t *testing.T) {
	metadata := newProvider().Metadata()
	if metadata.ID != fake.ID {
		t.Fatalf("ID = %q", metadata.ID)
	}
	if !metadata.TestOnly {
		t.Fatal("TestOnly = false")
	}
	if !strings.Contains(metadata.DisplayName, "Test provider") {
		t.Fatalf("DisplayName = %q", metadata.DisplayName)
	}
	if !metadata.Capabilities.RequiresOriginID || !metadata.Capabilities.RequiresDestinationID {
		t.Fatal("fake provider should require origin/destination ids")
	}
	if metadata.Capabilities.SupportsBookingURL || metadata.Capabilities.SupportsGender {
		t.Fatal("fake provider advertises unsupported capabilities")
	}
	if metadata.Capabilities.MaxPassengers != 9 {
		t.Fatalf("MaxPassengers = %d", metadata.Capabilities.MaxPassengers)
	}
}

func TestValidateConfig(t *testing.T) {
	p := newProvider()
	valid := provider.MonitorProviderConfig{
		OriginID:      fake.OriginAvailable,
		DestinationID: fake.DestinationDefault,
	}
	if err := p.ValidateConfig(valid); err != nil {
		t.Fatalf("valid config: %v", err)
	}

	tests := []struct {
		name string
		cfg  provider.MonitorProviderConfig
	}{
		{"empty origin", provider.MonitorProviderConfig{DestinationID: fake.DestinationDefault}},
		{"wrong origin prefix", provider.MonitorProviderConfig{OriginID: "1", DestinationID: fake.DestinationDefault}},
		{"wrong destination prefix", provider.MonitorProviderConfig{OriginID: fake.OriginAvailable, DestinationID: "2"}},
		{"same ids", provider.MonitorProviderConfig{OriginID: fake.OriginAvailable, DestinationID: fake.OriginAvailable}},
		{"booking slug", provider.MonitorProviderConfig{OriginID: fake.OriginAvailable, DestinationID: fake.DestinationDefault, BookingSlug: "slug"}},
		{"gender code", provider.MonitorProviderConfig{OriginID: fake.OriginAvailable, DestinationID: fake.DestinationDefault, GenderCode: "M"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := p.ValidateConfig(test.cfg)
			if err == nil {
				t.Fatal("ValidateConfig succeeded, want error")
			}
			var providerErr *provider.Error
			if !errors.As(err, &providerErr) || providerErr.Kind != provider.ErrConfiguration {
				t.Fatalf("error = %v, want configuration provider error", err)
			}
		})
	}
}

func TestCheckAvailableIsDeterministic(t *testing.T) {
	p := newProvider()
	req := testRequest(fake.OriginAvailable)
	first, err := p.Check(context.Background(), req)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	second, err := p.Check(context.Background(), req)
	if err != nil {
		t.Fatalf("Check second: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("fake provider result is not deterministic")
	}
	if len(first.Journeys) != 1 || len(first.Journeys[0].Offerings) != 1 {
		t.Fatalf("result shape = %+v", first)
	}
	offering := first.Journeys[0].Offerings[0]
	if offering.State != provider.StateReservable {
		t.Fatalf("State = %q", offering.State)
	}
	if offering.Reason != "fake_available" {
		t.Fatalf("Reason = %q", offering.Reason)
	}
	if offering.Capacity == nil || *offering.Capacity != 3 {
		t.Fatalf("Capacity = %v, want 3", offering.Capacity)
	}
	if offering.BookingURL != nil {
		t.Fatal("fake provider returned a booking URL")
	}
	if !first.ObservedAt.Equal(fakeNow) {
		t.Fatalf("ObservedAt = %v, want %v", first.ObservedAt, fakeNow)
	}
	if first.Journeys[0].Journey.Key != "fake:test-origin-available:test-destination-default:2026-11-01" {
		t.Fatalf("journey key = %q", first.Journeys[0].Journey.Key)
	}
	if first.Journeys[0].Offerings[0].Offering.Key != "fake:offering:1" {
		t.Fatalf("offering key = %q", first.Journeys[0].Offerings[0].Offering.Key)
	}
}

func TestCheckAvailabilityScenarios(t *testing.T) {
	p := newProvider()
	tests := []struct {
		name       string
		origin     string
		wantState  provider.State
		wantReason string
		wantCap    *int
		wantEmpty  bool
	}{
		{"sold out", fake.OriginSoldOut, provider.StateNotReservable, "fake_sold_out", intPointer(0), false},
		{"unknown", fake.OriginUnknown, provider.StateUnknown, "fake_ambiguous", nil, false},
		{"empty", fake.OriginEmpty, "", "", nil, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := p.Check(context.Background(), testRequest(test.origin))
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if test.wantEmpty {
				if len(result.Journeys) != 0 {
					t.Fatalf("empty result journeys = %d", len(result.Journeys))
				}
				return
			}
			if len(result.Journeys) != 1 || len(result.Journeys[0].Offerings) != 1 {
				t.Fatalf("result shape = %+v", result)
			}
			offering := result.Journeys[0].Offerings[0]
			if offering.State != test.wantState || offering.Reason != test.wantReason {
				t.Fatalf("offering = %+v, want state %q reason %q", offering, test.wantState, test.wantReason)
			}
			if test.wantCap == nil {
				if offering.Capacity != nil {
					t.Fatalf("Capacity = %v, want nil", offering.Capacity)
				}
			} else if offering.Capacity == nil || *offering.Capacity != *test.wantCap {
				t.Fatalf("Capacity = %v, want %d", offering.Capacity, *test.wantCap)
			}
		})
	}
}

func TestCheckErrorScenariosClassify(t *testing.T) {
	p := newProvider()
	tests := []struct {
		name         string
		origin       string
		wantKind     provider.ErrorKind
		wantStatus   int
		wantRetry    time.Duration
		wantCategory provider.Category
	}{
		{"timeout", fake.OriginTimeout, provider.ErrTimeout, 0, 0, provider.CategoryInfrastructure},
		{"network", fake.OriginNetwork, provider.ErrNetwork, 0, 0, provider.CategoryInfrastructure},
		{"rate limited", fake.OriginRateLimited, provider.ErrRateLimited, 429, 2 * time.Second, provider.CategoryInfrastructure},
		{"http 500", fake.OriginHTTP500, provider.ErrHTTPStatus, 500, 0, provider.CategoryInfrastructure},
		{"http 404", fake.OriginHTTP404, provider.ErrHTTPStatus, 404, 0, provider.CategoryRequest},
		{"invalid response", fake.OriginInvalidResponse, provider.ErrInvalidResponse, 0, 0, provider.CategoryInfrastructure},
		{"route not found", fake.OriginRouteNotFound, provider.ErrRouteNotFound, 0, 0, provider.CategoryRoute},
		{"provider canceled", fake.OriginProviderCanceled, provider.ErrCanceled, 0, 0, provider.CategoryInfrastructure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := p.Check(context.Background(), testRequest(test.origin))
			if err == nil {
				t.Fatal("Check succeeded, want error")
			}
			if len(result.Journeys) != 0 {
				t.Fatal("error result contains journeys")
			}
			var providerErr *provider.Error
			if !errors.As(err, &providerErr) {
				t.Fatalf("error = %v, want provider.Error", err)
			}
			if providerErr.Kind != test.wantKind || providerErr.StatusCode != test.wantStatus || providerErr.RetryAfter != test.wantRetry {
				t.Fatalf("provider error = %+v", providerErr)
			}
			classification := provider.Classify(context.Background(), err)
			if classification.Category != test.wantCategory {
				t.Fatalf("classification = %+v, want category %q", classification, test.wantCategory)
			}
			if test.wantCategory == provider.CategoryInfrastructure && (!classification.CountsTowardOutage || !classification.TargetBackoff) {
				t.Fatalf("infrastructure classification = %+v", classification)
			}
			if test.wantCategory != provider.CategoryInfrastructure && (classification.CountsTowardOutage || classification.TargetBackoff) {
				t.Fatalf("non-infrastructure classification = %+v", classification)
			}
		})
	}
}

func TestCheckRespectsCallerCancellationAndDeadline(t *testing.T) {
	p := newProvider()

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Check(canceledCtx, testRequest(fake.OriginAvailable))
	if err == nil {
		t.Fatal("Check with canceled context succeeded")
	}
	var providerErr *provider.Error
	if !errors.As(err, &providerErr) || providerErr.Kind != provider.ErrCanceled {
		t.Fatalf("canceled error = %v", err)
	}
	if classification := provider.Classify(canceledCtx, err); !classification.IsAbandoned() {
		t.Fatalf("canceled classification = %+v", classification)
	}

	deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer deadlineCancel()
	_, err = p.Check(deadlineCtx, testRequest(fake.OriginAvailable))
	if err == nil {
		t.Fatal("Check with expired deadline succeeded")
	}
	if !errors.As(err, &providerErr) || providerErr.Kind != provider.ErrTimeout {
		t.Fatalf("deadline error = %v", err)
	}
	if classification := provider.Classify(deadlineCtx, err); classification.Category != provider.CategoryInfrastructure || classification.Kind != provider.ErrTimeout {
		t.Fatalf("deadline classification = %+v", classification)
	}
}

func TestCheckRejectsInvalidRequests(t *testing.T) {
	p := newProvider()
	base := testRequest(fake.OriginAvailable)
	tests := []struct {
		name   string
		mutate func(*provider.CheckRequest)
	}{
		{"empty monitor", func(r *provider.CheckRequest) { r.MonitorID = "" }},
		{"bad date", func(r *provider.CheckRequest) { r.DepartureDate = "01-11-2026" }},
		{"zero adults", func(r *provider.CheckRequest) { r.Passengers.Adults = 0 }},
		{"negative child", func(r *provider.CheckRequest) { r.Passengers.Children = -1 }},
		{"too many passengers", func(r *provider.CheckRequest) {
			r.Passengers = provider.Passengers{Adults: 8, Children: 1, Infants: 1}
		}},
		{"route origin mismatch", func(r *provider.CheckRequest) { r.Route.OriginID = fake.OriginSoldOut }},
		{"route destination mismatch", func(r *provider.CheckRequest) { r.Route.DestinationID = "test-destination-other" }},
		{"booking slug", func(r *provider.CheckRequest) { r.Config.BookingSlug = "slug" }},
		{"gender code", func(r *provider.CheckRequest) { r.Config.GenderCode = "M" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := base
			test.mutate(&req)
			_, err := p.Check(context.Background(), req)
			if err == nil {
				t.Fatal("Check succeeded, want error")
			}
			var providerErr *provider.Error
			if !errors.As(err, &providerErr) || providerErr.Kind != provider.ErrConfiguration {
				t.Fatalf("error = %v, want configuration error", err)
			}
		})
	}
}

func intPointer(value int) *int { return &value }
