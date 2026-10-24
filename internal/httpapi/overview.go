package httpapi

import (
	"net/http"
	"time"

	"github.com/am-fallah/neda/internal/health"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/store"
)

type overviewResponse struct {
	GeneratedAt      time.Time                `json:"generatedAt"`
	Monitors         overviewMonitorCounts    `json:"monitors"`
	Providers        overviewProviderCounts   `json:"providers"`
	Deliveries       overviewDeliveryCounts   `json:"deliveries"`
	RecentEvents     []eventResponse          `json:"recentEvents"`
	RecentDeliveries []deliveryRecordResponse `json:"recentDeliveries"`
}

type overviewMonitorCounts struct {
	Total   int `json:"total"`
	Active  int `json:"active"`
	Paused  int `json:"paused"`
	Expired int `json:"expired"`
}

type overviewProviderCounts struct {
	Total    int `json:"total"`
	Degraded int `json:"degraded"`
	Stale    int `json:"stale"`
}

type overviewDeliveryCounts struct {
	Total          int `json:"total"`
	Pending        int `json:"pending"`
	Sending        int `json:"sending"`
	Sent           int `json:"sent"`
	RetryScheduled int `json:"retryScheduled"`
	Failed         int `json:"failed"`
	Skipped        int `json:"skipped"`
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "read_model_unavailable", "overview is not available")
		return
	}
	if !requireMethods(w, r, http.MethodGet) {
		return
	}
	now := s.now().UTC()
	interval, err := s.pollInterval(r.Context())
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	monitorCounts, err := s.store.CountMonitorsByStatus(r.Context(), now)
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	deliveryCounts, err := s.store.CountDeliveriesByStatus(r.Context())
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	healthRecords, err := s.store.ListProviderHealth(r.Context())
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	healthByID := make(map[string]store.ProviderHealthRecord, len(healthRecords))
	for _, record := range healthRecords {
		healthByID[record.ProviderID] = record
	}
	metadata := make([]provider.Metadata, 0)
	if s.providers != nil {
		metadata = s.providers.List()
	}
	providerCounts := overviewProviderCounts{Total: len(metadata)}
	for _, meta := range metadata {
		record, ok := healthByID[string(meta.ID)]
		if !ok {
			continue
		}
		if record.OpenEpisode != nil {
			providerCounts.Degraded++
		}
		if health.IsStale(record.LastSuccessAt, record.OpenEpisode != nil, now, interval) {
			providerCounts.Stale++
		}
	}

	eventsPage, err := s.store.ListEvents(r.Context(), store.EventQuery{
		Limit: 5,
		Kind:  notify.KindAvailability,
	})
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	recentEvents := make([]eventResponse, 0, len(eventsPage.Events))
	for _, event := range eventsPage.Events {
		recentEvents = append(recentEvents, eventDTO(event))
	}
	deliveriesPage, err := s.store.ListDeliveries(r.Context(), store.DeliveryQuery{Limit: 5})
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	recentDeliveries := make([]deliveryRecordResponse, 0, len(deliveriesPage.Deliveries))
	for _, delivery := range deliveriesPage.Deliveries {
		recentDeliveries = append(recentDeliveries, deliveryRecordResponse{
			ID:              delivery.ID,
			EventID:         delivery.EventID,
			EventKind:       string(delivery.EventKind),
			EventOccurredAt: delivery.EventOccurredAt.UTC(),
			ProviderID:      delivery.ProviderID,
			MonitorID:       delivery.MonitorID,
			Channel:         string(delivery.Channel),
			Status:          string(delivery.Status),
			AttemptCount:    delivery.AttemptCount,
			NextAttemptAt:   utcOptional(delivery.NextAttemptAt),
			LastAttemptAt:   utcOptional(delivery.LastAttemptAt),
			SentAt:          utcOptional(delivery.SentAt),
			LastErrorKind:   delivery.LastErrorKind,
			CreatedAt:       delivery.CreatedAt.UTC(),
			UpdatedAt:       delivery.UpdatedAt.UTC(),
		})
	}

	writeJSON(w, http.StatusOK, overviewResponse{
		GeneratedAt: now,
		Monitors: overviewMonitorCounts{
			Total:   monitorCounts.Total,
			Active:  monitorCounts.Active,
			Paused:  monitorCounts.Paused,
			Expired: monitorCounts.Expired,
		},
		Providers: providerCounts,
		Deliveries: overviewDeliveryCounts{
			Total:          deliveryCounts.Total,
			Pending:        deliveryCounts.Pending,
			Sending:        deliveryCounts.Sending,
			Sent:           deliveryCounts.Sent,
			RetryScheduled: deliveryCounts.RetryScheduled,
			Failed:         deliveryCounts.Failed,
			Skipped:        deliveryCounts.Skipped,
		},
		RecentEvents:     recentEvents,
		RecentDeliveries: recentDeliveries,
	})
}
