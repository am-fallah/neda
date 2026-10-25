package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/auth"
	"github.com/am-fallah/neda/internal/provider"
)

type searchProviderStub struct {
	metadata provider.Metadata
	result   provider.SearchResult
	err      error
	calls    int
	last     provider.SearchRequest
}

func (s *searchProviderStub) Metadata() provider.Metadata { return s.metadata }
func (s *searchProviderStub) ValidateConfig(provider.MonitorProviderConfig) error {
	return nil
}
func (s *searchProviderStub) Check(context.Context, provider.CheckRequest) (provider.Result, error) {
	return provider.Result{}, nil
}
func (s *searchProviderStub) SearchURL(_ context.Context, req provider.SearchRequest) (provider.SearchResult, error) {
	s.calls++
	s.last = req
	return s.result, s.err
}

func stubSearchResult() provider.SearchResult {
	departure := time.Date(2026, 11, 5, 11, 20, 0, 0, time.UTC)
	return provider.SearchResult{
		URL:           "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14",
		Slug:          "yazd-mashhad",
		JalaliDate:    "1405-08-14",
		DepartureDate: "2026-11-05",
		Route: provider.Route{
			OriginID: "219", DestinationID: "191",
			OriginName: "Yazd", DestinationName: "Mashhad",
		},
		Passengers: provider.Passengers{Adults: 1},
		Result: provider.Result{
			ObservedAt: time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC),
			Journeys: []provider.JourneyObservation{{
				Journey: provider.Journey{
					Key: "id:9001", NativeID: "9001", TrainNumber: "682", Operator: "Raja",
					OriginName: "Yazd", DestinationName: "Mashhad", DepartureAt: &departure,
				},
				Offerings: []provider.OfferingObservation{{
					Offering: provider.Offering{
						Key: "id:9102", NativeID: "9102", WagonID: "9202",
						WagonName: "Five-star Samen", ClassName: "Samen",
						CompartmentType: "4-person compartment",
					},
					State: provider.StateReservable, Capacity: intPointer(3),
					Reason: "positive_capacity_and_flags",
				}},
			}},
		},
	}
}

func intPointer(value int) *int { return &value }

func TestProviderSearchEndpoint(t *testing.T) {
	stub := &searchProviderStub{
		metadata: provider.Metadata{
			ID: "mrbilit", DisplayName: "MrBilit",
			Capabilities: provider.Capabilities{
				RequiresOriginID: true, RequiresDestinationID: true,
				RequiresOptionSelection: true, MaxPassengers: 9,
			},
		},
		result: stubSearchResult(),
	}
	fixture := newAuthFixtureWithTelegram(t, auth.DefaultRateLimitConfig(), true, false, stub)
	session, csrf, login := fixture.login(t, authTestPass)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d", login.Code)
	}
	providersRecorder := fixture.request(t, http.MethodGet, "/api/v1/providers", "", session, "", "")
	if providersRecorder.Code != http.StatusOK {
		t.Fatalf("providers status = %d", providersRecorder.Code)
	}
	var providerList struct {
		Providers []struct {
			ID           string `json:"id"`
			Capabilities struct {
				RequiresOptionSelection bool `json:"requiresOptionSelection"`
			} `json:"capabilities"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(providersRecorder.Body.Bytes(), &providerList); err != nil {
		t.Fatalf("decode providers: %v", err)
	}
	foundOptionCapability := false
	for _, item := range providerList.Providers {
		if item.ID == "mrbilit" && item.Capabilities.RequiresOptionSelection {
			foundOptionCapability = true
		}
	}
	if !foundOptionCapability {
		t.Fatalf("mrbilit option capability not exposed: %+v", providerList)
	}

	body := `{"url":"https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14&utm_source=x","adults":1,"children":0,"infants":0}`
	recorder := fixture.request(t, http.MethodPost, "/api/v1/providers/mrbilit/search", body, session, csrf, authOrigin)
	if recorder.Code != http.StatusOK {
		t.Fatalf("search status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		URL           string `json:"url"`
		JalaliDate    string `json:"jalaliDate"`
		DepartureDate string `json:"departureDate"`
		Route         struct {
			OriginID string `json:"originId"`
		} `json:"route"`
		Trains []struct {
			JourneyKey string `json:"journeyKey"`
			Classes    []struct {
				OfferingKey string `json:"offeringKey"`
				State       string `json:"state"`
				Capacity    *int   `json:"capacity"`
			} `json:"classes"`
		} `json:"trains"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.URL != stub.result.URL || payload.Route.OriginID != "219" {
		t.Fatalf("payload = %+v", payload)
	}
	if len(payload.Trains) != 1 || len(payload.Trains[0].Classes) != 1 {
		t.Fatalf("trains = %+v", payload.Trains)
	}
	if payload.Trains[0].Classes[0].State != "reservable" || payload.Trains[0].Classes[0].Capacity == nil {
		t.Fatalf("class = %+v", payload.Trains[0].Classes[0])
	}
	if stub.last.Passengers.Adults != 1 || stub.last.URL == "" {
		t.Fatalf("stub request = %+v", stub.last)
	}
}

func TestProviderSearchEndpointRequiresAuthAndMapsErrors(t *testing.T) {
	stub := &searchProviderStub{
		metadata: provider.Metadata{
			ID: "mrbilit", DisplayName: "MrBilit",
			Capabilities: provider.Capabilities{RequiresOriginID: true, RequiresDestinationID: true, RequiresOptionSelection: true},
		},
		err: &provider.InputError{Field: "url", Code: "unsupported_url", Message: "Use a MrBilit URL."},
	}
	fixture := newAuthFixtureWithTelegram(t, auth.DefaultRateLimitConfig(), true, false, stub)
	body := `{"url":"https://example.com/x","adults":1,"children":0,"infants":0}`
	unauthenticated := fixture.request(t, http.MethodPost, "/api/v1/providers/mrbilit/search", body, "", "", authOrigin)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", unauthenticated.Code)
	}
	session, csrf, login := fixture.login(t, authTestPass)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d", login.Code)
	}
	recorder := fixture.request(t, http.MethodPost, "/api/v1/providers/mrbilit/search", body, session, csrf, authOrigin)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("error status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var payload errorPayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Error.Code != "unsupported_url" || payload.Error.Fields["url"] == "" {
		t.Fatalf("error payload = %+v", payload)
	}
}

func TestMonitorCreateWithSelectedOptions(t *testing.T) {
	stub := &searchProviderStub{
		metadata: provider.Metadata{
			ID: "mrbilit", DisplayName: "MrBilit",
			Capabilities: provider.Capabilities{
				RequiresOriginID: true, RequiresDestinationID: true,
				RequiresOptionSelection: true, MaxPassengers: 9,
			},
		},
	}
	fixture := newAuthFixtureWithTelegram(t, auth.DefaultRateLimitConfig(), true, false, stub)
	session, csrf, login := fixture.login(t, authTestPass)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d", login.Code)
	}
	body := map[string]any{
		"label":           "MrBilit monitor",
		"originName":      "Yazd",
		"destinationName": "Mashhad",
		"departureDate":   "2026-11-05",
		"adults":          1,
		"children":        0,
		"infants":         0,
		"providers": []map[string]any{{
			"providerId":    "mrbilit",
			"originId":      "219",
			"destinationId": "191",
			"bookingSlug":   "yazd-mashhad",
			"selections": []map[string]any{{
				"journeyKey": "id:9001", "offeringKey": "id:9102",
				"journeyNativeId": "9001", "offeringNativeId": "9102",
				"trainNumber": "682", "operator": "Raja",
				"departureAt": "2026-11-05T11:20:00Z",
				"wagonId":     "9202", "wagonName": "Five-star Samen",
				"className": "Samen", "compartmentType": "4-person compartment",
			}},
		}},
	}
	recorder := fixture.request(t, http.MethodPost, "/api/v1/monitors", mustJSON(t, body), session, csrf, authOrigin)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var created struct {
		Providers []struct {
			Selections []struct {
				JourneyKey  string `json:"journeyKey"`
				OfferingKey string `json:"offeringKey"`
			} `json:"selections"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(created.Providers) != 1 || len(created.Providers[0].Selections) != 1 {
		t.Fatalf("created = %+v", created)
	}
	if created.Providers[0].Selections[0].JourneyKey != "id:9001" {
		t.Fatalf("selection = %+v", created.Providers[0].Selections[0])
	}

	delete(body["providers"].([]map[string]any)[0], "selections")
	missing := fixture.request(t, http.MethodPost, "/api/v1/monitors", mustJSON(t, body), session, csrf, authOrigin)
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing selections status = %d, body=%s", missing.Code, missing.Body.String())
	}
}

func TestProviderSearchRouteNotFoundIsNotAvailability(t *testing.T) {
	stub := &searchProviderStub{
		metadata: provider.Metadata{
			ID: "mrbilit", DisplayName: "MrBilit",
			Capabilities: provider.Capabilities{RequiresOriginID: true, RequiresDestinationID: true, RequiresOptionSelection: true},
		},
		err: &provider.Error{Kind: provider.ErrRouteNotFound, Op: "test", Err: errors.New("route not found")},
	}
	fixture := newAuthFixtureWithTelegram(t, auth.DefaultRateLimitConfig(), true, false, stub)
	session, csrf, login := fixture.login(t, authTestPass)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d", login.Code)
	}
	body := `{"url":"https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14","adults":1,"children":0,"infants":0}`
	recorder := fixture.request(t, http.MethodPost, "/api/v1/providers/mrbilit/search", body, session, csrf, authOrigin)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var payload errorPayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Error.Code != "route_not_found" {
		t.Fatalf("code = %q", payload.Error.Code)
	}
}
