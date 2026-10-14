package api

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
