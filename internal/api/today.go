package api

import (
	"fmt"
	"html/template"
	"regexp"
	"strconv"
	"strings"
	"time"

	"lifelog/internal/core"
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
			if m["metric"] == metric && m["session_id"] == nil {
				if tile.ID != "" {
					tile.More++
				}
				tile.ID, tile.Value = num(m["id"]), num(m["value"])
			}
		}
		points, _ := t["readings"].([]any)
		tile.Line = trendLine(metric, unit, day, points)
		out = append(out, tile)
	}
	return out
}

// SessionReadings are the day's readings of a session: no trend holds them, as they are not daily values.
func (v *view) SessionReadings() []map[string]any {
	props, _ := v.Props.(map[string]any)
	readings, _ := props["readings"].([]any)
	var out []map[string]any
	for _, r := range readings {
		if m, _ := r.(map[string]any); m["session_id"] != nil {
			out = append(out, m)
		}
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
// through the day, the last one marked; "" with no reading. Its label says the first and the last value in words.
func trendLine(metric, unit, day string, points []any) template.HTML {
	end, ok := dayOf(day)
	if !ok || len(points) == 0 {
		return ""
	}
	type pt struct {
		x, value float64
		day      string
	}
	var pts []pt
	lo, hi := 0.0, 0.0
	for i, p := range points {
		m, _ := p.(map[string]any)
		d, _ := m["day"].(string)
		value, _ := m["value"].(float64)
		t, ok := dayOf(d)
		if !ok {
			continue
		}
		back := end.Sub(t).Hours() / 24 // 0 on the day, 13 on the first of the 14
		pts = append(pts, pt{x: 4 + (float64(glanceDays-1)-back)*112/float64(glanceDays-1), value: value, day: d})
		if i == 0 || value < lo {
			lo = value
		}
		if i == 0 || value > hi {
			hi = value
		}
	}
	if len(pts) == 0 {
		return ""
	}
	y := func(v float64) float64 {
		if hi == lo {
			return 14
		}
		return 24 - (v-lo)/(hi-lo)*20
	}
	c := clock{today: day}
	first, last := pts[0], pts[len(pts)-1]
	label := fmt.Sprintf("%s from %s %s %s to %s %s %s", metric, strconv.FormatFloat(first.value, 'f', -1, 64), unitWords(unit),
		c.relative(first.day), strconv.FormatFloat(last.value, 'f', -1, 64), unitWords(unit), c.relative(last.day))
	if len(pts) == 1 {
		label = fmt.Sprintf("%s: one reading in 14 days, %s %s %s", metric, strconv.FormatFloat(last.value, 'f', -1, 64), unitWords(unit), c.relative(last.day))
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg class="trend" viewBox="0 0 120 28" role="img" aria-label="%s"><polyline points="`, template.HTMLEscapeString(strings.Join(strings.Fields(label), " ")))
	for _, p := range pts {
		fmt.Fprintf(&sb, "%.1f,%.1f ", p.x, y(p.value))
	}
	fmt.Fprintf(&sb, `"/><circle cx="%.1f" cy="%.1f" r="2.5"/></svg>`, last.x, y(last.value))
	return template.HTML(sb.String())
}

// habitCell is one day of a habit's strip.
type habitCell struct {
	State, Title string
}

// habitStrip is a habit's last 14 days: a cell each, and how many of its active days were done.
type habitStrip struct {
	Cells        []habitCell
	Done, Active int
}

// HabitStrip draws a habit's recent states, oldest first, ending on the entity's day (a day's, or the root's today).
func (v *view) HabitStrip(recent any) habitStrip {
	props, _ := v.Props.(map[string]any)
	end, _ := props["day"].(string)
	if end == "" {
		end, _ = props["today"].(string)
	}
	states, _ := recent.([]any)
	var s habitStrip
	last, _ := dayOf(end)
	c := v.clock()
	for i, x := range states {
		state, _ := x.(string)
		d := last.AddDate(0, 0, i-(len(states)-1)).Format(time.DateOnly)
		s.Cells = append(s.Cells, habitCell{State: strings.ReplaceAll(state, " ", "-"), Title: c.day(d) + ": " + state})
		if state != "inactive" {
			s.Active++
		}
		if state == core.StateDone {
			s.Done++
		}
	}
	return s
}

// capLater keeps the first n occurrences of the group Later; the last one kept stands for the rest. A series on its
// own page then shows its next slots, not every slot of its window.
func capLater(groups []dueGroup, n int) []dueGroup {
	for i := range groups {
		if g := &groups[i]; g.Name == "Later" && len(g.Items) > n {
			rest := len(g.Items) - n
			g.Items = g.Items[:n]
			g.Items[n-1].More += rest
		}
	}
	return groups
}
