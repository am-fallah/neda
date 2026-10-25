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
	ProviderID    string                    `json:"providerId"`
	OriginID      string                    `json:"originId"`
	DestinationID string                    `json:"destinationId"`
	BookingSlug   string                    `json:"bookingSlug"`
	GenderCode    string                    `json:"genderCode"`
	Selections    []monitorSelectionRequest `json:"selections"`
}

type monitorSelectionRequest struct {
	JourneyKey       string     `json:"journeyKey"`
	OfferingKey      string     `json:"offeringKey"`
	JourneyNativeID  string     `json:"journeyNativeId"`
	OfferingNativeID string     `json:"offeringNativeId"`
	TrainNumber      string     `json:"trainNumber"`
	Operator         string     `json:"operator"`
	OriginName       string     `json:"originName"`
	DestinationName  string     `json:"destinationName"`
	DepartureAt      *time.Time `json:"departureAt"`
	ArrivalAt        *time.Time `json:"arrivalAt"`
	WagonID          string     `json:"wagonId"`
	WagonName        string     `json:"wagonName"`
	ClassName        string     `json:"className"`
	CompartmentType  string     `json:"compartmentType"`
}

type monitorPatchRequest struct {
	Label           *string                        `json:"label"`
	OriginName      *string                        `json:"originName"`
	DestinationName *string                        `json:"destinationName"`
	DepartureDate   *string                        `json:"departureDate"`
	Adults          *int                           `json:"adults"`
	Children        *int                           `json:"children"`
	Infants         *int                           `json:"infants"`
	Providers       *[]monitorProviderPatchRequest `json:"providers"`
}

type monitorProviderPatchRequest struct {
	ProviderID    string                     `json:"providerId"`
	OriginID      *string                    `json:"originId"`
	DestinationID *string                    `json:"destinationId"`
	BookingSlug   *string                    `json:"bookingSlug"`
	GenderCode    *string                    `json:"genderCode"`
	Selections    *[]monitorSelectionRequest `json:"selections"`
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
	ExpiredAt       *time.Time                `json:"expiredAt"`
	CreatedAt       time.Time                 `json:"createdAt"`
	UpdatedAt       time.Time                 `json:"updatedAt"`
	Providers       []monitorProviderResponse `json:"providers"`
}

type monitorProviderResponse struct {
	ProviderID          string                     `json:"providerId"`
	Active              bool                       `json:"active"`
	OriginID            string                     `json:"originId"`
	DestinationID       string                     `json:"destinationId"`
	BookingSlug         string                     `json:"bookingSlug,omitempty"`
	GenderCode          string                     `json:"genderCode,omitempty"`
	ConfigVersion       int64                      `json:"configVersion"`
	NextDueAt           *time.Time                 `json:"nextDueAt,omitempty"`
	LastAttemptAt       *time.Time                 `json:"lastAttemptAt,omitempty"`
	LastSuccessAt       *time.Time                 `json:"lastSuccessAt,omitempty"`
	LastOutcome         string                     `json:"lastOutcome,omitempty"`
	LastErrorKind       string                     `json:"lastErrorKind,omitempty"`
	LastErrorAt         *time.Time                 `json:"lastErrorAt,omitempty"`
	ConsecutiveFailures int                        `json:"consecutiveFailures"`
	Selections          []monitorSelectionResponse `json:"selections"`
	CreatedAt           time.Time                  `json:"createdAt"`
	UpdatedAt           time.Time                  `json:"updatedAt"`
}

type monitorSelectionResponse struct {
	JourneyKey       string     `json:"journeyKey"`
	OfferingKey      string     `json:"offeringKey"`
	JourneyNativeID  string     `json:"journeyNativeId,omitempty"`
	OfferingNativeID string     `json:"offeringNativeId,omitempty"`
	TrainNumber      string     `json:"trainNumber,omitempty"`
	Operator         string     `json:"operator,omitempty"`
	OriginName       string     `json:"originName,omitempty"`
	DestinationName  string     `json:"destinationName,omitempty"`
	DepartureAt      *time.Time `json:"departureAt,omitempty"`
	ArrivalAt        *time.Time `json:"arrivalAt,omitempty"`
	WagonID          string     `json:"wagonId,omitempty"`
	WagonName        string     `json:"wagonName,omitempty"`
	ClassName        string     `json:"className,omitempty"`
	CompartmentType  string     `json:"compartmentType,omitempty"`
}

type monitorListResponse struct {
	Monitors []monitorResponse `json:"monitors"`
}

type monitorDetailResponse struct {
	monitorResponse
	Providers []monitorProviderDetailResponse `json:"providers"`
}

type monitorProviderDetailResponse struct {
	monitorProviderResponse
	Observations          []inventoryObservationResponse `json:"observations"`
	ObservationsTruncated bool                           `json:"observationsTruncated"`
	RecentChecks          []checkRunResponse             `json:"recentChecks"`
	RecentChecksTruncated bool                           `json:"recentChecksTruncated"`
}

type inventoryObservationResponse struct {
	JourneyKey  string `json:"journeyKey"`
	OfferingKey string `json:"offeringKey"`

	State      string    `json:"state"`
	StateSince time.Time `json:"stateSince"`

	LastObservedAt time.Time  `json:"lastObservedAt"`
	LastDecisiveAt *time.Time `json:"lastDecisiveAt"`
	LastMissingAt  *time.Time `json:"lastMissingAt"`
	Missing        bool       `json:"missing"`

	LastReason   string `json:"lastReason,omitempty"`
	LastCapacity *int   `json:"lastCapacity,omitempty"`

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

	UpdatedAt time.Time `json:"updatedAt"`
}

type checkRunResponse struct {
	ID            int64      `json:"id"`
	ConfigVersion int64      `json:"configVersion"`
	StartedAt     time.Time  `json:"startedAt"`
	FinishedAt    *time.Time `json:"finishedAt"`
	Outcome       string     `json:"outcome"`
	ErrorKind     string     `json:"errorKind,omitempty"`
	HTTPStatus    *int       `json:"httpStatus,omitempty"`
	DurationMS    *int64     `json:"durationMs,omitempty"`

	JourneyCount       int `json:"journeyCount"`
	OfferingCount      int `json:"offeringCount"`
	ReservableCount    int `json:"reservableCount"`
	NotReservableCount int `json:"notReservableCount"`
	UnknownCount       int `json:"unknownCount"`
	WarningCount       int `json:"warningCount"`

	CreatedAt time.Time `json:"createdAt"`
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
	now := s.now().UTC()
	for _, monitor := range monitors {
		response.Monitors = append(response.Monitors, monitorDTO(monitor, now))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) createMonitor(w http.ResponseWriter, r *http.Request) {
	var body monitorRequest
	if err := decodeJSON(r, &body); err != nil {
		writeDecodeError(w, r, err, "invalid monitor request")
		return
	}
	monitor, err := s.monitors.Create(r.Context(), body.toDomain())
	if err != nil {
		s.writeMonitorError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/monitors/"+monitor.ID)
	writeJSON(w, http.StatusCreated, monitorDTO(monitor, s.now().UTC()))
}

func (s *Server) getMonitor(w http.ResponseWriter, r *http.Request, id string) {
	detail, err := s.monitors.Detail(r.Context(), id)
	if err != nil {
		s.writeMonitorError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, monitorDetailDTO(detail, s.now().UTC()))
}

func (s *Server) updateMonitor(w http.ResponseWriter, r *http.Request, id string) {
	current, err := s.monitors.Get(r.Context(), id)
	if err != nil {
		s.writeMonitorError(w, r, err)
		return
	}
	var body monitorPatchRequest
	if err := decodeJSON(r, &body); err != nil {
		writeDecodeError(w, r, err, "invalid monitor request")
		return
	}
	monitor, err := s.monitors.Update(r.Context(), id, body.toDomain(current))
	if err != nil {
		s.writeMonitorError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, monitorDTO(monitor, s.now().UTC()))
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
	writeJSON(w, http.StatusOK, monitorDTO(monitor, s.now().UTC()))
}

func (s *Server) resumeMonitor(w http.ResponseWriter, r *http.Request, id string) {
	monitor, err := s.monitors.Resume(r.Context(), id)
	if err != nil {
		s.writeMonitorError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, monitorDTO(monitor, s.now().UTC()))
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
		s.logger.Error("monitor request failed",
			"request_id", logging.RequestID(r.Context()),
			"error", err,
		)
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
			Selections:    selectionRequestsToDomain(candidate.Selections),
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

func (p monitorPatchRequest) toDomain(current domain.Monitor) domain.MonitorInput {
	input := monitorInputFromMonitor(current)
	if p.Label != nil {
		input.Label = *p.Label
	}
	if p.OriginName != nil {
		input.OriginName = *p.OriginName
	}
	if p.DestinationName != nil {
		input.DestinationName = *p.DestinationName
	}
	if p.DepartureDate != nil {
		input.DepartureDate = *p.DepartureDate
	}
	if p.Adults != nil {
		input.Adults = *p.Adults
	}
	if p.Children != nil {
		input.Children = *p.Children
	}
	if p.Infants != nil {
		input.Infants = *p.Infants
	}
	if p.Providers == nil {
		return input
	}

	existing := make(map[provider.ID]domain.MonitorProvider, len(current.Providers))
	for _, target := range current.Providers {
		if target.Active {
			existing[target.ProviderID] = target
		}
	}
	input.Providers = make([]domain.ProviderInput, 0, len(*p.Providers))
	for _, candidate := range *p.Providers {
		providerID := provider.ID(candidate.ProviderID)
		previous, found := existing[providerID]
		originID := ""
		if candidate.OriginID != nil {
			originID = *candidate.OriginID
		} else if found {
			originID = previous.OriginID
		}
		destinationID := ""
		if candidate.DestinationID != nil {
			destinationID = *candidate.DestinationID
		} else if found {
			destinationID = previous.DestinationID
		}
		bookingSlug := ""
		if candidate.BookingSlug != nil {
			bookingSlug = *candidate.BookingSlug
		} else if found {
			bookingSlug = previous.BookingSlug
		}
		genderCode := ""
		if candidate.GenderCode != nil {
			genderCode = *candidate.GenderCode
		} else if found {
			genderCode = previous.GenderCode
		}
		var selections []provider.SelectedOption
		if candidate.Selections != nil {
			selections = selectionRequestsToDomain(*candidate.Selections)
		} else if found {
			selections = selectionRowsToOptions(previous.Selections)
		}
		input.Providers = append(input.Providers, domain.ProviderInput{
			ProviderID:    providerID,
			OriginID:      originID,
			DestinationID: destinationID,
			BookingSlug:   bookingSlug,
			GenderCode:    genderCode,
			Selections:    selections,
		})
	}
	return input
}

func monitorInputFromMonitor(monitor domain.Monitor) domain.MonitorInput {
	providers := make([]domain.ProviderInput, 0, len(monitor.Providers))
	for _, target := range monitor.ActiveProviders() {
		providers = append(providers, domain.ProviderInput{
			ProviderID:    target.ProviderID,
			OriginID:      target.OriginID,
			DestinationID: target.DestinationID,
			BookingSlug:   target.BookingSlug,
			GenderCode:    target.GenderCode,
			Selections:    selectionRowsToOptions(target.Selections),
		})
	}
	return domain.MonitorInput{
		Label:           monitor.Label,
		OriginName:      monitor.OriginName,
		DestinationName: monitor.DestinationName,
		DepartureDate:   monitor.DepartureDate,
		Adults:          monitor.Adults,
		Children:        monitor.Children,
		Infants:         monitor.Infants,
		Providers:       providers,
	}
}

func selectionRequestsToDomain(input []monitorSelectionRequest) []provider.SelectedOption {
	if len(input) == 0 {
		return nil
	}
	result := make([]provider.SelectedOption, 0, len(input))
	for _, item := range input {
		result = append(result, provider.SelectedOption{
			JourneyKey:       item.JourneyKey,
			OfferingKey:      item.OfferingKey,
			JourneyNativeID:  item.JourneyNativeID,
			OfferingNativeID: item.OfferingNativeID,
			TrainNumber:      item.TrainNumber,
			Operator:         item.Operator,
			OriginName:       item.OriginName,
			DestinationName:  item.DestinationName,
			DepartureAt:      utcOptional(item.DepartureAt),
			ArrivalAt:        utcOptional(item.ArrivalAt),
			WagonID:          item.WagonID,
			WagonName:        item.WagonName,
			ClassName:        item.ClassName,
			CompartmentType:  item.CompartmentType,
		})
	}
	return result
}

func selectionRowsToOptions(rows []domain.MonitorProviderSelection) []provider.SelectedOption {
	if len(rows) == 0 {
		return nil
	}
	result := make([]provider.SelectedOption, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.Option)
	}
	return result
}

func selectionDTOs(rows []domain.MonitorProviderSelection) []monitorSelectionResponse {
	if len(rows) == 0 {
		return []monitorSelectionResponse{}
	}
	result := make([]monitorSelectionResponse, 0, len(rows))
	for _, row := range rows {
		option := row.Option
		result = append(result, monitorSelectionResponse{
			JourneyKey:       option.JourneyKey,
			OfferingKey:      option.OfferingKey,
			JourneyNativeID:  option.JourneyNativeID,
			OfferingNativeID: option.OfferingNativeID,
			TrainNumber:      option.TrainNumber,
			Operator:         option.Operator,
			OriginName:       option.OriginName,
			DestinationName:  option.DestinationName,
			DepartureAt:      utcOptional(option.DepartureAt),
			ArrivalAt:        utcOptional(option.ArrivalAt),
			WagonID:          option.WagonID,
			WagonName:        option.WagonName,
			ClassName:        option.ClassName,
			CompartmentType:  option.CompartmentType,
		})
	}
	return result
}

func monitorDTO(monitor domain.Monitor, now time.Time) monitorResponse {
	providers := make([]monitorProviderResponse, 0, len(monitor.Providers))
	for _, target := range monitor.Providers {
		providers = append(providers, monitorProviderDTO(target))
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

func monitorProviderDTO(target domain.MonitorProvider) monitorProviderResponse {
	return monitorProviderResponse{
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
		Selections:          selectionDTOs(target.Selections),
		CreatedAt:           target.CreatedAt.UTC(),
		UpdatedAt:           target.UpdatedAt.UTC(),
	}
}

func monitorDetailDTO(detail domain.MonitorDetail, now time.Time) monitorDetailResponse {
	base := monitorDTO(detail.Monitor, now)
	targets := make(map[int64]domain.MonitorTargetDetail, len(detail.Targets))
	for _, target := range detail.Targets {
		targets[target.MonitorProviderID] = target
	}

	providers := make([]monitorProviderDetailResponse, 0, len(detail.Monitor.Providers))
	for _, target := range detail.Monitor.Providers {
		diagnostics := targets[target.ID]
		observations := make([]inventoryObservationResponse, 0, len(diagnostics.Observations))
		for _, observation := range diagnostics.Observations {
			observations = append(observations, inventoryObservationDTO(observation))
		}
		checks := make([]checkRunResponse, 0, len(diagnostics.RecentChecks))
		for _, check := range diagnostics.RecentChecks {
			checks = append(checks, checkRunDTO(check))
		}
		providers = append(providers, monitorProviderDetailResponse{
			monitorProviderResponse: monitorProviderDTO(target),
			Observations:            observations,
			ObservationsTruncated:   diagnostics.ObservationsTruncated,
			RecentChecks:            checks,
			RecentChecksTruncated:   diagnostics.RecentChecksTruncated,
		})
	}
	return monitorDetailResponse{monitorResponse: base, Providers: providers}
}

func inventoryObservationDTO(observation domain.InventoryObservation) inventoryObservationResponse {
	return inventoryObservationResponse{
		JourneyKey:      observation.JourneyKey,
		OfferingKey:     observation.OfferingKey,
		State:           string(observation.State),
		StateSince:      observation.StateSince.UTC(),
		LastObservedAt:  observation.LastObservedAt.UTC(),
		LastDecisiveAt:  utcOptional(observation.LastDecisiveAt),
		LastMissingAt:   utcOptional(observation.LastMissingAt),
		Missing:         observation.Missing,
		LastReason:      observation.LastReason,
		LastCapacity:    observation.LastCapacity,
		TrainNumber:     observation.TrainNumber,
		Operator:        observation.Operator,
		OriginName:      observation.OriginName,
		DestinationName: observation.DestinationName,
		DepartureAt:     utcOptional(observation.DepartureAt),
		ArrivalAt:       utcOptional(observation.ArrivalAt),
		WagonName:       observation.WagonName,
		ClassName:       observation.ClassName,
		PriceAmount:     observation.PriceAmount,
		PriceCurrency:   observation.PriceCurrency,
		BookingURL:      observation.BookingURL,
		UpdatedAt:       observation.UpdatedAt.UTC(),
	}
}

func checkRunDTO(check domain.CheckRunDiagnostic) checkRunResponse {
	return checkRunResponse{
		ID:                 check.ID,
		ConfigVersion:      check.ConfigVersion,
		StartedAt:          check.StartedAt.UTC(),
		FinishedAt:         utcOptional(check.FinishedAt),
		Outcome:            string(check.Outcome),
		ErrorKind:          check.ErrorKind,
		HTTPStatus:         check.HTTPStatus,
		DurationMS:         check.DurationMS,
		JourneyCount:       check.JourneyCount,
		OfferingCount:      check.OfferingCount,
		ReservableCount:    check.ReservableCount,
		NotReservableCount: check.NotReservableCount,
		UnknownCount:       check.UnknownCount,
		WarningCount:       check.WarningCount,
		CreatedAt:          check.CreatedAt.UTC(),
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
