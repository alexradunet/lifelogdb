package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"lifelog/internal/core"
	"lifelog/internal/importer"
)

// SourceHeader names the writer of a request's rows (lifelog_meta.source). Without it a browser form writes as
// "ui" and any other client as "api".
const SourceHeader = "Lifelog-Source"

type server struct {
	s  *core.Store
	ws *importer.Workspace // nil unless started with --workspace
}

// New returns the whole API as one handler: lifelog serve mounts it on a socket, the CLI and the MCP server
// call it in-process. With an import workspace the /import routes are mounted too.
func New(s *core.Store, ws *importer.Workspace) http.Handler {
	h := &server{s, ws}
	m := http.NewServeMux()
	get := func(p string, f func(*http.Request) (*Entity, error)) { m.HandleFunc("GET "+p, h.serve(f)) }
	post := func(p string, f func(*http.Request, string) (*Entity, error)) {
		m.HandleFunc("POST "+p, h.serve(func(r *http.Request) (*Entity, error) {
			src, err := source(r)
			if err != nil {
				return nil, err
			}
			return f(r, src)
		}))
	}
	get("/{$}", h.root)
	get("/actions", h.actions)
	get("/days", h.days)
	get("/days/today", h.today)
	get("/days/{day}", h.day)
	get("/pages", h.find)
	get("/pages/{id}", h.page)
	get("/people", h.named("person", "people", "create-person"))
	get("/places", h.named("place", "places", "create-place"))
	get("/files", h.named("file", "files", "add-file"))
	m.HandleFunc("GET /pages/{id}/preview", picture(func(r *http.Request) ([]byte, error) {
		id, err := idOf(r)
		if err != nil {
			return nil, err
		}
		return h.s.Preview(r.Context(), id)
	}))
	m.HandleFunc("GET /previews", picture(func(r *http.Request) ([]byte, error) {
		return h.s.PreviewByTitle(r.Context(), r.URL.Query().Get("title"))
	}))
	get("/metrics", h.metrics)
	get("/metrics/{name}", h.series)
	get("/measurements/{id}", h.measurement)
	get("/ghosts", h.ghosts)
	get("/search", h.search)
	post("/days/{day}/capture", h.capture)
	post("/pages", h.createPage)
	post("/pages/{id}/body", h.saveBody)
	post("/people", h.createPerson)
	post("/places", h.createPlace)
	post("/files", h.addFile)
	post("/pages/{id}/promote", h.promote)
	post("/pages/{id}/tombstone", h.tombstone)
	post("/pages/{id}/revive", h.revive)
	post("/pages/{id}/links", h.link)
	post("/pages/{id}/unlink", h.unlink)
	post("/measurements", h.record)
	post("/measurements/{id}/correct", h.correct)
	post("/measurements/{id}/retract", h.retract)
	post("/query", h.query)
	post("/pages/{id}/rename", h.rename)
	post("/pages/{id}/locate", h.locate)
	post("/metrics", ownerOnly(h.registerMetric))
	post("/metrics/{name}/periods", h.startHabit)
	post("/metrics/{name}/stop", h.stopHabit)
	post("/metrics/{name}/check-in", h.checkIn)
	get("/habits", h.habits)
	get("/integrity", h.integrity)
	if ws != nil {
		h.mountImport(get, post)
	}
	guard := http.NewCrossOriginProtection()
	guard.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e, status := errorEntity(&core.Error{Status: http.StatusForbidden, Msg: "cross-origin browser write refused"})
		write(w, r, status, e)
	}))
	return guard.Handler(m)
}

// redirect is returned by a handler that answers with another resource's address.
type redirect struct{ to string }

func (r redirect) Error() string { return "see " + r.to }

// ownerOnly refuses an action that is the owner's alone to an agent (a model never registers a metric or approves).
func ownerOnly(f func(*http.Request, string) (*Entity, error)) func(*http.Request, string) (*Entity, error) {
	return func(r *http.Request, src string) (*Entity, error) {
		if strings.HasPrefix(src, "agent:") {
			return nil, &core.Error{Status: http.StatusForbidden, Msg: "this action is the owner's: an agent may not run it"}
		}
		return f(r, src)
	}
}

func (h *server) serve(f func(*http.Request) (*Entity, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		e, err := f(r)
		status := http.StatusOK
		var rd redirect
		switch {
		case errors.As(err, &rd):
			http.Redirect(w, r, rd.to, http.StatusSeeOther)
			return
		case err != nil:
			e, status = errorEntity(err)
		case r.Method == "POST":
			if self := selfOf(e); self != "" {
				if wantsHTML(r) { // a browser form: post, then redirect, so a reload never posts again
					http.Redirect(w, r, self, http.StatusSeeOther)
					return
				}
				w.Header().Set("Location", self)
			}
		}
		write(w, r, status, e)
	}
}

func selfOf(e *Entity) string {
	for _, l := range e.Links {
		for _, rel := range l.Rel {
			if rel == "self" {
				return l.Href
			}
		}
	}
	return ""
}

func errorEntity(err error) (*Entity, int) {
	status := http.StatusInternalServerError
	e := &Entity{Class: []string{"error"}, Title: "Error", Links: []Link{{Rel: []string{"index"}, Href: "/", Title: "Home"}}}
	var ce *core.Error
	var ex *core.ExistsError
	switch {
	case errors.As(err, &ex):
		status = http.StatusConflict
		e.Links = append(e.Links, Link{Rel: []string{"existing"}, Href: pageHref(ex.ID), Title: ex.Title})
	case errors.As(err, &ce):
		status = ce.Status
	case errors.Is(err, context.DeadlineExceeded):
		status = http.StatusGatewayTimeout
	}
	e.Properties = map[string]any{"status": status, "message": err.Error()}
	return e, status
}

func write(w http.ResponseWriter, r *http.Request, status int, e *Entity) {
	if wantsHTML(r) {
		renderHTML(w, r, status, e)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.siren+json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(e)
}

func wantsHTML(r *http.Request) bool {
	a := r.Header.Get("Accept")
	return strings.Contains(a, "text/html") && !strings.Contains(a, "json")
}

func source(r *http.Request) (string, error) {
	s := r.Header.Get(SourceHeader)
	switch {
	case s != "":
	case wantsHTML(r):
		s = "ui"
	default:
		s = "api"
	}
	return s, core.CheckSource(s)
}

// form reads an action's fields: urlencoded (what a browser and the CLI send) or a JSON object.
func form(r *http.Request) (url.Values, error) {
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct == "application/json" {
		var m map[string]any
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			return nil, &core.Error{Status: 400, Msg: "body: " + err.Error()}
		}
		v := url.Values{}
		for k, x := range m {
			switch x := x.(type) {
			case string:
				v.Set(k, x)
			case nil:
			default:
				b, _ := json.Marshal(x)
				v.Set(k, string(b))
			}
		}
		return v, nil
	}
	if err := r.ParseForm(); err != nil {
		return nil, &core.Error{Status: 400, Msg: err.Error()}
	}
	return r.Form, nil
}

func required(v url.Values, names ...string) error {
	for _, n := range names {
		if v.Get(n) == "" {
			return &core.Error{Status: 422, Msg: "missing field " + n}
		}
	}
	return nil
}

func idOf(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, &core.Error{Status: 404, Msg: "no such id " + r.PathValue("id")}
	}
	return id, nil
}

func number(v url.Values, name string) (*float64, error) {
	s := v.Get(name)
	if s == "" {
		return nil, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, &core.Error{Status: 422, Msg: fmt.Sprintf("%s %q is not a number", name, s)}
	}
	return &f, nil
}

func pageHref(id int64) string        { return "/pages/" + strconv.FormatInt(id, 10) }
func dayHref(day string) string       { return "/days/" + day }
func measurementHref(id int64) string { return "/measurements/" + strconv.FormatInt(id, 10) }
func link(rel, href, title string) Link {
	return Link{Rel: []string{rel}, Href: href, Title: title}
}

// ---- reads

func (h *server) root(r *http.Request) (*Entity, error) {
	today := core.Today()
	return &Entity{
		Class: []string{"root"}, Title: "Lifelog",
		Properties: map[string]any{"today": today},
		Links: []Link{link("self", "/", "Lifelog"), link("today", dayHref(today), "Today"), link("days", "/days", "Days"),
			link("people", "/people", "People"), link("places", "/places", "Places"), link("files", "/files", "Files"), link("metrics", "/metrics", "Metrics"),
			link("habits", "/habits", "Habits"), link("ghosts", "/ghosts", "Ghost pages"), link("actions", "/actions", "All actions")},
		Actions: []Action{
			action("capture", map[string]string{"day": today}, nil), action("search", nil, nil), action("find", nil, nil),
			action("create-page", nil, nil), action("create-person", nil, nil), action("create-place", nil, nil), action("add-file", nil, nil),
			h.recordAction(r.Context(), "", today), action("register-metric", nil, nil), action("query", nil, nil)},
	}, nil
}

func (h *server) actions(*http.Request) (*Entity, error) {
	e := &Entity{Class: []string{"actions"}, Title: "Actions", Links: []Link{link("self", "/actions", "Actions"), link("index", "/", "Home")}}
	specs := catalog
	if h.ws != nil {
		specs = append(append([]spec{}, catalog...), importCatalog...)
		e.Links = append(e.Links, link("import", "/import", "Import status"))
	}
	for _, s := range specs {
		e.Actions = append(e.Actions, templated(s))
	}
	return e, nil
}

func (h *server) today(*http.Request) (*Entity, error) { return nil, redirect{dayHref(core.Today())} }

func (h *server) days(r *http.Request) (*Entity, error) {
	days, err := h.s.RecentDays(r.Context(), 60)
	if err != nil {
		return nil, err
	}
	e := &Entity{Class: []string{"days"}, Title: "Days", Links: []Link{link("self", "/days", "Days"), link("index", "/", "Home")}}
	for _, d := range days {
		e.Entities = append(e.Entities, link("item", dayHref(d.Title), d.Title))
	}
	return e, nil
}

func (h *server) day(r *http.Request) (*Entity, error) {
	day := r.PathValue("day")
	d, err := h.s.Day(r.Context(), day)
	if err != nil {
		return nil, err
	}
	t, _ := time.Parse(time.DateOnly, day)
	e := &Entity{
		Class: []string{"day"}, Title: day, Properties: d,
		Links: []Link{link("self", dayHref(day), day), link("prev", dayHref(t.AddDate(0, 0, -1).Format(time.DateOnly)), "Previous day"),
			link("next", dayHref(t.AddDate(0, 0, 1).Format(time.DateOnly)), "Next day"), link("metrics", "/metrics", "Metrics"), link("index", "/", "Home")},
		Actions: []Action{action("capture", map[string]string{"day": day}, nil), h.recordAction(r.Context(), "", day)},
	}
	if d.PageID != 0 {
		e.Links = append(e.Links, link("page", pageHref(d.PageID), "The day page"))
	}
	for _, m := range d.Readings {
		e.Entities = append(e.Entities, link("reading", measurementHref(m.ID), fmt.Sprintf("%s %g", m.Metric, m.Value)))
	}
	return e, nil
}

func (h *server) find(r *http.Request) (*Entity, error) {
	title := r.URL.Query().Get("title")
	if title == "" {
		return nil, &core.Error{Status: 422, Msg: "missing field title"}
	}
	id, err := h.s.PageID(r.Context(), title)
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, &core.Error{Status: 404, Msg: "no page titled " + title}
	}
	return nil, redirect{pageHref(id)}
}

func (h *server) page(r *http.Request) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), id)
}

func (h *server) pageEntity(ctx context.Context, id int64) (*Entity, error) {
	p, err := h.s.PageByID(ctx, id)
	if err != nil {
		return nil, err
	}
	ids := map[string]string{"id": strconv.FormatInt(id, 10)}
	e := &Entity{Class: []string{p.Type}, Title: p.Title, Properties: p,
		Links: []Link{link("self", pageHref(id), p.Title), link("index", "/", "Home")}}
	if p.IsDayPage {
		e.Class = append(e.Class, "day-page")
		e.Links = append(e.Links, link("day", dayHref(p.Day), "Day view"))
	}
	if p.File != nil && p.File.Preview {
		e.Links = append(e.Links, link("preview", pageHref(id)+"/preview", "Picture"))
	}
	if p.Point != nil {
		e.Links = append(e.Links, link("map", core.MapLink(p.Point.Lat, p.Point.Lon), "Map"))
	}
	for _, l := range p.Out {
		e.Entities = append(e.Entities, Link{Rel: []string{l.Kind}, Href: pageHref(l.ID), Title: l.Title, Class: []string{l.Type}})
	}
	for _, l := range p.In {
		e.Entities = append(e.Entities, Link{Rel: []string{"backlink", l.Kind}, Href: pageHref(l.ID), Title: l.Title, Class: []string{l.Type}})
	}
	if p.DeletedAt != "" {
		e.Class = append(e.Class, "deleted")
		e.Actions = []Action{action("revive", ids, nil)}
		return e, nil
	}
	if p.IsStub {
		e.Class = append(e.Class, "redirect-stub")
	} else {
		e.Actions = append(e.Actions, action("save-body", ids, map[string]any{"body": p.Body, "version": p.Version}))
		if p.Type == "page" && !p.IsDayPage {
			e.Actions = append(e.Actions, action("promote", ids, nil), action("rename", ids, nil))
		}
		if p.Type == "place" {
			values := map[string]any{"radius_m": core.DefaultRadius, "link_days": "1"}
			if pt := p.Point; pt != nil {
				values = map[string]any{"lat": pt.Lat, "lon": pt.Lon, "radius_m": pt.RadiusM, "link_days": map[bool]string{true: "1", false: "0"}[pt.LinkDays]}
			}
			e.Actions = append(e.Actions, action("locate", ids, values))
		}
	}
	kinds, err := h.linkKinds(ctx, p.Type, p.IsDayPage)
	if err != nil {
		return nil, err
	}
	if len(kinds) > 0 {
		e.Actions = append(e.Actions, withOptions(action("link", ids, nil), "kind", kinds))
		e.Actions = append(e.Actions, withOptions(action("unlink", ids, nil), "kind", kinds))
	}
	e.Actions = append(e.Actions, action("tombstone", ids, nil))
	return e, nil
}

// linkKinds are the kinds a page of this type may start (link_kinds.from_types), minus the two the save
// contract and renames own; `at` only from a day page (D16).
func (h *server) linkKinds(ctx context.Context, typ string, dayPage bool) ([]string, error) {
	res, err := h.s.Query(ctx, `SELECT kind FROM link_kinds WHERE kind NOT IN ('wikilink', 'redirect')
	                             AND (from_types IS NULL OR instr(','||from_types||',', ','||'`+typ+`'||',') > 0) ORDER BY kind`, 100)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, row := range res.Rows {
		k := row[0].(string)
		if k == "at" && !dayPage {
			continue
		}
		out = append(out, k)
	}
	return out, nil
}

func withOptions(a Action, field string, opts []string) Action {
	for i := range a.Fields {
		if a.Fields[i].Name == field {
			a.Fields[i].Options = opts
		}
	}
	return a
}

// inUseDays is how far back a view looks for the metrics it offers to record: a series read at least monthly
// stays offered, a lab marker read twice a year is recorded from its own page (/metrics/{name}).
const inUseDays = 60

// recordAction offers the metrics in use on day (and metric, when the view is that metric's), not the whole
// registry; the record action itself takes any registered metric.
func (h *server) recordAction(ctx context.Context, metric, day string) Action {
	a := action("record", nil, map[string]any{"day": day})
	if metric != "" {
		a.Fields[0].Value = metric
	}
	if names, err := h.s.InUse(ctx, day, inUseDays); err == nil {
		if metric != "" && !slices.Contains(names, metric) {
			names = append(names, metric)
			slices.Sort(names)
		}
		a = withOptions(a, "metric", names)
	}
	return a
}

func (h *server) named(typ, plural, create string) func(*http.Request) (*Entity, error) {
	return func(r *http.Request) (*Entity, error) {
		list, err := h.s.Named(r.Context(), typ)
		if err != nil {
			return nil, err
		}
		e := &Entity{Class: []string{plural}, Title: strings.ToUpper(plural[:1]) + plural[1:],
			Links:   []Link{link("self", "/"+plural, plural), link("index", "/", "Home")},
			Actions: []Action{action(create, nil, nil)}}
		for _, p := range list {
			e.Entities = append(e.Entities, Link{Rel: []string{"item"}, Href: pageHref(p.ID), Title: p.Title, Class: []string{typ}})
		}
		return e, nil
	}
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
		Actions: []Action{withOptions(action("record", nil, map[string]any{"day": core.Today()}), "metric", names)}}
	for _, m := range ms {
		e.Entities = append(e.Entities, link("item", "/metrics/"+url.PathEscape(m.Name), m.Name))
	}
	return e, nil
}

// series is cookbook/metric-series.md; ?from= and ?to= default to the 90 days up to today.
func (h *server) series(r *http.Request) (*Entity, error) {
	name := r.PathValue("name")
	q := r.URL.Query()
	to := q.Get("to")
	if to == "" {
		to = core.Today()
	}
	from := q.Get("from")
	if from == "" {
		t, _ := time.Parse(time.DateOnly, to)
		from = t.AddDate(0, 0, -90).Format(time.DateOnly)
	}
	rows, err := h.s.Series(r.Context(), name, from, to)
	if err != nil {
		return nil, err
	}
	periods, err := h.s.Periods(r.Context(), name)
	if err != nil {
		return nil, err
	}
	self := "/metrics/" + url.PathEscape(name) + "?from=" + from + "&to=" + to
	m := h.metricOf(r.Context(), name)
	var categories []string
	if m != nil {
		categories = m.Categories
	}
	e := &Entity{Class: []string{"series"}, Title: name,
		Properties: map[string]any{"metric": name, "from": from, "to": to, "readings": rows, "habit_periods": periods, "categories": categories},
		Links:      []Link{link("self", self, name), link("up", "/metrics", "Metrics"), link("habits", "/habits", "Habits"), link("index", "/", "Home")},
		Actions:    []Action{h.recordAction(r.Context(), name, core.Today())}}
	ids := map[string]string{"name": url.PathEscape(name)}
	if len(periods) > 0 && periods[len(periods)-1].End == "" {
		e.Actions = append(e.Actions, action("stop-habit", ids, map[string]any{"day": core.Today()}),
			action("check-in", ids, map[string]any{"day": core.Today()}))
	} else if h.unitless(r.Context(), name) {
		e.Actions = append(e.Actions, action("start-habit", ids, map[string]any{"start_day": core.Today()}))
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
			link("series", "/metrics/"+url.PathEscape(m.Metric), m.Metric), link("index", "/", "Home")}}
	if m.Supersedes != 0 {
		e.Links = append(e.Links, link("supersedes", measurementHref(m.Supersedes), "Corrects"))
	}
	if m.SupersededBy != 0 {
		e.Links = append(e.Links, link("superseded-by", measurementHref(m.SupersededBy), "Corrected by"))
	} else {
		ids := map[string]string{"id": strconv.FormatInt(id, 10)}
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

func (h *server) ghosts(r *http.Request) (*Entity, error) {
	gs, err := h.s.Ghosts(r.Context())
	if err != nil {
		return nil, err
	}
	e := &Entity{Class: []string{"ghosts"}, Title: "Ghost pages", Links: []Link{link("self", "/ghosts", "Ghost pages"), link("index", "/", "Home")}}
	for _, g := range gs {
		e.Entities = append(e.Entities, link("item", pageHref(g.ID), g.Title))
	}
	return e, nil
}

func (h *server) search(r *http.Request) (*Entity, error) {
	q := r.URL.Query().Get("q")
	e := &Entity{Class: []string{"search"}, Title: "Search",
		Links:   []Link{link("self", "/search?q="+url.QueryEscape(q), "Search"), link("index", "/", "Home")},
		Actions: []Action{action("search", nil, map[string]any{"q": q})}}
	if q == "" {
		return e, nil
	}
	hits, err := h.s.Search(r.Context(), q, 50)
	if err != nil {
		return nil, err
	}
	e.Properties = map[string]any{"q": q, "hits": hits}
	for _, x := range hits {
		e.Entities = append(e.Entities, link("item", pageHref(x.ID), x.Title))
	}
	return e, nil
}

// ---- writes: each answers with the resource it changed, and what it did in Result

func (h *server) capture(r *http.Request, src string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	mood, err := number(v, "mood")
	if err != nil {
		return nil, err
	}
	day := r.PathValue("day")
	if _, sync, err := h.s.Capture(r.Context(), src, day, v.Get("text"), mood); err != nil {
		return nil, err
	} else {
		e, err := h.day(r)
		if e != nil {
			e.Result = sync
		}
		return e, err
	}
}

func (h *server) createPage(r *http.Request, src string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "title"); err != nil {
		return nil, err
	}
	id, sync, err := h.s.CreatePage(r.Context(), src, v.Get("title"), v.Get("body"))
	if err != nil {
		return nil, err
	}
	return withResult(h.pageEntity(r.Context(), id))(sync)
}

func (h *server) saveBody(r *http.Request, src string) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "version"); err != nil {
		return nil, err
	}
	sync, err := h.s.SaveBody(r.Context(), src, id, v.Get("body"), v.Get("version"))
	if err != nil {
		return nil, err
	}
	return withResult(h.pageEntity(r.Context(), id))(sync)
}

func (h *server) createPerson(r *http.Request, src string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "title"); err != nil {
		return nil, err
	}
	id, err := h.s.CreatePerson(r.Context(), src, v.Get("title"), v.Get("name"), v.Get("birth_day"), v.Get("death_day"))
	if err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), id)
}

func (h *server) createPlace(r *http.Request, src string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "title"); err != nil {
		return nil, err
	}
	id, err := h.s.CreatePlace(r.Context(), src, v.Get("title"))
	if err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), id)
}

func (h *server) promote(r *http.Request, src string) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := h.s.Promote(r.Context(), src, id, v.Get("to"), v.Get("name")); err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), id)
}

func (h *server) tombstone(r *http.Request, src string) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	if err := h.s.Tombstone(r.Context(), src, id); err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), id)
}

func (h *server) revive(r *http.Request, src string) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	if err := h.s.Revive(r.Context(), src, id); err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), id)
}

// linkEnds resolves the page of the path and the page titled `to`.
func (h *server) linkEnds(r *http.Request) (from, to int64, v url.Values, err error) {
	if from, err = idOf(r); err != nil {
		return
	}
	if v, err = form(r); err != nil {
		return
	}
	if err = required(v, "to", "kind"); err != nil {
		return
	}
	if to, err = h.s.PageID(r.Context(), v.Get("to")); err == nil && to == 0 {
		err = &core.Error{Status: 404, Msg: "no page titled " + v.Get("to") + ": create it first"}
	}
	return
}

func (h *server) link(r *http.Request, src string) (*Entity, error) {
	from, to, v, err := h.linkEnds(r)
	if err != nil {
		return nil, err
	}
	if err := h.s.Link(r.Context(), src, from, to, v.Get("kind"), v.Get("note")); err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), from)
}

func (h *server) unlink(r *http.Request, src string) (*Entity, error) {
	from, to, v, err := h.linkEnds(r)
	if err != nil {
		return nil, err
	}
	if err := h.s.Unlink(r.Context(), src, from, to, v.Get("kind")); err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), from)
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
	id, err := h.s.Record(r.Context(), src, core.Reading{Metric: v.Get("metric"), Day: v.Get("day"), Value: *value,
		TakenAt: v.Get("taken_at"), TZ: v.Get("tz"), Key: v.Get("import_key")})
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
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	value, err := number(v, "value")
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, &core.Error{Status: 422, Msg: "missing field value (to remove a reading, retract it)"}
	}
	return h.correctTo(r, src, id, value)
}

func (h *server) retract(r *http.Request, src string) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	return h.correctTo(r, src, id, nil)
}

func (h *server) query(r *http.Request, _ string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "sql"); err != nil {
		return nil, err
	}
	res, err := h.s.Query(r.Context(), v.Get("sql"), 500)
	if err != nil {
		return nil, err
	}
	return &Entity{Class: []string{"result"}, Title: "Query", Properties: res,
		Links:   []Link{link("index", "/", "Home")},
		Actions: []Action{action("query", nil, map[string]any{"sql": v.Get("sql")})}}, nil
}

func withResult(e *Entity, err error) func(any) (*Entity, error) {
	return func(res any) (*Entity, error) {
		if e != nil {
			e.Result = res
		}
		return e, err
	}
}
