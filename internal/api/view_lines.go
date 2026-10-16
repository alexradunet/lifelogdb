package api

import (
	"fmt"
	"html/template"
	"strconv"
	"strings"
	"time"

	"lifelog/internal/core"
)

// The lines of the readings (docs/plans/093-words-phone-and-today.md): a reading's tile on a day, with its unit and
// the line of its last 14 days, and the line of a metric's recent readings on its card. A line draws only the
// readings that have a day and a number: a retraction has no number.

// trendLine draws a trend as an inline SVG, 120 by 28: one point per reading, placed by its day among the 14 days
// through the day, the last one marked; "" with no reading. Its label says the first and the last value in words,
// with their days against today on the clock c, also on a past day.
func trendLine(c clock, metric, unit, day string, points []any) template.HTML {
	end, ok := core.ParseDay(day)
	if !ok {
		return ""
	}
	var pts []linePoint
	for _, p := range points {
		m, _ := p.(map[string]any)
		d, _ := m["day"].(string)
		value, isNumber := m["value"].(float64)
		t, ok := core.ParseDay(d)
		if !ok || !isNumber { // no day, or no value (a retraction): nothing to draw
			continue
		}
		back := end.Sub(t).Hours() / 24 // 0 on the day, 13 on the first of the 14
		pts = append(pts, linePoint{x: 4 + (float64(glanceDays-1)-back)*112/float64(glanceDays-1), value: value, day: d})
	}
	if len(pts) == 0 {
		return ""
	}
	first, last := pts[0], pts[len(pts)-1]
	label := fmt.Sprintf("%s from %s %s %s to %s %s %s", metric, strconv.FormatFloat(first.value, 'f', -1, 64), unitWords(unit),
		c.relative(first.day), strconv.FormatFloat(last.value, 'f', -1, 64), unitWords(unit), c.relative(last.day))
	if len(pts) == 1 {
		label = fmt.Sprintf("%s: one reading in 14 days, %s %s %s", metric, strconv.FormatFloat(last.value, 'f', -1, 64), unitWords(unit), c.relative(last.day))
	}
	return line(label, pts)
}

// sparkLine draws a metric's recent readings as an inline SVG, 120 by 28: each point placed by its day between the
// first reading and the last, so a marker read twice a year is drawn as it was read; "" with no reading. Its label
// says how many readings, and the first and the last value in words, against today.
func sparkLine(metric, unit, today string, points []any) template.HTML {
	type read struct {
		t     time.Time
		value float64
		day   string
	}
	var rs []read
	for _, p := range points {
		m, _ := p.(map[string]any)
		d, _ := m["day"].(string)
		value, isNumber := m["value"].(float64)
		if t, ok := core.ParseDay(d); ok && isNumber {
			rs = append(rs, read{t, value, d})
		}
	}
	if len(rs) == 0 {
		return ""
	}
	span := rs[len(rs)-1].t.Sub(rs[0].t).Hours()
	pts := make([]linePoint, len(rs))
	for i, r := range rs {
		x := 116.0 // one day only: the points stand at the end, where the latest is
		if span > 0 {
			x = 4 + r.t.Sub(rs[0].t).Hours()/span*112
		}
		pts[i] = linePoint{x: x, value: r.value, day: r.day}
	}
	c := clock{today: today}
	first, last := pts[0], pts[len(pts)-1]
	label := fmt.Sprintf("%s: %d readings, from %s %s %s to %s %s %s", metric, len(pts), strconv.FormatFloat(first.value, 'f', -1, 64), unitWords(unit),
		c.relative(first.day), strconv.FormatFloat(last.value, 'f', -1, 64), unitWords(unit), c.relative(last.day))
	if len(pts) == 1 {
		label = fmt.Sprintf("%s: one reading, %s %s %s", metric, strconv.FormatFloat(last.value, 'f', -1, 64), unitWords(unit), c.relative(last.day))
	}
	return line(label, pts)
}

// linePoint is one reading of a drawn line: where it stands across, its value and its day.
type linePoint struct {
	x, value float64
	day      string
}

// line draws points as an inline SVG, 120 by 28, with the values between their least and their greatest, the last
// point marked; label is what it says to a screen reader.
func line(label string, pts []linePoint) template.HTML {
	lo, hi := pts[0].value, pts[0].value
	for _, p := range pts {
		lo, hi = min(lo, p.value), max(hi, p.value)
	}
	y := func(v float64) float64 {
		if hi == lo {
			return 14
		}
		return 24 - (v-lo)/(hi-lo)*20
	}
	last := pts[len(pts)-1]
	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg class="trend" viewBox="0 0 120 28" role="img" aria-label="%s"><polyline points="`, template.HTMLEscapeString(strings.Join(strings.Fields(label), " ")))
	for _, p := range pts {
		fmt.Fprintf(&sb, "%.1f,%.1f ", p.x, y(p.value))
	}
	fmt.Fprintf(&sb, `"/><circle cx="%.1f" cy="%.1f" r="2.5"/></svg>`, last.x, y(last.value))
	return template.HTML(sb.String())
}

// chartPoints is how many readings the chart draws a point for: more points than this overlap on its 640 units and
// only add weight, so the line alone draws a longer series (docs/plans/100-one-page-a-metric.md).
const chartPoints = 200

// chart draws a series as an inline SVG: a line through its readings, each placed by its day between from and to,
// and a point a reading while there are at most chartPoints; "" with no reading to draw. The least and the greatest
// value of the axis are those of the readings that it draws.
func chart(from, to string, readings any) template.HTML {
	list, _ := readings.([]any)
	// An absent lower bound is explicit all-history mode. Plot from the earliest
	// returned observation, without giving an ordinary calendar date sentinel meaning.
	if from == "" {
		for _, it := range list {
			m, _ := it.(map[string]any)
			day, _ := m["day"].(string)
			_, isNumber := m["value"].(float64)
			if core.IsDay(day) && isNumber && (from == "" || day < from) {
				from = day
			}
		}
	}
	f, ok1 := core.ParseDay(from)
	t, ok2 := core.ParseDay(to)
	if len(list) == 0 || !ok1 || !ok2 || t.Before(f) {
		return ""
	}
	type pt struct {
		day   time.Time
		x     float64
		value float64
	}
	var pts []pt
	lo, hi := 0.0, 0.0
	for _, it := range list {
		m, _ := it.(map[string]any)
		day, _ := m["day"].(string)
		v, isNumber := m["value"].(float64)
		d, ok := core.ParseDay(day)
		if !ok || !isNumber { // no day, or no value (a retraction): nothing to draw
			continue
		}
		if len(pts) == 0 || v < lo { // the range starts at the first reading that is drawn
			lo = v
		}
		if len(pts) == 0 || v > hi {
			hi = v
		}
		// Unix seconds cover the full supported date range without duration saturation.
		x := 0.5
		if t.After(f) {
			x = float64(d.Unix()-f.Unix()) / float64(t.Unix()-f.Unix())
		}
		pts = append(pts, pt{d, x, v})
	}
	if len(pts) == 0 {
		return ""
	}
	const w, h, pad, left, bottom = 640.0, 200.0, 14.0, 48.0, 22.0
	span := hi - lo
	if span == 0 {
		span = 1
	}
	x := func(p pt) float64 { return left + p.x*(w-left-pad) }
	y := func(v float64) float64 { return pad + (1-(v-lo)/span)*(h-2*pad-bottom) }
	var sb strings.Builder
	first, last := f.Format(layoutDateYear), t.Format(layoutDateYear) // the ends in words, with the year: a range can span years
	fmt.Fprintf(&sb, `<svg class="chart" viewBox="0 0 %g %g" role="img" aria-label="%d readings from %s to %s">`,
		w, h, len(pts), first, last)
	fmt.Fprintf(&sb, `<line class="axis" x1="%g" y1="%g" x2="%g" y2="%g"/><line class="axis" x1="%g" y1="%g" x2="%g" y2="%g"/>`,
		left, y(hi), w-pad, y(hi), left, y(lo), w-pad, y(lo))
	fmt.Fprintf(&sb, `<text x="%g" y="%g">%g</text><text x="%g" y="%g">%g</text>`, left-6, y(hi)+4, hi, left-6, y(lo)+4, lo)
	fmt.Fprintf(&sb, `<text class="x" x="%g" y="%g">%s</text><text class="x" x="%g" y="%g">%s</text>`,
		left+40, h-4, first, w-pad-40, h-4, last)
	sb.WriteString(`<polyline points="`)
	for _, p := range pts {
		fmt.Fprintf(&sb, "%.1f,%.1f ", x(p), y(p.value))
	}
	sb.WriteString(`"/>`)
	if len(pts) <= chartPoints {
		for _, p := range pts {
			fmt.Fprintf(&sb, `<circle cx="%.1f" cy="%.1f" r="3"><title>%s: %g</title></circle>`, x(p), y(p.value),
				p.day.Format(layoutDateYear), p.value)
		}
	}
	sb.WriteString(`</svg>`)
	return template.HTML(sb.String())
}
