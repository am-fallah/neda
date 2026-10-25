package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/am-fallah/neda/internal/provider"
)

const (
	// MaxLabelRunes is the documented label bound.
	MaxLabelRunes = 100
	// MaxAdults/MaxChildren/MaxInfants and MaxPassengersTotal are the
	// documented provisional passenger bounds; provider-specific validation may
	// tighten them in the adapter.
	MaxAdults          = 9
	MaxChildren        = 9
	MaxInfants         = 9
	MaxPassengersTotal = 9
	// MaxBookingSlugLength and MaxGenderCodeLength are generic storage bounds;
	// provider adapters own pattern/enum validation.
	MaxBookingSlugLength = 256
	MaxGenderCodeLength  = 64
	// MaxSelectionsPerProvider bounds persisted train/class selections.
	MaxSelectionsPerProvider = 50
	MaxSelectionKeyLength    = 512
	MaxSelectionTextLength   = 512
)

func normalizeMonitorInput(input MonitorInput, now time.Time, loc *time.Location, providers ProviderResolver) (MonitorInput, time.Time, error) {
	if providers == nil {
		return MonitorInput{}, time.Time{}, validationError("providers", "provider resolver is required", nil)
	}
	if loc == nil {
		return MonitorInput{}, time.Time{}, validationError("departureDate", "timezone is required", nil)
	}

	normalized := MonitorInput{
		Label:           strings.TrimSpace(input.Label),
		OriginName:      strings.TrimSpace(input.OriginName),
		DestinationName: strings.TrimSpace(input.DestinationName),
		DepartureDate:   strings.TrimSpace(input.DepartureDate),
		Adults:          input.Adults,
		Children:        input.Children,
		Infants:         input.Infants,
	}

	if normalized.Label == "" {
		return MonitorInput{}, time.Time{}, validationError("label", "is required", nil)
	}
	if utf8.RuneCountInString(normalized.Label) > MaxLabelRunes {
		return MonitorInput{}, time.Time{}, validationError("label", fmt.Sprintf("must be at most %d characters", MaxLabelRunes), nil)
	}
	if normalized.OriginName == "" {
		return MonitorInput{}, time.Time{}, validationError("originName", "is required", nil)
	}
	if normalized.DestinationName == "" {
		return MonitorInput{}, time.Time{}, validationError("destinationName", "is required", nil)
	}
	if strings.EqualFold(normalized.OriginName, normalized.DestinationName) {
		return MonitorInput{}, time.Time{}, validationError("destinationName", "must differ from originName", nil)
	}

	expiresAt, err := ValidateDepartureDate(normalized.DepartureDate, now, loc)
	if err != nil {
		return MonitorInput{}, time.Time{}, validationError("departureDate", err.Error(), err)
	}

	if normalized.Adults < 1 || normalized.Adults > MaxAdults {
		return MonitorInput{}, time.Time{}, validationError("adults", fmt.Sprintf("must be between 1 and %d", MaxAdults), nil)
	}
	if normalized.Children < 0 || normalized.Children > MaxChildren {
		return MonitorInput{}, time.Time{}, validationError("children", fmt.Sprintf("must be between 0 and %d", MaxChildren), nil)
	}
	if normalized.Infants < 0 || normalized.Infants > MaxInfants {
		return MonitorInput{}, time.Time{}, validationError("infants", fmt.Sprintf("must be between 0 and %d", MaxInfants), nil)
	}
	if normalized.Adults+normalized.Children+normalized.Infants > MaxPassengersTotal {
		return MonitorInput{}, time.Time{}, validationError("passengers", fmt.Sprintf("total must be at most %d", MaxPassengersTotal), nil)
	}

	if len(input.Providers) == 0 {
		return MonitorInput{}, time.Time{}, validationError("providers", "at least one provider is required", nil)
	}
	seen := make(map[provider.ID]bool, len(input.Providers))
	normalized.Providers = make([]ProviderInput, 0, len(input.Providers))
	for index, candidate := range input.Providers {
		field := fmt.Sprintf("providers[%d]", index)
		providerID := provider.ID(strings.TrimSpace(string(candidate.ProviderID)))
		if providerID == "" {
			return MonitorInput{}, time.Time{}, validationError(field+".providerId", "is required", nil)
		}
		if seen[providerID] {
			return MonitorInput{}, time.Time{}, validationError(field+".providerId", "is duplicated", nil)
		}
		seen[providerID] = true

		originID := strings.TrimSpace(candidate.OriginID)
		destinationID := strings.TrimSpace(candidate.DestinationID)

		adapter, ok := providers.Lookup(providerID)
		if !ok || adapter == nil {
			return MonitorInput{}, time.Time{}, validationError(field+".providerId", "provider is not registered", nil)
		}
		capabilities := adapter.Metadata().Capabilities
		if capabilities.RequiresOriginID && originID == "" {
			return MonitorInput{}, time.Time{}, validationError(field+".originId", "is required by this provider", nil)
		}
		if capabilities.RequiresDestinationID && destinationID == "" {
			return MonitorInput{}, time.Time{}, validationError(field+".destinationId", "is required by this provider", nil)
		}
		if originID != "" && destinationID != "" && originID == destinationID {
			return MonitorInput{}, time.Time{}, validationError(field+".destinationId", "must differ from originId", nil)
		}

		bookingSlug := strings.TrimSpace(candidate.BookingSlug)
		if len(bookingSlug) > MaxBookingSlugLength {
			return MonitorInput{}, time.Time{}, validationError(field+".bookingSlug", fmt.Sprintf("must be at most %d bytes", MaxBookingSlugLength), nil)
		}
		if containsControl(bookingSlug) {
			return MonitorInput{}, time.Time{}, validationError(field+".bookingSlug", "must not contain control characters", nil)
		}
		genderCode := strings.TrimSpace(candidate.GenderCode)
		if len(genderCode) > MaxGenderCodeLength {
			return MonitorInput{}, time.Time{}, validationError(field+".genderCode", fmt.Sprintf("must be at most %d bytes", MaxGenderCodeLength), nil)
		}
		if containsControl(genderCode) {
			return MonitorInput{}, time.Time{}, validationError(field+".genderCode", "must not contain control characters", nil)
		}

		selections, err := normalizeSelections(candidate.Selections, capabilities.RequiresOptionSelection, field, normalized.DepartureDate, loc)
		if err != nil {
			return MonitorInput{}, time.Time{}, err
		}
		if err := adapter.ValidateConfig(provider.MonitorProviderConfig{
			OriginID:      originID,
			DestinationID: destinationID,
			BookingSlug:   bookingSlug,
			GenderCode:    genderCode,
		}); err != nil {
			return MonitorInput{}, time.Time{}, validationError(field, "provider configuration rejected", err)
		}

		normalized.Providers = append(normalized.Providers, ProviderInput{
			ProviderID:    providerID,
			OriginID:      originID,
			DestinationID: destinationID,
			BookingSlug:   bookingSlug,
			GenderCode:    genderCode,
			Selections:    selections,
		})
	}

	return normalized, expiresAt, nil
}

func normalizeSelections(input []provider.SelectedOption, required bool, field, departureDate string, loc *time.Location) ([]provider.SelectedOption, error) {
	if len(input) == 0 {
		if required {
			return nil, validationError(field+".selections", "at least one train/class option is required", nil)
		}
		return nil, nil
	}
	if !required {
		return nil, validationError(field+".selections", "this provider does not support option selection", nil)
	}
	if len(input) > MaxSelectionsPerProvider {
		return nil, validationError(field+".selections", fmt.Sprintf("must contain at most %d options", MaxSelectionsPerProvider), nil)
	}
	seen := make(map[string]struct{}, len(input))
	result := make([]provider.SelectedOption, 0, len(input))
	for index, candidate := range input {
		itemField := fmt.Sprintf("%s.selections[%d]", field, index)
		option := provider.SelectedOption{
			JourneyKey:       strings.TrimSpace(candidate.JourneyKey),
			OfferingKey:      strings.TrimSpace(candidate.OfferingKey),
			JourneyNativeID:  strings.TrimSpace(candidate.JourneyNativeID),
			OfferingNativeID: strings.TrimSpace(candidate.OfferingNativeID),
			TrainNumber:      strings.TrimSpace(candidate.TrainNumber),
			Operator:         strings.TrimSpace(candidate.Operator),
			OriginName:       strings.TrimSpace(candidate.OriginName),
			DestinationName:  strings.TrimSpace(candidate.DestinationName),
			WagonID:          strings.TrimSpace(candidate.WagonID),
			WagonName:        strings.TrimSpace(candidate.WagonName),
			ClassName:        strings.TrimSpace(candidate.ClassName),
			CompartmentType:  strings.TrimSpace(candidate.CompartmentType),
		}
		if option.JourneyKey == "" {
			return nil, validationError(itemField+".journeyKey", "is required", nil)
		}
		if option.OfferingKey == "" {
			return nil, validationError(itemField+".offeringKey", "is required", nil)
		}
		if len(option.JourneyKey) > MaxSelectionKeyLength || len(option.OfferingKey) > MaxSelectionKeyLength {
			return nil, validationError(itemField, "selection keys are too long", nil)
		}
		for _, value := range []string{
			option.JourneyNativeID, option.OfferingNativeID, option.TrainNumber, option.Operator,
			option.OriginName, option.DestinationName, option.WagonID, option.WagonName,
			option.ClassName, option.CompartmentType,
		} {
			if len(value) > MaxSelectionTextLength {
				return nil, validationError(itemField, "selection metadata is too long", nil)
			}
			if containsControl(value) {
				return nil, validationError(itemField, "selection metadata must not contain control characters", nil)
			}
		}
		key := option.JourneyKey + "\x00" + option.OfferingKey
		if _, exists := seen[key]; exists {
			return nil, validationError(itemField, "is a duplicate selection", nil)
		}
		seen[key] = struct{}{}
		if candidate.DepartureAt != nil {
			if candidate.DepartureAt.IsZero() {
				return nil, validationError(itemField+".departureAt", "must not be zero", nil)
			}
			value := candidate.DepartureAt.UTC()
			option.DepartureAt = &value
		}
		if candidate.ArrivalAt != nil {
			if candidate.ArrivalAt.IsZero() {
				return nil, validationError(itemField+".arrivalAt", "must not be zero", nil)
			}
			value := candidate.ArrivalAt.UTC()
			option.ArrivalAt = &value
		}
		if option.DepartureAt != nil && loc != nil {
			localDate := option.DepartureAt.In(loc).Format(dateLayout)
			if localDate != departureDate {
				return nil, validationError(itemField+".departureAt", "does not match the monitor departure date; re-run the MrBilit search", nil)
			}
		}
		result = append(result, option)
	}
	return result, nil
}

func validationError(field, message string, cause error) error {
	return &ValidationError{Field: field, Message: message, Cause: cause}
}

func containsControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
