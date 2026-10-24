package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/logging"
	"github.com/am-fallah/neda/internal/provider"
)

type monitorRequest struct {
	Label           string                   `json:"label"`
	OriginName      string                   `json:"originName"`
	DestinationName string                   `json:"destinationName"`
	DepartureDate   string                   `json:"departureDate"`
	Adults          int                      `json:"adults"`
	Children        int                      `json:"children"`
	Infants         int                      `json:"infants"`
	Providers       []monitorProviderRequest `json:"providers"`
}

type monitorProviderRequest struct {
	ProviderID    string `json:"providerId"`
	OriginID      string `json:"originId"`
	DestinationID string `json:"destinationId"`
	BookingSlug   string `json:"bookingSlug"`
	GenderCode    string `json:"genderCode"`
}

type monitorResponse struct {
	ID              string                    `json:"id"`
	Label           string                    `json:"label"`
	OriginName      string                    `json:"originName"`
	DestinationName string                    `json:"destinationName"`
	DepartureDate   string                    `json:"departureDate"`
	Adults          int                       `json:"adults"`
	Children        int                       `json:"children"`
	Infants         int                       `json:"infants"`
	Enabled         bool                      `json:"enabled"`
	Status          string                    `json:"status"`
	ExpiresAt       time.Time                 `json:"expiresAt"`
	ExpiredAt       *time.Time                `json:"expiredAt,omitempty"`
	CreatedAt       time.Time                 `json:"createdAt"`
	UpdatedAt       time.Time                 `json:"updatedAt"`
	Providers       []monitorProviderResponse `json:"providers"`
}

type monitorProviderResponse struct {
	ProviderID          string     `json:"providerId"`
	Active              bool       `json:"active"`
	OriginID            string     `json:"originId"`
	DestinationID       string     `json:"destinationId"`
	BookingSlug         string     `json:"bookingSlug,omitempty"`
	GenderCode          string     `json:"genderCode,omitempty"`
	ConfigVersion       int64      `json:"configVersion"`
	NextDueAt           *time.Time `json:"nextDueAt,omitempty"`
	LastAttemptAt       *time.Time `json:"lastAttemptAt,omitempty"`
	LastSuccessAt       *time.Time `json:"lastSuccessAt,omitempty"`
	LastOutcome         string     `json:"lastOutcome,omitempty"`
	LastErrorKind       string     `json:"lastErrorKind,omitempty"`
	LastErrorAt         *time.Time `json:"lastErrorAt,omitempty"`
	ConsecutiveFailures int        `json:"consecutiveFailures"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

type monitorListResponse struct {
	Monitors []monitorResponse `json:"monitors"`
}

func (s *Server) handleMonitors(w http.ResponseWriter, r *http.Request) {
	if s.monitors == nil {
		writeError(w, r, http.StatusServiceUnavailable, "monitors_unavailable", "monitor service is not available")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/monitors")
	if path == "" {
		switch r.Method {
		case http.MethodGet:
			s.listMonitors(w, r)
		case http.MethodPost:
			s.createMonitor(w, r)
		default:
			requireMethods(w, r, http.MethodGet, http.MethodPost)
		}
		return
	}
	if !strings.HasPrefix(path, "/") {
		writeError(w, r, http.StatusNotFound, "not_found", "not found")
		return
	}

	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	switch {
	case len(parts) == 1 && parts[0] != "":
		id := parts[0]
		switch r.Method {
		case http.MethodGet:
			s.getMonitor(w, r, id)
		case http.MethodPatch:
			s.updateMonitor(w, r, id)
		case http.MethodDelete:
			s.deleteMonitor(w, r, id)
		default:
			requireMethods(w, r, http.MethodGet, http.MethodPatch, http.MethodDelete)
		}
	case len(parts) == 2 && parts[0] != "" && parts[1] == "pause":
		if !requireMethods(w, r, http.MethodPost) {
			return
		}
		s.pauseMonitor(w, r, parts[0])
	case len(parts) == 2 && parts[0] != "" && parts[1] == "resume":
		if !requireMethods(w, r, http.MethodPost) {
			return
		}
		s.resumeMonitor(w, r, parts[0])
	default:
		writeError(w, r, http.StatusNotFound, "not_found", "not found")
	}
}

func (s *Server) listMonitors(w http.ResponseWriter, r *http.Request) {
	monitors, err := s.monitors.List(r.Context())
	if err != nil {
		s.writeMonitorError(w, r, err)
		return
	}
	response := monitorListResponse{Monitors: make([]monitorResponse, 0, len(monitors))}
	now := time.Now().UTC()
	for _, monitor := range monitors {
		response.Monitors = append(response.Monitors, monitorDTO(monitor, now))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) createMonitor(w http.ResponseWriter, r *http.Request) {
	var body monitorRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid monitor request")
		return
	}
	monitor, err := s.monitors.Create(r.Context(), body.toDomain())
	if err != nil {
		s.writeMonitorError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/monitors/"+monitor.ID)
	writeJSON(w, http.StatusCreated, monitorDTO(monitor, time.Now().UTC()))
}

func (s *Server) getMonitor(w http.ResponseWriter, r *http.Request, id string) {
	monitor, err := s.monitors.Get(r.Context(), id)
	if err != nil {
		s.writeMonitorError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, monitorDTO(monitor, time.Now().UTC()))
}

func (s *Server) updateMonitor(w http.ResponseWriter, r *http.Request, id string) {
	var body monitorRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid monitor request")
		return
	}
	monitor, err := s.monitors.Update(r.Context(), id, body.toDomain())
	if err != nil {
		s.writeMonitorError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, monitorDTO(monitor, time.Now().UTC()))
}

func (s *Server) deleteMonitor(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.monitors.Delete(r.Context(), id); err != nil {
		s.writeMonitorError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) pauseMonitor(w http.ResponseWriter, r *http.Request, id string) {
	monitor, err := s.monitors.Pause(r.Context(), id)
	if err != nil {
		s.writeMonitorError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, monitorDTO(monitor, time.Now().UTC()))
}

func (s *Server) resumeMonitor(w http.ResponseWriter, r *http.Request, id string) {
	monitor, err := s.monitors.Resume(r.Context(), id)
	if err != nil {
		s.writeMonitorError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, monitorDTO(monitor, time.Now().UTC()))
}

func (s *Server) writeMonitorError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *domain.ValidationError
	switch {
	case errors.As(err, &validation):
		fields := map[string]string{}
		if validation.Field != "" {
			fields[validation.Field] = validation.Message
		}
		if len(fields) == 0 {
			writeError(w, r, http.StatusBadRequest, "validation_error", "validation failed")
			return
		}
		writeErrorWithFields(w, r, http.StatusBadRequest, "validation_error", "validation failed", fields)
	case errors.Is(err, domain.ErrMonitorNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "monitor not found")
	case errors.Is(err, domain.ErrMonitorExpired):
		writeError(w, r, http.StatusConflict, "monitor_expired", "monitor is expired; edit the departure date first")
	default:
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func (m monitorRequest) toDomain() domain.MonitorInput {
	providers := make([]domain.ProviderInput, 0, len(m.Providers))
	for _, candidate := range m.Providers {
		providers = append(providers, domain.ProviderInput{
			ProviderID:    provider.ID(candidate.ProviderID),
			OriginID:      candidate.OriginID,
			DestinationID: candidate.DestinationID,
			BookingSlug:   candidate.BookingSlug,
			GenderCode:    candidate.GenderCode,
		})
	}
	return domain.MonitorInput{
		Label:           m.Label,
		OriginName:      m.OriginName,
		DestinationName: m.DestinationName,
		DepartureDate:   m.DepartureDate,
		Adults:          m.Adults,
		Children:        m.Children,
		Infants:         m.Infants,
		Providers:       providers,
	}
}

func monitorDTO(monitor domain.Monitor, now time.Time) monitorResponse {
	providers := make([]monitorProviderResponse, 0, len(monitor.Providers))
	for _, target := range monitor.Providers {
		providers = append(providers, monitorProviderResponse{
			ProviderID:          string(target.ProviderID),
			Active:              target.Active,
			OriginID:            target.OriginID,
			DestinationID:       target.DestinationID,
			BookingSlug:         target.BookingSlug,
			GenderCode:          target.GenderCode,
			ConfigVersion:       target.ConfigVersion,
			NextDueAt:           utcOptional(target.NextDueAt),
			LastAttemptAt:       utcOptional(target.LastAttemptAt),
			LastSuccessAt:       utcOptional(target.LastSuccessAt),
			LastOutcome:         target.LastOutcome,
			LastErrorKind:       target.LastErrorKind,
			LastErrorAt:         utcOptional(target.LastErrorAt),
			ConsecutiveFailures: target.ConsecutiveFailures,
			CreatedAt:           target.CreatedAt.UTC(),
			UpdatedAt:           target.UpdatedAt.UTC(),
		})
	}
	return monitorResponse{
		ID:              monitor.ID,
		Label:           monitor.Label,
		OriginName:      monitor.OriginName,
		DestinationName: monitor.DestinationName,
		DepartureDate:   monitor.DepartureDate,
		Adults:          monitor.Adults,
		Children:        monitor.Children,
		Infants:         monitor.Infants,
		Enabled:         monitor.Enabled,
		Status:          string(monitor.Status(now)),
		ExpiresAt:       monitor.ExpiresAt.UTC(),
		ExpiredAt:       utcOptional(monitor.ExpiredAt),
		CreatedAt:       monitor.CreatedAt.UTC(),
		UpdatedAt:       monitor.UpdatedAt.UTC(),
		Providers:       providers,
	}
}

func utcOptional(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func requireMethods(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	for _, method := range methods {
		if r.Method == method {
			return true
		}
	}
	w.Header().Set(headerAllow, strings.Join(methods, ", "))
	writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	return false
}

func writeErrorWithFields(w http.ResponseWriter, r *http.Request, status int, code, message string, fields map[string]string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
			"fields":  fields,
		},
		"requestId": logging.RequestID(r.Context()),
	})
}
