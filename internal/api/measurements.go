package api

// The readings: the metrics, a series, a reading, and the writes of readings.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"lifelog/internal/api/siren"
	"lifelog/internal/api/view"
	"lifelog/internal/core"
	"lifelog/internal/text"
)

// recordAction offers the metrics in use on day (and metric, when the view is that metric's), not the whole
// registry; the record action itself takes any registered metric.
func (h *server) recordAction(ctx context.Context, metric, day string) (siren.Action, error) {
	a := action("record", nil, map[string]any{"day": day})
	if metric != "" {
		a.Field("metric").Value = metric
	}
	names, err := h.s.InUse(ctx, day, view.InUseDays)
	if err != nil {
		return siren.Action{}, err
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

func (h *server) metrics(r *http.Request) (*siren.Entity, error) {
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
	var titles []string // the category of file-pages and of register-metric offers them, as Browse and a page do
	for _, c := range cs {
		titles = append(titles, c.Title)
	}
	today := h.s.Today()
	e := &siren.Entity{Class: []string{"metrics"}, Title: "Metrics",
		Properties: map[string]any{"metrics": rows, "groups": groupMetrics(ms, cs), "categories": cs, "today": today},
		Links:      []siren.Link{link("self", "/metrics", "Metrics"), link("index", "/", "Home")},
		Actions: []siren.Action{withOptions(action("record", nil, map[string]any{"day": today}), "metric", names),
			withOptions(action("file-pages", nil, nil), "category", titles), withOptions(action("register-metric", nil, nil), "category", titles)}}
	for _, m := range ms {
		e.Entities = append(e.Entities, link("item", siren.MetricHref(m.Name), m.Name))
	}
	return e, nil
}

// metricGroup is one section of /metrics: a category page with the metrics filed in it (D26), or the metrics
// filed nowhere. Depth is the category's depth in the drawn tree.
type metricGroup struct {
	Title   string        `json:"title"`
	Path    string        `json:"path,omitempty"`
	Page    int64         `json:"page,omitempty"`
	Depth   int           `json:"depth"`
	Metrics []core.Metric `json:"metrics"`
}

// groupMetrics lists a metric under each category it is filed in, in the order of the tree; the rest last. A category is shown when it or a category under it holds a metric.
func groupMetrics(ms []core.Metric, cs []core.Category) []metricGroup {
	filed := map[string][]core.Metric{}
	var none []core.Metric
	for _, m := range ms {
		switch {
		case len(m.Categories) > 0:
			for _, p := range m.Categories {
				filed[p] = append(filed[p], m)
			}
		default:
			none = append(none, m)
		}
	}
	var out []metricGroup
	for _, c := range cs { // by path: a category comes before the categories under it
		held := false
		for p := range filed {
			if p == c.Path || strings.HasPrefix(p, c.Path+"/") {
				held = true
				break
			}
		}
		if !held {
			continue
		}
		out = append(out, metricGroup{Title: c.Title, Path: c.Path, Page: c.ID, Depth: strings.Count(c.Path, "/"),
			Metrics: append([]core.Metric{}, filed[c.Path]...)})
	}
	if len(none) > 0 {
		out = append(out, metricGroup{Title: "Not filed", Metrics: none})
	}
	return out
}

// series is cookbook/metric-series.md; ?to= defaults to today, and with no ?from= every reading through it is read.
func (h *server) series(r *http.Request) (*siren.Entity, error) {
	return h.seriesEntity(r.Context(), r.PathValue("name"), r.URL.Query())
}

// seriesEntity is the resource /metrics/{name} serves, the one page of a metric (docs/plans/100-one-page-a-metric.md):
// its readings, the writes of a reading and of a habit, and, as the metric is a page (D27), the page's text, edges
// and actions. q is its query (from, to, all_history), nil for the defaults.
func (h *server) seriesEntity(ctx context.Context, name string, q url.Values) (*siren.Entity, error) {
	today := h.s.Today()
	to := q.Get("to")
	if to == "" {
		to = today
	}
	if !text.IsDay(to) {
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
		return siren.MetricHref(name) + "?" + v.Encode()
	}
	self, allHref := seriesHref(allHistory), seriesHref(true)
	if len(q) == 0 { // asked with no query, the series names itself so: a browser that registers a metric lands on it
		self = siren.MetricHref(name)
	}
	record, err := h.recordAction(ctx, name, today)
	if err != nil {
		return nil, err
	}
	props := map[string]any{"metric": name, "from": from, "to": to, "readings": rows, "all_history": allHistory, "categories": []string(nil)}
	e := &siren.Entity{Class: []string{"series"}, Title: name, Properties: props,
		Links:   []siren.Link{link("all-readings", allHref, "All readings"), link("self", self, name), link("up", "/metrics", "Metrics"), link("index", "/", "Home")},
		Actions: []siren.Action{record}}
	if m != nil { // the metric is a page (D27): the page's text, edges and actions are the series' too, built once
		page, err := h.pageEntity(ctx, m.ID)
		if err != nil {
			return nil, err
		}
		p := page.Properties.(*core.Page)
		props["categories"], props["id"], props["unit"], props["body"], props["links"], props["backlinks"] = m.Categories, m.ID, m.Unit, p.Body, p.Out, p.In
		e.Links = append(e.Links, link("page", siren.PageHref(m.ID), "Page"))
		e.Actions = append(e.Actions, page.Actions...)
	}
	for _, m := range rows {
		e.Entities = append(e.Entities, link("reading", siren.MeasurementHref(m.ID), m.Day+" "+view.Num(m.Value)))
	}
	return e, nil
}

// metricOf is the live metric that name names, by its name or by an alias its renames kept (D27, as a series is
// read); nil when it names none.
func (h *server) metricOf(ctx context.Context, name string) (*core.Metric, error) {
	id, err := h.s.PageID(ctx, name)
	if err != nil || id == 0 {
		return nil, err
	}
	ms, err := h.s.Metrics(ctx)
	if err != nil {
		return nil, err
	}
	for i := range ms {
		if ms[i].ID == id {
			return &ms[i], nil
		}
	}
	return nil, nil
}

func (h *server) measurement(r *http.Request) (*siren.Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	return h.measurementEntity(r.Context(), id)
}

func (h *server) measurementEntity(ctx context.Context, id int64) (*siren.Entity, error) {
	m, err := h.s.Measurement(ctx, id)
	if err != nil {
		return nil, err
	}
	e := &siren.Entity{Class: []string{"measurement"}, Title: fmt.Sprintf("%s on %s", m.Metric, m.Day), Properties: m,
		Links: []siren.Link{link("self", siren.MeasurementHref(id), "This reading"), link("day", siren.DayHref(m.Day), m.Day),
			link("series", siren.MetricHref(m.Metric), m.Metric), link("index", "/", "Home")}}
	if m.Supersedes != 0 {
		e.Links = append(e.Links, link("supersedes", siren.MeasurementHref(m.Supersedes), "Corrects"))
	}
	if m.SupersededBy != 0 {
		e.Links = append(e.Links, link("superseded-by", siren.MeasurementHref(m.SupersededBy), "Corrected by"))
	} else {
		ids := idParam(id)
		e.Actions = []siren.Action{action("correct", ids, nil)}
		if !m.Retraction {
			e.Actions = append(e.Actions, action("retract", ids, nil))
		}
	}
	if m.CapturedWith != 0 {
		e.Links = append(e.Links, link("captured-with", siren.PageHref(m.CapturedWith), "Captured with"))
	}
	return e, nil
}

func (h *server) record(r *http.Request, act spec, src string) (*siren.Entity, error) {
	v, err := fieldsOf(r, act)
	if err != nil {
		return nil, err
	}
	value, err := number(v, "value")
	if err != nil {
		return nil, err
	}
	day := h.dayNamed(v.Get("day"))
	id, err := h.s.Record(r.Context(), src, core.Reading{Metric: v.Get("metric"), Day: day, Value: *value,
		TakenAt: v.Get("taken_at"), TZ: v.Get("tz"), ImportKey: v.Get("import_key")})
	if err != nil {
		return nil, err
	}
	if id == 0 { // a re-send: the first one stands (docs/contract/imports.md), and the answer links its day
		if day, err = h.s.RecordedDay(r.Context(), src, v.Get("import_key"), v.Get("metric")); err != nil {
			return nil, err
		}
		return &siren.Entity{Class: []string{"result"}, Title: "Already recorded",
			Properties: map[string]any{"message": "a reading with this import_key was recorded before; nothing changed"},
			Links:      []siren.Link{link("day", siren.DayHref(day), day), link("index", "/", "Home")}}, nil
	}
	return h.measurementEntity(r.Context(), id)
}

func (h *server) correct(r *http.Request, act spec, src string) (*siren.Entity, error) {
	id, v, err := idForm(r, act)
	if err != nil {
		return nil, err
	}
	value, err := number(v, "value") // never nil: the catalog requires value, and a nil value retracts
	if err != nil {
		return nil, err
	}
	return h.correctTo(r.Context(), src, id, value)
}

func (h *server) retract(r *http.Request, _ spec, src string) (*siren.Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	return h.correctTo(r.Context(), src, id, nil)
}

// correctTo corrects or retracts a reading.
func (h *server) correctTo(ctx context.Context, src string, id int64, value *float64) (*siren.Entity, error) {
	fix, err := h.s.Correct(ctx, src, id, value)
	if err != nil {
		return nil, err
	}
	return h.measurementEntity(ctx, fix)
}

func (h *server) registerMetric(r *http.Request, act spec, src string) (*siren.Entity, error) {
	v, err := fieldsOf(r, act)
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
