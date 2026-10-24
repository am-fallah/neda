package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/store"
)

type deliveryListResponse struct {
	Deliveries []deliveryRecordResponse `json:"deliveries"`
	NextCursor string                   `json:"nextCursor"`
}

type deliveryRecordResponse struct {
	ID              string     `json:"id"`
	EventID         string     `json:"eventId"`
	EventKind       string     `json:"eventKind"`
	EventOccurredAt time.Time  `json:"eventOccurredAt"`
	ProviderID      string     `json:"providerId"`
	MonitorID       string     `json:"monitorId,omitempty"`
	Channel         string     `json:"channel"`
	Status          string     `json:"status"`
	AttemptCount    int        `json:"attemptCount"`
	NextAttemptAt   *time.Time `json:"nextAttemptAt"`
	LastAttemptAt   *time.Time `json:"lastAttemptAt"`
	SentAt          *time.Time `json:"sentAt"`
	LastErrorKind   string     `json:"lastErrorKind,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

func (s *Server) handleDeliveries(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "read_model_unavailable", "delivery history is not available")
		return
	}
	if !requireMethods(w, r, http.MethodGet) {
		return
	}
	limit, queryErr := queryLimit(r, store.MaxReadLimit)
	if queryErr != nil {
		writeQueryError(w, r, queryErr)
		return
	}
	channel, queryErr := queryChannel(r)
	if queryErr != nil {
		writeQueryError(w, r, queryErr)
		return
	}
	status, queryErr := queryDeliveryStatus(r)
	if queryErr != nil {
		writeQueryError(w, r, queryErr)
		return
	}
	kind, queryErr := queryEventKind(r)
	if queryErr != nil {
		writeQueryError(w, r, queryErr)
		return
	}
	page, err := s.store.ListDeliveries(r.Context(), store.DeliveryQuery{
		Limit:   limit,
		Cursor:  strings.TrimSpace(r.URL.Query().Get("cursor")),
		Channel: channel,
		Status:  status,
		EventID: strings.TrimSpace(r.URL.Query().Get("eventId")),
		Kind:    kind,
	})
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	deliveries := make([]deliveryRecordResponse, 0, len(page.Deliveries))
	for _, delivery := range page.Deliveries {
		deliveries = append(deliveries, deliveryRecordResponse{
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
	writeJSON(w, http.StatusOK, deliveryListResponse{Deliveries: deliveries, NextCursor: page.NextCursor})
}
