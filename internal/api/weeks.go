package api

import (
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"lifelog/internal/core"
	"lifelog/internal/text"
)

// A week and a year look back over the calendar (docs/plans/097-the-almanac-reflects.md). The week is ISO 8601: it
// starts on Monday, and week 1 is the week that holds 4 January. The days of the week are the month's, with their
// items (plan 101); its metrics are the series of the metrics read in it.

var weekPattern = regexp.MustCompile(`^(\d{4})-W(\d{2})$`)

// weekOf parses a week, YYYY-Www, and gives its Monday. ok is false for anything else, and for a week number that
// the year does not have (week 53 of a year with 52 weeks).
func weekOf(s string) (monday time.Time, ok bool) {
	m := weekPattern.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	year, _ := strconv.Atoi(m[1])
	week, _ := strconv.Atoi(m[2])
	jan4 := time.Date(year, time.January, 4, 0, 0, 0, 0, time.UTC) // 4 January is always in week 1
	monday = jan4.AddDate(0, 0, -((int(jan4.Weekday())+6)%7)+7*(week-1))
	y, w := monday.ISOWeek()
	return monday, week >= 1 && y == year && w == week && text.IsDay(monday.Format(time.DateOnly))
}

// WeekKey is the ISO 8601 week of a Monday, YYYY-Www.
func WeekKey(monday time.Time) string {
	y, w := monday.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", y, w)
}

// WeekHref is the address of the week of a Monday.
func WeekHref(monday time.Time) string { return "/weeks/" + WeekKey(monday) }

func weekName(monday time.Time) string {
	y, w := monday.ISOWeek()
	return fmt.Sprintf("Week %d of %d", w, y)
}

// YearHref is the address of a year.
func YearHref(year int) string { return fmt.Sprintf("/years/%04d", year) }

// IsYear reports a year that has a page: 0001 to 9999. The year 0000 holds days, but it has no page (API.md).
func IsYear(year int) bool { return 1 <= year && year <= 9999 }

// lastDay is the last day that a date can name (lifelog_meta.days). The last week, 9999-W52, ends on it, a Friday.
var lastDay = time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC)

func (h *server) currentWeek(*http.Request) (*Entity, error) {
	t, _ := text.ParseDay(h.s.Today())
	return nil, redirect{WeekHref(t)}
}

// weekMetric is a metric read in a week: its current readings, their mean, and the mean of the week before.
type weekMetric struct {
	Metric       string   `json:"metric"`
	Unit         string   `json:"unit"`
	Readings     int      `json:"readings"`
	Mean         float64  `json:"mean"`
	PreviousMean *float64 `json:"previous_mean,omitempty"`
}

// round2 rounds x to two places. For a value near the largest float64, x*100 overflows: such a value is a whole
// number, so round2 returns it as it is.
func round2(x float64) float64 {
	if math.IsInf(x*100, 0) {
		return x
	}
	return math.Round(x*100) / 100
}

// mean is the mean of xs, rounded to two places; xs holds one value at least. The sum is divided by the count one
// time: a division of each value adds a rounding error for each value, and a mean on the boundary of two places (81
// over 24 is 3.375) then goes to the wrong side. A reading can be as large as a float64 holds
// (measurements_value_finite), so the sum can overflow: only then is each value divided by the count before the sum.
// Near the largest float64 the rounding of that sum can overflow still, and a mean is never less than the least
// value or more than the greatest: that mean is held between the two.
func mean(xs []float64) float64 {
	n := float64(len(xs))
	sum, lo, hi := 0.0, xs[0], xs[0]
	for _, x := range xs {
		sum += x
		lo, hi = min(lo, x), max(hi, x)
	}
	m := sum / n
	if math.IsInf(sum, 0) {
		m = 0
		for _, x := range xs {
			m += x / n
		}
		m = min(max(m, lo), hi)
	}
	return round2(m)
}

func (h *server) week(r *http.Request) (*Entity, error) {
	ctx := r.Context()
	today := h.s.Today()
	monday, ok := weekOf(r.PathValue("week"))
	if !ok {
		return nil, core.Invalid("a week is YYYY-Www")
	}
	sunday, before := monday.AddDate(0, 0, 6), monday.AddDate(0, 0, -7)
	if sunday.After(lastDay) {
		sunday = lastDay
	}
	from, through, previous := monday.Format(time.DateOnly), sunday.Format(time.DateOnly), before.Format(time.DateOnly)
	cal, err := h.calendarOf(ctx, from, through)
	if err != nil {
		return nil, err
	}
	days := cal.days
	e := &Entity{Class: []string{"week"}, Title: weekName(monday), Actions: []Action{action("open-day", nil, nil)}}
	for _, d := range days {
		e.Entities = append(e.Entities, link("day", DayHref(d.Day), d.Day))
	}

	// A metric is shown when it has a reading in the week.
	inUse, err := h.s.InUse(ctx, through, 14) // the week and the week before
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	for _, name := range inUse {
		used[name] = true
	}
	all, err := h.s.Metrics(ctx)
	if err != nil {
		return nil, err
	}
	metrics := []weekMetric{}
	for _, m := range all {
		if !used[m.Name] {
			continue
		}
		// A series is (from, to], so the day before the previous week starts it.
		rows, err := h.s.Series(ctx, m.Name, seriesFrom(before.AddDate(0, 0, -1)), through)
		if err != nil {
			return nil, err
		}
		var inWeek, weekBefore []float64
		for _, x := range rows {
			switch {
			case x.Day >= from:
				inWeek = append(inWeek, x.Value)
			case x.Day >= previous:
				weekBefore = append(weekBefore, x.Value)
			}
		}
		if len(inWeek) == 0 {
			continue
		}
		wm := weekMetric{Metric: m.Name, Unit: m.Unit, Readings: len(inWeek), Mean: mean(inWeek)}
		if len(weekBefore) > 0 {
			pm := mean(weekBefore)
			wm.PreviousMean = &pm
		}
		metrics = append(metrics, wm)
	}

	// A link leads only to a page that answers: a week has a page when its Monday is a day, a year when IsYear.
	next, thursday := monday.AddDate(0, 0, 7), monday.AddDate(0, 0, 3)
	e.Links = []Link{link("self", WeekHref(monday), e.Title)}
	if text.IsDay(before.Format(time.DateOnly)) {
		e.Links = append(e.Links, link("prev", WeekHref(before), weekName(before)))
	}
	if text.IsDay(next.Format(time.DateOnly)) {
		e.Links = append(e.Links, link("next", WeekHref(next), weekName(next)))
	}
	e.Links = append(e.Links, link("month", MonthHref(thursday), thursday.Format(layoutMonth)))
	if IsYear(thursday.Year()) {
		e.Links = append(e.Links, link("year", YearHref(thursday.Year()), strconv.Itoa(thursday.Year())))
	}
	e.Links = append(e.Links, link("index", "/", "Home"))
	e.Properties = map[string]any{"week": WeekKey(monday), "from": from, "through": through, "today": today,
		"days": days, "metrics": metrics}
	return e, nil
}

// yearMonth is a month of a year: its days, and how many of them have a day page.
type yearMonth struct {
	Month   string       `json:"month"`
	Written int          `json:"written"`
	Days    []writtenDay `json:"days"`
}

func (h *server) currentYear(*http.Request) (*Entity, error) {
	t, _ := text.ParseDay(h.s.Today())
	return nil, redirect{YearHref(t.Year())}
}

var yearPattern = regexp.MustCompile(`^\d{4}$`)

func (h *server) year(r *http.Request) (*Entity, error) {
	ctx := r.Context()
	s := r.PathValue("year")
	year, _ := strconv.Atoi(s)
	if !yearPattern.MatchString(s) || !IsYear(year) {
		return nil, core.Invalid("a year is YYYY")
	}
	first, last := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC), time.Date(year, time.December, 31, 0, 0, 0, 0, time.UTC)
	from, through := first.Format(time.DateOnly), last.Format(time.DateOnly)
	entries, err := h.s.DayEntries(ctx, from, through, false, 366)
	if err != nil {
		return nil, err
	}
	byDay, err := h.s.ReadingsByDay(ctx, from, through)
	if err != nil {
		return nil, err
	}
	readings := 0
	for _, n := range byDay {
		readings += n
	}
	e := &Entity{Class: []string{"year"}, Title: s, Actions: []Action{action("open-day", nil, nil)}}
	months := []yearMonth{}
	days := writtenDays(entries, first, last) // the days of the year, built once: each month takes its own
	for m := first; m.Year() == year; m = m.AddDate(0, 1, 0) {
		n := m.AddDate(0, 1, -1).Day() // how many days the month has
		ym := yearMonth{Month: m.Format("2006-01"), Days: days[:n:n]}
		days = days[n:]
		for _, d := range ym.Days {
			if d.Page {
				ym.Written++
			}
		}
		months = append(months, ym)
		e.Entities = append(e.Entities, link("month", MonthHref(m), m.Format(layoutMonth)))
	}
	e.Links = []Link{link("self", YearHref(year), s)}
	if IsYear(year - 1) {
		e.Links = append(e.Links, link("prev", YearHref(year-1), fmt.Sprintf("%04d", year-1)))
	}
	if IsYear(year + 1) {
		e.Links = append(e.Links, link("next", YearHref(year+1), fmt.Sprintf("%04d", year+1)))
	}
	e.Links = append(e.Links, link("index", "/", "Home"))
	e.Properties = map[string]any{"year": s, "today": h.s.Today(), "written": len(entries), "readings": readings, "months": months}
	return e, nil
}
