package api

import "testing"

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
