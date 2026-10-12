package api

// The readings: the metrics, a series, a reading, and the writes of readings.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

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

func (h *server) metrics(r *http.Request) (*Entity, error) {
	ms, err := h.s.Metrics(r.Context())
	if err != nil {
		return nil, err
	}
	var names []string
	for _, m := range ms {
		names = append(names, m.Name)
	}
	cs, err := h.s.Categories(r.Context())
	if err != nil {
		return nil, err
	}
	e := &Entity{Class: []string{"metrics"}, Title: "Metrics", Properties: map[string]any{"metrics": ms, "groups": groupMetrics(ms, cs)},
		Links:   []Link{link("self", "/metrics", "Metrics"), link("index", "/", "Home")},
		Actions: []Action{withOptions(action("record", nil, map[string]any{"day": h.localDay()}), "metric", names)}}
	for _, m := range ms {
		e.Entities = append(e.Entities, link("item", metricHref(m.Name), m.Name))
	}
	return e, nil
}

// series is cookbook/metric-series.md; ?from= and ?to= default to the 90 days up to today.
func (h *server) series(r *http.Request) (*Entity, error) {
	return h.seriesEntity(r.Context(), r.PathValue("name"), r.URL.Query())
}

// seriesEntity is the resource /metrics/{name} serves; q is its query (from, to, scope, session_id, include_deleted,
// all_history), nil for the defaults.
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
	if !allHistory && from == "" {
		t, _ := dayOf(to) // canonical day was checked above
		lower := t.AddDate(0, 0, -90)
		if lower.Year() < 0 {
			allHistory = true // the default window reaches the minimum admitted calendar day
		} else {
			from = lower.Format(time.DateOnly)
		}
	}
	sessionID, err := optionalID(q, "session_id")
	if err != nil {
		return nil, err
	}
	includeDeleted, err := flag(q, "include_deleted")
	if err != nil {
		return nil, err
	}
	scope := q.Get("scope")
	if scope == "" {
		scope = "unassociated"
	}
	var rows []core.Reading
	if allHistory {
		rows, err = h.s.SeriesHistory(ctx, name, to, scope, sessionID, includeDeleted)
	} else {
		rows, err = h.s.SeriesScope(ctx, name, from, to, scope, sessionID, includeDeleted)
	}
	if err != nil {
		return nil, err
	}
	periods, err := h.s.HabitPeriods(ctx, name)
	if err != nil {
		return nil, err
	}
	selfQuery := url.Values{"to": {to}, "scope": {scope}, "session_id": {strconv.FormatInt(sessionID, 10)}, "include_deleted": {boolField(includeDeleted)}}
	if allHistory {
		selfQuery.Set("all_history", "1")
	} else {
		selfQuery.Set("from", from)
	}
	self := metricHref(name) + "?" + selfQuery.Encode()
	allURL, err := url.Parse(self)
	if err != nil {
		return nil, err
	}
	allQuery := allURL.Query()
	allQuery.Del("from")
	allQuery.Set("all_history", "1")
	allURL.RawQuery = allQuery.Encode()
	m, err := h.metricOf(ctx, name)
	if err != nil {
		return nil, err
	}
	record, err := h.recordAction(ctx, name, today)
	if err != nil {
		return nil, err
	}
	var categories []string
	if m != nil {
		categories = m.Categories
	}
	e := &Entity{Class: []string{"series"}, Title: name,
		Properties: map[string]any{"metric": name, "from": from, "to": to, "readings": rows, "habit_periods": periods, "categories": categories, "scope": scope, "session_id": sessionID, "include_deleted": includeDeleted, "all_history": allHistory},
		Links:      []Link{link("all-readings", allURL.String(), "All readings"), link("self", self, name), link("up", "/metrics", "Metrics"), link("habits", "/habits", "Habits"), link("index", "/", "Home")},
		Actions:    []Action{record}}
	if f := e.Actions[0].field("session_id"); scope == "session" && f != nil {
		f.Value = sessionID
	}
	ids := map[string]string{"name": name}
	if len(periods) > 0 && periods[len(periods)-1].End == "" {
		e.Actions = append(e.Actions, action("stop-habit", ids, map[string]any{"day": today}),
			action("check-in", ids, map[string]any{"day": today}))
	} else if m != nil && m.Unit == "" { // a unitless metric may become a habit
		e.Actions = append(e.Actions, action("start-habit", ids, map[string]any{"start_day": today}))
	}
	if m != nil { // the metric is a page (D27): its text, its backlinks, and filing it in a category
		e.Links = append(e.Links, link("page", pageHref(m.ID), "Page"))
		e.Actions = append(e.Actions, fileAction(m))
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
		e.Actions = []Action{}
		if m.SessionDeletedAt == "" {
			e.Actions = append(e.Actions, action("correct", ids, nil))
		}
		if !m.Retraction {
			e.Actions = append(e.Actions, action("retract", ids, nil))
		}
	}
	if m.SessionID != 0 {
		e.Links = append(e.Links, link("session", sessionHref(m.SessionID), "Session scope"))
	}
	allowed, err := h.s.RelocationAllowed(ctx, id)
	if err != nil {
		return nil, err
	}
	if allowed {
		e.Actions = append(e.Actions, action("relocate-reading", idParam(id), map[string]any{"metric": m.Metric, "day": m.Day, "value": m.Value, "taken_at": m.TakenAt, "tz": m.TZ, "captured_with_id": m.CapturedWith}))
	}
	if m.CapturedWith != 0 {
		e.Links = append(e.Links, link("captured-with", pageHref(m.CapturedWith), "Captured with"))
	}
	return e, nil
}

// readingsFromTable is readings-from-table: the readings a page's table gives, under the fixed source of
// core.TableSource, so a run from any surface finds what an earlier one wrote. The answer returns to the page.
func (h *server) readingsFromTable(r *http.Request, _ string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "page", "metric"); err != nil {
		return nil, err
	}
	res, err := h.s.ReadingsFromTable(r.Context(), v.Get("page"), v.Get("metric"), v.Get("column"))
	if err != nil {
		return nil, err
	}
	self := pageHref(res.PageID)
	return &Entity{Class: []string{"readings-from-table"}, Title: "Readings from " + res.Page, Properties: res, Result: res,
		Links: []Link{link("self", self, res.Page), link("metric", metricHref(res.Metric), res.Metric), link("index", "/", "Home")}}, nil
}

func (h *server) record(r *http.Request, src string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "metric", "day", "value"); err != nil {
		return nil, err
	}
	value, err := number(v, "value")
	if err != nil {
		return nil, err
	}
	sessionID, err := optionalID(v, "session_id")
	if err != nil {
		return nil, err
	}
	id, err := h.s.Record(r.Context(), src, core.Reading{SessionID: sessionID, Metric: v.Get("metric"), Day: v.Get("day"), Value: *value,
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
	id, v, err := idForm(r)
	if err != nil {
		return nil, err
	}
	value, err := number(v, "value")
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, core.Invalid("missing field value (to remove a reading, retract it)")
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
