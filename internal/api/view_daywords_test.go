package api

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestACalendarItemInWords: each kind of item of a day of the calendar has its tag, the word of its cell and the note
// of Day by day. A birthday of the year of birth is "born"; a death of this year has no note. Any other kind, or no
// kind, is a page.
func TestACalendarItemInWords(t *testing.T) {
	for _, c := range []struct {
		item any
		want calWords
	}{
		{map[string]any{"kind": "birthday", "years": 0.0}, calWords{Tag: "born", Word: "born"}},
		{map[string]any{"kind": "birthday"}, calWords{Tag: "born", Word: "born"}},
		{map[string]any{"kind": "birthday", "years": 1.0}, calWords{Tag: "birthday", Word: "birthday", Note: "1 year"}},
		{map[string]any{"kind": "birthday", "years": 2.0}, calWords{Tag: "birthday", Word: "birthday", Note: "2 years"}},
		{map[string]any{"kind": "birthday", "years": 1234567.0}, calWords{Tag: "birthday", Word: "birthday", Note: "1234567 years"}},
		{map[string]any{"kind": "death", "years": 0.0}, calWords{Tag: "died", Word: "died"}},
		{map[string]any{"kind": "death", "years": 1.0}, calWords{Tag: "died", Word: "died", Note: "1 year ago"}},
		{map[string]any{"kind": "death", "years": 2.0}, calWords{Tag: "died", Word: "died", Note: "2 years ago"}},
		{map[string]any{"kind": "death", "years": 1234567.0}, calWords{Tag: "died", Word: "died", Note: "1234567 years ago"}},
		{map[string]any{"kind": "place"}, calWords{Tag: "place", Word: "place"}},
		{map[string]any{"kind": "file"}, calWords{Tag: "file"}},
		{map[string]any{"kind": "page"}, calWords{Tag: "page"}},
		{map[string]any{"kind": "something new"}, calWords{Tag: "page"}},
		{map[string]any{}, calWords{Tag: "page"}},
		{nil, calWords{Tag: "page"}},
	} {
		if got := calItem(c.item); got != c.want {
			t.Errorf("calItem(%v) = %+v, want %+v", c.item, got, c.want)
		}
	}
}

// TestAWeeksMeanAgainstTheWeekBefore: the words of a week's mean against the mean of the week before, with the number
// written in full. With no number for either week, it is the first week with readings.
func TestAWeeksMeanAgainstTheWeekBefore(t *testing.T) {
	for _, c := range []struct {
		mean, previous any
		want           string
	}{
		{71.4, 71.9, "down from 71.9"},
		{72.0, 71.9, "up from 71.9"},
		{0.0, -1.5, "up from -1.5"},
		{70.0, 70.0, "same as last week"},
		{2000000.0, 1000000.0, "up from 1000000"},
		{70.0, nil, "first week with readings"},
		{nil, 70.0, "first week with readings"},
		{"70", 69.0, "first week with readings"},
	} {
		if got := sinceLastWeek(c.mean, c.previous); got != c.want {
			t.Errorf("sinceLastWeek(%v, %v) = %q, want %q", c.mean, c.previous, got, c.want)
		}
	}
}

// TestWeeksStartOnMonday lays months out by hand-checked first weekdays: a month that starts on a Monday, one that
// starts on a Sunday, and a February of four whole weeks.
func TestWeeksStartOnMonday(t *testing.T) {
	month := func(first string, n int) []any {
		t0, _ := time.Parse(time.DateOnly, first)
		var out []any
		for i := range n {
			out = append(out, map[string]any{"day": t0.AddDate(0, 0, i).Format(time.DateOnly)})
		}
		return out
	}
	show := func(ws [][]any) string {
		var rows []string
		for _, w := range ws {
			var cells []string
			for _, d := range w {
				if d == nil {
					cells = append(cells, "--")
				} else {
					cells = append(cells, d.(map[string]any)["day"].(string)[8:])
				}
			}
			rows = append(rows, strings.Join(cells, " "))
		}
		return strings.Join(rows, " / ")
	}
	for _, c := range []struct {
		first string
		n     int
		want  string
	}{
		{"2026-06-01", 30, "01 02 03 04 05 06 07 / 08 09 10 11 12 13 14 / 15 16 17 18 19 20 21 / 22 23 24 25 26 27 28 / 29 30 -- -- -- -- --"},
		{"2026-11-01", 30, "-- -- -- -- -- -- 01 / 02 03 04 05 06 07 08 / 09 10 11 12 13 14 15 / 16 17 18 19 20 21 22 / 23 24 25 26 27 28 29 / 30 -- -- -- -- -- --"},
		{"2027-02-01", 28, "01 02 03 04 05 06 07 / 08 09 10 11 12 13 14 / 15 16 17 18 19 20 21 / 22 23 24 25 26 27 28"},
	} {
		if got := show(weeks(month(c.first, c.n))); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.first, got, c.want)
		}
	}
	if weeks([]any{}) != nil {
		t.Error("no day makes weeks")
	}
}

// TestTheWeekOfADayIsAPageOrNothing: the week link of a day leads to the page of its ISO week, and a day whose week
// has no page has none. 0000-01-01 is a Saturday: it and 0000-01-02 are in week 52 of the year -1, whose Monday is
// not a day. 0000-01-03 is the Monday of 0000-W01; 9999-12-31, a Friday, is in 9999-W52.
func TestTheWeekOfADayIsAPageOrNothing(t *testing.T) {
	for day, want := range map[string]string{
		"0000-01-01": "",
		"0000-01-02": "",
		"0000-01-03": "/weeks/0000-W01",
		"2026-10-16": "/weeks/2026-W42",
		"2027-01-03": "/weeks/2026-W53",
		"9999-12-31": "/weeks/9999-W52",
		"not a day":  "",
	} {
		if got := weekPage(day); got != want {
			t.Errorf("weekPage(%q) = %q, want %q", day, got, want)
		}
	}
}

// TestAgendaDaysAreTheDaysWithAnItem: Day by day lists the days of the month that have an item, in their order; a
// day with an empty list of items, or with none, is not listed.
func TestAgendaDaysAreTheDaysWithAnItem(t *testing.T) {
	item := map[string]any{"kind": "page", "title": "Notes"}
	d1 := map[string]any{"day": "2026-10-01", "items": []any{item}}
	d2 := map[string]any{"day": "2026-10-02", "items": []any{}}
	d3 := map[string]any{"day": "2026-10-03"}
	d4 := map[string]any{"day": "2026-10-04", "items": []any{item, item}}
	for _, c := range []struct {
		name  string
		props any
		want  []any
	}{
		{"no properties", nil, nil},
		{"no days", map[string]any{}, nil},
		{"no day with an item", map[string]any{"days": []any{d2, d3}}, nil},
		{"two days with items", map[string]any{"days": []any{d1, d2, d3, d4}}, []any{d1, d4}},
		{"a day that is not an object", map[string]any{"days": []any{"x", d4}}, []any{d4}},
	} {
		if got := (&view{Props: c.props}).AgendaDays(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
