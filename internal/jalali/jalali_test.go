package jalali

import (
	"testing"
	"time"
)

func tehran(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		t.Fatalf("load Tehran: %v", err)
	}
	return loc
}

func TestKnownGregorianToJalaliDates(t *testing.T) {
	loc := tehran(t)
	cases := []struct {
		gregorian string
		want      string
	}{
		{"2026-10-20", "1405-07-28"},
		{"2026-11-05", "1405-08-14"},
		{"2026-03-20", "1404-12-29"},
		{"2026-03-21", "1405-01-01"},
		{"2025-03-20", "1403-12-30"}, // leap Esfand
		{"2025-03-21", "1404-01-01"},
		{"2026-02-19", "1404-11-30"},
		{"2026-02-20", "1404-12-01"},
	}
	for _, tc := range cases {
		t.Run(tc.gregorian, func(t *testing.T) {
			value, err := time.ParseInLocation("2006-01-02", tc.gregorian, loc)
			if err != nil {
				t.Fatal(err)
			}
			if got := FormatDate(value); got != tc.want {
				t.Fatalf("FormatDate(%s) = %q, want %q", tc.gregorian, got, tc.want)
			}
			if got := FormatDateTime(value.Add(14*time.Hour + 30*time.Minute)); got != tc.want+" 14:30" {
				t.Fatalf("FormatDateTime = %q", got)
			}
		})
	}
}

func TestToGregorianDateKnownValues(t *testing.T) {
	tests := []struct {
		year, month, day int
		want             string
	}{
		{1405, 8, 14, "2026-11-05"},
		{1405, 8, 24, "2026-11-15"},
		{1405, 7, 28, "2026-10-20"},
		{1403, 12, 30, "2025-03-20"},
		{1406, 1, 1, "2027-03-21"},
	}
	for _, tc := range tests {
		got, ok := ToGregorianDate(tc.year, tc.month, tc.day)
		if !ok || got.Format("2006-01-02") != tc.want {
			t.Fatalf("ToGregorianDate(%d-%d-%d) = (%v,%v), want %s", tc.year, tc.month, tc.day, got, ok, tc.want)
		}
	}
	for _, tc := range []struct{ year, month, day int }{
		{0, 1, 1}, {1405, 13, 1}, {1405, 0, 1}, {1405, 1, 32},
	} {
		if _, ok := ToGregorianDate(tc.year, tc.month, tc.day); ok {
			t.Fatalf("ToGregorianDate(%d-%d-%d) accepted invalid date", tc.year, tc.month, tc.day)
		}
	}
}

func TestFromGregorianDateAndBoundaries(t *testing.T) {
	for _, tc := range []struct {
		year, month, day int
		want             string
		ok               bool
	}{
		{2026, 10, 20, "1405-07-28", true},
		{2025, 3, 20, "1403-12-30", true}, // Esfand 30 in a leap year
		{2024, 3, 20, "1403-01-01", true},
		{2026, 13, 1, "", false},
		{2026, 0, 1, "", false},
		{2026, 1, 32, "", false},
		{2026, 1, 0, "", false},
	} {
		got, ok := FromGregorianDate(tc.year, tc.month, tc.day)
		if ok != tc.ok || got.String() != tc.want {
			t.Fatalf("FromGregorianDate(%d-%d-%d) = (%q,%v), want (%q,%v)",
				tc.year, tc.month, tc.day, got.String(), ok, tc.want, tc.ok)
		}
	}
}

func TestZeroAndInvalidDates(t *testing.T) {
	if got := FormatDate(time.Time{}); got != "" {
		t.Fatalf("zero FormatDate = %q", got)
	}
	if got := FormatDateTime(time.Time{}); got != "" {
		t.Fatalf("zero FormatDateTime = %q", got)
	}
	if got := (Date{Year: 0, Month: 1, Day: 1}).String(); got != "" {
		t.Fatalf("invalid Date.String = %q", got)
	}
	if got := (Date{Year: 1405, Month: 13, Day: 1}).String(); got != "" {
		t.Fatalf("invalid month Date.String = %q", got)
	}
}

// TestTehranMidnightBoundary proves conversion follows the instant's Tehran
// date rather than the server's local timezone.
func TestTehranMidnightBoundary(t *testing.T) {
	loc := tehran(t)
	before := time.Date(2026, 3, 20, 23, 59, 59, 0, loc)
	after := time.Date(2026, 3, 21, 0, 0, 0, 0, loc)
	if got := FormatDate(before); got != "1404-12-29" {
		t.Fatalf("before midnight = %q", got)
	}
	if got := FormatDate(after); got != "1405-01-01" {
		t.Fatalf("after midnight = %q", got)
	}
}
