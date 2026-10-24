package mrbilit

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

const (
	// ID is the explicit MrBilit registry identifier.
	ID provider.ID = "mrbilit"
	// DisplayName is the code-owned user-facing provider name.
	DisplayName = "MrBilit"
	// DefaultBaseURL is the documented/provisional production origin.
	DefaultBaseURL = "https://train.mrbilit.com"
	// EndpointPath is the documented/provisional availability endpoint.
	EndpointPath = "/api/GetAvailable/v2"

	maxPassengers = 9
)

// ValidateConfig validates the provider-specific per-monitor configuration.
// GenderCode is rejected until an enum is validated; it is never invented or
// sent. BookingSlug is retained by the monitor layer for the future booking-URL
// task but is not used by this transport/parsing slice.
func ValidateConfig(cfg provider.MonitorProviderConfig) error {
	origin, err := normalizePositiveID(cfg.OriginID)
	if err != nil {
		return configurationError("origin id", err)
	}
	destination, err := normalizePositiveID(cfg.DestinationID)
	if err != nil {
		return configurationError("destination id", err)
	}
	if origin == destination {
		return configurationError("destination id", errors.New("must differ from origin id"))
	}
	if strings.TrimSpace(cfg.GenderCode) != "" {
		return configurationError("gender code", errors.New("is not validated for MrBilit and must be omitted"))
	}
	if err := validateBookingSlug(cfg.BookingSlug); err != nil {
		return configurationError("booking slug", err)
	}
	return nil
}

func validateCheckRequest(req provider.CheckRequest) error {
	if strings.TrimSpace(req.MonitorID) == "" {
		return configurationError("monitor id", errors.New("is required"))
	}
	if _, err := time.Parse("2006-01-02", req.DepartureDate); err != nil || len(req.DepartureDate) != len("2006-01-02") {
		return configurationError("departure date", errors.New("must be a Gregorian YYYY-MM-DD value"))
	}
	if req.Passengers.Adults < 1 || req.Passengers.Adults > maxPassengers {
		return configurationError("adults", fmt.Errorf("must be between 1 and %d", maxPassengers))
	}
	if req.Passengers.Children < 0 || req.Passengers.Children > maxPassengers {
		return configurationError("children", fmt.Errorf("must be between 0 and %d", maxPassengers))
	}
	if req.Passengers.Infants < 0 || req.Passengers.Infants > maxPassengers {
		return configurationError("infants", fmt.Errorf("must be between 0 and %d", maxPassengers))
	}
	if req.Passengers.Adults+req.Passengers.Children+req.Passengers.Infants > maxPassengers {
		return configurationError("passengers", fmt.Errorf("total must be at most %d", maxPassengers))
	}
	if err := ValidateConfig(req.Config); err != nil {
		return err
	}
	if req.Route.OriginID != "" {
		if _, err := normalizePositiveID(req.Route.OriginID); err != nil {
			return configurationError("route origin id", err)
		}
		if strings.TrimSpace(req.Route.OriginID) != strings.TrimSpace(req.Config.OriginID) {
			return configurationError("route origin id", errors.New("differs from provider config"))
		}
	}
	if req.Route.DestinationID != "" {
		if _, err := normalizePositiveID(req.Route.DestinationID); err != nil {
			return configurationError("route destination id", err)
		}
		if strings.TrimSpace(req.Route.DestinationID) != strings.TrimSpace(req.Config.DestinationID) {
			return configurationError("route destination id", errors.New("differs from provider config"))
		}
	}
	return nil
}

func buildURL(base *url.URL, req provider.CheckRequest) (*url.URL, error) {
	if base == nil {
		return nil, configurationError("base url", errors.New("is required"))
	}
	if err := validateBaseURL(base); err != nil {
		return nil, err
	}
	origin, err := normalizePositiveID(req.Config.OriginID)
	if err != nil {
		return nil, configurationError("origin id", err)
	}
	destination, err := normalizePositiveID(req.Config.DestinationID)
	if err != nil {
		return nil, configurationError("destination id", err)
	}

	endpoint := *base
	endpoint.Path = strings.TrimRight(base.Path, "/") + EndpointPath
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	query := url.Values{}
	query.Set("from", origin)
	query.Set("to", destination)
	query.Set("date", req.DepartureDate)
	query.Set("adultCount", strconv.Itoa(req.Passengers.Adults))
	query.Set("childCount", strconv.Itoa(req.Passengers.Children))
	query.Set("infantCount", strconv.Itoa(req.Passengers.Infants))
	// genderCode, exclusive, and availableStatus are intentionally omitted:
	// their enum/value semantics are unverified. Never invent them.
	endpoint.RawQuery = query.Encode()
	return &endpoint, nil
}

func normalizePositiveID(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("is required")
	}
	value, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || value <= 0 || value > math.MaxInt32 {
		return "", errors.New("must be a positive integer")
	}
	canonical := strconv.FormatInt(value, 10)
	if trimmed != canonical {
		return "", errors.New("must be a canonical positive integer")
	}
	return canonical, nil
}

func validateBaseURL(base *url.URL) error {
	if base == nil {
		return configurationError("base url", errors.New("is required"))
	}
	if base.Scheme != "https" && !(base.Scheme == "http" && isLoopbackHost(base.Hostname())) {
		return configurationError("base url", errors.New("must use https or a loopback test server"))
	}
	if base.Host == "" {
		return configurationError("base url", errors.New("host is required"))
	}
	if base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return configurationError("base url", errors.New("must not contain userinfo, query, or fragment"))
	}
	return nil
}

func configurationError(field string, err error) error {
	return &provider.Error{
		Kind: provider.ErrConfiguration,
		Op:   "mrbilit." + field,
		Err:  err,
	}
}
