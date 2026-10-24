package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

var (
	planNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	planDue = planNow.Add(2 * time.Second)
)

func timePtr(value time.Time) *time.Time { return &value }

func fixtureMonitor() Monitor {
	return Monitor{
		ID:              "monitor-1",
		Label:           "Tehran to Mashhad",
		OriginName:      "Tehran",
		DestinationName: "Mashhad",
		DepartureDate:   "2026-10-30",
		Adults:          1,
		Children:        1,
		Infants:         0,
		Enabled:         true,
		ExpiresAt:       time.Date(2026, 10, 31, 0, 0, 0, 0, mustLocation()).UTC(),
		CreatedAt:       planNow.Add(-time.Hour),
		UpdatedAt:       planNow.Add(-time.Hour),
		Providers: []MonitorProvider{
			{
				ID:            1,
				MonitorID:     "monitor-1",
				ProviderID:    "fake",
				Active:        true,
				OriginID:      "origin-a",
				DestinationID: "destination-a",
				ConfigVersion: 1,
				NextDueAt:     timePtr(planDue),
				CreatedAt:     planNow.Add(-time.Hour),
				UpdatedAt:     planNow.Add(-time.Hour),
			},
			{
				ID:            2,
				MonitorID:     "monitor-1",
				ProviderID:    "other",
				Active:        true,
				OriginID:      "origin-b",
				DestinationID: "destination-b",
				ConfigVersion: 1,
				NextDueAt:     timePtr(planDue),
				CreatedAt:     planNow.Add(-time.Hour),
				UpdatedAt:     planNow.Add(-time.Hour),
			},
		},
	}
}

func mustLocation() *time.Location {
	location, err := TehranLocation()
	if err != nil {
		panic(err)
	}
	return location
}

func inputFor(monitor Monitor) MonitorInput {
	input := MonitorInput{
		Label:           monitor.Label,
		OriginName:      monitor.OriginName,
		DestinationName: monitor.DestinationName,
		DepartureDate:   monitor.DepartureDate,
		Adults:          monitor.Adults,
		Children:        monitor.Children,
		Infants:         monitor.Infants,
	}
	for _, target := range monitor.Providers {
		if !target.Active {
			continue
		}
		input.Providers = append(input.Providers, ProviderInput{
			ProviderID:    target.ProviderID,
			OriginID:      target.OriginID,
			DestinationID: target.DestinationID,
			BookingSlug:   target.BookingSlug,
			GenderCode:    target.GenderCode,
		})
	}
	return input
}

func TestDepartureExpiryUsesTehranEndOfDay(t *testing.T) {
	location := mustLocation()
	expiry, err := DepartureExpiry("2026-10-30", location)
	if err != nil {
		t.Fatalf("DepartureExpiry: %v", err)
	}
	want := time.Date(2026, 10, 31, 0, 0, 0, 0, location).UTC()
	if !expiry.Equal(want) {
		t.Fatalf("expiry = %v, want %v", expiry, want)
	}
	if _, err := DepartureExpiry("2026-02-30", location); err == nil {
		t.Fatal("DepartureExpiry accepted invalid date")
	}
	if _, err := DepartureExpiry("2026-1-2", location); err == nil {
		t.Fatal("DepartureExpiry accepted non-canonical date")
	}
}

func TestValidateDepartureDateTodayAndPast(t *testing.T) {
	location := mustLocation()
	if _, err := ValidateDepartureDate("2026-10-30", planNow, location); err != nil {
		t.Fatalf("future date: %v", err)
	}
	if _, err := ValidateDepartureDate("2026-10-24", planNow, location); err != nil {
		t.Fatalf("today in Tehran: %v", err)
	}
	if _, err := ValidateDepartureDate("2026-10-23", planNow, location); err == nil {
		t.Fatal("past date accepted")
	}
}

func TestMonitorStatus(t *testing.T) {
	monitor := fixtureMonitor()
	if got := monitor.Status(planNow); got != StatusActive {
		t.Fatalf("Status = %q, want active", got)
	}
	monitor.Enabled = false
	if got := monitor.Status(planNow); got != StatusPaused {
		t.Fatalf("Status = %q, want paused", got)
	}
	monitor.Enabled = true
	monitor.ExpiredAt = timePtr(planNow)
	if got := monitor.Status(planNow); got != StatusExpired {
		t.Fatalf("Status = %q, want expired", got)
	}
	monitor.ExpiredAt = nil
	monitor.ExpiresAt = planNow
	if got := monitor.Status(planNow); got != StatusExpired {
		t.Fatalf("Status = %q, want expired at expiry", got)
	}
}

func TestPlanUpdateLabelOnly(t *testing.T) {
	current := fixtureMonitor()
	input := inputFor(current)
	input.Label = "Renamed monitor"
	plan, err := PlanUpdate(current, input, planNow, mustLocation())
	if err != nil {
		t.Fatalf("PlanUpdate: %v", err)
	}
	if plan.Material {
		t.Fatal("label-only edit marked material")
	}
	if plan.Monitor.Label != "Renamed monitor" {
		t.Fatalf("label = %q", plan.Monitor.Label)
	}
	for index, target := range plan.Monitor.Providers {
		if target.ConfigVersion != 1 {
			t.Fatalf("provider %d version = %d", index, target.ConfigVersion)
		}
		if target.NextDueAt == nil || !target.NextDueAt.Equal(planDue) {
			t.Fatalf("provider %d next due = %v", index, target.NextDueAt)
		}
	}
	if len(plan.CloseEpisodes) != 0 {
		t.Fatalf("close episodes = %d", len(plan.CloseEpisodes))
	}
}

func TestPlanUpdateBookingSlugOnly(t *testing.T) {
	current := fixtureMonitor()
	current.Providers[0].BookingSlug = "old-slug"
	input := inputFor(current)
	input.Providers[0].BookingSlug = "new-slug"
	plan, err := PlanUpdate(current, input, planNow, mustLocation())
	if err != nil {
		t.Fatalf("PlanUpdate: %v", err)
	}
	if plan.Material {
		t.Fatal("booking-slug-only edit marked material")
	}
	if plan.Monitor.Providers[0].ConfigVersion != 1 {
		t.Fatalf("version = %d", plan.Monitor.Providers[0].ConfigVersion)
	}
	if plan.Monitor.Providers[0].BookingSlug != "new-slug" {
		t.Fatalf("slug = %q", plan.Monitor.Providers[0].BookingSlug)
	}
	if plan.Monitor.Providers[0].NextDueAt == nil || !plan.Monitor.Providers[0].NextDueAt.Equal(planDue) {
		t.Fatalf("next due changed: %v", plan.Monitor.Providers[0].NextDueAt)
	}
	if len(plan.CloseEpisodes) != 0 {
		t.Fatalf("close episodes = %d", len(plan.CloseEpisodes))
	}
}

func TestPlanUpdateMaterialBumpsAllActiveProviders(t *testing.T) {
	current := fixtureMonitor()
	input := inputFor(current)
	input.Adults = 2
	plan, err := PlanUpdate(current, input, planNow, mustLocation())
	if err != nil {
		t.Fatalf("PlanUpdate: %v", err)
	}
	if !plan.Material {
		t.Fatal("passenger edit was not material")
	}
	if len(plan.CloseEpisodes) != 2 {
		t.Fatalf("close episodes = %d, want 2", len(plan.CloseEpisodes))
	}
	for index, target := range plan.Monitor.Providers {
		if target.ConfigVersion != 2 {
			t.Fatalf("provider %d version = %d, want 2", index, target.ConfigVersion)
		}
		if target.NextDueAt != nil {
			t.Fatalf("provider %d next due = %v, want nil until post-commit scheduling", index, target.NextDueAt)
		}
	}
}

func TestPlanUpdatePreservesConsecutiveFailures(t *testing.T) {
	current := fixtureMonitor()
	current.Providers[0].ConsecutiveFailures = 3
	current.Providers[1].ConsecutiveFailures = 4
	input := inputFor(current)
	input.Adults = 2
	plan, err := PlanUpdate(current, input, planNow, mustLocation())
	if err != nil {
		t.Fatalf("PlanUpdate: %v", err)
	}
	if !plan.Material {
		t.Fatal("passenger edit was not material")
	}
	if plan.Monitor.Providers[0].ConsecutiveFailures != 3 ||
		plan.Monitor.Providers[1].ConsecutiveFailures != 4 {
		t.Fatalf("material edit reset failures: %d/%d",
			plan.Monitor.Providers[0].ConsecutiveFailures,
			plan.Monitor.Providers[1].ConsecutiveFailures)
	}
}

func TestPlanUpdateDateChangeClearsExpiryAndKeepsPaused(t *testing.T) {
	current := fixtureMonitor()
	current.Enabled = false
	current.ExpiredAt = timePtr(planNow)
	current.ExpiresAt = planNow.Add(-time.Hour)
	input := inputFor(current)
	input.DepartureDate = "2026-11-05"
	plan, err := PlanUpdate(current, input, planNow, mustLocation())
	if err != nil {
		t.Fatalf("PlanUpdate: %v", err)
	}
	if plan.Monitor.Enabled {
		t.Fatal("paused monitor became enabled")
	}
	if plan.Monitor.ExpiredAt != nil {
		t.Fatalf("ExpiredAt = %v, want nil", plan.Monitor.ExpiredAt)
	}
	wantExpiry := time.Date(2026, 11, 6, 0, 0, 0, 0, mustLocation()).UTC()
	if !plan.Monitor.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("ExpiresAt = %v, want %v", plan.Monitor.ExpiresAt, wantExpiry)
	}
	for _, target := range plan.Monitor.Providers {
		if target.NextDueAt != nil {
			t.Fatalf("paused target next due = %v, want nil", target.NextDueAt)
		}
	}
}

func TestPlanUpdateProviderRemovalAndReAdd(t *testing.T) {
	current := fixtureMonitor()
	input := inputFor(current)
	input.Providers = input.Providers[:1]
	plan, err := PlanUpdate(current, input, planNow, mustLocation())
	if err != nil {
		t.Fatalf("PlanUpdate removal: %v", err)
	}
	if !plan.Material {
		t.Fatal("provider removal was not material")
	}
	if len(plan.Monitor.Providers) != 2 {
		t.Fatalf("providers = %d, want retained row", len(plan.Monitor.Providers))
	}
	removed := plan.Monitor.Providers[1]
	if removed.Active || removed.ConfigVersion != 2 || removed.NextDueAt != nil {
		t.Fatalf("removed provider = %+v", removed)
	}
	if len(plan.CloseEpisodes) != 1 || plan.CloseEpisodes[0].TargetID != 2 || plan.CloseEpisodes[0].ConfigVersion != 1 {
		t.Fatalf("close episodes = %+v", plan.CloseEpisodes)
	}

	readded := plan.Monitor
	readdInput := inputFor(readded)
	readdInput.Providers = append(readdInput.Providers, ProviderInput{
		ProviderID:    "other",
		OriginID:      "origin-b",
		DestinationID: "destination-b",
	})
	plan, err = PlanUpdate(readded, readdInput, planNow.Add(time.Minute), mustLocation())
	if err != nil {
		t.Fatalf("PlanUpdate re-add: %v", err)
	}
	if !plan.Material {
		t.Fatal("provider re-add was not material")
	}
	readdedTarget := plan.Monitor.Providers[1]
	if !readdedTarget.Active || readdedTarget.ConfigVersion != 3 || readdedTarget.NextDueAt != nil {
		t.Fatalf("re-added provider = %+v", readdedTarget)
	}
	if len(plan.CloseEpisodes) != 1 || plan.CloseEpisodes[0].TargetID != 2 || plan.CloseEpisodes[0].ConfigVersion != 2 {
		t.Fatalf("re-add close episodes = %+v", plan.CloseEpisodes)
	}
}

func TestPlanUpdateProviderFieldsAreMaterial(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*MonitorInput)
	}{
		{"origin id", func(input *MonitorInput) { input.Providers[0].OriginID = "new-origin" }},
		{"destination id", func(input *MonitorInput) { input.Providers[0].DestinationID = "new-destination" }},
		{"gender code", func(input *MonitorInput) { input.Providers[0].GenderCode = "F" }},
		{"origin name", func(input *MonitorInput) { input.OriginName = "Tehran Central" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			current := fixtureMonitor()
			input := inputFor(current)
			test.mutate(&input)
			plan, err := PlanUpdate(current, input, planNow, mustLocation())
			if err != nil {
				t.Fatalf("PlanUpdate: %v", err)
			}
			if !plan.Material {
				t.Fatal("provider field change was not material")
			}
			if plan.Monitor.Providers[0].ConfigVersion != 2 {
				t.Fatalf("version = %d, want 2", plan.Monitor.Providers[0].ConfigVersion)
			}
			if len(plan.CloseEpisodes) == 0 || plan.CloseEpisodes[0].TargetID != 1 {
				t.Fatalf("close episodes = %+v", plan.CloseEpisodes)
			}
		})
	}
}

func TestPlanUpdateAddsNewProviderWithoutResettingExisting(t *testing.T) {
	current := fixtureMonitor()
	input := inputFor(current)
	input.Providers = append(input.Providers, ProviderInput{
		ProviderID:    "new",
		OriginID:      "origin-c",
		DestinationID: "destination-c",
	})
	plan, err := PlanUpdate(current, input, planNow, mustLocation())
	if err != nil {
		t.Fatalf("PlanUpdate: %v", err)
	}
	if !plan.Material {
		t.Fatal("provider addition was not material")
	}
	if plan.Monitor.Providers[0].ConfigVersion != 1 || plan.Monitor.Providers[1].ConfigVersion != 1 {
		t.Fatal("existing provider was reset on provider addition")
	}
	newTarget := plan.Monitor.Providers[2]
	if !newTarget.Active || newTarget.ConfigVersion != 1 || newTarget.ProviderID != provider.ID("new") {
		t.Fatalf("new provider = %+v", newTarget)
	}
	if len(plan.CloseEpisodes) != 0 {
		t.Fatalf("new provider close episodes = %d", len(plan.CloseEpisodes))
	}
}

func TestValidationErrorIsMonitorInvalid(t *testing.T) {
	err := validationError("label", "is required", nil)
	if !errors.Is(err, ErrMonitorInvalid) {
		t.Fatalf("errors.Is = false for %v", err)
	}
}
