package api

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"lifelog/internal/core"
)

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
	if day == "today" { // the server's day, as /days/today: a client need not know it
		day = h.localDay()
	}
	isDone, err := flag(v, "done")
	if err != nil {
		return nil, err
	}
	if _, err := h.s.CheckIn(r.Context(), src, name, day, isDone); err != nil {
		return nil, err
	}
	return h.habitsEntity(r.Context(), url.Values{"day": {day}})
}

// habits is the habits of ?day= (default today), each with its check-in, and completion over ?from=..?to=
// (default the 30 days up to that day), cookbook/habits.md.
//
// In a browser the habits are a lens of Metrics (docs/plans/111-one-metrics-view.md): the page is redirected, with
// its day and window kept. JSON, the CLI and MCP keep this resource.
func (h *server) habits(r *http.Request) (*Entity, error) {
	q := r.URL.Query()
	if wantsHTML(r) {
		q.Set("show", "habits")
		return nil, redirect{"/metrics?" + q.Encode()}
	}
	return h.habitsEntity(r.Context(), q)
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
	first := t.AddDate(0, 0, -(glanceDays - 1)).Format(time.DateOnly)
	if !core.IsDay(first) {
		first = "0000-01-01" // before the first day a date can name: no habit was active
	}
	read, err := h.s.HabitStates(ctx, first, day) // one statement for the window, not one a day
	if err != nil {
		return nil, nil, err
	}
	byDay := make([]map[string]string, glanceDays)
	for i := range byDay {
		byDay[i] = map[string]string{}
	}
	states := []core.HabitState{}
	for _, x := range read {
		at, _ := dayOf(x.Day)
		i := glanceDays - 1 - int(t.Sub(at).Hours()/24)
		if i < 0 || i >= glanceDays {
			continue
		}
		byDay[i][x.Metric] = x.State
		if x.Day == day {
			states = append(states, x.HabitState)
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

// trends are the trends of the metrics with a current reading among a day's readings, in the order of those
// readings. A habit is told by its strip, so it has no trend.
func (h *server) trends(ctx context.Context, day string, readings []core.Reading, habits []habitView) ([]trend, error) {
	out := []trend{}
	seen := map[string]bool{}
	for _, x := range habits {
		seen[x.Metric] = true
	}
	var names []string
	for _, r := range readings {
		if !seen[r.Metric] {
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

// habitsLens adds the habits read to the metrics entity, for ?show=habits (docs/plans/111-one-metrics-view.md): the
// day's habits, the completion, the step to the next and previous day, and the check-ins. The rule has one home,
// habitsEntity.
func (h *server) habitsLens(r *http.Request, e *Entity) error {
	he, err := h.habitsEntity(r.Context(), r.URL.Query())
	if err != nil {
		return err
	}
	props := he.Properties.(map[string]any)
	mine := e.Properties.(map[string]any)
	for _, k := range []string{"day", "habits", "from", "to", "completion"} {
		mine[k] = props[k]
	}
	mine["show"] = "habits"
	day := props["day"].(string)
	lens := func(d string) string {
		return "/metrics?" + url.Values{"show": {"habits"}, "day": {d}}.Encode()
	}
	e.Links = append(e.Links, link("day", dayHref(day), day), link("prev", lens(addDays(day, -1)), "Previous day"),
		link("next", lens(addDays(day, 1)), "Next day"))
	e.Entities = append(e.Entities, he.Entities...)
	e.Actions = append(e.Actions, he.Actions...)
	return nil
}
