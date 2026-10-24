package mrbilit

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/jalali"
	"github.com/am-fallah/neda/internal/provider"
)

const (
	bookingHost      = "mrbilit.com"
	bookingDateKey   = "departureDate"
	maxBookingSlug   = 100
	bookingDateShape = "2006-01-02"
)

// The owner-provided reference URL is:
// https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14
// for 2026-11-05. The slug is supplied manually; it is never derived from the
// display city names.
var bookingSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// validateBookingSlug accepts only a conservative, already-lowercase route
// slug. An empty slug is allowed and means "no booking link".
func validateBookingSlug(slug string) error {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil
	}
	if len(slug) > maxBookingSlug || !bookingSlugPattern.MatchString(slug) {
		return errors.New("must be lowercase letters, digits, and single hyphens")
	}
	return nil
}

// buildBookingURL constructs the owner-verified MrBilit booking URL from a
// configured route slug and a Gregorian departure date. It returns false for
// any unvalidated slug/date rather than emitting a misleading link.
func buildBookingURL(slug, departureDate string) (*url.URL, bool) {
	slug = strings.TrimSpace(slug)
	if slug == "" || validateBookingSlug(slug) != nil {
		return nil, false
	}
	departureDate = strings.TrimSpace(departureDate)
	parsedDate, err := time.Parse(bookingDateShape, departureDate)
	if err != nil || len(departureDate) != len(bookingDateShape) {
		return nil, false
	}
	persian, ok := jalali.FromGregorianDate(parsedDate.Year(), int(parsedDate.Month()), parsedDate.Day())
	if !ok {
		return nil, false
	}
	persianDate := persian.String()
	if persianDate == "" {
		return nil, false
	}
	query := url.Values{}
	query.Set(bookingDateKey, persianDate)
	return &url.URL{
		Scheme:   "https",
		Host:     bookingHost,
		Path:     "/trains/" + slug,
		RawQuery: query.Encode(),
	}, true
}

func attachBookingURL(offerings []provider.OfferingObservation, slug, departureDate string) []provider.OfferingObservation {
	booking, ok := buildBookingURL(slug, departureDate)
	if !ok {
		return offerings
	}
	for index := range offerings {
		copyURL := *booking
		offerings[index].BookingURL = &copyURL
	}
	return offerings
}
