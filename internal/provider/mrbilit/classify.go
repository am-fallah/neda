package mrbilit

import (
	"math"

	"github.com/am-fallah/neda/internal/provider"
)

// classifyOffering implements the conservative decision table in
// Architecture §9.4. It returns a normalized state, a stable reason code, and a
// non-negative capacity pointer only when the capacity is usable evidence.
func classifyOffering(capacity optionalInt, isAvailable, reservationAvailable optionalBool, requiredSeats int) (provider.State, string, *int) {
	if capacity.Unexpected || isAvailable.Unexpected || reservationAvailable.Unexpected {
		return provider.StateUnknown, "unexpected_type", nil
	}
	if capacity.Invalid || (capacity.Valid && (capacity.Value < 0 || capacity.Value > math.MaxInt)) {
		return provider.StateUnknown, "invalid_capacity", nil
	}

	var seats *int
	if capacity.Valid {
		value := int(capacity.Value)
		seats = &value
	}
	var available *bool
	if isAvailable.Valid {
		value := isAvailable.Value
		available = &value
	}
	var reservation *bool
	if reservationAvailable.Valid {
		value := reservationAvailable.Value
		reservation = &value
	}

	switch {
	case seats != nil && *seats == 0 && available != nil && !*available:
		return provider.StateNotReservable, "capacity_zero_unavailable", seats
	case seats != nil && available != nil && reservation != nil &&
		*available && *reservation && *seats > 0 && *seats >= requiredSeats:
		return provider.StateReservable, "positive_capacity_and_flags", seats
	case seats != nil && available != nil && reservation != nil &&
		*available && *reservation && *seats > 0 && *seats < requiredSeats:
		return provider.StateUnknown, "capacity_below_requested_unverified", seats
	case seats != nil && *seats == 0 && available != nil && *available:
		return provider.StateUnknown, "zero_capacity_contradiction", seats
	case seats != nil && *seats > 0 && available != nil && !*available:
		return provider.StateUnknown, "positive_capacity_but_unavailable", seats
	case seats == nil && available != nil && !*available:
		return provider.StateUnknown, "unavailable_without_capacity", nil
	case reservation != nil && !*reservation:
		return provider.StateUnknown, "reservation_unavailable", seats
	case available == nil:
		return provider.StateUnknown, "availability_flag_missing", seats
	case seats == nil && available != nil && *available && reservation != nil && *reservation:
		return provider.StateUnknown, "capacity_missing", nil
	default:
		return provider.StateUnknown, "insufficient_evidence", seats
	}
}
