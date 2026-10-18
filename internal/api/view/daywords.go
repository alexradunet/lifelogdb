package view

import (
	"strconv"
	"time"

	"lifelog/internal/api/siren"
	"lifelog/internal/text"
)

// The calendar in words and numbers: days stepped, ISO weeks, weekdays, and the words of a week.

// addDays is the day n days after day ("" for a day that is not one).
func addDays(day string, n int) string {
	t, ok := text.ParseDay(day)
	if !ok {
		return ""
	}
	return t.AddDate(0, 0, n).Format(time.DateOnly)
}

// isoWeek is the ISO 8601 week of a day, 0 for a day that is not one.
func isoWeek(day string) int {
	t, ok := text.ParseDay(day)
	if !ok {
		return 0
	}
	_, w := t.ISOWeek()
	return w
}

// weekPage is the address of the page of a day's ISO 8601 week, "/weeks/2026-W41"; "" for a day that is not one,
// and for a week that has no page because its Monday is not a day (0000-01-01 and 0000-01-02 are in a week of the
// year -1).
func weekPage(day string) string {
	t, ok := text.ParseDay(day)
	if !ok {
		return ""
	}
	monday := t.AddDate(0, 0, -((int(t.Weekday()) + 6) % 7))
	if !text.IsDay(monday.Format(time.DateOnly)) {
		return ""
	}
	return siren.WeekHref(monday)
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
		return "up from " + Num(p)
	case m < p:
		return "down from " + Num(p)
	}
	return "same as last week"
}

// weekday is the name of a day's weekday, "" for a day that is not one.
func weekday(day string) string {
	t, ok := text.ParseDay(day)
	if !ok {
		return ""
	}
	return t.Weekday().String()
}

// shortDay is a day without its year, "Mon 12 Oct": the sidebar's today, a day's own heading and the step to the
// day beside it, where the year is the page's. Everywhere else a day is written by Date, with its year when it is
// not this year's.
func shortDay(day string) string {
	t, ok := text.ParseDay(day)
	if !ok {
		return day
	}
	return t.Format(layoutDate)
}

// dayOfMonth is the number of a day in its month, for the calendar's cells.
func dayOfMonth(day string) string {
	if t, ok := text.ParseDay(day); ok {
		return strconv.Itoa(t.Day())
	}
	return day
}

// monthBand is the band of the calendar leaf: "Oct 2026".
func monthBand(day string) string {
	if t, ok := text.ParseDay(day); ok {
		return t.Format("Jan 2006")
	}
	return ""
}

// calWords are the words of an item of the calendar (docs/plans/101-the-calendar-holds-every-dated-thing.md): Tag is
// its tag in Day by day, Word the muted word a cell writes after the title, Note the muted note Day by day writes
// after the link.
type calWords struct{ Tag, Word, Note string }

// calItem says an item of a day of the calendar in words.
func calItem(item any) calWords {
	m, _ := item.(map[string]any)
	str := func(k string) string { s, _ := m[k].(string); return s }
	years, _ := m["years"].(float64)
	switch str("kind") {
	case "birthday":
		if years == 0 {
			return calWords{Tag: "born", Word: "born"}
		}
		return calWords{Tag: "birthday", Word: "birthday", Note: Count(years, "year", "years")}
	case "death":
		w := calWords{Tag: "died", Word: "died"}
		if years > 0 {
			w.Note = Count(years, "year", "years") + " ago"
		}
		return w
	case "place":
		return calWords{Tag: "place", Word: "place"}
	case "file":
		return calWords{Tag: "file"}
	}
	return calWords{Tag: "page"}
}

// AgendaDays are the days of the month that have an item, in order: Day by day lists them under the calendar.
func (v *view) AgendaDays() []any {
	props, _ := v.Props.(map[string]any)
	var out []any
	for _, d := range anyList(props["days"]) {
		m, _ := d.(map[string]any)
		if len(anyList(m["items"])) > 0 {
			out = append(out, d)
		}
	}
	return out
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

// weeks puts the days of a month into the rows of a calendar, Monday first: nil fills a cell before the first day and
// after the last.
func weeks(list any) [][]any {
	days, _ := list.([]any)
	if len(days) == 0 {
		return nil
	}
	m, _ := days[0].(map[string]any)
	first, _ := m["day"].(string)
	t, ok := text.ParseDay(first)
	if !ok {
		return nil
	}
	var out [][]any
	row := make([]any, (int(t.Weekday())+6)%7)
	for _, d := range days {
		if row = append(row, d); len(row) == 7 {
			out, row = append(out, row), nil
		}
	}
	if len(row) > 0 {
		out = append(out, append(row, make([]any, 7-len(row))...))
	}
	return out
}

// yearsAgo is how long before today a day was, in whole years, as On this day says it: "1 year ago", "3 years ago".
// A day after today, or less than a year before it, is "this year". A year is whole on the month and day of its day:
// 29 February is a year older on 1 March of a common year.
func yearsAgo(day, today string) string {
	d, ok1 := text.ParseDay(day)
	t, ok2 := text.ParseDay(today)
	if !ok1 || !ok2 {
		return ""
	}
	n := t.Year() - d.Year()
	if t.Month() < d.Month() || t.Month() == d.Month() && t.Day() < d.Day() {
		n--
	}
	if n < 1 {
		return "this year"
	}
	return Count(n, "year ago", "years ago")
}
