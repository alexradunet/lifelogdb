package core

import (
	"fmt"
	"testing"
)

func TestOngoingHorizonDoesNotPrunePossibleStarts(t *testing.T) {
	for _, tc := range []struct {
		start  string
		count  int
		query  string
		passed int
	}{
		{"2018-09", 30, "2018-09-15", 15}, {"2018", 365, "2018-06-15", 166},
	} {
		end := ".."
		// Independently enumerate possible start-day positions. A query horizon
		// never removes a start that falls later than that horizon.
		hits := 0
		for start := 1; start <= tc.count; start++ {
			if start <= tc.passed {
				hits++
			}
		}
		want := "possible"
		if hits == tc.count {
			want = "definite"
		}
		for _, horizon := range []string{tc.query, "2018-12-31", "2020-12-31"} {
			got, err := periodMembership(&tc.start, &end, tc.query, horizon)
			if err != nil || got != want {
				t.Fatalf("%s/%s day%s horizon%s: got%s,%v want%s (%d/%d starts)", tc.start, end, tc.query, horizon, got, err, want, hits, tc.count)
			}
		}
		observed := tc.query
		if got, err := periodMembership(&tc.start, &observed, tc.query, ""); err != nil || got != "definite" {
			t.Fatalf("observed end contrast: %s %v", got, err)
		}
	}
	start, end := "2018-09", ".."
	for day := 1; day <= 30; day++ {
		query := fmt.Sprintf("2018-09-%02d", day)
		want := "possible"
		if day == 30 {
			want = "definite"
		}
		for _, horizon := range []string{query, "2020-12-31"} {
			got, err := periodMembership(&start, &end, query, horizon)
			if err != nil || got != want {
				t.Fatalf("day%d horizon%s: %s %v", day, horizon, got, err)
			}
		}
	}
}
