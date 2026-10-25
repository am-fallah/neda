package mrbilit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/jalali"
	"github.com/am-fallah/neda/internal/provider"
)

const (
	searchURLMaxLength = 2048
	searchPageMaxBytes = 4 << 20
	searchPageTimeout  = 10 * time.Second
	searchDateLayout   = "2006-01-02"
)

var (
	nuxtURLParamsPattern      = regexp.MustCompile(`urlParams\s*:\s*\{([^{}]*)\}`)
	originCodePattern         = regexp.MustCompile(`\boriginCode\s*:\s*(\d+)`)
	destinationCodePattern    = regexp.MustCompile(`\bdestinationCode\s*:\s*(\d+)`)
	originStationIDPattern    = regexp.MustCompile(`domesticOrigin\s*:\s*\{[^{}]*\bstationId\s*:\s*(\d+)`)
	destinationStationPattern = regexp.MustCompile(`domesticDestination\s*:\s*\{[^{}]*\bstationId\s*:\s*(\d+)`)
)

type searchURL struct {
	raw           string
	slug          string
	jalaliDate    string
	gregorianDate time.Time
}

func (s searchURL) canonical() string {
	value := url.URL{
		Scheme: "https",
		Host:   bookingHost,
		Path:   "/trains/" + s.slug,
	}
	query := url.Values{}
	query.Set(bookingDateKey, s.jalaliDate)
	value.RawQuery = query.Encode()
	return value.String()
}

// SearchURL resolves a public MrBilit train-search URL, fetches the initial
// result through the existing API adapter, and returns normalized data without
// touching monitor state.
func (p *Provider) SearchURL(ctx context.Context, req provider.SearchRequest) (provider.SearchResult, error) {
	if p == nil || p.client == nil || p.pageClient == nil || p.pageBase == nil {
		return provider.SearchResult{}, errors.New("mrbilit: provider is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return provider.SearchResult{}, contextError(err)
	}
	parsed, err := parseSearchURL(req.URL)
	if err != nil {
		return provider.SearchResult{}, err
	}
	if err := p.validateSearchDate(parsed); err != nil {
		return provider.SearchResult{}, err
	}
	if err := validateSearchPassengers(req.Passengers); err != nil {
		return provider.SearchResult{}, err
	}
	originID, destinationID, err := p.resolveRouteIDs(ctx, parsed)
	if err != nil {
		return provider.SearchResult{}, err
	}
	checkRequest := provider.CheckRequest{
		MonitorID:     "search",
		DepartureDate: parsed.gregorianDate.Format(searchDateLayout),
		Route: provider.Route{
			OriginID:      originID,
			DestinationID: destinationID,
		},
		Passengers: req.Passengers,
		Config: provider.MonitorProviderConfig{
			OriginID:      originID,
			DestinationID: destinationID,
			BookingSlug:   parsed.slug,
		},
	}
	result, body, err := p.fetch(ctx, checkRequest)
	if err != nil {
		return provider.SearchResult{}, err
	}
	originName, destinationName := routeNames(body)
	if originName == "" {
		originName = firstJourneyName(result, true)
	}
	if destinationName == "" {
		destinationName = firstJourneyName(result, false)
	}
	if originName == "" {
		originName = displaySlugPart(parsed.slug, 0)
	}
	if destinationName == "" {
		destinationName = displaySlugPart(parsed.slug, 1)
	}
	return provider.SearchResult{
		URL:           parsed.canonical(),
		Slug:          parsed.slug,
		JalaliDate:    parsed.jalaliDate,
		DepartureDate: parsed.gregorianDate.Format(searchDateLayout),
		Route: provider.Route{
			OriginID:        originID,
			DestinationID:   destinationID,
			OriginName:      originName,
			DestinationName: destinationName,
		},
		Passengers: req.Passengers,
		Result:     result,
	}, nil
}

func parseSearchURL(raw string) (searchURL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return searchURL{}, searchInputError("url", "url_required", "Enter a MrBilit train search URL.")
	}
	if len(raw) > searchURLMaxLength {
		return searchURL{}, searchInputError("url", "url_too_long", "The MrBilit search URL is too long.")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return searchURL{}, searchInputError("url", "invalid_url", "Enter a valid MrBilit train search URL.")
	}
	if parsed.Scheme != "https" || parsed.Host != bookingHost || parsed.User != nil || parsed.Fragment != "" || parsed.Port() != "" {
		return searchURL{}, searchInputError("url", "unsupported_url", "Use an https://mrbilit.com/trains/<route>?departureDate=<Jalali date> URL.")
	}
	path := parsed.Path
	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}
	slug := strings.TrimPrefix(path, "/trains/")
	if slug == path || slug == "" || strings.Contains(slug, "/") || validateBookingSlug(slug) != nil {
		return searchURL{}, searchInputError("url", "unsupported_url", "The URL must contain a supported MrBilit train route slug.")
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return searchURL{}, searchInputError("url", "invalid_url", "The MrBilit search URL query is invalid.")
	}
	dates, ok := query[bookingDateKey]
	if !ok || len(dates) != 1 {
		return searchURL{}, searchInputError("url", "missing_date", "The URL must contain exactly one departureDate value.")
	}
	jalaliDate, gregorian, err := parseJalaliSearchDate(dates[0])
	if err != nil {
		return searchURL{}, err
	}
	return searchURL{
		raw:           raw,
		slug:          slug,
		jalaliDate:    jalaliDate,
		gregorianDate: gregorian,
	}, nil
}

func parseJalaliSearchDate(value string) (string, time.Time, error) {
	value = strings.TrimSpace(value)
	if len(value) != len(searchDateLayout) {
		return "", time.Time{}, searchInputError("url", "invalid_date", "departureDate must be a Jalali date in YYYY-MM-DD form.")
	}
	parsed, err := time.Parse(searchDateLayout, value)
	if err != nil {
		return "", time.Time{}, searchInputError("url", "invalid_date", "departureDate must be a Jalali date in YYYY-MM-DD form.")
	}
	gregorian, ok := jalali.ToGregorianDate(parsed.Year(), int(parsed.Month()), parsed.Day())
	if !ok {
		return "", time.Time{}, searchInputError("url", "invalid_date", "departureDate is not a valid Jalali calendar date.")
	}
	return value, gregorian, nil
}

func (p *Provider) validateSearchDate(parsed searchURL) error {
	now := p.now().In(tehranLocation)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	candidate := time.Date(parsed.gregorianDate.Year(), parsed.gregorianDate.Month(), parsed.gregorianDate.Day(), 0, 0, 0, 0, time.UTC)
	if candidate.Before(today) {
		return searchInputError("url", "past_date", "The departure date must not be before today in Tehran.")
	}
	return nil
}

func validateSearchPassengers(passengers provider.Passengers) error {
	if passengers.Adults < 1 || passengers.Adults > maxPassengers {
		return searchInputError("adults", "invalid_adults", fmt.Sprintf("Adults must be between 1 and %d.", maxPassengers))
	}
	if passengers.Children < 0 || passengers.Children > maxPassengers {
		return searchInputError("children", "invalid_children", fmt.Sprintf("Children must be between 0 and %d.", maxPassengers))
	}
	if passengers.Infants < 0 || passengers.Infants > maxPassengers {
		return searchInputError("infants", "invalid_infants", fmt.Sprintf("Infants must be between 0 and %d.", maxPassengers))
	}
	if passengers.Adults+passengers.Children+passengers.Infants > maxPassengers {
		return searchInputError("passengers", "too_many_passengers", fmt.Sprintf("Total passengers must be at most %d.", maxPassengers))
	}
	return nil
}

func searchInputError(field, code, message string) error {
	return &provider.InputError{Field: field, Code: code, Message: message}
}

func (p *Provider) resolveRouteIDs(ctx context.Context, parsed searchURL) (string, string, error) {
	pageURL := *p.pageBase
	pageURL.Path = strings.TrimRight(p.pageBase.Path, "/") + "/trains/" + parsed.slug
	query := url.Values{}
	query.Set(bookingDateKey, parsed.jalaliDate)
	pageURL.RawQuery = query.Encode()
	pageURL.Fragment = ""

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL.String(), nil)
	if err != nil {
		return "", "", &provider.Error{Kind: provider.ErrConfiguration, Op: "mrbilit.search.request", Err: errors.New("invalid search page request")}
	}
	request.Header.Set("Accept", "text/html")
	request.Header.Set("User-Agent", "Neda/0.1.0 (self-hosted monitor)")
	started := time.Now()
	response, err := p.pageClient.Do(request)
	duration := time.Since(started)
	if err != nil {
		if errors.Is(err, errRedirectRejected) {
			p.client.logAttempt(&pageURL, 0, duration, "redirect_rejected")
			return "", "", &provider.Error{Kind: provider.ErrInvalidResponse, Op: "mrbilit.search.page", StatusCode: http.StatusFound, Err: errors.New("redirect rejected")}
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			p.client.logAttempt(&pageURL, 0, duration, "caller_done")
			return "", "", contextError(ctxErr)
		}
		if isTimeoutError(err) {
			p.client.logAttempt(&pageURL, 0, duration, "timeout")
			return "", "", &provider.Error{Kind: provider.ErrTimeout, Op: "mrbilit.search.page", Err: context.DeadlineExceeded}
		}
		p.client.logAttempt(&pageURL, 0, duration, "network_error")
		return "", "", &provider.Error{Kind: provider.ErrNetwork, Op: "mrbilit.search.page", Err: errors.New("network error")}
	}
	defer response.Body.Close()
	p.client.logAttempt(&pageURL, response.StatusCode, duration, "response")

	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
		return "", "", searchInputError("url", "route_not_found", "MrBilit did not find a train page for this route and date.")
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return "", "", &provider.Error{
			Kind:       provider.ErrRateLimited,
			Op:         "mrbilit.search.page",
			StatusCode: response.StatusCode,
			RetryAfter: parseRetryAfter(response.Header.Get("Retry-After")),
			Err:        errors.New("rate limited"),
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", "", &provider.Error{
			Kind:       provider.ErrHTTPStatus,
			Op:         "mrbilit.search.page",
			StatusCode: response.StatusCode,
			Err:        errors.New("unexpected status"),
		}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, searchPageMaxBytes+1))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", "", contextError(ctxErr)
		}
		return "", "", &provider.Error{Kind: provider.ErrNetwork, Op: "mrbilit.search.page", Err: errors.New("read failed")}
	}
	if int64(len(body)) > searchPageMaxBytes {
		return "", "", &provider.Error{Kind: provider.ErrInvalidResponse, Op: "mrbilit.search.page", Err: errors.New("page too large")}
	}
	return extractRouteIDs(body)
}

func extractRouteIDs(body []byte) (string, string, error) {
	text := string(body)
	pairs := make(map[string]struct{})
	for _, match := range nuxtURLParamsPattern.FindAllStringSubmatch(text, -1) {
		block := match[1]
		origin := firstCapture(originCodePattern, block)
		destination := firstCapture(destinationCodePattern, block)
		if origin != "" && destination != "" {
			pairs[origin+"\x00"+destination] = struct{}{}
		}
	}
	if len(pairs) == 0 {
		origin := firstCapture(originStationIDPattern, text)
		destination := firstCapture(destinationStationPattern, text)
		if origin != "" && destination != "" {
			pairs[origin+"\x00"+destination] = struct{}{}
		}
	}
	if len(pairs) == 0 {
		return "", "", searchInputError("url", "route_resolution_failed", "Could not read the route IDs from this MrBilit search page. Use a standard train search URL.")
	}
	if len(pairs) > 1 {
		return "", "", &provider.Error{Kind: provider.ErrInvalidResponse, Op: "mrbilit.search.page", Err: errors.New("conflicting route ids")}
	}
	var origin, destination string
	for pair := range pairs {
		parts := strings.SplitN(pair, "\x00", 2)
		origin, destination = parts[0], parts[1]
	}
	normalizedOrigin, err := normalizePositiveID(origin)
	if err != nil {
		return "", "", &provider.Error{Kind: provider.ErrInvalidResponse, Op: "mrbilit.search.page", Err: errors.New("invalid origin id")}
	}
	normalizedDestination, err := normalizePositiveID(destination)
	if err != nil || normalizedOrigin == normalizedDestination {
		return "", "", &provider.Error{Kind: provider.ErrInvalidResponse, Op: "mrbilit.search.page", Err: errors.New("invalid destination id")}
	}
	return normalizedOrigin, normalizedDestination, nil
}

func firstCapture(pattern *regexp.Regexp, value string) string {
	match := pattern.FindStringSubmatch(value)
	if len(match) < 2 {
		return ""
	}
	parsed, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil || parsed <= 0 {
		return ""
	}
	return strconv.FormatInt(parsed, 10)
}

func routeNames(payload []byte) (string, string) {
	var envelope rawRealEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return "", ""
	}
	origin := ""
	destination := ""
	if envelope.FromLocation.Title.Valid {
		origin = normalizeText(envelope.FromLocation.Title.Value)
	}
	if envelope.ToLocation.Title.Valid {
		destination = normalizeText(envelope.ToLocation.Title.Value)
	}
	return origin, destination
}

func firstJourneyName(result provider.Result, origin bool) string {
	for _, journey := range result.Journeys {
		if origin && journey.Journey.OriginName != "" {
			return journey.Journey.OriginName
		}
		if !origin && journey.Journey.DestinationName != "" {
			return journey.Journey.DestinationName
		}
	}
	return ""
}

func displaySlugPart(slug string, index int) string {
	parts := strings.Split(slug, "-")
	if index < 0 || index >= len(parts) || parts[index] == "" {
		return ""
	}
	value := parts[index]
	return strings.ToUpper(value[:1]) + value[1:]
}

func validateSearchPageBaseURL(base *url.URL) (*url.URL, error) {
	if base == nil {
		parsed, err := url.Parse("https://" + bookingHost)
		if err != nil {
			return nil, configurationError("search page base url", err)
		}
		return parsed, nil
	}
	if (base.Scheme != "https" && base.Scheme != "http") || base.Host == "" || base.User != nil {
		return nil, configurationError("search page base url", errors.New("must be an http(s) URL without credentials"))
	}
	cloned := *base
	cloned.Path = strings.TrimRight(cloned.Path, "/")
	cloned.RawQuery = ""
	cloned.Fragment = ""
	return &cloned, nil
}
