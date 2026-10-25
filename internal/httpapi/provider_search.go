package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/logging"
	"github.com/am-fallah/neda/internal/provider"
)

const searchTimeout = 30 * time.Second

type providerSearchRequest struct {
	URL      string `json:"url"`
	Adults   int    `json:"adults"`
	Children int    `json:"children"`
	Infants  int    `json:"infants"`
}

type providerSearchResponse struct {
	URL           string                  `json:"url"`
	Slug          string                  `json:"slug"`
	JalaliDate    string                  `json:"jalaliDate"`
	DepartureDate string                  `json:"departureDate"`
	Route         searchRouteResponse     `json:"route"`
	Passengers    searchPassengerResponse `json:"passengers"`
	Trains        []searchTrainResponse   `json:"trains"`
	Warnings      []string                `json:"warnings"`
	ObservedAt    time.Time               `json:"observedAt"`
}

type searchRouteResponse struct {
	OriginID        string `json:"originId"`
	DestinationID   string `json:"destinationId"`
	OriginName      string `json:"originName"`
	DestinationName string `json:"destinationName"`
}

type searchPassengerResponse struct {
	Adults   int `json:"adults"`
	Children int `json:"children"`
	Infants  int `json:"infants"`
}

type searchTrainResponse struct {
	JourneyKey      string                `json:"journeyKey"`
	NativeID        string                `json:"nativeId,omitempty"`
	TrainNumber     string                `json:"trainNumber,omitempty"`
	Operator        string                `json:"operator,omitempty"`
	OriginName      string                `json:"originName,omitempty"`
	DestinationName string                `json:"destinationName,omitempty"`
	DepartureAt     *time.Time            `json:"departureAt,omitempty"`
	ArrivalAt       *time.Time            `json:"arrivalAt,omitempty"`
	Classes         []searchClassResponse `json:"classes"`
}

type searchClassResponse struct {
	OfferingKey     string `json:"offeringKey"`
	NativeID        string `json:"nativeId,omitempty"`
	WagonID         string `json:"wagonId,omitempty"`
	WagonName       string `json:"wagonName,omitempty"`
	ClassName       string `json:"className,omitempty"`
	CompartmentType string `json:"compartmentType,omitempty"`
	Capacity        *int   `json:"capacity"`
	State           string `json:"state"`
	Reason          string `json:"reason,omitempty"`
	PriceAmount     *int64 `json:"priceAmount,omitempty"`
	PriceCurrency   string `json:"priceCurrency,omitempty"`
}

func (s *Server) handleProviderSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set(headerAllow, http.MethodPost)
		writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/providers/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "search" {
		writeError(w, r, http.StatusNotFound, "not_found", "not found")
		return
	}
	if s.providers == nil {
		writeError(w, r, http.StatusServiceUnavailable, "providers_unavailable", "providers are not available")
		return
	}
	adapter, ok := s.providers.Lookup(provider.ID(parts[0]))
	if !ok || adapter == nil {
		writeError(w, r, http.StatusNotFound, "provider_not_registered", "provider is not registered")
		return
	}
	searcher, ok := adapter.(provider.URLSearcher)
	if !ok {
		writeError(w, r, http.StatusNotFound, "search_not_supported", "provider does not support URL search")
		return
	}
	var body providerSearchRequest
	if err := decodeJSON(r, &body); err != nil {
		writeDecodeError(w, r, err, "invalid search request")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), searchTimeout)
	defer cancel()
	result, err := searcher.SearchURL(ctx, provider.SearchRequest{
		URL: body.URL,
		Passengers: provider.Passengers{
			Adults:   body.Adults,
			Children: body.Children,
			Infants:  body.Infants,
		},
	})
	if err != nil {
		s.writeSearchError(w, r, adapter.Metadata(), err)
		return
	}
	writeJSON(w, http.StatusOK, searchDTO(result))
}

func (s *Server) writeSearchError(w http.ResponseWriter, r *http.Request, metadata provider.Metadata, err error) {
	providerName := strings.TrimSpace(metadata.DisplayName)
	if providerName == "" {
		providerName = "The provider"
	}
	var inputErr *provider.InputError
	if errors.As(err, &inputErr) {
		if inputErr.Field == "" {
			writeError(w, r, http.StatusUnprocessableEntity, inputErr.Code, inputErr.Message)
			return
		}
		writeErrorWithFields(w, r, http.StatusUnprocessableEntity, inputErr.Code, inputErr.Message, map[string]string{
			inputErr.Field: inputErr.Message,
		})
		return
	}
	var providerErr *provider.Error
	if errors.As(err, &providerErr) {
		switch providerErr.Kind {
		case provider.ErrRouteNotFound:
			writeError(w, r, http.StatusUnprocessableEntity, "route_not_found", providerName+" did not find this route and date. This is not a sold-out result.")
		case provider.ErrTimeout:
			writeError(w, r, http.StatusGatewayTimeout, "provider_timeout", providerName+" did not respond in time. No availability conclusion was made.")
		case provider.ErrRateLimited:
			writeError(w, r, http.StatusServiceUnavailable, "provider_rate_limited", providerName+" rate-limited the request. Try again later.")
		case provider.ErrNetwork, provider.ErrHTTPStatus, provider.ErrInvalidResponse:
			writeError(w, r, http.StatusBadGateway, "provider_error", providerName+" returned an unusable response. No availability conclusion was made.")
		case provider.ErrConfiguration:
			writeError(w, r, http.StatusInternalServerError, "provider_configuration", providerName+" search is not configured correctly.")
		case provider.ErrCanceled:
			writeError(w, r, http.StatusRequestTimeout, "provider_canceled", providerName+" search was canceled.")
		default:
			writeError(w, r, http.StatusBadGateway, "provider_error", providerName+" search failed. No availability conclusion was made.")
		}
		s.logger.Warn("provider search failed",
			"request_id", logging.RequestID(r.Context()),
			"provider_id", string(metadata.ID),
			"kind", string(providerErr.Kind),
			"op", providerErr.Op,
			"upstream_status", providerErr.StatusCode,
		)
		return
	}
	s.logger.Error("provider search failed",
		"request_id", logging.RequestID(r.Context()),
		"provider_id", string(metadata.ID),
		"code", "search_failed",
	)
	writeError(w, r, http.StatusBadGateway, "provider_error", providerName+" search failed. No availability conclusion was made.")
}

func searchDTO(result provider.SearchResult) providerSearchResponse {
	trains := make([]searchTrainResponse, 0, len(result.Result.Journeys))
	for _, journey := range result.Result.Journeys {
		classes := make([]searchClassResponse, 0, len(journey.Offerings))
		for _, offering := range journey.Offerings {
			var priceAmount *int64
			priceCurrency := ""
			if offering.Offering.Price != nil {
				amount := offering.Offering.Price.Amount
				priceAmount = &amount
				priceCurrency = offering.Offering.Price.Currency
			}
			classes = append(classes, searchClassResponse{
				OfferingKey:     offering.Offering.Key,
				NativeID:        offering.Offering.NativeID,
				WagonID:         offering.Offering.WagonID,
				WagonName:       offering.Offering.WagonName,
				ClassName:       offering.Offering.ClassName,
				CompartmentType: offering.Offering.CompartmentType,
				Capacity:        offering.Capacity,
				State:           string(offering.State),
				Reason:          offering.Reason,
				PriceAmount:     priceAmount,
				PriceCurrency:   priceCurrency,
			})
		}
		trains = append(trains, searchTrainResponse{
			JourneyKey:      journey.Journey.Key,
			NativeID:        journey.Journey.NativeID,
			TrainNumber:     journey.Journey.TrainNumber,
			Operator:        journey.Journey.Operator,
			OriginName:      journey.Journey.OriginName,
			DestinationName: journey.Journey.DestinationName,
			DepartureAt:     utcOptional(journey.Journey.DepartureAt),
			ArrivalAt:       utcOptional(journey.Journey.ArrivalAt),
			Classes:         classes,
		})
	}
	warnings := result.Result.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	return providerSearchResponse{
		URL:           result.URL,
		Slug:          result.Slug,
		JalaliDate:    result.JalaliDate,
		DepartureDate: result.DepartureDate,
		Route: searchRouteResponse{
			OriginID:        result.Route.OriginID,
			DestinationID:   result.Route.DestinationID,
			OriginName:      result.Route.OriginName,
			DestinationName: result.Route.DestinationName,
		},
		Passengers: searchPassengerResponse{
			Adults:   result.Passengers.Adults,
			Children: result.Passengers.Children,
			Infants:  result.Passengers.Infants,
		},
		Trains:     trains,
		Warnings:   warnings,
		ObservedAt: result.Result.ObservedAt.UTC(),
	}
}
