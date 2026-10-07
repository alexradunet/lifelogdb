package core

import (
	"fmt"
	"testing"
)

// docs/contract/period-boundaries.md defines membership over ordered endpoint
// pairs. Enumerate those pairs independently rather than reproducing its extrema formula.
func FuzzPeriodMembership(f *testing.F) {
	for _, seed := range []struct {
		year              uint16
		month, start, end uint8
		day, horizon      uint16
		shapes            uint8
	}{
		{0, 1, 28, 29, 59, 59, 0},
		{1900, 1, 0, 27, 59, 59, 1},
		{2000, 1, 28, 0, 60, 59, 3},
		{2018, 8, 19, 14, 257, 257, 4},
		{9999, 11, 30, 0, 364, 0, 8},
	} {
		f.Add(seed.year, seed.month, seed.start, seed.end, seed.day, seed.horizon, seed.shapes)
	}
	f.Fuzz(func(t *testing.T, year uint16, month, start, end uint8, day, horizon uint16, shapes uint8) {
		y := int(year) % 10000
		lengths := [...]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
		if y%4 == 0 && (y%100 != 0 || y%400 == 0) {
			lengths[1] = 29
		}
		m := int(month) % 12
		yearDays, monthStart := 0, 0
		for i, count := range lengths {
			yearDays += count
			if i < m {
				monthStart += count
			}
		}
		calendarDay := func(ordinal int) string {
			for i, count := range lengths {
				if ordinal <= count {
					return fmt.Sprintf("%04d-%02d-%02d", y, i+1, ordinal)
				}
				ordinal -= count
			}
			t.Fatalf("oracle day outside year: %d", ordinal)
			return ""
		}
		boundary := func(rawDay, shape uint8) (string, []int) {
			lo, hi := monthStart+int(rawDay)%lengths[m]+1, monthStart+int(rawDay)%lengths[m]+1
			text := calendarDay(lo)
			switch shape % 3 {
			case 1:
				text, lo, hi = fmt.Sprintf("%04d-%02d", y, m+1), monthStart+1, monthStart+lengths[m]
			case 2:
				text, lo, hi = fmt.Sprintf("%04d", y), 1, yearDays
			}
			candidates := make([]int, 0, hi-lo+1)
			for candidate := lo; candidate <= hi; candidate++ {
				candidates = append(candidates, candidate)
			}
			return text, candidates
		}
		s, starts := boundary(start, shapes%3)
		e, ends := boundary(end, shapes/3)
		query, asOf := int(day)%yearDays+1, int(horizon)%yearDays+1
		d, h := calendarDay(query), calendarDay(asOf)
		total, matches := 0, 0
		for _, first := range starts {
			for _, last := range ends {
				if first <= last {
					total++
					if first <= query && query <= last {
						matches++
					}
				}
			}
		}
		got, err := periodMembership(&s, &e, d, "")
		if total == 0 {
			if err == nil {
				t.Fatalf("accepted definitely reversed %s/%s", s, e)
			}
		} else {
			want := "possible"
			if matches == 0 {
				want = "outside"
			} else if matches == total {
				want = "definite"
			}
			if err != nil || got != want {
				t.Fatalf("%s/%s day %s: got %q, %v; want %s (%d of %d pairs)", s, e, d, got, err, want, matches, total)
			}
		}
		ongoing, want := "..", "possible"
		switch {
		case query < starts[0]:
			want = "outside"
		case query > asOf:
			want = "incomparable"
		case query >= starts[len(starts)-1]:
			want = "definite"
		}
		if got, err := periodMembership(&s, &ongoing, d, h); err != nil || got != want {
			t.Fatalf("ongoing %s day %s horizon %s: got %q, %v; want %s", s, d, h, got, err, want)
		}
		qualifiedStart, qualifiedEnd := s+"?", e+"~"
		if got, err := periodMembership(&qualifiedStart, &qualifiedEnd, d, ""); err != nil || got != "incomparable" {
			t.Fatalf("qualified boundaries gained a numerical range: %q, %v", got, err)
		}
		for _, invalid := range []string{s + "\x00", e + " ", qualifiedStart + "%"} {
			if _, err := periodBoundary(&invalid, false); err == nil {
				t.Fatalf("accepted noncanonical boundary %q", invalid)
			}
		}
	})
}
