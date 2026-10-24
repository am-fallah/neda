package mrbilit

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

var testNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func testRequest() provider.CheckRequest {
	return provider.CheckRequest{
		MonitorID:     "m1",
		DepartureDate: "2026-10-20",
		Route: provider.Route{
			OriginName:      "Yazd",
			DestinationName: "Mashhad",
			OriginID:        "219",
			DestinationID:   "191",
		},
		Passengers: provider.Passengers{Adults: 2},
		Config: provider.MonitorProviderConfig{
			OriginID:      "219",
			DestinationID: "191",
		},
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "mrbilit", "synthetic", name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return body
}

func TestBuildURLUsesOnlyDocumentedParameters(t *testing.T) {
	base, _ := url.Parse("http://127.0.0.1:9999")
	req := testRequest()
	endpoint, err := buildURL(base, req)
	if err != nil {
		t.Fatalf("buildURL: %v", err)
	}
	if endpoint.Path != EndpointPath {
		t.Fatalf("path = %q, want %q", endpoint.Path, EndpointPath)
	}
	query := endpoint.Query()
	want := map[string]string{
		"from":        "219",
		"to":          "191",
		"date":        "2026-10-20",
		"adultCount":  "2",
		"childCount":  "0",
		"infantCount": "0",
	}
	if len(query) != len(want) {
		t.Fatalf("query = %v, want exactly %v", query, want)
	}
	for key, value := range want {
		if query.Get(key) != value {
			t.Fatalf("%s = %q, want %q", key, query.Get(key), value)
		}
	}
	for _, omitted := range []string{"genderCode", "exclusive", "availableStatus"} {
		if query.Has(omitted) {
			t.Fatalf("unverified parameter %q was sent", omitted)
		}
	}
}

func TestRequestValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*provider.CheckRequest)
	}{
		{"missing-monitor", func(req *provider.CheckRequest) { req.MonitorID = "" }},
		{"bad-date", func(req *provider.CheckRequest) { req.DepartureDate = "2026-2-01" }},
		{"zero-adults", func(req *provider.CheckRequest) { req.Passengers.Adults = 0 }},
		{"too-many-passengers", func(req *provider.CheckRequest) { req.Passengers.Children = 8 }},
		{"route-mismatch", func(req *provider.CheckRequest) { req.Route.OriginID = "220" }},
		{"gender-unverified", func(req *provider.CheckRequest) { req.Config.GenderCode = "M" }},
		{"zero-origin", func(req *provider.CheckRequest) { req.Config.OriginID = "0" }},
		{"leading-zero", func(req *provider.CheckRequest) { req.Config.OriginID = "0219" }},
		{"same-route", func(req *provider.CheckRequest) { req.Config.DestinationID = "219"; req.Route.DestinationID = "219" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := testRequest()
			tc.mutate(&req)
			if err := validateCheckRequest(req); err == nil {
				t.Fatal("request accepted")
			}
		})
	}
}

func TestBaseURLValidation(t *testing.T) {
	for _, raw := range []string{
		"http://api.telegram.org",
		"ftp://train.mrbilit.com",
		"https://user:pass@train.mrbilit.com",
		"https://train.mrbilit.com?x=1",
		"https://train.mrbilit.com#x",
		"https:///path",
	} {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateBaseURL(parsed); err == nil {
			t.Fatalf("base URL %q accepted", raw)
		}
	}
	loopback, _ := url.Parse("http://127.0.0.1:1234")
	if err := validateBaseURL(loopback); err != nil {
		t.Fatalf("loopback base rejected: %v", err)
	}
}

func TestNormalizationAndIdentity(t *testing.T) {
	if got := normalizeText("  Ya\u0301zd\t Mashhad  "); got != "Yázd Mashhad" {
		t.Fatalf("normalizeText = %q", got)
	}
	if got := normalizeText("a\u00a0b"); got != "a b" {
		t.Fatalf("normalizeText nbsp = %q", got)
	}
	if got := normalizeNativeID("  AbC-1  "); got != "AbC-1" {
		t.Fatalf("normalizeNativeID = %q", got)
	}

	departure := time.Date(2026, 10, 20, 8, 0, 0, 0, time.FixedZone("IRST", 3*60*60+30*60))
	native := provider.Journey{NativeID: "Tr-AbC", TrainNumber: "101", Operator: "Raja", DepartureAt: &departure}
	first, err := journeyKey(native, "2026-10-20", "219", "191")
	if err != nil {
		t.Fatal(err)
	}
	if first != "id:Tr-AbC" {
		t.Fatalf("native journey key = %q", first)
	}
	fallback := native
	fallback.NativeID = ""
	fallbackKey, err := journeyKey(fallback, "2026-10-20", "219", "191")
	if err != nil {
		t.Fatal(err)
	}
	again, err := journeyKey(fallback, "2026-10-20", "219", "191")
	if err != nil || again != fallbackKey {
		t.Fatalf("fallback key not stable: %q/%q err=%v", fallbackKey, again, err)
	}
	fallback.DepartureAt = nil
	if _, err := journeyKey(fallback, "2026-10-20", "219", "191"); err == nil {
		t.Fatal("fallback identity accepted without departure time")
	}

	model := provider.Offering{NativeID: "Off-1", WagonName: "Wagon", ClassName: "Economy"}
	offeringNative, ok := offeringKey(model, "w1", "open", "Economy")
	if !ok || offeringNative != "id:Off-1" {
		t.Fatalf("native offering key = %q, %v", offeringNative, ok)
	}
	model.NativeID = ""
	firstKey, ok := offeringKey(model, "w1", "open", "Economy")
	if !ok {
		t.Fatal("fallback offering key missing")
	}
	withPrice := model
	withPrice.Price = &provider.Price{Amount: 12345, Currency: "IRR"}
	secondKey, _ := offeringKey(withPrice, "w1", "open", "Economy")
	if firstKey != secondKey {
		t.Fatalf("price leaked into offering identity: %q/%q", firstKey, secondKey)
	}
	if _, ok := offeringKey(provider.Offering{}, "", "", ""); ok {
		t.Fatal("offering with no identity fields accepted")
	}
}

func TestClassificationDecisionTable(t *testing.T) {
	capacity := func(value int64) optionalInt { return optionalInt{Present: true, Valid: true, Value: value} }
	boolean := func(value bool) optionalBool { return optionalBool{Present: true, Valid: true, Value: value} }
	missingInt := optionalInt{Present: true}
	missingBool := optionalBool{Present: true}
	unexpectedInt := optionalInt{Present: true, Unexpected: true}
	invalidInt := optionalInt{Present: true, Invalid: true}
	unexpectedBool := optionalBool{Present: true, Unexpected: true}

	cases := []struct {
		name       string
		capacity   optionalInt
		available  optionalBool
		reserve    optionalBool
		required   int
		wantState  provider.State
		wantReason string
		wantCap    *int
	}{
		{"known-negative", capacity(0), boolean(false), boolean(true), 1, provider.StateNotReservable, "capacity_zero_unavailable", intPtr(0)},
		{"positive", capacity(3), boolean(true), boolean(true), 2, provider.StateReservable, "positive_capacity_and_flags", intPtr(3)},
		{"shortfall", capacity(1), boolean(true), boolean(true), 2, provider.StateUnknown, "capacity_below_requested_unverified", intPtr(1)},
		{"zero-with-available", capacity(0), boolean(true), boolean(true), 1, provider.StateUnknown, "zero_capacity_contradiction", intPtr(0)},
		{"positive-unavailable", capacity(2), boolean(false), boolean(true), 1, provider.StateUnknown, "positive_capacity_but_unavailable", intPtr(2)},
		{"missing-capacity-unavailable", missingInt, boolean(false), boolean(true), 1, provider.StateUnknown, "unavailable_without_capacity", nil},
		{"reservation-false", capacity(2), boolean(true), boolean(false), 1, provider.StateUnknown, "reservation_unavailable", intPtr(2)},
		{"availability-missing", capacity(2), missingBool, boolean(true), 1, provider.StateUnknown, "availability_flag_missing", intPtr(2)},
		{"capacity-missing", missingInt, boolean(true), boolean(true), 1, provider.StateUnknown, "capacity_missing", nil},
		{"unexpected-capacity", unexpectedInt, boolean(true), boolean(true), 1, provider.StateUnknown, "unexpected_type", nil},
		{"unexpected-availability", capacity(2), unexpectedBool, boolean(true), 1, provider.StateUnknown, "unexpected_type", nil},
		{"invalid-capacity", invalidInt, boolean(true), boolean(true), 1, provider.StateUnknown, "invalid_capacity", nil},
		{"negative-capacity", capacity(-1), boolean(true), boolean(true), 1, provider.StateUnknown, "invalid_capacity", nil},
		{"all-missing", missingInt, missingBool, missingBool, 1, provider.StateUnknown, "availability_flag_missing", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, reason, gotCapacity := classifyOffering(tc.capacity, tc.available, tc.reserve, tc.required)
			if state != tc.wantState || reason != tc.wantReason {
				t.Fatalf("got %s/%s, want %s/%s", state, reason, tc.wantState, tc.wantReason)
			}
			if !equalIntPointer(gotCapacity, tc.wantCap) {
				t.Fatalf("capacity = %v, want %v", gotCapacity, tc.wantCap)
			}
		})
	}
}

func TestParseSyntheticMixedFixture(t *testing.T) {
	result, err := parseSyntheticResponse(readFixture(t, "valid-mixed.json"), testRequest(), testNow)
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if len(result.Journeys) != 2 {
		t.Fatalf("journeys = %d", len(result.Journeys))
	}
	first := result.Journeys[0]
	if first.Journey.Key != "id:tr-1" || first.Journey.TrainNumber != "101" || first.Journey.Operator != "Raja" {
		t.Fatalf("journey = %+v", first.Journey)
	}
	if first.Journey.DepartureAt == nil || !first.Journey.DepartureAt.Equal(time.Date(2026, 10, 20, 4, 30, 0, 0, time.UTC)) {
		t.Fatalf("departure = %v", first.Journey.DepartureAt)
	}
	if len(first.Offerings) != 4 {
		t.Fatalf("offerings = %d", len(first.Offerings))
	}
	wantStates := []provider.State{provider.StateReservable, provider.StateNotReservable, provider.StateUnknown, provider.StateUnknown}
	for index, want := range wantStates {
		if first.Offerings[index].State != want {
			t.Fatalf("offering %d state = %s, want %s", index, first.Offerings[index].State, want)
		}
	}
	if first.Offerings[2].Reason != "capacity_missing" {
		t.Fatalf("missing-capacity reason = %q", first.Offerings[2].Reason)
	}
	if first.Offerings[3].Reason != "unexpected_type" {
		t.Fatalf("unexpected-capacity reason = %q", first.Offerings[3].Reason)
	}
	if result.Journeys[1].Journey.DepartureAt != nil {
		t.Fatal("unparseable time was not kept null")
	}
	if !containsWarning(result.Warnings, "unparseable_time") {
		t.Fatalf("warnings = %v", result.Warnings)
	}
	if err := provider.ValidateResult(result); err != nil {
		t.Fatalf("ValidateResult: %v", err)
	}
}

func TestParseKnownNegativeAndFallbackIdentity(t *testing.T) {
	result, err := parseSyntheticResponse(readFixture(t, "known-negative.json"), testRequest(), testNow)
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if len(result.Journeys) != 1 {
		t.Fatalf("journeys = %d", len(result.Journeys))
	}
	journey := result.Journeys[0]
	if len(journey.Journey.Key) != 64 {
		t.Fatalf("journey key = %q", journey.Journey.Key)
	}
	if journey.Journey.OriginName != "Yazd" {
		t.Fatalf("origin name = %q", journey.Journey.OriginName)
	}
	if len(journey.Offerings) != 1 || journey.Offerings[0].State != provider.StateNotReservable {
		t.Fatalf("offerings = %+v", journey.Offerings)
	}
	if journey.Offerings[0].Capacity == nil || *journey.Offerings[0].Capacity != 0 {
		t.Fatalf("capacity = %v", journey.Offerings[0].Capacity)
	}
}

func TestParseEmptyAndInvalidShapes(t *testing.T) {
	result, err := parseSyntheticResponse(readFixture(t, "empty.json"), testRequest(), testNow)
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	if len(result.Journeys) != 0 || result.ObservedAt.IsZero() {
		t.Fatalf("empty result = %+v", result)
	}
	for _, name := range []string{"envelope-object.json", "invalid-json.json", "identity-insufficient.json", "offerings-shape.json"} {
		t.Run(name, func(t *testing.T) {
			_, err := parseSyntheticResponse(readFixture(t, name), testRequest(), testNow)
			var providerErr *provider.Error
			if !errors.As(err, &providerErr) || providerErr.Kind != provider.ErrInvalidResponse {
				t.Fatalf("error = %v, want invalid response", err)
			}
		})
	}
}

func TestParseDuplicateOfferingsFailsClosed(t *testing.T) {
	result, err := parseSyntheticResponse(readFixture(t, "duplicate-offerings.json"), testRequest(), testNow)
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	offerings := result.Journeys[0].Offerings
	if len(offerings) != 1 {
		t.Fatalf("offerings = %d", len(offerings))
	}
	if offerings[0].State != provider.StateUnknown || offerings[0].Reason != "duplicate_offering_key" || offerings[0].Capacity != nil {
		t.Fatalf("duplicate observation = %+v", offerings[0])
	}
	if !containsWarning(result.Warnings, "duplicate_offering_key") {
		t.Fatalf("warnings = %v", result.Warnings)
	}
}

func TestParseCapsBeforeNormalization(t *testing.T) {
	t.Run("journeys", func(t *testing.T) {
		var builder strings.Builder
		builder.WriteByte('[')
		for i := 0; i <= provider.MaxJourneys; i++ {
			if i > 0 {
				builder.WriteByte(',')
			}
			builder.WriteString(`{"id":"tr-`)
			builder.WriteString(strconvItoa(i))
			builder.WriteString(`","classes":[]}`)
		}
		builder.WriteByte(']')
		_, err := parseSyntheticResponse([]byte(builder.String()), testRequest(), testNow)
		if err == nil {
			t.Fatal("oversized journey list accepted")
		}
	})

	t.Run("per-journey-offerings", func(t *testing.T) {
		var builder strings.Builder
		builder.WriteString(`[{"id":"tr-1","classes":[`)
		for i := 0; i <= provider.MaxOfferingsPerJourney; i++ {
			if i > 0 {
				builder.WriteByte(',')
			}
			builder.WriteString(`{"id":"off-`)
			builder.WriteString(strconvItoa(i))
			builder.WriteString(`"}`)
		}
		builder.WriteString(`]}]`)
		_, err := parseSyntheticResponse([]byte(builder.String()), testRequest(), testNow)
		if err == nil {
			t.Fatal("oversized offering list accepted")
		}
	})

	t.Run("total-offerings", func(t *testing.T) {
		var builder strings.Builder
		builder.WriteByte('[')
		for journey := 0; journey < 100; journey++ {
			if journey > 0 {
				builder.WriteByte(',')
			}
			builder.WriteString(`{"id":"tr-`)
			builder.WriteString(strconvItoa(journey))
			builder.WriteString(`","classes":[`)
			for offering := 0; offering < 6; offering++ {
				if offering > 0 {
					builder.WriteByte(',')
				}
				builder.WriteString(`{"id":"off-`)
				builder.WriteString(strconvItoa(offering))
				builder.WriteString(`"}`)
			}
			builder.WriteString(`]}`)
		}
		builder.WriteByte(']')
		_, err := parseSyntheticResponse([]byte(builder.String()), testRequest(), testNow)
		if err == nil {
			t.Fatal("oversized total offering list accepted")
		}
	})
}

func TestParseMalformedOptionalFields(t *testing.T) {
	body := `[{"id":"tr-1","classes":[
		{"id":"a","capacity":1.5,"isAvailable":true,"reservationAvailable":true},
		{"id":"b","capacity":-1,"isAvailable":true,"reservationAvailable":true},
		{"id":"c","capacity":2,"isAvailable":"true","reservationAvailable":true},
		{"id":"d","capacity":2,"isAvailable":true,"reservationAvailable":null},
		{"id":"e"}
	]}]`
	result, err := parseSyntheticResponse([]byte(body), testRequest(), testNow)
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	want := []struct {
		state  provider.State
		reason string
	}{
		{provider.StateUnknown, "invalid_capacity"},
		{provider.StateUnknown, "invalid_capacity"},
		{provider.StateUnknown, "unexpected_type"},
		{provider.StateUnknown, "insufficient_evidence"},
		{provider.StateUnknown, "availability_flag_missing"},
	}
	for index, expected := range want {
		got := result.Journeys[0].Offerings[index]
		if got.State != expected.state || got.Reason != expected.reason {
			t.Fatalf("offering %d = %s/%s, want %s/%s", index, got.State, got.Reason, expected.state, expected.reason)
		}
	}
}

func TestParseMissingOfferingsField(t *testing.T) {
	body := `[{"id":"tr-1","trainNumber":"1","departureTime":"2026-10-20T08:00:00Z","providerName":"MrBilit"}]`
	result, err := parseSyntheticResponse([]byte(body), testRequest(), testNow)
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if len(result.Journeys[0].Offerings) != 0 {
		t.Fatalf("offerings = %d", len(result.Journeys[0].Offerings))
	}
	if !containsWarning(result.Warnings, "offerings_missing") {
		t.Fatalf("warnings = %v", result.Warnings)
	}
}

func TestParseDuplicateJourneyMergeAndConflict(t *testing.T) {
	merge := `[{"id":"tr-1","classes":[{"id":"a"}]},{"id":"tr-1","classes":[{"id":"b"}]}]`
	result, err := parseSyntheticResponse([]byte(merge), testRequest(), testNow)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(result.Journeys) != 1 || len(result.Journeys[0].Offerings) != 2 {
		t.Fatalf("merged result = %+v", result)
	}
	conflict := `[{"id":"tr-1","trainNumber":"1","classes":[]},{"id":"tr-1","trainNumber":"2","classes":[]}]`
	if _, err := parseSyntheticResponse([]byte(conflict), testRequest(), testNow); err == nil {
		t.Fatal("conflicting duplicate journey accepted")
	}
}

func intPtr(value int) *int { return &value }

func containsWarning(warnings []string, want string) bool {
	for _, warning := range warnings {
		if warning == want {
			return true
		}
	}
	return false
}

func strconvItoa(value int) string {
	return fmt.Sprint(value)
}
