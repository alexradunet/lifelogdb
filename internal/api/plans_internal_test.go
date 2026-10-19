package api

import "testing"

// TestPeriodWindowIsTheFirstAndTheLastDayOfAPeriod: the windows are written by hand. Week 1 of ISO 8601 is the week of
// 4 January, and a week starts on a Monday.
func TestPeriodWindowIsTheFirstAndTheLastDayOfAPeriod(t *testing.T) {
	for _, tc := range []struct{ period, key, from, through string }{
		{"year", "2026", "2026-01-01", "2026-12-31"},
		{"month", "2026-10", "2026-10-01", "2026-10-31"},
		{"month", "2028-02", "2028-02-01", "2028-02-29"},
		{"quarter", "2026-Q4", "2026-10-01", "2026-12-31"},
		{"quarter", "2026-Q1", "2026-01-01", "2026-03-31"},
		{"week", "2026-W43", "2026-10-19", "2026-10-25"},
		{"week", "2026-W01", "2025-12-29", "2026-01-04"},
		{"week", "2026-W53", "2026-12-28", "2027-01-03"},
		{"week", "2024-W01", "2024-01-01", "2024-01-07"},
	} {
		from, through, ok := periodWindow(tc.period, tc.key)
		if !ok || from != tc.from || through != tc.through {
			t.Errorf("%s %s: %s to %s (%v), want %s to %s", tc.period, tc.key, from, through, ok, tc.from, tc.through)
		}
	}
	for _, tc := range []struct{ period, key string }{{"", "2026"}, {"week", "x"}, {"month", "2026"}, {"day", "2026-10-19"}} {
		if from, through, ok := periodWindow(tc.period, tc.key); ok {
			t.Errorf("%s %s: %s to %s, want no window", tc.period, tc.key, from, through)
		}
	}
}
