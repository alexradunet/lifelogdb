package api

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"lifelog/internal/core"
)

// correctTo corrects or retracts a reading.
func (h *server) correctTo(ctx context.Context, src string, id int64, value *float64) (*Entity, error) {
	fix, err := h.s.Correct(ctx, src, id, value)
	if err != nil {
		return nil, err
	}
	return h.measurementEntity(ctx, fix)
}

func (h *server) rename(r *http.Request, src string) (*Entity, error) {
	id, v, err := idForm(r, "title")
	if err != nil {
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
	return h.seriesEntity(r.Context(), v.Get("name"), nil)
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
	return h.seriesEntity(r.Context(), r.PathValue("name"), nil)
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
	return h.seriesEntity(r.Context(), r.PathValue("name"), nil)
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
	name, day := r.PathValue("name"), v.Get("day")
	isDone, err := flag(v, "done")
	if err != nil {
		return nil, err
	}
	periods, err := h.s.HabitPeriods(r.Context(), name)
	if err != nil {
		return nil, err
	}
	on := false
	for _, p := range periods {
		on = on || (p.Start <= day && (p.End == "" || p.End >= day))
	}
	if !on {
		return nil, core.Invalid("%s is not a habit on %s: start it first", name, day)
	}
	value := 0.0
	if isDone {
		value = 1
	}
	if _, err := h.s.Record(r.Context(), src, core.Reading{Metric: name, Day: day, Value: value}); err != nil {
		return nil, err
	}
	return h.habitsEntity(r.Context(), url.Values{"day": {day}})
}

// habits is the habits of ?day= (default today), each with its check-in, and completion over ?from=..?to=
// (default the 30 days up to that day), cookbook/habits.md.
func (h *server) habits(r *http.Request) (*Entity, error) {
	return h.habitsEntity(r.Context(), r.URL.Query())
}

// habitsEntity is the resource /habits serves; q is its query (day, from, to).
func (h *server) habitsEntity(ctx context.Context, q url.Values) (*Entity, error) {
	day := q.Get("day")
	if day == "" {
		day = h.localDay()
	}
	hs, err := h.s.Habits(ctx, day)
	if err != nil {
		return nil, err
	}
	to := q.Get("to")
	if to == "" {
		to = day
	}
	from := q.Get("from")
	if from == "" {
		from = addDays(to, -29)
	}
	comp, err := h.s.Completion(ctx, from, to)
	if err != nil {
		return nil, err
	}
	e := &Entity{Class: []string{"habits"}, Title: "Habits on " + day,
		Properties: map[string]any{"day": day, "habits": hs, "from": from, "to": to, "completion": comp},
		Links: []Link{link("self", "/habits?day="+day, "Habits"), link("day", dayHref(day), day),
			link("prev", "/habits?day="+addDays(day, -1), "Previous day"),
			link("next", "/habits?day="+addDays(day, 1), "Next day"), link("index", "/", "Home")}}
	for _, x := range hs {
		e.Entities = append(e.Entities, link("habit", metricHref(x.Metric), x.Metric+": "+x.State))
	}
	e.Actions = checkIns(hs, day)
	return e, nil
}

// checkIns are the check-in actions of the habits active on a day, that day filled in: /habits, a day and the root
// offer them.
func checkIns(hs []core.HabitState, day string) []Action {
	out := []Action{}
	for _, x := range hs {
		out = append(out, action("check-in", map[string]string{"name": x.Metric}, map[string]any{"day": day}))
	}
	return out
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
		return nil, nil, core.Invalid("day must be a day, YYYY-MM-DD")
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
// those readings. A reading of a session is not a daily value, so it starts no trend. A habit is told by its strip,
// so it has no trend.
func (h *server) trends(ctx context.Context, day string, readings []core.Reading, habits []habitView) ([]trend, error) {
	out := []trend{}
	seen := map[string]bool{}
	for _, x := range habits {
		seen[x.Metric] = true
	}
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
