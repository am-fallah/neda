package mrbilit

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

const maxWarnings = 20

type rawTrain struct {
	ID              optionalID      `json:"id"`
	FromName        optionalString  `json:"fromName"`
	ToName          optionalString  `json:"toName"`
	TrainNumber     optionalString  `json:"trainNumber"`
	DepartureTime   optionalString  `json:"departureTime"`
	ArrivalTime     optionalString  `json:"arrivalTime"`
	ProviderName    optionalString  `json:"providerName"`
	CorporationName optionalString  `json:"corporationName"`
	Classes         json.RawMessage `json:"classes"`
}

type rawOffering struct {
	ID                   optionalID     `json:"id"`
	WagonID              optionalID     `json:"wagonID"`
	WagonName            optionalString `json:"wagonName"`
	ShortTitle           optionalString `json:"shortTitle"`
	CompartmentType      optionalString `json:"compartmentType"`
	Capacity             optionalInt    `json:"capacity"`
	IsAvailable          optionalBool   `json:"isAvailable"`
	ReservationAvailable optionalBool   `json:"reservationAvailable"`
}

type warningSet struct {
	seen   map[string]struct{}
	values []string
}

func newWarningSet() *warningSet {
	return &warningSet{seen: make(map[string]struct{})}
}

func (w *warningSet) add(category string) {
	if category == "" {
		return
	}
	if _, exists := w.seen[category]; exists {
		return
	}
	if len(w.values) >= maxWarnings {
		return
	}
	w.seen[category] = struct{}{}
	w.values = append(w.values, category)
}

func (w *warningSet) list() []string {
	if len(w.values) == 0 {
		return nil
	}
	return append([]string(nil), w.values...)
}

// parseResponse decodes the explicit synthetic response contract. The real
// MrBilit envelope and offering-container key are unverified: this parser fails
// closed on any other top-level shape rather than guessing.
func parseSyntheticResponse(payload []byte, req provider.CheckRequest, observedAt time.Time) (provider.Result, error) {
	warnings := newWarningSet()
	decoder := json.NewDecoder(bytes.NewReader(payload))
	token, err := decoder.Token()
	if err != nil {
		return provider.Result{}, parseError("invalid_json")
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '[' {
		return provider.Result{}, parseError("envelope_shape_unverified")
	}

	journeys := make([]provider.JourneyObservation, 0)
	journeyIndex := make(map[string]int)
	totalOfferings := 0
	trainCount := 0
	for decoder.More() {
		if trainCount >= provider.MaxJourneys {
			return provider.Result{}, parseError("too_many_journeys")
		}
		trainCount++
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return provider.Result{}, parseError("invalid_train")
		}
		journey, offeringCount, err := parseTrain(raw, req, warnings)
		if err != nil {
			return provider.Result{}, err
		}
		if totalOfferings+offeringCount > provider.MaxOfferings {
			return provider.Result{}, parseError("too_many_offerings")
		}
		if existing, exists := journeyIndex[journey.Journey.Key]; exists {
			if !sameJourney(journeys[existing].Journey, journey.Journey) {
				return provider.Result{}, parseError("duplicate_journey_key")
			}
			merged, err := mergeOfferings(journeys[existing].Offerings, journey.Offerings, warnings)
			if err != nil {
				return provider.Result{}, err
			}
			journeys[existing].Offerings = merged
			warnings.add("duplicate_journey_key")
			totalOfferings += offeringCount
			continue
		}
		journeyIndex[journey.Journey.Key] = len(journeys)
		journeys = append(journeys, journey)
		totalOfferings += offeringCount
	}
	closing, err := decoder.Token()
	if err != nil {
		return provider.Result{}, parseError("invalid_envelope_end")
	}
	if delim, ok := closing.(json.Delim); !ok || delim != ']' {
		return provider.Result{}, parseError("invalid_envelope_end")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return provider.Result{}, parseError("trailing_data")
	}

	return provider.Result{
		Journeys:   journeys,
		Warnings:   warnings.list(),
		ObservedAt: observedAt.UTC(),
	}, nil
}

func parseTrain(raw json.RawMessage, req provider.CheckRequest, warnings *warningSet) (provider.JourneyObservation, int, error) {
	var train rawTrain
	if err := json.Unmarshal(raw, &train); err != nil {
		return provider.JourneyObservation{}, 0, parseError("invalid_train")
	}
	if train.ID.Unexpected {
		warnings.add("unexpected_train_id_type")
	}
	if train.TrainNumber.Unexpected {
		warnings.add("unexpected_train_number_type")
	}
	if train.FromName.Unexpected || train.ToName.Unexpected || train.ProviderName.Unexpected || train.CorporationName.Unexpected {
		warnings.add("unexpected_display_field_type")
	}

	departure, depWarning := parseTimeField(train.DepartureTime)
	if depWarning != "" {
		warnings.add(depWarning)
	}
	arrival, arrWarning := parseTimeField(train.ArrivalTime)
	if arrWarning != "" {
		warnings.add(arrWarning)
	}

	operator := normalizeText(train.CorporationName.Value)
	if operator == "" {
		operator = normalizeText(train.ProviderName.Value)
	}
	originName := normalizeText(train.FromName.Value)
	if originName == "" {
		originName = normalizeText(req.Route.OriginName)
	}
	destinationName := normalizeText(train.ToName.Value)
	if destinationName == "" {
		destinationName = normalizeText(req.Route.DestinationName)
	}
	journey := provider.Journey{
		NativeID:        nativeIDValue(train.ID),
		TrainNumber:     normalizeText(train.TrainNumber.Value),
		Operator:        operator,
		OriginName:      originName,
		DestinationName: destinationName,
		DepartureAt:     departure,
		ArrivalAt:       arrival,
	}
	key, err := journeyKey(journey, req.DepartureDate, req.Config.OriginID, req.Config.DestinationID)
	if err != nil {
		return provider.JourneyObservation{}, 0, parseError("journey_identity_insufficient")
	}
	journey.Key = key

	offerings, elementCount, err := parseOfferings(train.Classes, req, warnings)
	if err != nil {
		return provider.JourneyObservation{}, 0, err
	}
	return provider.JourneyObservation{Journey: journey, Offerings: offerings}, elementCount, nil
}

func mergeOfferings(existing, incoming []provider.OfferingObservation, warnings *warningSet) ([]provider.OfferingObservation, error) {
	recognized := make(map[string]int, len(existing))
	for index, observation := range existing {
		recognized[observation.Offering.Key] = index
	}
	for _, observation := range incoming {
		if index, exists := recognized[observation.Offering.Key]; exists {
			if sameOfferingObservation(existing[index], observation) {
				warnings.add("duplicate_offering_key")
				continue
			}
			existing[index].State = provider.StateUnknown
			existing[index].Reason = "duplicate_offering_key"
			existing[index].Capacity = nil
			warnings.add("duplicate_offering_key")
			continue
		}
		recognized[observation.Offering.Key] = len(existing)
		existing = append(existing, observation)
	}
	return existing, nil
}

func parseOfferings(raw json.RawMessage, req provider.CheckRequest, warnings *warningSet) ([]provider.OfferingObservation, int, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		warnings.add("offerings_missing")
		return []provider.OfferingObservation{}, 0, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil, 0, parseError("invalid_offerings")
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '[' {
		return nil, 0, parseError("offerings_shape_unverified")
	}

	offerings := make([]provider.OfferingObservation, 0)
	elementCount := 0
	for decoder.More() {
		if elementCount >= provider.MaxOfferingsPerJourney {
			return nil, 0, parseError("too_many_offerings")
		}
		elementCount++
		var rawOffering json.RawMessage
		if err := decoder.Decode(&rawOffering); err != nil {
			return nil, 0, parseError("invalid_offering")
		}
		observation, ok, identityWarning := parseOffering(rawOffering, req.Passengers)
		if identityWarning != "" {
			warnings.add(identityWarning)
		}
		if !ok {
			warnings.add("offering_identity_insufficient")
			continue
		}
		merged, err := mergeOfferings(offerings, []provider.OfferingObservation{observation}, warnings)
		if err != nil {
			return nil, 0, err
		}
		offerings = merged
	}
	closing, err := decoder.Token()
	if err != nil {
		return nil, 0, parseError("invalid_offerings_end")
	}
	if delim, ok := closing.(json.Delim); !ok || delim != ']' {
		return nil, 0, parseError("invalid_offerings_end")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, 0, parseError("trailing_offering_data")
	}
	return offerings, elementCount, nil
}

func parseOffering(raw json.RawMessage, passengers provider.Passengers) (provider.OfferingObservation, bool, string) {
	var offering rawOffering
	if err := json.Unmarshal(raw, &offering); err != nil {
		return provider.OfferingObservation{}, false, "invalid_offering"
	}
	identityWarning := ""
	if offering.ID.Unexpected || offering.WagonID.Unexpected || offering.WagonName.Unexpected ||
		offering.ShortTitle.Unexpected || offering.CompartmentType.Unexpected {
		identityWarning = "unexpected_offer_identity_type"
	}
	model := provider.Offering{
		NativeID:  nativeIDValue(offering.ID),
		WagonName: normalizeText(offering.WagonName.Value),
		ClassName: normalizeText(offering.ShortTitle.Value),
	}
	key, ok := offeringKey(model, offering.WagonID.Value, offering.CompartmentType.Value, offering.ShortTitle.Value)
	if !ok {
		return provider.OfferingObservation{}, false, identityWarning
	}
	model.Key = key

	requiredSeats := passengers.Adults + passengers.Children + passengers.Infants
	state, reason, capacity := classifyOffering(offering.Capacity, offering.IsAvailable, offering.ReservationAvailable, requiredSeats)
	return provider.OfferingObservation{
		Offering: model,
		State:    state,
		Reason:   reason,
		Capacity: capacity,
	}, true, identityWarning
}

func parseTimeField(field optionalString) (*time.Time, string) {
	if !field.Present {
		return nil, ""
	}
	if field.Unexpected {
		return nil, "unexpected_time_type"
	}
	if !field.Valid || strings.TrimSpace(field.Value) == "" {
		return nil, ""
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(field.Value))
	if err != nil {
		return nil, "unparseable_time"
	}
	utc := parsed.UTC()
	return &utc, ""
}

func nativeIDValue(field optionalID) string {
	if !field.Valid {
		return ""
	}
	return normalizeNativeID(field.Value)
}

func sameJourney(a, b provider.Journey) bool {
	return a.Key == b.Key &&
		a.NativeID == b.NativeID &&
		a.TrainNumber == b.TrainNumber &&
		a.Operator == b.Operator &&
		a.OriginName == b.OriginName &&
		a.DestinationName == b.DestinationName &&
		equalTime(a.DepartureAt, b.DepartureAt) &&
		equalTime(a.ArrivalAt, b.ArrivalAt)
}

func sameOfferingObservation(a, b provider.OfferingObservation) bool {
	return a.Offering.Key == b.Offering.Key &&
		a.Offering.NativeID == b.Offering.NativeID &&
		a.Offering.WagonName == b.Offering.WagonName &&
		a.Offering.ClassName == b.Offering.ClassName &&
		a.State == b.State &&
		a.Reason == b.Reason &&
		equalIntPointer(a.Capacity, b.Capacity) &&
		a.BookingURL == nil && b.BookingURL == nil
}

func equalTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func equalIntPointer(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func parseError(reason string) error {
	return &provider.Error{
		Kind: provider.ErrInvalidResponse,
		Op:   "mrbilit.parse",
		Err:  errors.New(reason),
	}
}
