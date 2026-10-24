package notify_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/notify"
)

func TestFormatPushAvailabilityUsesJalaliAndBookingURL(t *testing.T) {
	departure := time.Date(2026, 12, 20, 8, 0, 0, 0, time.FixedZone("IRST", 3*60*60+30*60))
	price := int64(12345)
	event := notify.Event{
		ID:              "11111111-2222-3333-4444-555555555555",
		Kind:            notify.KindAvailability,
		ProviderID:      "mrbilit",
		OccurredAt:      formatNow,
		TrainNumber:     "101",
		Operator:        "Raja",
		OriginName:      "Yazd",
		DestinationName: "Mashhad",
		DepartureAt:     &departure,
		WagonName:       "Wagon 1",
		ClassName:       "Economy",
		PriceAmount:     &price,
		PriceCurrency:   "IRR",
		BookingURL:      "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-09-29",
	}
	raw, err := notify.FormatPush(event, "https://neda.example.com")
	if err != nil {
		t.Fatalf("FormatPush: %v", err)
	}
	var payload notify.PushPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("payload JSON: %v (%s)", err, raw)
	}
	if payload.Title != "Ticket available" || payload.Kind != "availability" || payload.EventID != event.ID {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Tag != "neda-event-"+event.ID {
		t.Fatalf("tag = %q", payload.Tag)
	}
	if payload.URL != event.BookingURL {
		t.Fatalf("URL = %q", payload.URL)
	}
	for _, want := range []string{
		"Yazd → Mashhad",
		"Departure: 1405-09-29 08:00 (Tehran)",
		"Detected: 1405-08-02 15:30 (Tehran)",
		"Price: 12345 IRR",
		"Provider: MrBilit",
	} {
		if !strings.Contains(payload.Body, want) {
			t.Fatalf("body missing %q:\n%s", want, payload.Body)
		}
	}
	if strings.Contains(payload.Body, "<") {
		t.Fatalf("push body contains HTML: %s", payload.Body)
	}
}

func TestFormatPushFallsBackToEventDetail(t *testing.T) {
	event := notify.Event{
		ID:         "abc",
		Kind:       notify.KindProviderRecovered,
		ProviderID: "mrbilit",
		OccurredAt: formatNow,
	}
	raw, err := notify.FormatPush(event, "https://neda.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	var payload notify.PushPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.URL != "https://neda.example.com/events/abc" {
		t.Fatalf("URL = %q", payload.URL)
	}
	if payload.Title != "Provider recovered: MrBilit" {
		t.Fatalf("title = %q", payload.Title)
	}

	raw, err = notify.FormatPush(event, "")
	if err != nil {
		t.Fatal(err)
	}
	payload = notify.PushPayload{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.URL != "" {
		t.Fatalf("URL without public URL = %q", payload.URL)
	}
}

func TestFormatPushOmitsUnvalidatedURLs(t *testing.T) {
	event := notify.Event{
		ID:         "abc",
		Kind:       notify.KindAvailability,
		OccurredAt: formatNow,
		BookingURL: "https://evil.example/trains/x",
	}
	raw, err := notify.FormatPush(event, "javascript:alert(1)")
	if err != nil {
		t.Fatal(err)
	}
	var payload notify.PushPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.URL != "" {
		t.Fatalf("unsafe URL = %q", payload.URL)
	}
}

func TestFormatPushRejectsUnknownKind(t *testing.T) {
	if _, err := notify.FormatPush(notify.Event{Kind: "unknown", OccurredAt: formatNow}, ""); err == nil {
		t.Fatal("unknown event kind accepted")
	}
}
