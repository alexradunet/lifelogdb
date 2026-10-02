package api

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"lifelog/internal/core"
)

// correctTo corrects or retracts a reading; with an import workspace, a correction of an imported reading is
// also written to the workspace, so a replay makes it again (docs/guides/importing.md).
func (h *server) correctTo(r *http.Request, src string, id int64, value *float64) (*Entity, error) {
	fix, key, err := h.s.Correct(r.Context(), src, id, value)
	if err != nil {
		return nil, err
	}
	if h.ws != nil && core.IsImport(key.Source) && key.Key != "" {
		if err := h.ws.RecordCorrection(key); err != nil {
			return nil, err
		}
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
		day = core.Today()
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
		e.Actions = append(e.Actions, action("check-in", map[string]string{"name": url.PathEscape(x.Metric)}, map[string]any{"day": day}))
	}
	return e, nil
}

// metricEntity is the resource /metrics/{name} serves.
func (h *server) metricEntity(r *http.Request, name string) (*Entity, error) {
	r2 := r.Clone(r.Context())
	r2.SetPathValue("name", name)
	r2.URL.RawQuery = ""
	return h.series(r2)
}

func (h *server) unitless(ctx context.Context, name string) bool {
	ms, _ := h.s.Metrics(ctx)
	for _, m := range ms {
		if m.Name == name {
			return m.Unit == ""
		}
	}
	return false
}

func (h *server) integrity(r *http.Request) (*Entity, error) {
	res, err := h.s.Integrity(r.Context())
	if err != nil {
		return nil, err
	}
	return &Entity{Class: []string{"integrity"}, Title: "Integrity checks", Properties: res,
		Links: []Link{link("self", "/integrity", "Integrity"), link("index", "/", "Home")}}, nil
}
