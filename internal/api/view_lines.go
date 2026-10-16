package api

import (
	"fmt"
	"html/template"
	"strings"
	"time"

	"lifelog/internal/core"
)

// The lines of the readings (docs/plans/093-words-phone-and-today.md): a reading's tile on a day, with its unit and
// the line of its last 14 days, and the line of a metric's recent readings on its card. A line draws only the
// readings that have a day and a number: a retraction has no number.
//
// One drawer, plot, places the readings of each line: across by the line's own placement of a day, and up between
// the least and the greatest value. trendLine and sparkLine are a placement and one call of smallLine; chart is a
// placement, one call of plot, and its axes.

// lineReading is a reading that a line draws: its day, as a time and as written, and its value.
type lineReading struct {
	t     time.Time
	day   string
	value float64
}

// lineReadings are the readings of list that a line draws, in their order: those with a day and a number.
func lineReadings(list []any) []lineReading {
	var rs []lineReading
	for _, it := range list {
		m, _ := it.(map[string]any)
		day, _ := m["day"].(string)
		value, isNumber := m["value"].(float64)
		if t, ok := core.ParseDay(day); ok && isNumber { // no day, or no value (a retraction): nothing to draw
			rs = append(rs, lineReading{t, day, value})
		}
	}
	return rs
}

// plot places rs, which is not empty: x places a reading across, and y places a value up, given the least and the
// greatest value of rs. It gives the points of the SVG polyline, at (where a value stands up in this line), and the
// least and the greatest value.
func plot(rs []lineReading, x func(lineReading) float64, y func(v, lo, hi float64) float64) (points string, at func(float64) float64, lo, hi float64) {
	lo, hi = rs[0].value, rs[0].value
	for _, r := range rs[1:] {
		if r.value < lo {
			lo = r.value
		}
		if r.value > hi {
			hi = r.value
		}
	}
	at = func(v float64) float64 { return y(v, lo, hi) }
	var sb strings.Builder
	for _, r := range rs {
		fmt.Fprintf(&sb, "%.1f,%.1f ", x(r), at(r.value))
	}
	return sb.String(), at, lo, hi
}

// readingWords say a reading of a line for a screen reader: its value, its unit and its day against today on the
// clock c, "70 kg 2 days ago · Wed 14 Oct".
func readingWords(c clock, r lineReading, unit string) string {
	return num(r.value) + " " + unitWords(unit) + " " + c.relative(r.day)
}

// smallLine draws rs as an inline SVG, 120 by 28, each reading placed across by x, with the values between their
// least and their greatest and the last reading marked; "" with no reading. label is what it says to a screen
// reader.
func smallLine(rs []lineReading, label string, x func(lineReading) float64) template.HTML {
	if len(rs) == 0 {
		return ""
	}
	points, y, _, _ := plot(rs, x, func(v, lo, hi float64) float64 {
		if hi == lo {
			return 14
		}
		return 24 - (v-lo)/(hi-lo)*20
	})
	last := rs[len(rs)-1]
	return template.HTML(fmt.Sprintf(`<svg class="trend" viewBox="0 0 120 28" role="img" aria-label="%s"><polyline points="%s"/><circle cx="%.1f" cy="%.1f" r="2.5"/></svg>`,
		template.HTMLEscapeString(strings.Join(strings.Fields(label), " ")), points, x(last), y(last.value)))
}

// trendLine draws a trend as a small line: one point per reading, placed by its day among the 14 days through the
// day. Its label says the first and the last value in words, with their days against today on the clock c, also on
// a past day.
func trendLine(c clock, metric, unit, day string, points []any) template.HTML {
	end, ok := core.ParseDay(day)
	rs := lineReadings(points)
	if !ok || len(rs) == 0 {
		return ""
	}
	label := metric + " from " + readingWords(c, rs[0], unit) + " to " + readingWords(c, rs[len(rs)-1], unit)
	if len(rs) == 1 {
		label = metric + ": one reading in 14 days, " + readingWords(c, rs[0], unit)
	}
	return smallLine(rs, label, func(r lineReading) float64 {
		back := end.Sub(r.t).Hours() / 24 // 0 on the day, 13 on the first of the 14
		return 4 + (float64(glanceDays-1)-back)*112/float64(glanceDays-1)
	})
}

// sparkLine draws a metric's recent readings as a small line: each point placed by its day between the first
// reading and the last, so a marker read twice a year is drawn as it was read. Its label says how many readings,
// and the first and the last value in words, against today.
func sparkLine(metric, unit, today string, points []any) template.HTML {
	rs := lineReadings(points)
	if len(rs) == 0 {
		return ""
	}
	c, first := clock{today: today}, rs[0]
	label := fmt.Sprintf("%s: %d readings, from %s to %s", metric, len(rs), readingWords(c, first, unit), readingWords(c, rs[len(rs)-1], unit))
	if len(rs) == 1 {
		label = metric + ": one reading, " + readingWords(c, first, unit)
	}
	span := rs[len(rs)-1].t.Sub(first.t).Hours()
	return smallLine(rs, label, func(r lineReading) float64 {
		if span > 0 {
			return 4 + r.t.Sub(first.t).Hours()/span*112
		}
		return 116 // one day only: the points stand at the end, where the latest is
	})
}

// chartPoints is how many readings the chart draws a point for: more points than this overlap on its 640 units and
// only add weight, so the line alone draws a longer series (docs/plans/100-one-page-a-metric.md).
const chartPoints = 200

// chart draws a series as an inline SVG: a line through its readings, each placed by its day between from and to,
// and a point a reading while there are at most chartPoints; "" with no reading to draw. The least and the greatest
// value of the axis are those of the readings that it draws.
func chart(from, to string, readings any) template.HTML {
	rs := lineReadings(anyList(readings))
	// An absent lower bound is explicit all-history mode. Plot from the earliest
	// returned observation, without giving an ordinary calendar date sentinel meaning.
	if from == "" {
		for _, r := range rs {
			if from == "" || r.day < from {
				from = r.day
			}
		}
	}
	f, ok1 := core.ParseDay(from)
	t, ok2 := core.ParseDay(to)
	if len(rs) == 0 || !ok1 || !ok2 || t.Before(f) {
		return ""
	}
	const w, h, pad, left, bottom = 640.0, 200.0, 14.0, 48.0, 22.0
	x := func(r lineReading) float64 {
		// Unix seconds cover the full supported date range without duration saturation.
		across := 0.5
		if t.After(f) {
			across = float64(r.t.Unix()-f.Unix()) / float64(t.Unix()-f.Unix())
		}
		return left + across*(w-left-pad)
	}
	points, y, lo, hi := plot(rs, x, func(v, lo, hi float64) float64 {
		span := hi - lo
		if span == 0 {
			span = 1
		}
		return pad + (1-(v-lo)/span)*(h-2*pad-bottom)
	})
	var sb strings.Builder
	first, last := f.Format(layoutDateYear), t.Format(layoutDateYear) // the ends in words, with the year: a range can span years
	fmt.Fprintf(&sb, `<svg class="chart" viewBox="0 0 %g %g" role="img" aria-label="%d readings from %s to %s">`,
		w, h, len(rs), first, last)
	fmt.Fprintf(&sb, `<line class="axis" x1="%g" y1="%g" x2="%g" y2="%g"/><line class="axis" x1="%g" y1="%g" x2="%g" y2="%g"/>`,
		left, y(hi), w-pad, y(hi), left, y(lo), w-pad, y(lo))
	fmt.Fprintf(&sb, `<text x="%g" y="%g">%g</text><text x="%g" y="%g">%g</text>`, left-6, y(hi)+4, hi, left-6, y(lo)+4, lo)
	fmt.Fprintf(&sb, `<text class="x" x="%g" y="%g">%s</text><text class="x" x="%g" y="%g">%s</text>`,
		left+40, h-4, first, w-pad-40, h-4, last)
	sb.WriteString(`<polyline points="` + points + `"/>`)
	if len(rs) <= chartPoints {
		for _, r := range rs {
			fmt.Fprintf(&sb, `<circle cx="%.1f" cy="%.1f" r="3"><title>%s: %g</title></circle>`, x(r), y(r.value),
				r.t.Format(layoutDateYear), r.value)
		}
	}
	sb.WriteString(`</svg>`)
	return template.HTML(sb.String())
}
