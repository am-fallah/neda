package provider_test

import (
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

func TestValidateResultAcceptsValidShapes(t *testing.T) {
	observedAt := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	capacity := 2

	tests := []struct {
		name   string
		result provider.Result
	}{
		{
			name:   "empty",
			result: provider.Result{ObservedAt: observedAt},
		},
		{
			name: "journey without offerings",
			result: provider.Result{
				ObservedAt: observedAt,
				Journeys: []provider.JourneyObservation{{
					Journey: provider.Journey{Key: "journey-1"},
				}},
			},
		},
		{
			name: "valid observation",
			result: provider.Result{
				ObservedAt: observedAt,
				Journeys: []provider.JourneyObservation{{
					Journey: provider.Journey{Key: "journey-1"},
					Offerings: []provider.OfferingObservation{{
						Offering: provider.Offering{Key: "offering-1"},
						State:    provider.StateReservable,
						Capacity: &capacity,
					}},
				}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := provider.ValidateResult(test.result); err != nil {
				t.Fatalf("ValidateResult: %v", err)
			}
		})
	}
}

func TestValidateResultRejectsInvalidShapes(t *testing.T) {
	observedAt := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	negative := -1
	validJourney := provider.JourneyObservation{
		Journey: provider.Journey{Key: "journey-1"},
		Offerings: []provider.OfferingObservation{{
			Offering: provider.Offering{Key: "offering-1"},
			State:    provider.StateReservable,
		}},
	}

	tests := []struct {
		name   string
		result provider.Result
	}{
		{
			name:   "missing observed at",
			result: provider.Result{},
		},
		{
			name: "empty journey key",
			result: provider.Result{
				ObservedAt: observedAt,
				Journeys: []provider.JourneyObservation{{
					Journey: provider.Journey{Key: " "},
				}},
			},
		},
		{
			name: "empty offering key",
			result: provider.Result{
				ObservedAt: observedAt,
				Journeys: []provider.JourneyObservation{{
					Journey: provider.Journey{Key: "journey-1"},
					Offerings: []provider.OfferingObservation{{
						Offering: provider.Offering{Key: ""},
						State:    provider.StateReservable,
					}},
				}},
			},
		},
		{
			name: "invalid state",
			result: provider.Result{
				ObservedAt: observedAt,
				Journeys: []provider.JourneyObservation{{
					Journey: provider.Journey{Key: "journey-1"},
					Offerings: []provider.OfferingObservation{{
						Offering: provider.Offering{Key: "offering-1"},
						State:    provider.State("sold_out"),
					}},
				}},
			},
		},
		{
			name: "negative capacity",
			result: provider.Result{
				ObservedAt: observedAt,
				Journeys: []provider.JourneyObservation{{
					Journey: provider.Journey{Key: "journey-1"},
					Offerings: []provider.OfferingObservation{{
						Offering: provider.Offering{Key: "offering-1"},
						State:    provider.StateReservable,
						Capacity: &negative,
					}},
				}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := provider.ValidateResult(test.result); err == nil {
				t.Fatal("ValidateResult succeeded, want error")
			}
		})
	}

	if err := provider.ValidateResult(provider.Result{
		ObservedAt: observedAt,
		Journeys:   []provider.JourneyObservation{validJourney},
	}); err != nil {
		t.Fatalf("baseline valid journey rejected: %v", err)
	}
}

func TestValidateResultEnforcesCaps(t *testing.T) {
	observedAt := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)

	tooManyJourneys := make([]provider.JourneyObservation, provider.MaxJourneys+1)
	for i := range tooManyJourneys {
		tooManyJourneys[i] = provider.JourneyObservation{Journey: provider.Journey{Key: "j"}}
	}
	if err := provider.ValidateResult(provider.Result{ObservedAt: observedAt, Journeys: tooManyJourneys}); err == nil {
		t.Fatal("journey cap was not enforced")
	}

	offerings := make([]provider.OfferingObservation, provider.MaxOfferingsPerJourney+1)
	for i := range offerings {
		offerings[i] = provider.OfferingObservation{
			Offering: provider.Offering{Key: "o"},
			State:    provider.StateUnknown,
		}
	}
	if err := provider.ValidateResult(provider.Result{
		ObservedAt: observedAt,
		Journeys: []provider.JourneyObservation{{
			Journey:   provider.Journey{Key: "j"},
			Offerings: offerings,
		}},
	}); err == nil {
		t.Fatal("per-journey offering cap was not enforced")
	}

	journeys := make([]provider.JourneyObservation, 11)
	for i := range journeys {
		batch := make([]provider.OfferingObservation, 46)
		for j := range batch {
			batch[j] = provider.OfferingObservation{
				Offering: provider.Offering{Key: "o"},
				State:    provider.StateUnknown,
			}
		}
		journeys[i] = provider.JourneyObservation{
			Journey:   provider.Journey{Key: "j"},
			Offerings: batch,
		}
	}
	if err := provider.ValidateResult(provider.Result{ObservedAt: observedAt, Journeys: journeys}); err == nil {
		t.Fatal("total offering cap was not enforced")
	}
}
