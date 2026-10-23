package domain

import (
	"errors"
	"fmt"
	"time"

	_ "time/tzdata" // keep Asia/Tehran available without OS tzdata
)

const dateLayout = "2006-01-02"

// TehranLocation loads the IANA Asia/Tehran location used for departure-day
// expiry and "today" calculations.
func TehranLocation() (*time.Location, error) {
	location, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		return nil, fmt.Errorf("domain: load Asia/Tehran: %w", err)
	}
	return location, nil
}

// CivilDate returns the current civil date in loc.
func CivilDate(now time.Time, loc *time.Location) string {
	return now.In(loc).Format(dateLayout)
}

// DepartureExpiry returns the UTC instant at the start of the day after date
// in loc. The date must be a canonical Gregorian YYYY-MM-DD value.
func DepartureExpiry(date string, loc *time.Location) (time.Time, error) {
	if loc == nil {
		return time.Time{}, errors.New("domain: location is required")
	}
	parsed, err := time.Parse(dateLayout, date)
	if err != nil || parsed.Format(dateLayout) != date {
		return time.Time{}, fmt.Errorf("domain: departure date %q is not a Gregorian YYYY-MM-DD value", date)
	}
	expiry := time.Date(parsed.Year(), parsed.Month(), parsed.Day()+1, 0, 0, 0, 0, loc)
	return expiry.UTC(), nil
}

// ValidateDepartureDate checks canonical format and that the date is not
// before today in loc. It returns the computed expiry.
func ValidateDepartureDate(date string, now time.Time, loc *time.Location) (time.Time, error) {
	if loc == nil {
		return time.Time{}, errors.New("domain: location is required")
	}
	expiry, err := DepartureExpiry(date, loc)
	if err != nil {
		return time.Time{}, err
	}
	today := CivilDate(now, loc)
	if date < today {
		return time.Time{}, fmt.Errorf("domain: departure date %q is before today %q in Asia/Tehran", date, today)
	}
	return expiry, nil
}
