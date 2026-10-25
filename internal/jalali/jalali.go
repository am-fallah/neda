// Package jalali converts Gregorian instants and civil dates to the Persian
// (Solar Hijri) calendar for notification and API presentation.
//
// Conversion is delegated to the maintained MIT-licensed
// github.com/yaa110/go-persian-calendar library. This package deliberately
// contains no hand-written calendar arithmetic; it only fixes the output shape
// used by Neda's English-language interface and notification payloads.
package jalali

import (
	"fmt"
	"time"
	_ "time/tzdata" // keep Asia/Tehran available in minimal containers

	ptime "github.com/yaa110/go-persian-calendar"
)

// Date is one Persian-calendar civil date.
type Date struct {
	Year  int
	Month int
	Day   int
}

// String returns a stable numeric form such as "1405-07-28". ASCII digits are
// used so the value is unambiguous in JSON, logs, and notification text.
func (d Date) String() string {
	if d.Year <= 0 || d.Month < 1 || d.Month > 12 || d.Day < 1 || d.Day > 31 {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
}

// FromTime converts an instant using the instant's own location. Callers that
// display Tehran time should pass t.In(Asia/Tehran).
func FromTime(value time.Time) (Date, bool) {
	if value.IsZero() {
		return Date{}, false
	}
	converted := ptime.New(value)
	year, month, day := converted.Date()
	date := Date{Year: year, Month: int(month), Day: day}
	if date.String() == "" {
		return Date{}, false
	}
	return date, true
}

// FromGregorianDate converts a civil Gregorian date (year, month, day).
func FromGregorianDate(year, month, day int) (Date, bool) {
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return Date{}, false
	}
	value := time.Date(year, time.Month(month), day, 12, 0, 0, 0, time.UTC)
	return FromTime(value)
}

// ToGregorianDate converts a civil Persian-calendar date to a Gregorian civil
// date. The returned value is noon UTC so it is safe to format as YYYY-MM-DD
// without a timezone-dependent day shift.
func ToGregorianDate(year, month, day int) (time.Time, bool) {
	if year <= 0 || month < 1 || month > 12 || day < 1 || day > 31 {
		return time.Time{}, false
	}
	converted := ptime.Date(year, ptime.Month(month), day, 12, 0, 0, 0, ptime.Iran())
	gregorian := converted.Time().In(time.UTC)
	// Reject impossible dates that the upstream library normalizes forward.
	jy, jm, jd := converted.Date()
	if jy != year || int(jm) != month || jd != day {
		return time.Time{}, false
	}
	return time.Date(gregorian.Year(), gregorian.Month(), gregorian.Day(), 12, 0, 0, 0, time.UTC), true
}

// FormatDate renders the Persian-calendar date portion of an instant,
// such as "1405-07-28".
func FormatDate(value time.Time) string {
	date, ok := FromTime(value)
	if !ok {
		return ""
	}
	return date.String()
}

// FormatDateTime renders a Persian-calendar date and 24-hour clock value,
// such as "1405-07-28 14:30".
func FormatDateTime(value time.Time) string {
	date, ok := FromTime(value)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s %02d:%02d", date.String(), value.Hour(), value.Minute())
}
