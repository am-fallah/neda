package domain

import (
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

// EpisodeClose identifies the old target version whose open episodes must be
// closed with close_reason='config_changed'.
type EpisodeClose struct {
	TargetID      int64
	ConfigVersion int64
}

// UpdatePlan is the pure diff produced from a current monitor and a normalized
// edit input. The repository applies it transactionally.
type UpdatePlan struct {
	Monitor       Monitor
	Material      bool
	CloseEpisodes []EpisodeClose
}

// PlanUpdate computes monitor/provider changes, config-version bumps, expiry
// reconciliation, and old-episode closures. It performs no I/O.
func PlanUpdate(current Monitor, input MonitorInput, now time.Time, loc *time.Location) (UpdatePlan, error) {
	if loc == nil {
		return UpdatePlan{}, validationError("departureDate", "timezone is required", nil)
	}

	next := current
	next.Providers = append([]MonitorProvider(nil), current.Providers...)
	dateChanged := current.DepartureDate != input.DepartureDate
	globalMaterial := dateChanged ||
		current.OriginName != input.OriginName ||
		current.DestinationName != input.DestinationName ||
		current.Adults != input.Adults ||
		current.Children != input.Children ||
		current.Infants != input.Infants

	next.Label = input.Label
	next.OriginName = input.OriginName
	next.DestinationName = input.DestinationName
	next.DepartureDate = input.DepartureDate
	next.Adults = input.Adults
	next.Children = input.Children
	next.Infants = input.Infants
	next.UpdatedAt = now

	if dateChanged {
		expiry, err := DepartureExpiry(input.DepartureDate, loc)
		if err != nil {
			return UpdatePlan{}, validationError("departureDate", err.Error(), err)
		}
		if !expiry.After(now) {
			return UpdatePlan{}, validationError("departureDate", "computed expiry must be in the future", nil)
		}
		next.ExpiresAt = expiry
		next.ExpiredAt = nil
	}

	plan := UpdatePlan{Monitor: next, Material: globalMaterial}
	byID := make(map[provider.ID]int, len(current.Providers))
	for index, target := range current.Providers {
		byID[target.ProviderID] = index
	}
	selected := make(map[provider.ID]bool, len(input.Providers))

	for _, candidate := range input.Providers {
		selected[candidate.ProviderID] = true
		index, exists := byID[candidate.ProviderID]
		if !exists {
			target := MonitorProvider{
				MonitorID:     current.ID,
				ProviderID:    candidate.ProviderID,
				Active:        true,
				OriginID:      candidate.OriginID,
				DestinationID: candidate.DestinationID,
				BookingSlug:   candidate.BookingSlug,
				GenderCode:    candidate.GenderCode,
				ConfigVersion: 1,
				Selections:    selectionRows(0, 1, candidate.Selections, now),
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			plan.Monitor.Providers = append(plan.Monitor.Providers, target)
			plan.Material = true
			continue
		}

		target := plan.Monitor.Providers[index]
		reAdd := !target.Active
		providerMaterial := globalMaterial ||
			target.OriginID != candidate.OriginID ||
			target.DestinationID != candidate.DestinationID ||
			target.GenderCode != candidate.GenderCode ||
			!sameSelections(target.Selections, candidate.Selections)
		bookingChanged := target.BookingSlug != candidate.BookingSlug
		needsBump := reAdd || providerMaterial

		if needsBump {
			if target.ID > 0 {
				plan.CloseEpisodes = append(plan.CloseEpisodes, EpisodeClose{
					TargetID:      target.ID,
					ConfigVersion: target.ConfigVersion,
				})
			}
			target.ConfigVersion++
			target.Active = true
			target.OriginID = candidate.OriginID
			target.DestinationID = candidate.DestinationID
			target.BookingSlug = candidate.BookingSlug
			target.GenderCode = candidate.GenderCode
			target.Selections = selectionRows(target.ID, target.ConfigVersion, candidate.Selections, now)
			target.UpdatedAt = now
			target.NextDueAt = nil
			plan.Material = true
		} else if bookingChanged {
			target.BookingSlug = candidate.BookingSlug
			target.UpdatedAt = now
		}
		plan.Monitor.Providers[index] = target
	}

	for index, target := range current.Providers {
		if selected[target.ProviderID] || !target.Active {
			continue
		}
		removed := plan.Monitor.Providers[index]
		if removed.ID > 0 {
			plan.CloseEpisodes = append(plan.CloseEpisodes, EpisodeClose{
				TargetID:      removed.ID,
				ConfigVersion: removed.ConfigVersion,
			})
		}
		removed.Active = false
		removed.ConfigVersion++
		removed.NextDueAt = nil
		removed.UpdatedAt = now
		plan.Monitor.Providers[index] = removed
		plan.Material = true
	}

	return plan, nil
}

func selectionRows(targetID, configVersion int64, options []provider.SelectedOption, now time.Time) []MonitorProviderSelection {
	if len(options) == 0 {
		return nil
	}
	rows := make([]MonitorProviderSelection, 0, len(options))
	for _, option := range options {
		rows = append(rows, MonitorProviderSelection{
			MonitorProviderID: targetID,
			ConfigVersion:     configVersion,
			Option:            option,
			CreatedAt:         now,
		})
	}
	return rows
}

func sameSelections(current []MonitorProviderSelection, next []provider.SelectedOption) bool {
	if len(current) != len(next) {
		return false
	}
	for index, option := range next {
		row := current[index].Option
		if row.JourneyKey != option.JourneyKey ||
			row.OfferingKey != option.OfferingKey ||
			row.JourneyNativeID != option.JourneyNativeID ||
			row.OfferingNativeID != option.OfferingNativeID ||
			row.TrainNumber != option.TrainNumber ||
			row.Operator != option.Operator ||
			row.OriginName != option.OriginName ||
			row.DestinationName != option.DestinationName ||
			row.WagonID != option.WagonID ||
			row.WagonName != option.WagonName ||
			row.ClassName != option.ClassName ||
			row.CompartmentType != option.CompartmentType ||
			!sameOptionalTime(row.DepartureAt, option.DepartureAt) ||
			!sameOptionalTime(row.ArrivalAt, option.ArrivalAt) {
			return false
		}
	}
	return true
}

func sameOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
