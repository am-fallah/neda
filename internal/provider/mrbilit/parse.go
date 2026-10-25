package mrbilit

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

const maxWarnings = 20

type rawTrain struct {
	ID              optionalID      `json:"id"`
	From            optionalID      `json:"from"`
	To              optionalID      `json:"to"`
	FromName        optionalString  `json:"fromName"`
	ToName          optionalString  `json:"toName"`
	TrainNumber     optionalID      `json:"trainNumber"`
	DepartureTime   optionalString  `json:"departureTime"`
	ArrivalTime     optionalString  `json:"arrivalTime"`
	ProviderName    optionalString  `json:"providerName"`
	CorporationName optionalString  `json:"corporationName"`
	Classes         json.RawMessage `json:"classes"`
	Prices          json.RawMessage `json:"prices"`
}

// rawPriceGroup is the real MrBilit prices[] element. sellType is preserved
// only as sanitized metadata; no availability decision depends on it.
type rawPriceGroup struct {
	SellType optionalInt     `json:"sellType"`
	Classes  json.RawMessage `json:"classes"`
}

type rawMeta struct {
	RouteNotFound  optionalBool `json:"routeNotFound"`
	AlwaysHasTrain optionalBool `json:"alwaysHasTrain"`
}

type rawLocation struct {
	Title optionalString `json:"title"`
}

type rawRealEnvelope struct {
	Trains       json.RawMessage `json:"trains"`
	Meta         json.RawMessage `json:"meta"`
	FromLocation rawLocation     `json:"fromLocation"`
	ToLocation   rawLocation     `json:"toLocation"`
}

type rawOffering struct {
	ID                   optionalID     `json:"id"`
	WagonID              optionalID     `json:"wagonID"`
	WagonName            optionalString `json:"wagonName"`
	ShortTitle           optionalString `json:"shortTitle"`
	ClassLabel           optionalString `json:"class"`
	CompartmentType      optionalString `json:"compartmentType"`
	Capacity             optionalInt    `json:"capacity"`
	IsAvailable          optionalBool   `json:"isAvailable"`
	ReservationAvailable optionalBool   `json:"reservationAvailable"`
	Price                optionalInt    `json:"price"`
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

// parseResponse dispatches on the observed envelope shape. The real MrBilit
// contract is a JSON object with trains[] and nested prices[].classes[]. The
// top-level-array form is retained only for the original synthetic fixtures.
// Any other shape is an invalid response, never an empty result.
func parseResponse(payload []byte, req provider.CheckRequest, observedAt time.Time) (provider.Result, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return provider.Result{}, parseError("empty_body")
	}
	switch trimmed[0] {
	case '[':
		return parseSyntheticResponse(payload, req, observedAt)
	case '{':
		return parseRealResponse(payload, req, observedAt)
	default:
		return provider.Result{}, parseError("envelope_shape_unverified")
	}
}

// parseRealResponse decodes the observed production envelope. It requires a
// trains array; a missing/null trains field is an invalid response. Empty
// trains[] is a valid empty observation. meta.routeNotFound=true is a typed
// route error, never a sold-out verdict.
func parseRealResponse(payload []byte, req provider.CheckRequest, observedAt time.Time) (provider.Result, error) {
	var envelope rawRealEnvelope
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&envelope); err != nil {
		return provider.Result{}, parseError("invalid_json")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return provider.Result{}, parseError("trailing_data")
	}
	warnings := newWarningSet()
	if len(envelope.Meta) > 0 && !bytes.Equal(bytes.TrimSpace(envelope.Meta), []byte("null")) {
		var meta rawMeta
		if err := json.Unmarshal(envelope.Meta, &meta); err != nil {
			return provider.Result{}, parseError("invalid_meta")
		}
		if meta.RouteNotFound.Unexpected || meta.AlwaysHasTrain.Unexpected {
			return provider.Result{}, parseError("meta_unexpected_type")
		}
		if meta.RouteNotFound.Valid && meta.RouteNotFound.Value {
			return provider.Result{}, &provider.Error{
				Kind: provider.ErrRouteNotFound,
				Op:   "mrbilit.parse",
				Err:  errors.New("route not found"),
			}
		}
		if meta.AlwaysHasTrain.Valid && meta.AlwaysHasTrain.Value {
			warnings.add("meta_always_has_train")
		}
	} else {
		warnings.add("meta_missing")
	}
	if len(envelope.Trains) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Trains), []byte("null")) {
		return provider.Result{}, parseError("trains_missing")
	}
	var rawTrains []json.RawMessage
	if err := json.Unmarshal(envelope.Trains, &rawTrains); err != nil {
		return provider.Result{}, parseError("trains_shape_unverified")
	}
	if len(rawTrains) > provider.MaxJourneys {
		return provider.Result{}, parseError("too_many_journeys")
	}
	journeys := make([]provider.JourneyObservation, 0, len(rawTrains))
	journeyIndex := make(map[string]int)
	totalOfferings := 0
	for _, raw := range rawTrains {
		journey, offeringCount, err := parseRealTrain(raw, req, warnings)
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
	return provider.Result{
		Journeys:   journeys,
		Warnings:   warnings.list(),
		ObservedAt: observedAt.UTC(),
	}, nil
}

func parseRealTrain(raw json.RawMessage, req provider.CheckRequest, warnings *warningSet) (provider.JourneyObservation, int, error) {
	var train rawTrain
	if err := json.Unmarshal(raw, &train); err != nil {
		return provider.JourneyObservation{}, 0, parseError("invalid_train")
	}
	if err := validateTrainRoute(train, req); err != nil {
		return provider.JourneyObservation{}, 0, err
	}
	journey, err := buildJourney(train, req, warnings)
	if err != nil {
		return provider.JourneyObservation{}, 0, err
	}
	offerings := make([]provider.OfferingObservation, 0)
	total := 0
	if len(train.Prices) == 0 || bytes.Equal(bytes.TrimSpace(train.Prices), []byte("null")) {
		warnings.add("offerings_missing")
	} else {
		var groups []json.RawMessage
		if err := json.Unmarshal(train.Prices, &groups); err != nil {
			return provider.JourneyObservation{}, 0, parseError("prices_shape_unverified")
		}
		for _, rawGroup := range groups {
			var group rawPriceGroup
			if err := json.Unmarshal(rawGroup, &group); err != nil {
				return provider.JourneyObservation{}, 0, parseError("invalid_price_group")
			}
			if group.SellType.Unexpected {
				warnings.add("unexpected_sell_type")
			}
			parsed, count, err := parseOfferings(group.Classes, req, warnings)
			if err != nil {
				return provider.JourneyObservation{}, 0, err
			}
			total += count
			if total > provider.MaxOfferings {
				return provider.JourneyObservation{}, 0, parseError("too_many_offerings")
			}
			merged, err := mergeOfferings(offerings, parsed, warnings)
			if err != nil {
				return provider.JourneyObservation{}, 0, err
			}
			offerings = merged
		}
	}
	offerings = attachBookingURL(offerings, req.Config.BookingSlug, req.DepartureDate)
	return provider.JourneyObservation{Journey: journey, Offerings: offerings}, total, nil
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
	journey, err := buildJourney(train, req, warnings)
	if err != nil {
		return provider.JourneyObservation{}, 0, err
	}
	offerings, elementCount, err := parseOfferings(train.Classes, req, warnings)
	if err != nil {
		return provider.JourneyObservation{}, 0, err
	}
	offerings = attachBookingURL(offerings, req.Config.BookingSlug, req.DepartureDate)
	return provider.JourneyObservation{Journey: journey, Offerings: offerings}, elementCount, nil
}

// validateTrainRoute rejects a response whose train-level route differs from
// the persisted/requested route. A missing field is tolerated for compatibility
// with the original synthetic fixtures; a present but unparseable or different
// route is a fail-closed invalid response.
func validateTrainRoute(train rawTrain, req provider.CheckRequest) error {
	if train.From.Unexpected || train.To.Unexpected {
		return parseError("route_id_unexpected_type")
	}
	checks := []struct {
		field string
		got   optionalID
		want  string
	}{
		{"from", train.From, req.Config.OriginID},
		{"to", train.To, req.Config.DestinationID},
	}
	for _, check := range checks {
		if !check.got.Present || !check.got.Valid {
			continue
		}
		got, err := normalizePositiveID(check.got.Value)
		if err != nil {
			return parseError("route_id_invalid")
		}
		want, err := normalizePositiveID(check.want)
		if err != nil || got != want {
			return parseError("route_mismatch")
		}
	}
	return nil
}

func buildJourney(train rawTrain, req provider.CheckRequest, warnings *warningSet) (provider.Journey, error) {
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
		return provider.Journey{}, parseError("journey_identity_insufficient")
	}
	journey.Key = key
	return journey, nil
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
		offering.ShortTitle.Unexpected || offering.ClassLabel.Unexpected || offering.CompartmentType.Unexpected {
		identityWarning = "unexpected_offer_identity_type"
	}
	className := normalizeText(offering.ShortTitle.Value)
	if className == "" {
		className = normalizeText(offering.ClassLabel.Value)
	}
	model := provider.Offering{
		NativeID:        nativeIDValue(offering.ID),
		WagonID:         normalizeNativeID(offering.WagonID.Value),
		WagonName:       normalizeText(offering.WagonName.Value),
		ClassName:       className,
		CompartmentType: normalizeText(offering.CompartmentType.Value),
	}
	if offering.Capacity.Unexpected || offering.IsAvailable.Unexpected || offering.ReservationAvailable.Unexpected {
		identityWarning = "unexpected_availability_type"
	}
	if offering.Price.Unexpected {
		identityWarning = "unexpected_offer_price_type"
	} else if offering.Price.Invalid {
		identityWarning = "invalid_offer_price"
	}
	// Price currency is not verified (Rial vs Toman), so keep Price absent.
	// The numeric amount remains unparsed for decisions; see docs.
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
	raw := strings.TrimSpace(field.Value)
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		utc := parsed.UTC()
		return &utc, ""
	}
	// The observed production timestamps omit an offset and are local Tehran
	// wall-clock values (for example 2026-11-05T14:50:00). This is a verified
	// shape from the bounded live response; do not guess other zones.
	const localLayout = "2006-01-02T15:04:05.999999999"
	if tehranLocation != nil {
		if parsed, err := time.ParseInLocation(localLayout, raw, tehranLocation); err == nil {
			utc := parsed.UTC()
			return &utc, ""
		}
	}
	return nil, "unparseable_time"
}

var tehranLocation = func() *time.Location {
	location, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		return nil
	}
	return location
}()

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
		equalURL(a.BookingURL, b.BookingURL)
}

func equalURL(a, b *url.URL) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.String() == b.String()
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
