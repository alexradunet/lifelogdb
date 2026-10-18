package api

import (
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"lifelog/internal/api/siren"
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

func weekName(monday time.Time) string {
	y, w := monday.ISOWeek()
	return fmt.Sprintf("Week %d of %d", w, y)
}

// lastDay is the last day that a date can name (lifelog_meta.days). The last week, 9999-W52, ends on it, a Friday.
var lastDay = time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC)

func (h *server) currentWeek(*http.Request) (*siren.Entity, error) {
	t, _ := text.ParseDay(h.s.Today())
	return nil, redirect{siren.WeekHref(t)}
}

// weekMetric is a metric read in a week: its current readings, their mean, and the mean of the week before.
type weekMetric struct {
	Metric       string   `json:"metric"`
	Unit         string   `json:"unit"`
	Readings     int      `json:"readings"`
	Mean         float64  `json:"mean"`
	PreviousMean *float64 `json:"previous_mean,omitempty"`
}

// maxDecimals caps the decimals of a mean. A reading can hold more (a computed value as 5432.123456), but a mean
// with more than six places shows noise, not a measurement.
const maxDecimals = 6

// decimals is how many decimals a reading has in its shortest decimal text ("60.25" has 2, "58" has 0). It reads
// strconv.FormatFloat, not the value minus its floor: the float64 of 60.29 is 60.2899999999999991..., and only its
// shortest text is the number that the owner wrote. A reading with more than maxDecimals has maxDecimals.
func decimals(x float64) int {
	s := strconv.FormatFloat(x, 'f', -1, 64)
	i := strings.IndexByte(s, '.')
	if i < 0 {
		return 0
	}
	return min(len(s)-i-1, maxDecimals)
}

// meanDecimals is the decimals that a mean shows: the most that a reading of the two weeks has, and one at least.
func meanDecimals(weeks ...[]float64) int {
	n := 1
	for _, xs := range weeks {
		for _, x := range xs {
			n = max(n, decimals(x))
		}
	}
	return n
}

// roundTo rounds x to places decimals. For a value near the largest float64, x times the factor overflows: such a
// value is a whole number, so roundTo returns it as it is.
func roundTo(x float64, places int) float64 {
	f := math.Pow10(places)
	if math.IsInf(x*f, 0) {
		return x
	}
	return math.Round(x*f) / f
}

// mean is the mean of xs, rounded to places decimals; xs holds one value at least. The sum is divided by the count
// one time: a division of each value adds a rounding error for each value, and a mean on the boundary of two places
// (78 over 24 is 3.25) then goes to the wrong side. A reading can be as large as a float64 holds
// (measurements_value_finite), so the sum can overflow: only then is each value divided by the count before the sum.
// Near the largest float64 the rounding of that sum can overflow still, and a mean is never less than the least
// value or more than the greatest: that mean is held between the two.
func mean(xs []float64, places int) float64 {
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
	return roundTo(m, places)
}

func (h *server) week(r *http.Request) (*siren.Entity, error) {
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
	e := &siren.Entity{Class: []string{"week"}, Title: weekName(monday), Actions: []siren.Action{action("open-day", nil, nil)}}
	for _, d := range days {
		e.Entities = append(e.Entities, link("day", siren.DayHref(d.Day), d.Day))
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
		places := meanDecimals(inWeek, weekBefore)
		wm := weekMetric{Metric: m.Name, Unit: m.Unit, Readings: len(inWeek), Mean: mean(inWeek, places)}
		if len(weekBefore) > 0 {
			pm := mean(weekBefore, places)
			wm.PreviousMean = &pm
		}
		metrics = append(metrics, wm)
	}

	// A week has a page when its Monday is a day, a year when IsYear.
	next, thursday := monday.AddDate(0, 0, 7), monday.AddDate(0, 0, 3)
	e.Links = []siren.Link{link("self", siren.WeekHref(monday), e.Title)}
	e.Links = append(e.Links, neighbourLinks(
		neighbour{before, siren.WeekHref(before), weekName(before)},
		neighbour{next, siren.WeekHref(next), weekName(next)})...)
	// The zoom of a week is anchored on today when today is in it, else on its Thursday: ISO 8601 gives a week the
	// year of its Thursday, and a week that starts in one month has its Thursday in the month of most of its days.
	anchor := thursday
	if t, ok := text.ParseDay(today); ok && !t.Before(monday) && !t.After(sunday) {
		anchor = t
	}
	e.Links = append(e.Links, zoomLinks(anchor, streamAfter(sunday))...)
	e.Links = append(e.Links, link("index", "/", "Home"))
	e.Properties = map[string]any{"week": siren.WeekKey(monday), "from": from, "through": through, "today": today,
		"days": days, "metrics": metrics}
	return e, nil
}

// yearMonth is a month of a year: its days, and how many of them have a day page.
type yearMonth struct {
	Month   string       `json:"month"`
	Written int          `json:"written"`
	Days    []writtenDay `json:"days"`
}

func (h *server) currentYear(*http.Request) (*siren.Entity, error) {
	t, _ := text.ParseDay(h.s.Today())
	return nil, redirect{siren.YearHref(t.Year())}
}

var yearPattern = regexp.MustCompile(`^\d{4}$`)

func (h *server) year(r *http.Request) (*siren.Entity, error) {
	ctx := r.Context()
	s := r.PathValue("year")
	year, _ := strconv.Atoi(s)
	if !yearPattern.MatchString(s) || !siren.IsYear(year) {
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
	e := &siren.Entity{Class: []string{"year"}, Title: s, Actions: []siren.Action{action("open-day", nil, nil)}}
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
		e.Entities = append(e.Entities, link("month", siren.MonthHref(m), m.Format(layoutMonth)))
	}
	e.Links = []siren.Link{link("self", siren.YearHref(year), s)}
	if siren.IsYear(year - 1) {
		e.Links = append(e.Links, link("prev", siren.YearHref(year-1), fmt.Sprintf("%04d", year-1)))
	}
	if siren.IsYear(year + 1) {
		e.Links = append(e.Links, link("next", siren.YearHref(year+1), fmt.Sprintf("%04d", year+1)))
	}
	e.Links = append(e.Links, zoomLinks(anchorIn(h.s.Today(), first, last), streamAfter(last))...)
	e.Links = append(e.Links, link("index", "/", "Home"))
	e.Properties = map[string]any{"year": s, "today": h.s.Today(), "written": len(entries), "readings": readings, "months": months}
	return e, nil
}
