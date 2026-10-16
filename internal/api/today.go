package api

import (
	"fmt"
	"html/template"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The richer Today (docs/plans/093-words-phone-and-today.md): a reading's tile with its unit and the line of its
// last 14 days, a habit's strip of its last 14 days, and a series that does not fill its own page.

// readingTile is a metric read on the day: its last reading of the day (the one to correct), its unit in words,
// how many readings of the day it stands for, and the line of its trend.
type readingTile struct {
	Metric, Unit, Value string
	ID                  string
	More                int
	Line                template.HTML
}

// ReadingTiles are the tiles of the day's trends, one for each metric, in the order of the day's readings.
func (v *view) ReadingTiles() []readingTile {
	props, _ := v.Props.(map[string]any)
	day, _ := props["day"].(string)
	readings, _ := props["readings"].([]any)
	trends, _ := props["trends"].([]any)
	var out []readingTile
	for _, x := range trends {
		t, _ := x.(map[string]any)
		metric, _ := t["metric"].(string)
		unit, _ := t["unit"].(string)
		tile := readingTile{Metric: metric, Unit: unitWords(unit)}
		for _, r := range readings {
			m, _ := r.(map[string]any)
			if m["metric"] == metric {
				if tile.ID != "" {
					tile.More++
				}
				tile.ID, tile.Value = num(m["id"]), num(m["value"])
			}
		}
		points, _ := t["readings"].([]any)
		tile.Line = trendLine(v.clock(), metric, unit, day, points)
		out = append(out, tile)
	}
	return out
}

var scaleUnit = regexp.MustCompile(`^\d+-(\d+)$`)

// unitWords is a unit as it follows a value: a scale "1-5" is "of 5"; anything else is itself.
func unitWords(unit string) string {
	if m := scaleUnit.FindStringSubmatch(unit); m != nil {
		return "of " + m[1]
	}
	return unit
}

// trendLine draws a trend as an inline SVG, 120 by 28: one point per reading, placed by its day among the 14 days
// through the day, the last one marked; "" with no reading. Its label says the first and the last value in words,
// with their days against today on the clock c, also on a past day.
func trendLine(c clock, metric, unit, day string, points []any) template.HTML {
	end, ok := dayOf(day)
	if !ok {
		return ""
	}
	var pts []linePoint
	for _, p := range points {
		m, _ := p.(map[string]any)
		d, _ := m["day"].(string)
		value, _ := m["value"].(float64)
		t, ok := dayOf(d)
		if !ok {
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
		value, _ := m["value"].(float64)
		if t, ok := dayOf(d); ok {
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
