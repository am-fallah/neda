package notify_test

import (
	"strings"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/notify"
)

var formatNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func TestFormatTelegramAvailabilityEscapesAndValidates(t *testing.T) {
	departure := time.Date(2026, 12, 20, 8, 0, 0, 0, time.FixedZone("IRST", 3*60*60+30*60))
	arrival := departure.Add(16 * time.Hour)
	price := int64(12345)
	event := notify.Event{
		Kind:            notify.KindAvailability,
		ProviderID:      "mrbilit",
		OccurredAt:      formatNow,
		TrainNumber:     "101 <script>",
		Operator:        "Raja & Co",
		OriginName:      "Yazd <b>",
		DestinationName: "Mashhad",
		DepartureAt:     &departure,
		ArrivalAt:       &arrival,
		WagonName:       "Wagon <1>",
		ClassName:       "Economy",
		PriceAmount:     &price,
		PriceCurrency:   "IRR",
		BookingURL:      "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-07-28",
	}
	text, err := notify.FormatTelegram(event)
	if err != nil {
		t.Fatalf("FormatTelegram: %v", err)
	}
	for _, want := range []string{
		"Ticket available",
		"Yazd &lt;b&gt; → Mashhad",
		"Train: 101 &lt;script&gt; (Raja &amp; Co)",
		"Wagon/class: Wagon &lt;1&gt; / Economy",
		"Price: 12345 IRR",
		"Provider: MrBilit",
		"https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-07-28",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("message missing %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{"<script>", "<1>", "Raja & Co"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("message was not escaped (%q):\n%s", forbidden, text)
		}
	}
	if !strings.Contains(text, "(Tehran)") {
		t.Fatalf("message lacks Tehran timestamp:\n%s", text)
	}
}

func TestFormatTelegramOmitsUnvalidatedBookingURLs(t *testing.T) {
	event := notify.Event{
		Kind:       notify.KindAvailability,
		ProviderID: "mrbilit",
		OccurredAt: formatNow,
		BookingURL: "javascript:alert(1)",
	}
	text, err := notify.FormatTelegram(event)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "javascript") || strings.Contains(text, "Open booking") {
		t.Fatalf("unsafe booking URL rendered:\n%s", text)
	}

	event.BookingURL = "https://evil.example/trains/x"
	text, _ = notify.FormatTelegram(event)
	if strings.Contains(text, "evil.example") {
		t.Fatalf("untrusted host rendered:\n%s", text)
	}
}

func TestFormatTelegramHealthAndRecoveryAreDistinct(t *testing.T) {
	failures := 3
	degraded := notify.Event{
		Kind:         notify.KindProviderDegraded,
		ProviderID:   "mrbilit",
		OccurredAt:   formatNow,
		ErrorKind:    "timeout",
		FailureCount: &failures,
	}
	text, err := notify.FormatTelegram(degraded)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Provider issue: MrBilit", "Consecutive failures: 3", "Category: timeout", "not a sold-out verdict"} {
		if !strings.Contains(text, want) {
			t.Fatalf("degraded message missing %q:\n%s", want, text)
		}
	}
	recovered := degraded
	recovered.Kind = notify.KindProviderRecovered
	text, err = notify.FormatTelegram(recovered)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Provider recovered: MrBilit") || strings.Contains(text, "Consecutive failures") {
		t.Fatalf("recovery message = %s", text)
	}
}

func TestFormatTelegramRejectsUnknownKind(t *testing.T) {
	if _, err := notify.FormatTelegram(notify.Event{Kind: "unknown", OccurredAt: formatNow}); err == nil {
		t.Fatal("unknown event kind accepted")
	}
}
