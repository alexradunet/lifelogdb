package api

import (
	"html/template"
	"strings"
)

// The one page of a metric (docs/plans/100-one-page-a-metric.md): its readings newest first, in one fold a year.
// The series gives them oldest first; only the view orders and folds them.

// newestRows is how many readings a year's fold shows; the rest of the year are in a closed fold under them, so the
// page's other groups stay near on a long series.
const newestRows = 10

// openFolds is how many readings a series may hold for every year's fold to be open: a short series (a lab
// marker read twice a year) is read whole, and a long one opens on its newest year.
const openFolds = 60

// readingYear is one fold of a metric's readings: a year, its newest readings, the rest of them (More), the line its
// summary says of them all, and whether it is open.
type readingYear struct {
	Year    string
	Rows    []map[string]any
	More    []map[string]any
	Summary string
	Open    bool
}

// metricReadings are a series' readings as its page draws them: the folds, newest year first; the newest reading;
// whether a column of times is needed; and whether the values are a habit's check-ins (D24), said in words.
type metricReadings struct {
	Years  []readingYear
	Latest map[string]any
	Times  bool
	Habit  bool
}

// MetricReadings are the series' readings by year, newest first.
func (v *view) MetricReadings() metricReadings {
	props, _ := v.Props.(map[string]any)
	list, _ := props["readings"].([]any)
	periods, _ := props["habit_periods"].([]any)
	unit, _ := props["unit"].(string)
	r := metricReadings{Habit: len(periods) > 0}
	for i := len(list) - 1; i >= 0; i-- {
		m, _ := list[i].(map[string]any)
		day, _ := m["day"].(string)
		if !isDayText(day) {
			continue
		}
		if n := len(r.Years); n == 0 || r.Years[n-1].Year != day[:4] {
			r.Years = append(r.Years, readingYear{Year: day[:4]})
		}
		y := &r.Years[len(r.Years)-1]
		y.Rows = append(y.Rows, m)
		if _, ok := m["taken_at"]; ok {
			r.Times = true
		}
	}
	for i := range r.Years {
		y := &r.Years[i]
		y.Open = i == 0 || len(list) <= openFolds
		y.Summary = yearSummary(y.Rows, unit, r.Habit)
		if len(y.Rows) > newestRows {
			y.Rows, y.More = y.Rows[:newestRows], y.Rows[newestRows:]
		}
	}
	if len(r.Years) > 0 {
		r.Latest = r.Years[0].Rows[0]
	}
	return r
}

// isDayText reports a day as the series writes it, YYYY-MM-DD: its year is its first four characters.
func isDayText(day string) bool {
	_, ok := dayOf(day)
	return ok && len(day) == 10
}

// yearSummary is what a closed fold says of its year: "214 readings · 76.2 to 81 kg", or for a habit
// "9 done, 2 not done".
func yearSummary(rows []map[string]any, unit string, habit bool) string {
	var done, notDone float64
	var lo, hi float64
	n := 0
	for _, m := range rows {
		v, ok := m["value"].(float64)
		if !ok {
			continue
		}
		switch {
		case habit && v == 1:
			done++
		case habit && v == 0:
			notDone++
		}
		if n == 0 || v < lo {
			lo = v
		}
		if n == 0 || v > hi {
			hi = v
		}
		n++
	}
	if habit {
		return num(done) + " done, " + num(notDone) + " not done"
	}
	s := count(float64(len(rows)), "reading", "readings")
	if n == 0 {
		return s
	}
	values := num(lo)
	if hi != lo {
		values += " to " + num(hi)
	}
	if unit != "" {
		values += " " + unit
	}
	return s + " · " + values
}

// The metrics as cards (docs/plans/112-metrics-as-cards.md): a section for each top-level group of /metrics, its
// subcategories inside it, and a card for each metric with its latest reading and the line of its recent ones.

// metricCard is a metric as its card draws it. A habit's card holds its state today and its strip instead.
type metricCard struct {
	ID          string
	Name, Href  string
	Value, Unit string
	Measurement string // the latest reading's id, "" with none
	When        string // the latest reading's day in words
	Count       int    // how many recent readings the line holds
	Line        template.HTML
	Stale       bool // the latest reading is older than inUseDays
	Note        string
	Habit       bool
	Categories  []string // a habit's categories: it is drawn only under Habits
	State       string   // today's state; "stopped" for a habit no period holds today
	Strip       habitStrip
}

// metricSub is a category under a section's top: its path below the top, its page and its cards.
type metricSub struct {
	Title, Page string
	Cards       []metricCard
}

// metricSection is one top-level group: Habits, a category, or Not filed. Count is how many metrics it holds, each
// once, in its subcategories too.
type metricSection struct {
	Title, Page, Anchor string
	Count               int
	Habits              bool
	Cards               []metricCard
	Subs                []metricSub
}

// MetricSections are the groups of /metrics as sections of cards, in the order of the groups.
func (v *view) MetricSections() []metricSection {
	props, _ := v.Props.(map[string]any)
	today, _ := props["today"].(string)
	groups, _ := props["groups"].([]any)
	rows := map[string]map[string]any{}
	if ms, _ := props["metrics"].([]any); ms != nil {
		for _, x := range ms {
			if m, _ := x.(map[string]any); m != nil {
				rows[num(m["id"])] = m
			}
		}
	}
	habits := map[string]map[string]any{}
	if hs, _ := props["habit_days"].([]any); hs != nil {
		for _, x := range hs {
			if h, _ := x.(map[string]any); h != nil {
				name, _ := h["metric"].(string)
				habits[name] = h
			}
		}
	}
	c := clock{today: today}
	limit, _ := dayOf(today)
	limit = limit.AddDate(0, 0, -inUseDays)
	card := func(x any) metricCard {
		g, _ := x.(map[string]any)
		id := num(g["id"])
		m := rows[id]
		if m == nil {
			m = g
		}
		name, _ := m["name"].(string)
		unit, _ := m["unit"].(string)
		habit, _ := m["habit"].(bool)
		note, _ := m["note"].(string)
		k := metricCard{ID: id, Name: name, Href: metricHref(name), Unit: unitWords(unit), Note: note, Habit: habit}
		recent, _ := m["recent"].([]any)
		if n := len(recent); n > 0 {
			last, _ := recent[n-1].(map[string]any)
			day, _ := last["day"].(string)
			k.Value, k.Measurement, k.When, k.Count = num(last["value"]), num(last["id"]), c.relative(day), n
			if t, ok := dayOf(day); ok && !limit.IsZero() && t.Before(limit) {
				k.Stale = true
			}
			if !habit {
				k.Line = sparkLine(name, unit, today, recent)
			}
		}
		if habit {
			for _, c := range anyList(m["categories"]) {
				if p, ok := c.(string); ok {
					k.Categories = append(k.Categories, p)
				}
			}
			k.State = "stopped"
			if h := habits[name]; h != nil {
				k.State, _ = h["state"].(string)
				k.Strip = v.HabitStrip(h["recent"])
			}
		}
		return k
	}
	var out []metricSection
	var seen map[string]bool
	for _, x := range groups {
		g, _ := x.(map[string]any)
		title, _ := g["title"].(string)
		depth := num(g["depth"])
		ms, _ := g["metrics"].([]any)
		if depth == "0" || len(out) == 0 {
			s := metricSection{Title: title, Page: num(g["page"]), Habits: g["page"] == nil && title == "Habits"}
			switch {
			case s.Page != "":
				s.Anchor = "c-" + s.Page
			case s.Habits:
				s.Anchor = "habits"
			default:
				s.Anchor = "not-filed"
			}
			out = append(out, s)
			seen = map[string]bool{}
		}
		s := &out[len(out)-1]
		var cards []metricCard
		for _, m := range ms {
			k := card(m)
			cards = append(cards, k)
			if !seen[k.ID] {
				seen[k.ID] = true
				s.Count++
			}
		}
		if depth == "0" {
			s.Cards = cards
			continue
		}
		path, _ := g["path"].(string)
		sub := path
		if i := strings.Index(path, "/"); i >= 0 {
			sub = path[i+1:]
		}
		s.Subs = append(s.Subs, metricSub{Title: sub, Page: num(g["page"]), Cards: cards})
	}
	return out
}
