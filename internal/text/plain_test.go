package text

import (
	"testing"
	"time"
)

// TestParseDayReadsOnlyADayThatRoundTrips: ParseDay gives the time of a YYYY-MM-DD day (lifelog_meta.days), and a
// zero time and false for anything else. A string that time.Parse reads but that does not round-trip is not a day:
// "+026-10-05" is read as the year 26. IsDay agrees with ParseDay.
func TestParseDayReadsOnlyADayThatRoundTrips(t *testing.T) {
	for _, tc := range []struct {
		s    string
		ok   bool
		want time.Time
	}{
		{"2026-10-05", true, time.Date(2026, time.October, 5, 0, 0, 0, 0, time.UTC)},
		{"0000-01-01", true, time.Date(0, time.January, 1, 0, 0, 0, 0, time.UTC)},
		{"9999-12-31", true, time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC)},
		{"2028-02-29", true, time.Date(2028, time.February, 29, 0, 0, 0, 0, time.UTC)},
		{"2026-02-29", false, time.Time{}},
		{"2026-10-5", false, time.Time{}},
		{"+026-10-05", false, time.Time{}},
		{"-001-12-26", false, time.Time{}},
		{"-0001-12-26", false, time.Time{}},
		{"2026-10-05T00:00:00Z", false, time.Time{}},
		{" 2026-10-05", false, time.Time{}},
		{"", false, time.Time{}},
	} {
		got, ok := ParseDay(tc.s)
		if !got.Equal(tc.want) || ok != tc.ok || IsDay(tc.s) != tc.ok {
			t.Errorf("ParseDay(%q) = %v, %v (IsDay %v), want %v, %v", tc.s, got, ok, IsDay(tc.s), tc.want, tc.ok)
		}
	}
}
