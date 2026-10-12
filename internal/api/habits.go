package api

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"lifelog/internal/core"
)

// correctTo corrects or retracts a reading.
func (h *server) correctTo(r *http.Request, src string, id int64, value *float64) (*Entity, error) {
	fix, err := h.s.Correct(r.Context(), src, id, value)
	if err != nil {
		return nil, err
	}
	return h.measurementEntity(r.Context(), fix)
}

func (h *server) rename(r *http.Request, src string) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "title"); err != nil {
		return nil, err
	}
	to, err := h.s.Rename(r.Context(), src, id, v.Get("title"))
	if err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), to)
}

func (h *server) registerMetric(r *http.Request, src string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "name"); err != nil {
		return nil, err
	}
	if _, err := h.s.RegisterMetric(r.Context(), src, v.Get("name"), v.Get("unit"), v.Get("note")); err != nil {
		return nil, err
	}
	return h.metricEntity(r, v.Get("name"))
}

func (h *server) startHabit(r *http.Request, src string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "start_day"); err != nil {
		return nil, err
	}
	if err := h.s.StartHabit(r.Context(), src, r.PathValue("name"), v.Get("start_day"), v.Get("end_day")); err != nil {
		return nil, err
	}
	return h.metricEntity(r, r.PathValue("name"))
}

func (h *server) stopHabit(r *http.Request, src string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "day"); err != nil {
		return nil, err
	}
	if err := h.s.StopHabit(r.Context(), src, r.PathValue("name"), v.Get("day")); err != nil {
		return nil, err
	}
	return h.metricEntity(r, r.PathValue("name"))
}

// checkIn records a habit for a day inside one of its periods: 1 done, 0 not done.
func (h *server) checkIn(r *http.Request, src string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "day", "done"); err != nil {
		return nil, err
	}
	name, day, done := r.PathValue("name"), v.Get("day"), v.Get("done")
	if done != "0" && done != "1" {
		return nil, &core.Error{Status: 422, Msg: "done is 1 (done) or 0 (not done)"}
	}
	periods, err := h.s.Periods(r.Context(), name)
	if err != nil {
		return nil, err
	}
	on := false
	for _, p := range periods {
		on = on || (p.Start <= day && (p.End == "" || p.End >= day))
	}
	if !on {
		return nil, &core.Error{Status: 422, Msg: name + " is not a habit on " + day + ": start it first"}
	}
	value := 0.0
	if done == "1" {
		value = 1
	}
	if _, err := h.s.Record(r.Context(), src, core.Reading{Metric: name, Day: day, Value: value}); err != nil {
		return nil, err
	}
	r2 := r.Clone(r.Context())
	r2.URL.RawQuery = "day=" + url.QueryEscape(day)
	return h.habits(r2)
}

// habits is the habits of ?day= (default today), each with its check-in, and completion over ?from=..?to=
// (default the 30 days up to that day), cookbook/habits.md.
func (h *server) habits(r *http.Request) (*Entity, error) {
	q := r.URL.Query()
	day := q.Get("day")
	if day == "" {
		day = h.localDay()
	}
	hs, err := h.s.Habits(r.Context(), day)
	if err != nil {
		return nil, err
	}
	to := q.Get("to")
	if to == "" {
		to = day
	}
	from := q.Get("from")
	if from == "" {
		t, _ := time.Parse(time.DateOnly, to)
		from = t.AddDate(0, 0, -29).Format(time.DateOnly)
	}
	comp, err := h.s.Completion(r.Context(), from, to)
	if err != nil {
		return nil, err
	}
	t, _ := time.Parse(time.DateOnly, day)
	e := &Entity{Class: []string{"habits"}, Title: "Habits on " + day,
		Properties: map[string]any{"day": day, "habits": hs, "from": from, "to": to, "completion": comp},
		Links: []Link{link("self", "/habits?day="+day, "Habits"), link("day", dayHref(day), day),
			link("prev", "/habits?day="+t.AddDate(0, 0, -1).Format(time.DateOnly), "Previous day"),
			link("next", "/habits?day="+t.AddDate(0, 0, 1).Format(time.DateOnly), "Next day"), link("index", "/", "Home")}}
	for _, x := range hs {
		e.Entities = append(e.Entities, link("habit", "/metrics/"+url.PathEscape(x.Metric), x.Metric+": "+x.State))
	}
	e.Actions = checkIns(hs, day)
	return e, nil
}

// checkIns are the check-in actions of the habits active on a day, that day filled in: /habits, a day and the root
// offer them.
func checkIns(hs []core.HabitState, day string) []Action {
	out := []Action{}
	for _, x := range hs {
		out = append(out, action("check-in", map[string]string{"name": url.PathEscape(x.Metric)}, map[string]any{"day": day}))
	}
	return out
}

// metricEntity is the resource /metrics/{name} serves.
func (h *server) metricEntity(r *http.Request, name string) (*Entity, error) {
	r2 := r.Clone(r.Context())
	r2.SetPathValue("name", name)
	r2.URL.RawQuery = ""
	return h.series(r2)
}

func (h *server) unitless(ctx context.Context, name string) bool {
	m := h.metricOf(ctx, name)
	return m != nil && m.Unit == ""
}

func (h *server) integrity(r *http.Request) (*Entity, error) {
	res, err := h.s.Integrity(r.Context())
	if err != nil {
		return nil, err
	}
	return &Entity{Class: []string{"integrity"}, Title: "Integrity checks", Properties: res,
		Links: []Link{link("self", "/integrity", "Integrity"), link("index", "/", "Home")}}, nil
}

// glanceDays is how many days, the day itself the last, a habit's recent states and a reading's trend cover
// (docs/plans/093-words-phone-and-today.md).
const glanceDays = 14

// habitView is a habit on a day with its state on each of the glanceDays days through it, oldest first.
type habitView struct {
	core.HabitState
	Recent []string `json:"recent"`
}

// habitsOn are the habits active on day, each with its recent states: the state core.Habits reads on each day (so
// the rule has one home), or "inactive" on a day no period of the habit holds. The plain states are returned too,
// for the check-in actions.
func (h *server) habitsOn(ctx context.Context, day string) ([]habitView, []core.HabitState, error) {
	t, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return nil, nil, &core.Error{Status: 422, Msg: "day must be a day, YYYY-MM-DD"}
	}
	byDay := make([]map[string]string, glanceDays)
	var states []core.HabitState
	for i := range byDay {
		byDay[i] = map[string]string{}
		d := t.AddDate(0, 0, i-(glanceDays-1)).Format(time.DateOnly)
		if !core.IsDay(d) {
			continue // before the first day a date can name: no habit was active
		}
		hs, err := h.s.Habits(ctx, d)
		if err != nil {
			return nil, nil, err
		}
		for _, x := range hs {
			byDay[i][x.Metric] = x.State
		}
		if i == glanceDays-1 {
			states = hs
		}
	}
	out := []habitView{}
	for _, x := range states {
		v := habitView{HabitState: x, Recent: make([]string, glanceDays)}
		for i, m := range byDay {
			if s, ok := m[x.Metric]; ok {
				v.Recent[i] = s
			} else {
				v.Recent[i] = "inactive"
			}
		}
		out = append(out, v)
	}
	return out, states, nil
}

// trend is a metric read on a day: its unit, and its current unassociated readings of the glanceDays days through
// the day, oldest first (the series of the cookbook).
type trend struct {
	Metric   string       `json:"metric"`
	Unit     string       `json:"unit"`
	Readings []trendPoint `json:"readings"`
}

// trendPoint is one reading of a trend.
type trendPoint struct {
	Day   string  `json:"day"`
	Value float64 `json:"value"`
}

// trends are the trends of the metrics with a current unassociated reading among a day's readings, in the order of
// those readings. A reading of a session is not a daily value, so it starts no trend.
func (h *server) trends(ctx context.Context, day string, readings []core.Reading) ([]trend, error) {
	out := []trend{}
	seen := map[string]bool{}
	var names []string
	for _, r := range readings {
		if r.SessionID == 0 && !seen[r.Metric] {
			seen[r.Metric] = true
			names = append(names, r.Metric)
		}
	}
	if len(names) == 0 {
		return out, nil
	}
	ms, err := h.s.Metrics(ctx)
	if err != nil {
		return nil, err
	}
	units := map[string]string{}
	for _, m := range ms {
		units[m.Name] = m.Unit
	}
	t, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return nil, err
	}
	from := t.AddDate(0, 0, -glanceDays).Format(time.DateOnly) // a series is (from, to]
	if !core.IsDay(from) {
		from = "0000-01-01" // the first days a date can name: the trend starts the day after
	}
	for _, name := range names {
		rows, err := h.s.Series(ctx, name, from, day)
		if err != nil {
			return nil, err
		}
		tr := trend{Metric: name, Unit: units[name], Readings: []trendPoint{}}
		for _, r := range rows {
			tr.Readings = append(tr.Readings, trendPoint{Day: r.Day, Value: r.Value})
		}
		out = append(out, tr)
	}
	return out, nil
}
