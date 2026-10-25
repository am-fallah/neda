// Package selection filters a provider result down to the train/class options
// explicitly selected by the user. Missing or ambiguous matches become Unknown;
// they are never converted into NotReservable.
package selection

import (
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

const departureTolerance = 2 * time.Minute

type match struct {
	selection   provider.SelectedOption
	journey     provider.Journey
	offering    provider.OfferingObservation
	reason      string
	matched     bool
	identityKey string
}

// Filter returns only selected options. Unselected journeys/classes are
// dropped. The returned warnings are bounded, sanitized categories.
func Filter(result provider.Result, selections []provider.SelectedOption) (provider.Result, []string) {
	filtered := provider.Result{ObservedAt: result.ObservedAt}
	warnings := newWarnings()
	for _, warning := range result.Warnings {
		warnings.add(warning)
	}
	if len(selections) == 0 {
		return filtered, warnings.list()
	}

	journeys := make([]provider.JourneyObservation, 0, len(selections))
	matched := make([]match, 0, len(selections))
	for _, selected := range selections {
		item := match{selection: selected, reason: "selected_option_not_found"}
		journey, offering, reason, ok := findSelected(result, selected)
		if ok {
			item.journey = journey.Journey
			item.offering = offering
			item.matched = true
			item.identityKey = journey.Journey.Key + "\x00" + offering.Offering.Key
			if reason != "" {
				item.reason = reason
				warnings.add(reason)
			}
		} else {
			item.reason = reason
			warnings.add(reason)
			item.journey = journeyFromSelection(selected)
			item.offering = unknownOffering(selected, reason)
		}
		matched = append(matched, item)
	}

	// A fallback match must not satisfy two different selections.
	used := make(map[string][]int)
	for index, item := range matched {
		if item.matched {
			used[item.identityKey] = append(used[item.identityKey], index)
		}
	}
	for _, indexes := range used {
		if len(indexes) < 2 {
			continue
		}
		for _, index := range indexes {
			item := &matched[index]
			item.matched = false
			item.reason = "selected_option_ambiguous"
			item.journey = journeyFromSelection(item.selection)
			item.offering = unknownOffering(item.selection, item.reason)
		}
		warnings.add("selected_option_ambiguous")
	}

	groups := make(map[string]int)
	for _, item := range matched {
		journey := provider.JourneyObservation{
			Journey:   item.journey,
			Offerings: []provider.OfferingObservation{item.offering},
		}
		journey.Journey.Key = item.selection.JourneyKey
		journey.Offerings[0].Offering.Key = item.selection.OfferingKey
		if item.reason != "" {
			journey.Offerings[0].Reason = item.reason
		}
		if index, exists := groups[item.selection.JourneyKey]; exists {
			journeys[index].Offerings = append(journeys[index].Offerings, journey.Offerings[0])
			continue
		}
		groups[item.selection.JourneyKey] = len(journeys)
		journeys = append(journeys, journey)
	}
	filtered.Journeys = journeys
	filtered.Warnings = warnings.list()
	return filtered, filtered.Warnings
}

func findSelected(result provider.Result, selected provider.SelectedOption) (provider.JourneyObservation, provider.OfferingObservation, string, bool) {
	exactJourney, exactJourneyOK := findJourneyByKey(result, selected.JourneyKey)
	if exactJourneyOK {
		if journeyConflicts(selected, exactJourney.Journey) {
			return provider.JourneyObservation{}, provider.OfferingObservation{}, "selected_option_identity_conflict", false
		}
		exactOffering, exactOfferingOK := findOfferingByKey(exactJourney, selected.OfferingKey)
		if exactOfferingOK {
			if selectionConflicts(selected, exactJourney.Journey, exactOffering.Offering) {
				return provider.JourneyObservation{}, provider.OfferingObservation{}, "selected_option_identity_conflict", false
			}
			return exactJourney, exactOffering, "", true
		}
	}

	journey, reason, ok := fallbackJourney(result, selected)
	if !ok {
		return provider.JourneyObservation{}, provider.OfferingObservation{}, reason, false
	}
	offering, reason, ok := fallbackOffering(journey, selected)
	if !ok {
		return provider.JourneyObservation{}, provider.OfferingObservation{}, reason, false
	}
	return journey, offering, "selected_option_matched_by_fallback", true
}

func findJourneyByKey(result provider.Result, key string) (provider.JourneyObservation, bool) {
	for _, journey := range result.Journeys {
		if journey.Journey.Key == key {
			return journey, true
		}
	}
	return provider.JourneyObservation{}, false
}

func findOfferingByKey(journey provider.JourneyObservation, key string) (provider.OfferingObservation, bool) {
	for _, offering := range journey.Offerings {
		if offering.Offering.Key == key {
			return offering, true
		}
	}
	return provider.OfferingObservation{}, false
}

func journeyConflicts(selected provider.SelectedOption, journey provider.Journey) bool {
	if selected.TrainNumber != "" && journey.TrainNumber != "" && !strings.EqualFold(selected.TrainNumber, journey.TrainNumber) {
		return true
	}
	if selected.Operator != "" && journey.Operator != "" && !strings.EqualFold(selected.Operator, journey.Operator) {
		return true
	}
	return !sameTimeWithin(selected.DepartureAt, journey.DepartureAt, departureTolerance)
}

func selectionConflicts(selected provider.SelectedOption, journey provider.Journey, offering provider.Offering) bool {
	if journeyConflicts(selected, journey) {
		return true
	}
	if selected.WagonID != "" && offering.WagonID != "" && !strings.EqualFold(selected.WagonID, offering.WagonID) {
		return true
	}
	if selected.WagonName != "" && offering.WagonName != "" && !strings.EqualFold(selected.WagonName, offering.WagonName) {
		return true
	}
	if selected.ClassName != "" && offering.ClassName != "" && !strings.EqualFold(selected.ClassName, offering.ClassName) {
		return true
	}
	return false
}

func fallbackJourney(result provider.Result, selected provider.SelectedOption) (provider.JourneyObservation, string, bool) {
	candidates := make([]provider.JourneyObservation, 0)
	for _, journey := range result.Journeys {
		nativeMatch := selected.JourneyNativeID != "" && journey.Journey.NativeID != "" &&
			selected.JourneyNativeID == journey.Journey.NativeID
		trainMatch := selected.TrainNumber != "" && journey.Journey.TrainNumber != "" &&
			strings.EqualFold(selected.TrainNumber, journey.Journey.TrainNumber)
		timeMatch := sameTimeWithin(selected.DepartureAt, journey.Journey.DepartureAt, departureTolerance)
		operatorMatch := selected.Operator != "" && journey.Journey.Operator != "" &&
			strings.EqualFold(selected.Operator, journey.Journey.Operator)
		if nativeMatch || (trainMatch && (timeMatch || operatorMatch)) {
			candidates = append(candidates, journey)
		}
	}
	switch len(candidates) {
	case 0:
		return provider.JourneyObservation{}, "selected_option_not_found", false
	case 1:
		return candidates[0], "", true
	default:
		return provider.JourneyObservation{}, "selected_option_ambiguous", false
	}
}

func fallbackOffering(journey provider.JourneyObservation, selected provider.SelectedOption) (provider.OfferingObservation, string, bool) {
	candidates := make([]provider.OfferingObservation, 0)
	for _, offering := range journey.Offerings {
		nativeMatch := selected.OfferingNativeID != "" && offering.Offering.NativeID != "" &&
			selected.OfferingNativeID == offering.Offering.NativeID
		attributes := 0
		if selected.WagonID != "" && offering.Offering.WagonID != "" &&
			strings.EqualFold(selected.WagonID, offering.Offering.WagonID) {
			attributes++
		}
		if selected.WagonName != "" && offering.Offering.WagonName != "" &&
			strings.EqualFold(selected.WagonName, offering.Offering.WagonName) {
			attributes++
		}
		if selected.ClassName != "" && offering.Offering.ClassName != "" &&
			strings.EqualFold(selected.ClassName, offering.Offering.ClassName) {
			attributes++
		}
		if selected.CompartmentType != "" && offering.Offering.CompartmentType != "" &&
			strings.EqualFold(selected.CompartmentType, offering.Offering.CompartmentType) {
			attributes++
		}
		classIdentity := selected.ClassName != "" || selected.WagonName != ""
		if nativeMatch || (classIdentity && attributes >= 2) {
			candidates = append(candidates, offering)
		}
	}
	switch len(candidates) {
	case 0:
		return provider.OfferingObservation{}, "selected_option_not_found", false
	case 1:
		return candidates[0], "", true
	default:
		return provider.OfferingObservation{}, "selected_option_ambiguous", false
	}
}

func sameTimeWithin(left, right *time.Time, tolerance time.Duration) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	difference := left.Sub(*right)
	if difference < 0 {
		difference = -difference
	}
	return difference <= tolerance
}

func journeyFromSelection(selected provider.SelectedOption) provider.Journey {
	return provider.Journey{
		Key:             selected.JourneyKey,
		NativeID:        selected.JourneyNativeID,
		TrainNumber:     selected.TrainNumber,
		Operator:        selected.Operator,
		OriginName:      selected.OriginName,
		DestinationName: selected.DestinationName,
		DepartureAt:     selected.DepartureAt,
		ArrivalAt:       selected.ArrivalAt,
	}
}

func unknownOffering(selected provider.SelectedOption, reason string) provider.OfferingObservation {
	return provider.OfferingObservation{
		Offering: provider.Offering{
			Key:             selected.OfferingKey,
			NativeID:        selected.OfferingNativeID,
			WagonID:         selected.WagonID,
			WagonName:       selected.WagonName,
			ClassName:       selected.ClassName,
			CompartmentType: selected.CompartmentType,
		},
		State:  provider.StateUnknown,
		Reason: reason,
	}
}

type warningSet struct {
	seen   map[string]struct{}
	values []string
}

func newWarnings() *warningSet {
	return &warningSet{seen: make(map[string]struct{})}
}

func (w *warningSet) add(value string) {
	if value == "" {
		return
	}
	if _, exists := w.seen[value]; exists {
		return
	}
	if len(w.values) >= 20 {
		return
	}
	w.seen[value] = struct{}{}
	w.values = append(w.values, value)
}

func (w *warningSet) list() []string {
	if len(w.values) == 0 {
		return nil
	}
	return append([]string(nil), w.values...)
}
