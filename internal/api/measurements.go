package api

// The readings: the metrics, a series, a reading, and the writes of readings.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"

	"lifelog/internal/core"
)

// inUseDays is how far back a view looks for the metrics it offers to record: a series read at least monthly
// stays offered, a lab marker read twice a year is recorded from its own page (/metrics/{name}).
const inUseDays = 60

// recordAction offers the metrics in use on day (and metric, when the view is that metric's), not the whole
// registry; the record action itself takes any registered metric.
func (h *server) recordAction(ctx context.Context, metric, day string) (Action, error) {
	a := action("record", nil, map[string]any{"day": day})
	if metric != "" {
		a.field("metric").Value = metric
	}
	names, err := h.s.InUse(ctx, day, inUseDays)
	if err != nil {
		return Action{}, err
	}
	if metric != "" && !slices.Contains(names, metric) {
		names = append(names, metric)
		slices.Sort(names)
	}
	return withOptions(a, "metric", names), nil
}

// recentReadings is how many of a metric's newest readings /metrics reads for its card: the latest, and the line of
// the ones before it.
const recentReadings = 30

// metricRow is a metric as /metrics lists it: the metric, and its newest current unassociated readings, oldest first
// (the last is its latest).
type metricRow struct {
	core.Metric
	Recent []core.Recent `json:"recent"`
}

func (h *server) metrics(r *http.Request) (*Entity, error) {
	ctx := r.Context()
	ms, err := h.s.Metrics(ctx)
	if err != nil {
		return nil, err
	}
	recent, err := h.s.RecentReadings(ctx, recentReadings)
	if err != nil {
		return nil, err
	}
	var names []string
	rows := []metricRow{}
	for _, m := range ms {
		names = append(names, m.Name)
		rows = append(rows, metricRow{Metric: m, Recent: append([]core.Recent{}, recent[m.ID]...)})
	}
	cs, err := h.s.Categories(ctx)
	if err != nil {
		return nil, err
	}
	today := h.localDay()
	e := &Entity{Class: []string{"metrics"}, Title: "Metrics",
		Properties: map[string]any{"metrics": rows, "groups": groupMetrics(ms, cs), "categories": cs, "today": today},
		Links:      []Link{link("self", "/metrics", "Metrics"), link("index", "/", "Home")},
		Actions: []Action{withOptions(action("record", nil, map[string]any{"day": today}), "metric", names),
			action("file-pages", nil, nil), action("register-metric", nil, nil)}}
	for _, m := range ms {
		e.Entities = append(e.Entities, link("item", metricHref(m.Name), m.Name))
	}
	return e, nil
}

// series is cookbook/metric-series.md; ?to= defaults to today, and with no ?from= every reading through it is read.
func (h *server) series(r *http.Request) (*Entity, error) {
	return h.seriesEntity(r.Context(), r.PathValue("name"), r.URL.Query())
}

// seriesEntity is the resource /metrics/{name} serves, the one page of a metric (docs/plans/100-one-page-a-metric.md):
// its readings, the writes of a reading and of a habit, and, as the metric is a page (D27), the page's text, edges
// and actions. q is its query (from, to, all_history), nil for the defaults.
func (h *server) seriesEntity(ctx context.Context, name string, q url.Values) (*Entity, error) {
	today := h.localDay()
	to := q.Get("to")
	if to == "" {
		to = today
	}
	if !core.IsDay(to) {
		return nil, core.Invalid("to must be an exact day")
	}
	allHistory, err := flag(q, "all_history")
	if err != nil {
		return nil, err
	}
	from := q.Get("from")
	if allHistory && from != "" {
		return nil, core.Invalid("all_history cannot select a lower bound")
	}
	allHistory = from == "" // no lower bound: every admitted day through to
	m, err := h.metricOf(ctx, name)
	if err != nil {
		return nil, err
	}
	if m != nil { // an alias names the metric by its name
		name = m.Name
	}
	rows, err := h.s.Series(ctx, name, from, to) // an empty from is all history
	if err != nil {
		return nil, err
	}
	// The series' own address, and the address of all its history: the same query with no lower bound.
	seriesHref := func(allHistory bool) string {
		v := url.Values{"to": {to}}
		if allHistory {
			v.Set("all_history", "1")
		} else {
			v.Set("from", from)
		}
		return metricHref(name) + "?" + v.Encode()
	}
	self, allHref := seriesHref(allHistory), seriesHref(true)
	if len(q) == 0 { // asked with no query, the series names itself so: a browser that registers a metric lands on it
		self = metricHref(name)
	}
	record, err := h.recordAction(ctx, name, today)
	if err != nil {
		return nil, err
	}
	props := map[string]any{"metric": name, "from": from, "to": to, "readings": rows, "all_history": allHistory, "categories": []string(nil)}
	e := &Entity{Class: []string{"series"}, Title: name, Properties: props,
		Links:   []Link{link("all-readings", allHref, "All readings"), link("self", self, name), link("up", "/metrics", "Metrics"), link("index", "/", "Home")},
		Actions: []Action{record}}
	if m != nil { // the metric is a page (D27): the page's text, edges and actions are the series' too, built once
		page, err := h.pageEntity(ctx, m.ID)
		if err != nil {
			return nil, err
		}
		p := page.Properties.(*core.Page)
		props["categories"], props["id"], props["unit"], props["body"], props["links"], props["backlinks"] = m.Categories, m.ID, m.Unit, p.Body, p.Out, p.In
		e.Links = append(e.Links, link("page", pageHref(m.ID), "Page"))
		e.Actions = append(e.Actions, page.Actions...)
	}
	for _, m := range rows {
		e.Entities = append(e.Entities, link("reading", measurementHref(m.ID), fmt.Sprintf("%s %g", m.Day, m.Value)))
	}
	return e, nil
}

func (h *server) measurement(r *http.Request) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	return h.measurementEntity(r.Context(), id)
}

func (h *server) measurementEntity(ctx context.Context, id int64) (*Entity, error) {
	m, err := h.s.Measurement(ctx, id)
	if err != nil {
		return nil, err
	}
	e := &Entity{Class: []string{"measurement"}, Title: fmt.Sprintf("%s on %s", m.Metric, m.Day), Properties: m,
		Links: []Link{link("self", measurementHref(id), "This reading"), link("day", dayHref(m.Day), m.Day),
			link("series", metricHref(m.Metric), m.Metric), link("index", "/", "Home")}}
	if m.Supersedes != 0 {
		e.Links = append(e.Links, link("supersedes", measurementHref(m.Supersedes), "Corrects"))
	}
	if m.SupersededBy != 0 {
		e.Links = append(e.Links, link("superseded-by", measurementHref(m.SupersededBy), "Corrected by"))
	} else {
		ids := idParam(id)
		e.Actions = []Action{action("correct", ids, nil)}
		if !m.Retraction {
			e.Actions = append(e.Actions, action("retract", ids, nil))
		}
	}
	if m.CapturedWith != 0 {
		e.Links = append(e.Links, link("captured-with", pageHref(m.CapturedWith), "Captured with"))
	}
	return e, nil
}

func (h *server) record(r *http.Request, src string) (*Entity, error) {
	v, err := fieldsOf(r, "record")
	if err != nil {
		return nil, err
	}
	value, err := number(v, "value")
	if err != nil {
		return nil, err
	}
	id, err := h.s.Record(r.Context(), src, core.Reading{Metric: v.Get("metric"), Day: v.Get("day"), Value: *value,
		TakenAt: v.Get("taken_at"), TZ: v.Get("tz"), ImportKey: v.Get("import_key")})
	if err != nil {
		return nil, err
	}
	if id == 0 { // a re-send: the first one stands (docs/contract/imports.md)
		return &Entity{Class: []string{"result"}, Title: "Already recorded",
			Properties: map[string]any{"message": "a reading with this import_key was recorded before; nothing changed"},
			Links:      []Link{link("day", dayHref(v.Get("day")), v.Get("day")), link("index", "/", "Home")}}, nil
	}
	return h.measurementEntity(r.Context(), id)
}

func (h *server) correct(r *http.Request, src string) (*Entity, error) {
	id, v, err := idForm(r, "correct")
	if err != nil {
		return nil, err
	}
	value, err := number(v, "value") // never nil: the catalog requires value, and a nil value retracts
	if err != nil {
		return nil, err
	}
	return h.correctTo(r.Context(), src, id, value)
}

func (h *server) retract(r *http.Request, src string) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	return h.correctTo(r.Context(), src, id, nil)
}

// correctTo corrects or retracts a reading.
func (h *server) correctTo(ctx context.Context, src string, id int64, value *float64) (*Entity, error) {
	fix, err := h.s.Correct(ctx, src, id, value)
	if err != nil {
		return nil, err
	}
	return h.measurementEntity(ctx, fix)
}

func (h *server) registerMetric(r *http.Request, src string) (*Entity, error) {
	v, err := fieldsOf(r, "register-metric")
	if err != nil {
		return nil, err
	}
	m := core.NewMetric{Name: v.Get("name"), Unit: v.Get("unit"), Note: v.Get("note"), Category: v.Get("category")}
	added, err := h.s.CreateMetric(r.Context(), src, m)
	if err != nil {
		return nil, err
	}
	e, err := h.seriesEntity(r.Context(), m.Name, nil)
	if err != nil {
		return nil, err
	}
	result := "New metric " + e.Title
	if !added {
		result = e.Title + " exists"
	}
	if m.Category != "" {
		result += ", in " + m.Category
	}
	e.Result = result + "."
	return e, nil
}
