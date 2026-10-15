package api

// The calendar in words and numbers: days stepped, ISO weeks, weekdays, and the words of a week.

import (
	"strconv"
	"time"
)

// addDays is the day n days after day ("" for a day that is not one).
func addDays(day string, n int) string {
	t, ok := dayOf(day)
	if !ok {
		return ""
	}
	return t.AddDate(0, 0, n).Format(time.DateOnly)
}

// isoWeek is the ISO 8601 week of a day, 0 for a day that is not one.
func isoWeek(day string) int {
	t, ok := dayOf(day)
	if !ok {
		return 0
	}
	_, w := t.ISOWeek()
	return w
}

// isoWeekKey is the ISO 8601 week of a day as the address of the week writes it, "2026-W41"; "" for a day that is
// not one.
func isoWeekKey(day string) string {
	t, ok := dayOf(day)
	if !ok {
		return ""
	}
	return weekKey(t)
}

// sinceLastWeek says in words how a week's mean differs from the week before's: previous is nil when that week had
// no reading.
func sinceLastWeek(mean, previous any) string {
	m, ok1 := mean.(float64)
	p, ok2 := previous.(float64)
	switch {
	case !ok1 || !ok2:
		return "first week with readings"
	case m > p:
		return "up from " + num(p)
	case m < p:
		return "down from " + num(p)
	}
	return "same as last week"
}

// weekday is the name of a day's weekday, "" for a day that is not one.
func weekday(day string) string {
	t, ok := dayOf(day)
	if !ok {
		return ""
	}
	return t.Weekday().String()
}

// shortDay is a day without its year, "Mon 12 Oct": the sidebar's today, a day's own heading and the step to the
// day beside it, where the year is the page's. Everywhere else a day is written by Date, with its year when it is
// not this year's.
func shortDay(day string) string {
	t, ok := dayOf(day)
	if !ok {
		return day
	}
	return t.Format(layoutDate)
}

// dayOfMonth is the number of a day in its month, for the calendar's cells.
func dayOfMonth(day string) string {
	if t, ok := dayOf(day); ok {
		return strconv.Itoa(t.Day())
	}
	return day
}

// monthBand is the band of the calendar leaf: "Oct 2026".
func monthBand(day string) string {
	if t, ok := dayOf(day); ok {
		return t.Format("Jan 2006")
	}
	return ""
}

// calWords are the words of an item of the calendar (docs/plans/101-the-calendar-holds-every-dated-thing.md): Tag is
// its tag in Day by day, Class the colour of its square and its tag (the tags' open, overdue and done; "" is muted),
// Word the muted word a cell writes after the title, Note the muted note Day by day writes after the link.
type calWords struct{ Tag, Class, Word, Note string }

// CalItem says an item of a day of the calendar in the words of this view.
func (v *view) CalItem(item any, day, today string) calWords {
	m, _ := item.(map[string]any)
	str := func(k string) string { s, _ := m[k].(string); return s }
	years, _ := m["years"].(float64)
	switch str("kind") {
	case "birthday":
		if years == 0 {
			return calWords{Tag: "born", Word: "born"}
		}
		return calWords{Tag: "birthday", Word: "birthday", Note: yearsWords(years)}
	case "death":
		w := calWords{Tag: "died", Word: "died"}
		if years > 0 {
			w.Note = yearsWords(years) + " ago"
		}
		return w
	case "place":
		return calWords{Tag: "place", Word: "place"}
	case "file":
		return calWords{Tag: "file"}
	}
	return calWords{Tag: "page"}
}

// yearsWords is a count of whole years: "1 year", "36 years".
func yearsWords(n float64) string {
	if n == 1 {
		return "1 year"
	}
	return num(n) + " years"
}

// upTo is the first n items of a list: a cell shows three items, and a phone's cell four squares.
func upTo(list any, n int) []any {
	items := anyList(list)
	return items[:min(n, len(items))]
}

// beyond counts what a cell leaves out: the items after the first n, and the pages the day's items already left out.
func beyond(list any, n int, morePages any) int {
	items := anyList(list)
	extra, _ := morePages.(float64)
	return max(len(items)-n, 0) + int(extra)
}

// anyList is a list property as a template gets it; nil for anything else.
func anyList(list any) []any {
	items, _ := list.([]any)
	return items
}
