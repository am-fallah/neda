package scheduler_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/provider/mrbilit"
)

func TestSchedulerMrBilitObservedFixtureSelectedOptionAndOutbox(t *testing.T) {
	fixture, err := os.ReadFile("../../testdata/mrbilit/observed/sanitized-live-2026-11-05.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/api/GetAvailable/v2" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("from"); got != "219" {
			t.Errorf("from = %q", got)
		}
		if got := r.URL.Query().Get("to"); got != "191" {
			t.Errorf("to = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	t.Cleanup(server.Close)
	base, _ := url.Parse(server.URL)
	adapter, err := mrbilit.New(mrbilit.Config{BaseURL: base, NoRetries: true})
	if err != nil {
		t.Fatalf("mrbilit.New: %v", err)
	}
	h := newIntegrationHarness(t, adapter)
	departure := time.Date(2026, 11, 5, 11, 20, 0, 0, time.UTC)
	selection := domain.MonitorProviderSelection{
		ConfigVersion: 1,
		Option: provider.SelectedOption{
			JourneyKey: "id:9001", OfferingKey: "id:9102",
			JourneyNativeID: "9001", OfferingNativeID: "9102",
			TrainNumber: "682", Operator: "Raja",
			OriginName: "Yazd", DestinationName: "Mashhad",
			DepartureAt: &departure,
			WagonID:     "9202", WagonName: "Five-star Samen",
			ClassName: "Samen", CompartmentType: "4-person compartment",
		},
		CreatedAt: schedulerNow,
	}
	h.createMonitor("monitor-mrbilit", true, schedulerNow.Add(24*time.Hour), harnessTarget{
		providerID: mrbilit.ID, active: true, configVersion: 1,
		originID: "219", destinationID: "191", bookingSlug: "yazd-mashhad",
		departureDate: "2026-11-05",
		selections:    []domain.MonitorProviderSelection{selection},
	})
	result, err := h.runFor("monitor-mrbilit", mrbilit.ID)
	if err != nil || !result.Settled {
		t.Fatalf("run = %+v, err=%v", result, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("provider calls = %d", calls.Load())
	}
	target := h.target("monitor-mrbilit", mrbilit.ID)
	check := h.latestCheck(target.ID)
	if check.Outcome != "success" || check.Journeys != 1 || check.Offerings != 1 || check.Reservable != 1 {
		t.Fatalf("check = %+v", check)
	}
	var className, bookingURL string
	if err := h.sqlDB.QueryRow(`SELECT class_name, booking_url FROM events WHERE kind = 'availability'`).Scan(&className, &bookingURL); err != nil {
		t.Fatalf("load event: %v", err)
	}
	if className != "Samen" {
		t.Fatalf("event class = %q", className)
	}
	if bookingURL != "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14" {
		t.Fatalf("booking url = %q", bookingURL)
	}
	if got := h.count(`SELECT COUNT(*) FROM notification_deliveries`); got != 2 {
		t.Fatalf("deliveries = %d", got)
	}
	if _, err := h.runFor("monitor-mrbilit", mrbilit.ID); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("provider calls after repeat = %d", calls.Load())
	}
	if got := h.count(`SELECT COUNT(*) FROM events`); got != 1 {
		t.Fatalf("events after repeat = %d", got)
	}
	if got := h.count(`SELECT COUNT(*) FROM notification_deliveries`); got != 2 {
		t.Fatalf("deliveries after repeat = %d", got)
	}
}
