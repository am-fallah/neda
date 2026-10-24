package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/health"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/store"
)

type providerListResponse struct {
	Providers []providerResponse `json:"providers"`
}

type providerResponse struct {
	ID           string                       `json:"id"`
	DisplayName  string                       `json:"displayName"`
	TestOnly     bool                         `json:"testOnly"`
	Capabilities providerCapabilitiesResponse `json:"capabilities"`
	Health       *providerHealthResponse      `json:"health"`
}

type providerCapabilitiesResponse struct {
	RequiresOriginID      bool `json:"requiresOriginId"`
	RequiresDestinationID bool `json:"requiresDestinationId"`
	SupportsBookingURL    bool `json:"supportsBookingUrl"`
	SupportsGender        bool `json:"supportsGender"`
	MaxPassengers         int  `json:"maxPassengers"`
}

type providerHealthResponse struct {
	LastAttemptAt         *time.Time                     `json:"lastAttemptAt"`
	LastSuccessAt         *time.Time                     `json:"lastSuccessAt"`
	LastFailureAt         *time.Time                     `json:"lastFailureAt"`
	LastErrorKind         string                         `json:"lastErrorKind,omitempty"`
	LastDurationMS        *int64                         `json:"lastDurationMs"`
	ConsecutiveFailures   int                            `json:"consecutiveFailures"`
	OpenEpisode           *providerHealthEpisodeResponse `json:"openEpisode"`
	Stale                 bool                           `json:"stale"`
	StaleThresholdSeconds int                            `json:"staleThresholdSeconds"`
}

type providerHealthEpisodeResponse struct {
	ID                 int64     `json:"id"`
	OpenedAt           time.Time `json:"openedAt"`
	FailureCountAtOpen int       `json:"failureCountAtOpen"`
	LastErrorKind      string    `json:"lastErrorKind,omitempty"`
}

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "read_model_unavailable", "provider health is not available")
		return
	}
	if !requireMethods(w, r, http.MethodGet) {
		return
	}
	interval, err := s.pollInterval(r.Context())
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	now := s.now().UTC()
	records, err := s.store.ListProviderHealth(r.Context())
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	byID := make(map[string]store.ProviderHealthRecord, len(records))
	for _, record := range records {
		byID[record.ProviderID] = record
	}

	metadata := make([]provider.Metadata, 0)
	if s.providers != nil {
		metadata = s.providers.List()
	}
	providers := make([]providerResponse, 0, len(metadata))
	for _, meta := range metadata {
		response := providerResponse{
			ID:          string(meta.ID),
			DisplayName: meta.DisplayName,
			TestOnly:    meta.TestOnly,
			Capabilities: providerCapabilitiesResponse{
				RequiresOriginID:      meta.Capabilities.RequiresOriginID,
				RequiresDestinationID: meta.Capabilities.RequiresDestinationID,
				SupportsBookingURL:    meta.Capabilities.SupportsBookingURL,
				SupportsGender:        meta.Capabilities.SupportsGender,
				MaxPassengers:         meta.Capabilities.MaxPassengers,
			},
		}
		if record, ok := byID[string(meta.ID)]; ok {
			response.Health = providerHealthDTO(record, now, interval)
		}
		providers = append(providers, response)
	}
	writeJSON(w, http.StatusOK, providerListResponse{Providers: providers})
}

func providerHealthDTO(record store.ProviderHealthRecord, now time.Time, interval time.Duration) *providerHealthResponse {
	response := &providerHealthResponse{
		LastAttemptAt:         utcOptional(record.LastAttemptAt),
		LastSuccessAt:         utcOptional(record.LastSuccessAt),
		LastFailureAt:         utcOptional(record.LastFailureAt),
		LastErrorKind:         record.LastErrorKind,
		LastDurationMS:        record.LastDurationMS,
		ConsecutiveFailures:   record.ConsecutiveFailures,
		Stale:                 health.IsStale(record.LastSuccessAt, record.OpenEpisode != nil, now, interval),
		StaleThresholdSeconds: int(health.StaleThreshold(interval) / time.Second),
	}
	if record.OpenEpisode != nil {
		response.OpenEpisode = &providerHealthEpisodeResponse{
			ID:                 record.OpenEpisode.ID,
			OpenedAt:           record.OpenEpisode.OpenedAt.UTC(),
			FailureCountAtOpen: record.OpenEpisode.FailureCountAtOpen,
			LastErrorKind:      record.OpenEpisode.LastErrorKind,
		}
	}
	return response
}

func (s *Server) pollInterval(ctx context.Context) (time.Duration, error) {
	if s.settings != nil {
		return s.settings.PollInterval(ctx)
	}
	return domain.DefaultPollInterval, nil
}
