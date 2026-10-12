package api

// The calendar in words and numbers: days stepped, ISO weeks, weekdays, and the words of a week.

import (
	"slices"
	"strconv"
	"time"

	"lifelog/internal/core"
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

// settledFirst orders the occurrences of a week for reading back: done ones, then skipped ones, then open ones. The
// order inside each state stays.
func settledFirst(list any) []any {
	items, _ := list.([]any)
	rank := map[string]int{core.StateDone: 0, core.StateSkipped: 1}
	out := slices.Clone(items)
	slices.SortStableFunc(out, func(a, b any) int {
		state := func(x any) int {
			m, _ := x.(map[string]any)
			s, _ := m["state"].(string)
			if r, ok := rank[s]; ok {
				return r
			}
			return 2
		}
		return state(a) - state(b)
	})
	return out
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

// daysOf writes a habit's count over a week: "5 of 7 days".
func daysOf(done, active any) string {
	if a, _ := active.(float64); a == 1 {
		return num(done) + " of 1 day"
	}
	return num(done) + " of " + num(active) + " days"
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
