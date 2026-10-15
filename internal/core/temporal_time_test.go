package core

import (
	"fmt"
	"testing"
)

func TestPeriodBoundaryProfile(t *testing.T) {
	for _, v := range []string{"0000", "0000-02", "0000-02-29", "9999", "9999-12", "9999-12-31", "2019?", "2019-02~", "2019-02-28%"} {
		t.Run(v, func(t *testing.T) {
			if _, err := periodBoundary(&v, false); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, v := range []string{"", "..", "2019-02-29?", "2019-13", "2019-2", "2019~~", "2019-21", "-001", "10000", "２０１９", "2019-02-30", "2019-01-01 ", "2019/2020"} {
		t.Run("refuse_"+v, func(t *testing.T) {
			if _, err := periodBoundary(&v, false); err == nil {
				t.Fatal("accepted invalid boundary")
			}
		})
	}
}

func TestPeriodOrderedPairMembership(t *testing.T) {
	// Explicitly enumerable September boundaries. The expected answers enumerate
	// every start/end pair, not the implementation's conditional extrema formula.
	bounds := []struct {
		value  string
		lo, hi int
	}{{"2018-09", 1, 30}, {"2018-09-01", 1, 1}, {"2018-09-20", 20, 20}, {"2018-09-30", 30, 30}}
	for _, s := range bounds {
		for _, e := range bounds {
			for day := 1; day <= 31; day++ {
				total, hits := 0, 0
				for start := s.lo; start <= s.hi; start++ {
					for end := e.lo; end <= e.hi; end++ {
						if start <= end {
							total++
							if start <= day && day <= end {
								hits++
							}
						}
					}
				}
				expected := "possible"
				if hits == 0 {
					expected = "outside"
				}
				if total > 0 && hits == total {
					expected = "definite"
				}
				query := fmt.Sprintf("2018-09-%02d", day)
				if day == 31 {
					query = "2018-10-01"
				}
				got, err := periodMembership(&s.value, &e.value, query, "")
				if total == 0 {
					if err == nil {
						t.Fatalf("%s/%s accepted reversed span", s.value, e.value)
					}
					continue
				}
				if err != nil || got != expected {
					t.Fatalf("%s/%s day%s: got%q,%v want%s (%d/%d)", s.value, e.value, query, got, err, expected, hits, total)
				}
			}
		}
	}
}

func TestPeriodIncompleteAndOngoingMembership(t *testing.T) {
	ptr := func(s string) *string { return &s }
	for _, tc := range []struct {
		start, end         *string
		day, horizon, want string
		refuse             bool
	}{
		{ptr("2018-09"), ptr("2019-06"), "2018-09-01", "", "possible", false},
		{ptr("2018-09"), ptr("2019-06"), "2018-09-30", "", "definite", false},
		{ptr("2018-09"), ptr("2019-06"), "2019-06-30", "", "possible", false},
		{ptr("2019~"), ptr("2020-01-01"), "2018-06-01", "", "incomparable", false},
		{ptr("2019?"), ptr("2020-01-01"), "2020-01-02", "", "outside", false},
		{nil, nil, "2019-01-01", "", "incomparable", false},
		{ptr("2018-09"), nil, "2018-08-31", "", "outside", false},
		{ptr("2018-09"), nil, "2019-01-01", "", "incomparable", false},
		{ptr("2018-09"), ptr(".."), "2018-09-15", "2020-12-31", "possible", false},
		{ptr("2018-09"), ptr(".."), "2018-10-01", "2020-12-31", "definite", false},
		{ptr("2018-09"), ptr(".."), "2021-01-01", "2020-12-31", "incomparable", false},
		{ptr("2021-01"), ptr(".."), "2020-12-31", "2020-12-31", "outside", false},
		{ptr("2021-01"), ptr(".."), "2021-01-02", "2020-12-31", "incomparable", false},
		{ptr("2021-01"), ptr(".."), "2020-12-31", "", "", true},
		{ptr("2018-10"), ptr("2018-09"), "2018-09-20", "", "", true},
	} {
		got, err := periodMembership(tc.start, tc.end, tc.day, tc.horizon)
		if (err != nil) != tc.refuse || !tc.refuse && got != tc.want {
			t.Fatalf("day%s horizon%s: got%q,%v want%q refused%v", tc.day, tc.horizon, got, err, tc.want, tc.refuse)
		}
	}
}
