package domain

import (
	"context"
)

// Detail returns one monitor with bounded current-observation and recent-check
// diagnostics for each active provider target. It reuses the same persistence
// boundary as monitor CRUD and does not create, schedule, or mutate monitoring
// state.
func (s *MonitorService) Detail(ctx context.Context, id string) (MonitorDetail, error) {
	monitor, err := s.repo.GetMonitor(ctx, id)
	if err != nil {
		return MonitorDetail{}, err
	}
	detail := MonitorDetail{
		Monitor: monitor,
		Targets: make([]MonitorTargetDetail, 0, len(monitor.Providers)),
	}
	for _, target := range monitor.Providers {
		targetDetail := MonitorTargetDetail{
			MonitorProviderID: target.ID,
			ProviderID:        target.ProviderID,
			Active:            target.Active,
			Observations:      []InventoryObservation{},
			RecentChecks:      []CheckRunDiagnostic{},
		}
		// Inactive targets are retained for history but are not polled. Their
		// old observations and checks must not be presented as current.
		if !target.Active {
			detail.Targets = append(detail.Targets, targetDetail)
			continue
		}

		observations, err := s.repo.ListInventoryObservations(ctx, target.ID, target.ConfigVersion, DefaultObservationLimit)
		if err != nil {
			return MonitorDetail{}, err
		}
		targetDetail.Observations = observations.Observations
		targetDetail.ObservationsTruncated = observations.Truncated

		checks, err := s.repo.ListRecentCheckRuns(ctx, target.ID, DefaultRecentCheckLimit)
		if err != nil {
			return MonitorDetail{}, err
		}
		targetDetail.RecentChecks = checks.Checks
		targetDetail.RecentChecksTruncated = checks.Truncated

		detail.Targets = append(detail.Targets, targetDetail)
	}
	return detail, nil
}
