package mrbilit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

func readObservedFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "mrbilit", "observed", name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read observed fixture %s: %v", name, err)
	}
	return body
}

func observedRequest(t *testing.T, adults int) provider.CheckRequest {
	t.Helper()
	req := testRequest()
	req.DepartureDate = "2026-11-05"
	req.Route.OriginName = "Yazd"
	req.Route.DestinationName = "Mashhad"
	req.Route.OriginID = "219"
	req.Route.DestinationID = "191"
	req.Passengers = provider.Passengers{Adults: adults}
	req.Config.OriginID = "219"
	req.Config.DestinationID = "191"
	req.Config.BookingSlug = "yazd-mashhad"
	return req
}

func TestParseObservedSanitizedEnvelope(t *testing.T) {
	result, err := parseResponse(readObservedFixture(t, "sanitized-live-2026-11-05.json"), observedRequest(t, 1), testNow)
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if len(result.Journeys) != 1 {
		t.Fatalf("journeys = %d", len(result.Journeys))
	}
	journey := result.Journeys[0]
	if journey.Journey.Key != "id:9001" || journey.Journey.TrainNumber != "682" ||
		journey.Journey.Operator != "Raja" || journey.Journey.OriginName != "Yazd" ||
		journey.Journey.DestinationName != "Mashhad" {
		t.Fatalf("journey = %+v", journey.Journey)
	}
	wantDeparture := time.Date(2026, 11, 5, 14, 50, 0, 0, time.FixedZone("IRST", 3*60*60+30*60)).UTC()
	wantArrival := time.Date(2026, 11, 6, 6, 30, 0, 0, time.FixedZone("IRST", 3*60*60+30*60)).UTC()
	if journey.Journey.DepartureAt == nil || !journey.Journey.DepartureAt.Equal(wantDeparture) {
		t.Fatalf("departure = %v, want %v", journey.Journey.DepartureAt, wantDeparture)
	}
	if journey.Journey.ArrivalAt == nil || !journey.Journey.ArrivalAt.Equal(wantArrival) {
		t.Fatalf("arrival = %v, want %v", journey.Journey.ArrivalAt, wantArrival)
	}
	if len(journey.Offerings) != 6 {
		t.Fatalf("offerings = %d", len(journey.Offerings))
	}
	counts := map[provider.State]int{}
	capacities := map[provider.State][]int{}
	for _, offering := range journey.Offerings {
		counts[offering.State]++
		capacities[offering.State] = append(capacities[offering.State], *offering.Capacity)
		if offering.BookingURL == nil || offering.BookingURL.String() != "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14" {
			t.Fatalf("booking URL = %v", offering.BookingURL)
		}
	}
	if counts[provider.StateReservable] != 2 || counts[provider.StateNotReservable] != 4 {
		t.Fatalf("state counts = %v", counts)
	}
	if len(capacities[provider.StateReservable]) != 2 ||
		!(capacitySet(capacities[provider.StateReservable], 2) && capacitySet(capacities[provider.StateReservable], 3)) {
		t.Fatalf("reservable capacities = %v", capacities[provider.StateReservable])
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %v", result.Warnings)
	}
}

func TestObservedCapacityShortfallRemainsUnknown(t *testing.T) {
	result, err := parseResponse(readObservedFixture(t, "sanitized-live-2026-11-05.json"), observedRequest(t, 4), testNow)
	if err != nil {
		t.Fatal(err)
	}
	offerings := result.Journeys[0].Offerings
	counts := map[provider.State]int{}
	for _, offering := range offerings {
		counts[offering.State]++
		if offering.Capacity != nil && *offering.Capacity > 0 && *offering.Capacity < 4 && offering.State != provider.StateUnknown {
			t.Fatalf("capacity %d classified as %s", *offering.Capacity, offering.State)
		}
	}
	if counts[provider.StateReservable] != 0 || counts[provider.StateUnknown] != 2 || counts[provider.StateNotReservable] != 4 {
		t.Fatalf("state counts = %v", counts)
	}
}

func TestRealEnvelopeRouteNotFoundIsErrorNotUnavailable(t *testing.T) {
	payload := []byte(`{"trains":[],"meta":{"routeNotFound":true,"alwaysHasTrain":false}}`)
	_, err := parseResponse(payload, observedRequest(t, 1), testNow)
	var providerErr *provider.Error
	if !errors.As(err, &providerErr) || providerErr.Kind != provider.ErrRouteNotFound {
		t.Fatalf("error = %v, want route_not_found", err)
	}
}

func TestRealEnvelopeEmptyTrainsIsSuccessfulEmpty(t *testing.T) {
	payload := []byte(`{"trains":[],"meta":{"routeNotFound":false,"alwaysHasTrain":true}}`)
	result, err := parseResponse(payload, observedRequest(t, 1), testNow)
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if len(result.Journeys) != 0 {
		t.Fatalf("journeys = %d", len(result.Journeys))
	}
	if !containsWarning(result.Warnings, "meta_always_has_train") {
		t.Fatalf("warnings = %v", result.Warnings)
	}
}

func TestRealEnvelopeMissingTrainsIsInvalid(t *testing.T) {
	for _, payload := range []string{
		`{"meta":{"routeNotFound":false}}`,
		`{"trains":null,"meta":{"routeNotFound":false}}`,
		`{"trains":{},"meta":{"routeNotFound":false}}`,
	} {
		if _, err := parseResponse([]byte(payload), observedRequest(t, 1), testNow); err == nil {
			t.Fatalf("payload accepted: %s", payload)
		}
	}
}

func TestRealEnvelopeMalformedPricesIsInvalid(t *testing.T) {
	payload := []byte(`{"trains":[{"id":1,"trainNumber":1,"prices":{}}],"meta":{"routeNotFound":false}}`)
	if _, err := parseResponse(payload, observedRequest(t, 1), testNow); err == nil {
		t.Fatal("malformed prices accepted")
	}
}

func TestRealEnvelopeUnexpectedClassTypeBecomesUnknown(t *testing.T) {
	payload := []byte(`{
		"trains":[{
			"id":1,"trainNumber":1,
			"prices":[{"sellType":1,"classes":[
				{"id":11,"wagonID":21,"wagonName":"W","shortTitle":"C","capacity":"3","isAvailable":true,"reservationAvailable":true}
			]}]
		}],
		"meta":{"routeNotFound":false}
	}`)
	result, err := parseResponse(payload, observedRequest(t, 1), testNow)
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	offerings := result.Journeys[0].Offerings
	if len(offerings) != 1 || offerings[0].State != provider.StateUnknown || offerings[0].Reason != "unexpected_type" {
		t.Fatalf("offerings = %+v", offerings)
	}
	if !containsWarning(result.Warnings, "unexpected_availability_type") {
		t.Fatalf("warnings = %v", result.Warnings)
	}
}

func TestBookingURLMatchesOwnerReference(t *testing.T) {
	booking, ok := buildBookingURL("yazd-mashhad", "2026-11-05")
	if !ok {
		t.Fatal("booking URL rejected")
	}
	if got, want := booking.String(), "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14"; got != want {
		t.Fatalf("booking URL = %q, want %q", got, want)
	}
	for _, tc := range []struct{ slug, date string }{
		{"", "2026-11-05"},
		{"Yazd-Mashhad", "2026-11-05"},
		{"yazd--mashhad", "2026-11-05"},
		{"../yazd", "2026-11-05"},
		{"yazd-mashhad", "2026-13-05"},
		{"yazd-mashhad", "not-a-date"},
	} {
		if _, ok := buildBookingURL(tc.slug, tc.date); ok {
			t.Fatalf("invalid booking input accepted: %+v", tc)
		}
	}
}

func TestValidateConfigBookingSlug(t *testing.T) {
	cfg := provider.MonitorProviderConfig{OriginID: "219", DestinationID: "191", BookingSlug: "yazd-mashhad"}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("valid slug rejected: %v", err)
	}
	cfg.BookingSlug = "Yazd Mashhad"
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("invalid slug accepted")
	}
}

func TestProviderNoRetries(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	providerInstance := providerForServer(t, server, func(cfg *Config) { cfg.NoRetries = true })
	_, err := providerInstance.Check(context.Background(), observedRequest(t, 1))
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 with NoRetries", attempts)
	}
}

func capacitySet(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
