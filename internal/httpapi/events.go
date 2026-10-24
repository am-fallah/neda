package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/store"
)

type eventListResponse struct {
	Events     []eventResponse `json:"events"`
	NextCursor string          `json:"nextCursor"`
}

type eventResponse struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	ProviderID   string `json:"providerId"`
	MonitorID    string `json:"monitorId,omitempty"`
	MonitorLabel string `json:"monitorLabel,omitempty"`
	EpisodeID    *int64 `json:"episodeId,omitempty"`

	OccurredAt time.Time `json:"occurredAt"`
	State      string    `json:"state,omitempty"`

	TrainNumber     string     `json:"trainNumber,omitempty"`
	Operator        string     `json:"operator,omitempty"`
	OriginName      string     `json:"originName,omitempty"`
	DestinationName string     `json:"destinationName,omitempty"`
	DepartureAt     *time.Time `json:"departureAt,omitempty"`
	ArrivalAt       *time.Time `json:"arrivalAt,omitempty"`
	WagonName       string     `json:"wagonName,omitempty"`
	ClassName       string     `json:"className,omitempty"`
	PriceAmount     *int64     `json:"priceAmount,omitempty"`
	PriceCurrency   string     `json:"priceCurrency,omitempty"`
	BookingURL      string     `json:"bookingUrl,omitempty"`

	ErrorKind    string `json:"errorKind,omitempty"`
	FailureCount *int   `json:"failureCount,omitempty"`

	CreatedAt       time.Time               `json:"createdAt"`
	Deliveries      []deliveryStateResponse `json:"deliveries"`
	DeliverySummary deliverySummaryResponse `json:"deliverySummary"`
}

type deliveryStateResponse struct {
	ID            string     `json:"id"`
	Channel       string     `json:"channel"`
	Status        string     `json:"status"`
	AttemptCount  int        `json:"attemptCount"`
	NextAttemptAt *time.Time `json:"nextAttemptAt"`
	LastAttemptAt *time.Time `json:"lastAttemptAt"`
	SentAt        *time.Time `json:"sentAt"`
	LastErrorKind string     `json:"lastErrorKind,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type deliverySummaryResponse struct {
	Total          int `json:"total"`
	Pending        int `json:"pending"`
	Sending        int `json:"sending"`
	Sent           int `json:"sent"`
	RetryScheduled int `json:"retryScheduled"`
	Failed         int `json:"failed"`
	Skipped        int `json:"skipped"`
	Attempted      int `json:"attempted"`
	Terminal       int `json:"terminal"`
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "read_model_unavailable", "event history is not available")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/events")
	if path == "" {
		if !requireMethods(w, r, http.MethodGet) {
			return
		}
		s.listEvents(w, r)
		return
	}
	if !strings.HasPrefix(path, "/") {
		writeError(w, r, http.StatusNotFound, "not_found", "not found")
		return
	}
	id := strings.TrimPrefix(path, "/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, r, http.StatusNotFound, "not_found", "not found")
		return
	}
	if !requireMethods(w, r, http.MethodGet) {
		return
	}
	s.getEvent(w, r, id)
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	limit, queryErr := queryLimit(r, store.MaxReadLimit)
	if queryErr != nil {
		writeQueryError(w, r, queryErr)
		return
	}
	kind, queryErr := queryEventKind(r)
	if queryErr != nil {
		writeQueryError(w, r, queryErr)
		return
	}
	page, err := s.store.ListEvents(r.Context(), store.EventQuery{
		Limit:      limit,
		Cursor:     strings.TrimSpace(r.URL.Query().Get("cursor")),
		Kind:       kind,
		MonitorID:  strings.TrimSpace(r.URL.Query().Get("monitorId")),
		ProviderID: strings.TrimSpace(r.URL.Query().Get("providerId")),
	})
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	events := make([]eventResponse, 0, len(page.Events))
	for _, event := range page.Events {
		events = append(events, eventDTO(event))
	}
	writeJSON(w, http.StatusOK, eventListResponse{Events: events, NextCursor: page.NextCursor})
}

func (s *Server) getEvent(w http.ResponseWriter, r *http.Request, id string) {
	event, err := s.store.GetEvent(r.Context(), id)
	if err != nil {
		s.writeReadModelError(w, r, err, "event not found")
		return
	}
	writeJSON(w, http.StatusOK, eventDTO(event))
}

func eventDTO(event store.EventRecord) eventResponse {
	state := ""
	if event.Kind == notify.KindAvailability {
		state = string(provider.StateReservable)
	}
	deliveries := make([]deliveryStateResponse, 0, len(event.Deliveries))
	for _, delivery := range event.Deliveries {
		deliveries = append(deliveries, deliveryStateResponse{
			ID:            delivery.ID,
			Channel:       string(delivery.Channel),
			Status:        string(delivery.Status),
			AttemptCount:  delivery.AttemptCount,
			NextAttemptAt: utcOptional(delivery.NextAttemptAt),
			LastAttemptAt: utcOptional(delivery.LastAttemptAt),
			SentAt:        utcOptional(delivery.SentAt),
			LastErrorKind: delivery.LastErrorKind,
			CreatedAt:     delivery.CreatedAt.UTC(),
			UpdatedAt:     delivery.UpdatedAt.UTC(),
		})
	}
	return eventResponse{
		ID:              event.ID,
		Kind:            string(event.Kind),
		ProviderID:      event.ProviderID,
		MonitorID:       event.MonitorID,
		MonitorLabel:    event.MonitorLabel,
		EpisodeID:       event.EpisodeID,
		OccurredAt:      event.OccurredAt.UTC(),
		State:           state,
		TrainNumber:     event.TrainNumber,
		Operator:        event.Operator,
		OriginName:      event.OriginName,
		DestinationName: event.DestinationName,
		DepartureAt:     utcOptional(event.DepartureAt),
		ArrivalAt:       utcOptional(event.ArrivalAt),
		WagonName:       event.WagonName,
		ClassName:       event.ClassName,
		PriceAmount:     event.PriceAmount,
		PriceCurrency:   event.PriceCurrency,
		BookingURL:      event.BookingURL,
		ErrorKind:       event.ErrorKind,
		FailureCount:    event.FailureCount,
		CreatedAt:       event.CreatedAt.UTC(),
		Deliveries:      deliveries,
		DeliverySummary: deliverySummaryResponse{
			Total:          event.DeliverySummary.Total,
			Pending:        event.DeliverySummary.Pending,
			Sending:        event.DeliverySummary.Sending,
			Sent:           event.DeliverySummary.Sent,
			RetryScheduled: event.DeliverySummary.RetryScheduled,
			Failed:         event.DeliverySummary.Failed,
			Skipped:        event.DeliverySummary.Skipped,
			Attempted:      event.DeliverySummary.Attempted,
			Terminal:       event.DeliverySummary.Terminal,
		},
	}
}
