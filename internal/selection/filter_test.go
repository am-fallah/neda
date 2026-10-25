package selection

import (
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

var testDeparture = time.Date(2026, 11, 5, 11, 20, 0, 0, time.UTC)

func testResult() provider.Result {
	return provider.Result{
		Journeys: []provider.JourneyObservation{
			{
				Journey: provider.Journey{
					Key: "id:train-1", NativeID: "train-1", TrainNumber: "682",
					Operator: "Raja", OriginName: "Yazd", DestinationName: "Mashhad",
					DepartureAt: &testDeparture,
				},
				Offerings: []provider.OfferingObservation{
					{
						Offering: provider.Offering{
							Key: "id:class-1", NativeID: "class-1", WagonID: "wagon-1",
							WagonName: "Sepehr", ClassName: "Four star", CompartmentType: "4-person",
						},
						State: provider.StateReservable, Capacity: intPtr(3),
					},
					{
						Offering: provider.Offering{
							Key: "id:class-2", NativeID: "class-2", WagonID: "wagon-2",
							WagonName: "Samen", ClassName: "Five star", CompartmentType: "4-person",
						},
						State: provider.StateNotReservable, Capacity: intPtr(0),
					},
				},
			},
		},
		ObservedAt: testDeparture,
	}
}

func selectedClassOne() provider.SelectedOption {
	return provider.SelectedOption{
		JourneyKey: "id:train-1", OfferingKey: "id:class-1",
		JourneyNativeID: "train-1", OfferingNativeID: "class-1",
		TrainNumber: "682", Operator: "Raja", OriginName: "Yazd", DestinationName: "Mashhad",
		DepartureAt: &testDeparture, WagonID: "wagon-1", WagonName: "Sepehr",
		ClassName: "Four star", CompartmentType: "4-person",
	}
}

func TestFilterKeepsOnlySelectedExactOptions(t *testing.T) {
	filtered, warnings := Filter(testResult(), []provider.SelectedOption{selectedClassOne()})
	if len(filtered.Journeys) != 1 || len(filtered.Journeys[0].Offerings) != 1 {
		t.Fatalf("filtered = %+v", filtered)
	}
	offering := filtered.Journeys[0].Offerings[0]
	if offering.Offering.Key != "id:class-1" || offering.State != provider.StateReservable {
		t.Fatalf("offering = %+v", offering)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestFilterMissingSelectionBecomesUnknown(t *testing.T) {
	selected := selectedClassOne()
	selected.JourneyKey = "id:missing-train"
	selected.JourneyNativeID = "missing-train"
	selected.OfferingKey = "id:missing-class"
	selected.OfferingNativeID = "missing-class"
	selected.TrainNumber = "999"
	filtered, warnings := Filter(testResult(), []provider.SelectedOption{selected})
	if len(filtered.Journeys) != 1 || len(filtered.Journeys[0].Offerings) != 1 {
		t.Fatalf("filtered = %+v", filtered)
	}
	offering := filtered.Journeys[0].Offerings[0]
	if offering.State != provider.StateUnknown || offering.Reason != "selected_option_not_found" {
		t.Fatalf("offering = %+v", offering)
	}
	if offering.Capacity != nil {
		t.Fatalf("capacity = %v", offering.Capacity)
	}
	if len(warnings) != 1 || warnings[0] != "selected_option_not_found" {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestFilterAmbiguousFallbackBecomesUnknown(t *testing.T) {
	result := testResult()
	second := result.Journeys[0]
	second.Journey.Key = "id:train-2"
	second.Journey.NativeID = "train-2"
	second.Offerings = append([]provider.OfferingObservation(nil), result.Journeys[0].Offerings...)
	second.Offerings[0].Offering.Key = "id:class-3"
	second.Offerings[0].Offering.NativeID = "class-3"
	result.Journeys = append(result.Journeys, second)

	selected := selectedClassOne()
	selected.JourneyKey = "id:old-train"
	selected.OfferingKey = "id:old-class"
	selected.JourneyNativeID = "old-train"
	selected.OfferingNativeID = "old-class"
	filtered, warnings := Filter(result, []provider.SelectedOption{selected})
	offering := filtered.Journeys[0].Offerings[0]
	if offering.State != provider.StateUnknown || offering.Reason != "selected_option_ambiguous" {
		t.Fatalf("offering = %+v", offering)
	}
	if len(warnings) != 1 || warnings[0] != "selected_option_ambiguous" {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestFilterExactKeyWithConflictingMetadataBecomesUnknown(t *testing.T) {
	selected := selectedClassOne()
	selected.TrainNumber = "999"
	filtered, warnings := Filter(testResult(), []provider.SelectedOption{selected})
	offering := filtered.Journeys[0].Offerings[0]
	if offering.State != provider.StateUnknown || offering.Reason != "selected_option_identity_conflict" {
		t.Fatalf("offering = %+v", offering)
	}
	if len(warnings) != 1 || warnings[0] != "selected_option_identity_conflict" {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestFilterReliableFallbackKeepsSelectedKeys(t *testing.T) {
	selected := selectedClassOne()
	selected.JourneyKey = "id:old-train"
	selected.OfferingKey = "id:old-class"
	selected.JourneyNativeID = "old-train"
	selected.OfferingNativeID = "old-class"
	filtered, warnings := Filter(testResult(), []provider.SelectedOption{selected})
	journey := filtered.Journeys[0]
	offering := journey.Offerings[0]
	if journey.Journey.Key != "id:old-train" || offering.Offering.Key != "id:old-class" {
		t.Fatalf("keys were not retained: %+v / %+v", journey.Journey, offering.Offering)
	}
	if offering.State != provider.StateReservable {
		t.Fatalf("state = %s", offering.State)
	}
	if len(warnings) != 1 || warnings[0] != "selected_option_matched_by_fallback" {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestFilterDuplicateFallbackMatchBecomesUnknown(t *testing.T) {
	first := selectedClassOne()
	first.JourneyKey = "id:old-train-a"
	first.OfferingKey = "id:old-class-a"
	first.JourneyNativeID = "old-train-a"
	first.OfferingNativeID = "old-class-a"
	second := first
	second.JourneyKey = "id:old-train-b"
	second.OfferingKey = "id:old-class-b"
	second.JourneyNativeID = "old-train-b"
	second.OfferingNativeID = "old-class-b"
	filtered, warnings := Filter(testResult(), []provider.SelectedOption{first, second})
	for _, journey := range filtered.Journeys {
		for _, offering := range journey.Offerings {
			if offering.State != provider.StateUnknown || offering.Reason != "selected_option_ambiguous" {
				t.Fatalf("offering = %+v", offering)
			}
		}
	}
	if len(warnings) != 2 || warnings[0] != "selected_option_matched_by_fallback" || warnings[1] != "selected_option_ambiguous" {
		t.Fatalf("warnings = %v", warnings)
	}
}

func intPtr(value int) *int {
	return &value
}
