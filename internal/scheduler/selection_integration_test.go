package scheduler_test

import (
	"context"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/provider"
)

type optionProvider struct {
	id     provider.ID
	result provider.Result
}

func (p optionProvider) Metadata() provider.Metadata {
	return provider.Metadata{
		ID: p.id, DisplayName: "Selected option test provider",
		Capabilities: provider.Capabilities{
			RequiresOriginID: true, RequiresDestinationID: true,
			RequiresOptionSelection: true, MaxPassengers: 9,
		},
	}
}

func (p optionProvider) ValidateConfig(provider.MonitorProviderConfig) error { return nil }

func (p optionProvider) Check(context.Context, provider.CheckRequest) (provider.Result, error) {
	return p.result, nil
}

func optionResult(includeSelected bool) provider.Result {
	departure := time.Date(2026, 12, 15, 11, 20, 0, 0, time.UTC)
	offerings := make([]provider.OfferingObservation, 0, 2)
	if includeSelected {
		offerings = append(offerings, provider.OfferingObservation{
			Offering: provider.Offering{
				Key: "id:A", NativeID: "A", WagonID: "wagon-A",
				WagonName: "Wagon A", ClassName: "Class A",
			},
			State: provider.StateReservable, Capacity: intPtr(3),
		})
	}
	offerings = append(offerings, provider.OfferingObservation{
		Offering: provider.Offering{
			Key: "id:B", NativeID: "B", WagonID: "wagon-B",
			WagonName: "Wagon B", ClassName: "Class B",
		},
		State: provider.StateReservable, Capacity: intPtr(3),
	})
	return provider.Result{
		ObservedAt: schedulerNow,
		Journeys: []provider.JourneyObservation{{
			Journey: provider.Journey{
				Key: "id:train", NativeID: "train", TrainNumber: "682",
				Operator: "Raja", OriginName: "Yazd", DestinationName: "Mashhad",
				DepartureAt: &departure,
			},
			Offerings: offerings,
		}},
	}
}

func selectedOption() domain.MonitorProviderSelection {
	departure := time.Date(2026, 12, 15, 11, 20, 0, 0, time.UTC)
	return domain.MonitorProviderSelection{
		ConfigVersion: 1,
		Option: provider.SelectedOption{
			JourneyKey: "id:train", OfferingKey: "id:A",
			JourneyNativeID: "train", OfferingNativeID: "A",
			TrainNumber: "682", Operator: "Raja",
			OriginName: "Yazd", DestinationName: "Mashhad",
			DepartureAt: &departure, WagonID: "wagon-A",
			WagonName: "Wagon A", ClassName: "Class A",
		},
		CreatedAt: schedulerNow,
	}
}

func TestSchedulerAppliesOnlySelectedOptions(t *testing.T) {
	adapter := optionProvider{id: "selected", result: optionResult(true)}
	h := newIntegrationHarness(t, adapter)
	h.createMonitor("monitor-selected", true, schedulerNow.Add(24*time.Hour), harnessTarget{
		providerID: "selected", active: true, configVersion: 1,
		originID: "1", destinationID: "2",
		selections: []domain.MonitorProviderSelection{selectedOption()},
	})
	result, err := h.runFor("monitor-selected", "selected")
	if err != nil || !result.Settled {
		t.Fatalf("run = %+v, err=%v", result, err)
	}
	if got := h.count(`SELECT COUNT(*) FROM inventory_states WHERE offering_key = 'id:A'`); got != 1 {
		t.Fatalf("selected inventory rows = %d", got)
	}
	if got := h.count(`SELECT COUNT(*) FROM inventory_states WHERE offering_key = 'id:B'`); got != 0 {
		t.Fatalf("unselected inventory rows = %d", got)
	}
	if got := h.count(`SELECT COUNT(*) FROM events WHERE class_name = 'Class A'`); got != 1 {
		t.Fatalf("selected events = %d", got)
	}
	if got := h.count(`SELECT COUNT(*) FROM events WHERE class_name = 'Class B'`); got != 0 {
		t.Fatalf("unselected events = %d", got)
	}
	if got := h.count(`SELECT COUNT(*) FROM notification_deliveries`); got != 2 {
		t.Fatalf("deliveries = %d", got)
	}
	target := h.target("monitor-selected", "selected")
	check := h.latestCheck(target.ID)
	if check.Outcome != "success" || check.Offerings != 1 || check.Reservable != 1 {
		t.Fatalf("check = %+v", check)
	}
	if _, err := h.runFor("monitor-selected", "selected"); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if got := h.count(`SELECT COUNT(*) FROM events`); got != 1 {
		t.Fatalf("events after repeat = %d", got)
	}
	if got := h.count(`SELECT COUNT(*) FROM notification_deliveries`); got != 2 {
		t.Fatalf("deliveries after repeat = %d", got)
	}
}

func TestSchedulerMissingSelectedOptionBecomesUnknown(t *testing.T) {
	adapter := optionProvider{id: "selected", result: optionResult(false)}
	h := newIntegrationHarness(t, adapter)
	h.createMonitor("monitor-missing", true, schedulerNow.Add(24*time.Hour), harnessTarget{
		providerID: "selected", active: true, configVersion: 1,
		originID: "1", destinationID: "2",
		selections: []domain.MonitorProviderSelection{selectedOption()},
	})
	result, err := h.runFor("monitor-missing", "selected")
	if err != nil || !result.Settled {
		t.Fatalf("run = %+v, err=%v", result, err)
	}
	var state, reason string
	if err := h.sqlDB.QueryRow(`SELECT state, last_reason FROM inventory_states WHERE offering_key = 'id:A'`).Scan(&state, &reason); err != nil {
		t.Fatalf("load selected state: %v", err)
	}
	if state != "unknown" || reason != "selected_option_not_found" {
		t.Fatalf("state/reason = %q/%q", state, reason)
	}
	if got := h.count(`SELECT COUNT(*) FROM events`); got != 0 {
		t.Fatalf("events = %d", got)
	}
	if got := h.count(`SELECT COUNT(*) FROM notification_deliveries`); got != 0 {
		t.Fatalf("deliveries = %d", got)
	}
}

func TestSchedulerRequiresSelectionConfiguration(t *testing.T) {
	adapter := optionProvider{id: "selected", result: optionResult(true)}
	h := newIntegrationHarness(t, adapter)
	h.createMonitor("monitor-no-selection", true, schedulerNow.Add(24*time.Hour), harnessTarget{
		providerID: "selected", active: true, configVersion: 1,
		originID: "1", destinationID: "2",
	})
	result, err := h.runFor("monitor-no-selection", "selected")
	if err != nil || !result.Settled {
		t.Fatalf("run = %+v, err=%v", result, err)
	}
	target := h.target("monitor-no-selection", "selected")
	check := h.latestCheck(target.ID)
	if check.Outcome != "error" || check.ErrorKind.String != "configuration" {
		t.Fatalf("check = %+v", check)
	}
	if got := h.count(`SELECT COUNT(*) FROM inventory_states`); got != 0 {
		t.Fatalf("inventory rows = %d", got)
	}
	if got := h.count(`SELECT COUNT(*) FROM events`); got != 0 {
		t.Fatalf("events = %d", got)
	}
}

func intPtr(value int) *int { return &value }
