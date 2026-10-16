package api

import (
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"lifelog/internal/core"
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
	return monday, week >= 1 && y == year && w == week && core.IsDay(monday.Format(time.DateOnly))
}

func weekKey(monday time.Time) string {
	y, w := monday.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", y, w)
}

func weekHref(monday time.Time) string { return "/weeks/" + weekKey(monday) }

func weekName(monday time.Time) string {
	y, w := monday.ISOWeek()
	return fmt.Sprintf("Week %d of %d", w, y)
}

func yearHref(year int) string { return fmt.Sprintf("/years/%04d", year) }

// isYear reports a year that has a page: 0001 to 9999. The year 0000 holds days, but it has no page (API.md).
func isYear(year int) bool { return 1 <= year && year <= 9999 }

// yearPage is the address of the page of a month's year, YYYY-MM; "" for the year 0000, which has no page.
func yearPage(month string) string {
	year, err := strconv.Atoi(month[:min(4, len(month))])
	if err != nil || !isYear(year) {
		return ""
	}
	return yearHref(year)
}

// lastDay is the last day that a date can name (lifelog_meta.days). The last week, 9999-W52, ends on it, a Friday.
var lastDay = time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC)

func (h *server) currentWeek(*http.Request) (*Entity, error) {
	t, _ := core.ParseDay(h.s.Today())
	return nil, redirect{weekHref(t)}
}

// weekMetric is a metric read in a week: its current readings, their mean, and the mean of the week before.
type weekMetric struct {
	Metric       string   `json:"metric"`
	Unit         string   `json:"unit"`
	Readings     int      `json:"readings"`
	Mean         float64  `json:"mean"`
	PreviousMean *float64 `json:"previous_mean,omitempty"`
}

func round2(x float64) float64 { return math.Round(x*100) / 100 }

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
		e.Entities = append(e.Entities, link("day", dayHref(d.Day), d.Day))
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
		var sum, sumBefore float64
		var n, nBefore int
		for _, x := range rows {
			switch {
			case x.Day >= from:
				sum, n = sum+x.Value, n+1
			case x.Day >= previous:
				sumBefore, nBefore = sumBefore+x.Value, nBefore+1
			}
		}
		if n == 0 {
			continue
		}
		wm := weekMetric{Metric: m.Name, Unit: m.Unit, Readings: n, Mean: round2(sum / float64(n))}
		if nBefore > 0 {
			mean := round2(sumBefore / float64(nBefore))
			wm.PreviousMean = &mean
		}
		metrics = append(metrics, wm)
	}

	// A link leads only to a page that answers: a week has a page when its Monday is a day, a year when isYear.
	next, thursday := monday.AddDate(0, 0, 7), monday.AddDate(0, 0, 3)
	e.Links = []Link{link("self", weekHref(monday), e.Title)}
	if core.IsDay(before.Format(time.DateOnly)) {
		e.Links = append(e.Links, link("prev", weekHref(before), weekName(before)))
	}
	if core.IsDay(next.Format(time.DateOnly)) {
		e.Links = append(e.Links, link("next", weekHref(next), weekName(next)))
	}
	e.Links = append(e.Links, link("month", monthHref(thursday), thursday.Format(layoutMonth)))
	if isYear(thursday.Year()) {
		e.Links = append(e.Links, link("year", yearHref(thursday.Year()), strconv.Itoa(thursday.Year())))
	}
	e.Links = append(e.Links, link("index", "/", "Home"))
	e.Properties = map[string]any{"week": weekKey(monday), "from": from, "through": through, "today": today,
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
	t, _ := core.ParseDay(h.s.Today())
	return nil, redirect{yearHref(t.Year())}
}

var yearPattern = regexp.MustCompile(`^\d{4}$`)

func (h *server) year(r *http.Request) (*Entity, error) {
	ctx := r.Context()
	s := r.PathValue("year")
	year, _ := strconv.Atoi(s)
	if !yearPattern.MatchString(s) || !isYear(year) {
		return nil, core.Invalid("a year is YYYY")
	}
	first := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
	from, through := first.Format(time.DateOnly), time.Date(year, time.December, 31, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
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
	for m := first; m.Year() == year; m = m.AddDate(0, 1, 0) {
		ym := yearMonth{Month: m.Format("2006-01"), Days: writtenDays(entries, m, m.AddDate(0, 1, -1))}
		for _, d := range ym.Days {
			if d.Page {
				ym.Written++
			}
		}
		months = append(months, ym)
		e.Entities = append(e.Entities, link("month", monthHref(m), m.Format(layoutMonth)))
	}
	e.Links = []Link{link("self", yearHref(year), s)}
	if isYear(year - 1) {
		e.Links = append(e.Links, link("prev", yearHref(year-1), fmt.Sprintf("%04d", year-1)))
	}
	if isYear(year + 1) {
		e.Links = append(e.Links, link("next", yearHref(year+1), fmt.Sprintf("%04d", year+1)))
	}
	e.Links = append(e.Links, link("index", "/", "Home"))
	e.Properties = map[string]any{"year": s, "today": h.s.Today(), "written": len(entries), "readings": readings, "months": months}
	return e, nil
}
