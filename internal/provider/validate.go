package provider

import (
	"errors"
	"fmt"
	"strings"
)

// Global normalized-result bounds. Provider adapters must enforce these before
// allocating observations; ValidateResult is a core fail-closed defense that
// keeps an oversized or structurally invalid result out of the outcome
// transaction.
const (
	MaxJourneys            = 100
	MaxOfferingsPerJourney = 50
	MaxOfferings           = 500
)

// ValidateResult checks the structural invariants of a successful normalized
// provider result. A valid empty result has a non-zero ObservedAt and zero
// journeys. A journey with zero offerings is valid and represents missing
// observations. Duplicate/conflicting keys are intentionally not rejected here;
// the availability applier resolves them conservatively.
func ValidateResult(result Result) error {
	if result.ObservedAt.IsZero() {
		return errors.New("provider: result observed_at is required")
	}
	if len(result.Journeys) > MaxJourneys {
		return fmt.Errorf("provider: result has %d journeys, maximum is %d", len(result.Journeys), MaxJourneys)
	}

	totalOfferings := 0
	for journeyIndex, journey := range result.Journeys {
		if strings.TrimSpace(journey.Journey.Key) == "" {
			return fmt.Errorf("provider: journey %d has an empty key", journeyIndex)
		}
		if len(journey.Offerings) > MaxOfferingsPerJourney {
			return fmt.Errorf("provider: journey %d has %d offerings, maximum is %d", journeyIndex, len(journey.Offerings), MaxOfferingsPerJourney)
		}
		totalOfferings += len(journey.Offerings)
		if totalOfferings > MaxOfferings {
			return fmt.Errorf("provider: result has more than %d offerings", MaxOfferings)
		}
		for offeringIndex, offering := range journey.Offerings {
			if strings.TrimSpace(offering.Offering.Key) == "" {
				return fmt.Errorf("provider: journey %d offering %d has an empty key", journeyIndex, offeringIndex)
			}
			if !offering.State.Valid() {
				return fmt.Errorf("provider: journey %d offering %d has an invalid state", journeyIndex, offeringIndex)
			}
			if offering.Capacity != nil && *offering.Capacity < 0 {
				return fmt.Errorf("provider: journey %d offering %d has negative capacity", journeyIndex, offeringIndex)
			}
		}
	}
	return nil
}
