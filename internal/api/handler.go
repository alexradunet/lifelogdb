package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"lifelog/internal/core"
)

// SourceHeader names the writer of a request's rows (lifelog_meta.source). Without it a browser form writes as
// "ui" and any other client as "api".
const SourceHeader = "Lifelog-Source"

type server struct {
	s        *core.Store
	feedback feedbackStore
	localDay func() string  // the server's local day: what is due, overdue and today is read against it
	zone     *time.Location // the zone of that day: a browser's words for a time use it
	mux      *http.ServeMux // the routes: a refused browser form runs a GET of its page through it
}

// New returns the whole API as one handler: lifelog serve mounts it on a socket, the CLI and the MCP server
// call it in-process. trustedOrigins are the browser origins a public listener allows to write
// (docs/plans/078-network-server.md); a local listener and the in-process client pass none.
func New(s *core.Store, trustedOrigins ...string) http.Handler {
	return newHandler(s, core.Today, time.Local, trustedOrigins...)
}

// newHandler is New with the clock of the local day and its zone given: a test fixes them, so what is due and
// overdue is read against a fixed day, and a time is written in fixed words.
func newHandler(s *core.Store, localDay func() string, zone *time.Location, trustedOrigins ...string) http.Handler {
	h := &server{s: s, localDay: localDay, zone: zone}
	m := http.NewServeMux()
	h.mux = m
	get := func(p string, f func(*http.Request) (*Entity, error)) { m.HandleFunc("GET "+p, h.serve(f)) }
	post := func(p string, f func(*http.Request, string) (*Entity, error)) {
		if ownerPath("POST", p) { // the catalog says whose the action is; the route refuses an agent
			f = ownerOnly(f)
		}
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
	get("/days/go", h.openDay)
	get("/days/{day}", h.day)
	get("/months/current", h.currentMonth)
	get("/months/{month}", h.month)
	get("/weeks/current", h.currentWeek)
	get("/weeks/{week}", h.week)
	get("/years/current", h.currentYear)
	get("/years/{year}", h.year)
	get("/pages", h.find)
	get("/pages/{id}", h.page)
	get("/people", h.named("person", "people", "create-person"))
	get("/places", h.named("place", "places", "create-place"))
	post("/measurements/{id}/relocate", h.relocateReading)
	get("/sessions", h.sessions)
	get("/sessions/{id}", h.session)
	post("/sessions", h.captureSession)
	post("/sessions/{id}/edit", h.editSession)
	post("/sessions/{id}/tombstone", h.sessionLifecycle(true))
	post("/sessions/{id}/revive", h.sessionLifecycle(false))
	get("/periods", h.periods)
	post("/periods", h.createPeriod)
	post("/pages/{id}/period", h.writePeriod(false))
	post("/pages/{id}/promote-period", h.writePeriod(true))
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
	get("/titles", h.titles)
	m.HandleFunc("POST /render", h.render)
	m.HandleFunc("GET /assets/fonts/{file}", fonts)
	m.HandleFunc("GET /styleguide", h.styleguide)
	m.HandleFunc("GET /manifest.webmanifest", install("manifest.webmanifest", "application/manifest+json", "public, max-age=3600"))
	m.HandleFunc("GET /assets/icon.svg", install("icon.svg", "image/svg+xml", "public, max-age=31536000, immutable"))
	post("/days/{day}/capture", h.capture)
	post("/pages", h.createPage)
	post("/pages/{id}/body", h.saveBody)
	post("/people", h.createPerson)
	post("/places", h.createPlace)
	post("/files", h.addFile)
	post("/pages/{id}/promote", h.promote)
	post("/pages/{id}/tombstone", h.tombstone)
	post("/pages/{id}/revive", h.revive)
	post("/pages/{id}/links", h.linkPages)
	post("/pages/{id}/unlink", h.unlinkPages)
	post("/measurements", h.record)
	post("/measurements/from-table", h.readingsFromTable)
	post("/measurements/{id}/correct", h.correct)
	post("/measurements/{id}/retract", h.retract)
	post("/query", h.query)
	post("/pages/{id}/rename", h.rename)
	post("/pages/{id}/locate", h.locate)
	post("/metrics", h.registerMetric)
	post("/snapshots", h.snapshot)
	post("/metrics/{name}/periods", h.startHabit)
	post("/metrics/{name}/stop", h.stopHabit)
	post("/metrics/{name}/check-in", h.checkIn)
	get("/habits", h.habits)
	get("/integrity", h.integrity)
	get("/tasks", h.tasks)
	get("/tasks/{id}", h.task)
	get("/tasks/{id}/occurrences/{key}", h.occurrence)
	get("/deadlines", h.deadlines)
	post("/tasks", h.createTask)
	post("/tasks/{id}/edit", h.editTask)
	post("/tasks/{id}/stop", h.stopTask)
	post("/tasks/{id}/tombstone", h.taskLifecycle(true))
	post("/tasks/{id}/revive", h.taskLifecycle(false))
	post("/tasks/{id}/occurrences", h.captureOccurrence)
	post("/tasks/{id}/occurrences/{key}/edit", h.editOccurrence)
	post("/tasks/{id}/occurrences/{key}/tombstone", h.occurrenceLifecycle(true))
	post("/tasks/{id}/occurrences/{key}/revive", h.occurrenceLifecycle(false))
	guard := http.NewCrossOriginProtection()
	for _, origin := range trustedOrigins {
		if err := guard.AddTrustedOrigin(origin); err != nil {
			panic(err) // Public checked every origin before New was called
		}
	}
	guard.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e, status := errorEntity(&core.Error{Status: http.StatusForbidden, Msg: "cross-origin browser write refused", Code: "cross_origin"})
		writeEntity(w, r, status, e)
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
			if h.refusedOnItsPage(w, r, e, status) {
				return
			}
		case r.Method == "POST":
			if self := selfOf(e); self != "" {
				if wantsHTML(r) { // a browser form: post, then redirect, so a reload never posts again
					to := self
					if back := backOf(r); back != "" {
						to = back
					}
					http.Redirect(w, r, h.feedback.put(to, e.Result), http.StatusSeeOther)
					return
				}
				w.Header().Set("Location", self)
			}
		}
		if r.Method == "GET" && wantsHTML(r) && status < 400 {
			if feedback := h.feedback.take(r.URL.Path, r.URL.Query().Get("feedback")); feedback != "" {
				e.Result = feedback
				w.Header().Set("Cache-Control", "no-store")
				w.Header().Set("Referrer-Policy", "no-referrer")
			}
		}
		h.write(w, r, status, e)
	}
}

// refusal is a refused browser form, carried in the context of the GET that draws the page the form came from
// (docs/plans/097-the-almanac-reflects.md). renderHTML reads it: the page then shows the error and the form
// as the owner filled it. shown is set when it did.
type refusal struct {
	entity *Entity    // the error entity
	status int        // the error's status: the page answers with it
	href   string     // the refused action's address
	sent   url.Values // what the form sent
	shown  bool
}

type refusalKey struct{}

// refusedOnItsPage answers a browser form that failed with the page it came from, filled in, and reports whether it
// did. The server runs its own GET of the back path through its routes, on the context and the headers of the POST,
// and a refusal in the context tells renderHTML to add the alert and the sent values. The GET writes into a buffer:
// when it does not draw a page (a 404, a redirect, a picture), nothing is sent from it and the plain error view
// answers, as for a POST with no back path. The GET gets no body: only the refusal carries what the form sent.
func (h *server) refusedOnItsPage(w http.ResponseWriter, r *http.Request, e *Entity, status int) bool {
	back := backOf(r)
	if r.Method != "POST" || !wantsHTML(r) || back == "" {
		return false
	}
	rf := &refusal{entity: e, status: status, href: r.URL.Path, sent: r.PostForm}
	ctx := context.WithValue(r.Context(), refusalKey{}, rf)
	get, err := http.NewRequestWithContext(ctx, "GET", back, nil)
	if err != nil {
		return false
	}
	get.Header = r.Header.Clone()
	get.Header.Del("Content-Type")
	get.Header.Del("Content-Length")
	get.Host, get.RemoteAddr, get.TLS = r.Host, r.RemoteAddr, r.TLS
	out := &pageBuffer{header: http.Header{}, status: http.StatusOK}
	h.mux.ServeHTTP(out, get)
	if !rf.shown {
		return false
	}
	for k, v := range out.header {
		w.Header()[k] = v
	}
	w.WriteHeader(out.status)
	w.Write(out.body.Bytes())
	return true
}

// pageBuffer holds what a handler writes, so the caller can send it or drop it.
type pageBuffer struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *pageBuffer) Header() http.Header         { return b.header }
func (b *pageBuffer) Write(p []byte) (int, error) { return b.body.Write(p) }
func (b *pageBuffer) WriteHeader(status int)      { b.status = status }

// maxBack is the longest return path a browser form may send.
const maxBack = 2048

// backOf is the page a browser form asks to return to after its write (docs/plans/091-the-browser-for-daily-use.md):
// the form field back, when it is a path on this server, "" otherwise. Only a path that starts with one slash is
// kept: "//host" and "/\host" name another host to a browser, and a scheme, a host, a fragment or a control
// character makes the value something other than a path here. A value that is not kept is ignored, not refused:
// the write is done, and the browser goes to the resource it changed.
func backOf(r *http.Request) string {
	b := r.PostForm.Get("back")
	if b == "" || len(b) > maxBack || !strings.HasPrefix(b, "/") || strings.HasPrefix(b, "//") ||
		strings.ContainsAny(b, "\\#") || strings.IndexFunc(b, func(c rune) bool { return c < 0x20 || c == 0x7f }) >= 0 {
		return ""
	}
	u, err := url.Parse(b)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" || !strings.HasPrefix(u.Path, "/") {
		return ""
	}
	return b
}

func selfOf(e *Entity) string {
	if l := e.linkByRel("self"); l != nil {
		return l.Href
	}
	return ""
}

// errorEntity is the one error representation: status, a code a client branches on, and the message.
func errorEntity(err error) (*Entity, int) {
	status, code := http.StatusInternalServerError, ""
	e := &Entity{Class: []string{"error"}, Title: "Error", Links: []Link{{Rel: []string{"index"}, Href: "/", Title: "Home"}}}
	var ce *core.Error
	var ex *core.ExistsError
	switch {
	case errors.As(err, &ex):
		status, code = http.StatusConflict, "exists"
		e.Links = append(e.Links, Link{Rel: []string{"existing"}, Href: pageHref(ex.ID), Title: ex.Title})
	case errors.As(err, &ce):
		status, code = ce.Status, ce.Code
	case errors.Is(err, context.DeadlineExceeded):
		status = http.StatusGatewayTimeout
	}
	if code == "" {
		code = codeOf(status)
	}
	e.Properties = map[string]any{"status": status, "code": code, "message": err.Error()}
	return e, status
}

// codeOf is the general code of a status, for an error that names no more specific one.
func codeOf(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusRequestEntityTooLarge:
		return "too_large"
	case http.StatusUnprocessableEntity:
		return "invalid"
	case http.StatusGatewayTimeout:
		return "timeout"
	}
	return "internal"
}

// writeEntity answers with the entity as Siren JSON, or as a page when the client is a browser.
func writeEntity(w http.ResponseWriter, r *http.Request, status int, e *Entity) {
	if wantsHTML(r) {
		renderHTML(w, r, status, e, nil)
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

func pageHref(id int64) string        { return "/pages/" + strconv.FormatInt(id, 10) }
func dayHref(day string) string       { return "/days/" + day }
func measurementHref(id int64) string { return "/measurements/" + strconv.FormatInt(id, 10) }
func metricHref(name string) string   { return "/metrics/" + url.PathEscape(name) }
func link(rel, href, title string) Link {
	return Link{Rel: []string{rel}, Href: href, Title: title}
}

// ---- reads

// recentDays is how many of the newest day pages the root shows.
const recentDays = 5

// recentDay is a day page as the root lists it: the day, the first line of its body, and its mood.
type recentDay struct {
	core.DayEntry
	Mood []float64 `json:"mood,omitempty"`
}

// onThisDayMax is how many earlier years a day lists.
const onThisDayMax = 10

// withMoods adds to each day entry the day's moods.
func (h *server) withMoods(ctx context.Context, entries []core.DayEntry) ([]recentDay, error) {
	out := []recentDay{}
	if len(entries) == 0 {
		return out, nil
	}
	lo, hi := entries[0].Day, entries[0].Day
	for _, d := range entries {
		lo, hi = min(lo, d.Day), max(hi, d.Day)
	}
	moods, err := h.s.Moods(ctx, lo, hi)
	if err != nil {
		return nil, err
	}
	for _, d := range entries {
		out = append(out, recentDay{DayEntry: d, Mood: moods[d.Day]})
	}
	return out, nil
}

// onThisDay are the day pages of earlier years on the month and day of a day, the newest first.
func (h *server) onThisDay(ctx context.Context, day string) ([]recentDay, error) {
	entries, err := h.s.OnThisDay(ctx, day, onThisDayMax)
	if err != nil {
		return nil, err
	}
	return h.withMoods(ctx, entries)
}

// The root is today at a glance (docs/plans/091-the-browser-for-daily-use.md): what is due today and is overdue,
// today's habits, the newest day pages, and every action that starts something.
func (h *server) root(r *http.Request) (*Entity, error) {
	ctx := r.Context()
	today := h.localDay()
	due, err := h.overdueAndDue(ctx, today)
	if err != nil {
		return nil, err
	}
	hs, states, err := h.habitsOn(ctx, today)
	if err != nil {
		return nil, err
	}
	entries, err := h.s.DayEntries(ctx, "0000-01-01", today, true, recentDays)
	if err != nil {
		return nil, err
	}
	recent, err := h.withMoods(ctx, entries)
	if err != nil {
		return nil, err
	}
	onThisDay, err := h.onThisDay(ctx, today)
	if err != nil {
		return nil, err
	}
	actions := []Action{
		action("capture", map[string]string{"day": today}, nil), action("search", nil, nil), action("find", nil, nil), action("open-day", nil, nil), action("deadlines", nil, nil), action("create-task", nil, nil),
		action("capture-session", nil, nil), action("create-period", nil, nil), action("create-page", nil, nil), action("create-person", nil, nil), action("create-place", nil, nil), action("add-file", nil, nil),
		h.recordAction(ctx, "", today), action("register-metric", nil, nil), action("query", nil, nil), action("snapshot", nil, nil)}
	return &Entity{
		Class: []string{"root"}, Title: "Lifelog",
		Properties: map[string]any{"today": today, "due": due, "habits": hs, "recent": recent, "on_this_day": onThisDay},
		Links: []Link{link("self", "/", "Lifelog"), link("today", dayHref(today), "Today"), link("days", "/days", "Days"),
			link("sessions", "/sessions", "Sessions"), link("periods", "/periods", "Periods"), link("people", "/people", "People"), link("places", "/places", "Places"), link("files", "/files", "Files"), link("metrics", "/metrics", "Metrics"),
			link("habits", "/habits", "Habits"), link("tasks", "/tasks", "Tasks"), link("deadlines", "/deadlines", "Deadlines"),
			link("ghosts", "/ghosts", "Ghost pages"), link("actions", "/actions", "All actions"), link("calendar", "/months/current", "Calendar"),
			link("week", "/weeks/current", "This week"), link("year", "/years/current", "This year")},
		Actions: append(append(actions, checkIns(states, today)...), outcomesOf(due, today)...),
	}, nil
}

// overdueAndDue are the open occurrences due before today in the deadlines' default window, then the open ones due
// today.
func (h *server) overdueAndDue(ctx context.Context, today string) ([]occurrenceView, error) {
	t, _ := dayOf(today)
	from, yesterday := t.AddDate(0, 0, -windowBack).Format(time.DateOnly), t.AddDate(0, 0, -1).Format(time.DateOnly)
	late, err := h.dueIn(ctx, from, yesterday, core.StateOpen, false)
	if err != nil {
		return nil, err
	}
	due, err := h.dueIn(ctx, today, today, core.StateOpen, false)
	return append(late, due...), err
}

func (h *server) actions(*http.Request) (*Entity, error) {
	e := &Entity{Class: []string{"actions"}, Title: "Actions", Links: []Link{link("self", "/actions", "Actions"), link("index", "/", "Home")}}
	for _, s := range catalog {
		e.Actions = append(e.Actions, templated(s))
	}
	return e, nil
}

func (h *server) today(*http.Request) (*Entity, error) { return nil, redirect{dayHref(h.localDay())} }

func (h *server) days(r *http.Request) (*Entity, error) {
	limit, offset, err := pageOf(r, 60)
	if err != nil {
		return nil, err
	}
	days, err := h.s.RecentDays(r.Context(), limit+1, offset)
	if err != nil {
		return nil, err
	}
	e := &Entity{Class: []string{"days"}, Title: "Days", Links: []Link{link("self", "/days", "Days"), link("index", "/", "Home")}}
	days = paged(e, days, "/days", nil, limit, offset)
	for _, d := range days {
		e.Entities = append(e.Entities, link("item", dayHref(d.Title), d.Title))
	}
	return e, nil
}

// dayView is a day as the API shows it: the cookbook's day view, and beside it what is due that day (on today, the
// open work of the days before too), the day's mood, its habits and the day the server read as today
// (docs/plans/091-the-browser-for-daily-use.md).
type dayView struct {
	core.Day
	Due    []occurrenceView `json:"due"`
	Mood   []float64        `json:"mood"`
	Habits []habitView      `json:"habits"`
	Trends []trend          `json:"trends"`
	// OnThisDay are the day pages of earlier years on this month and day.
	OnThisDay []recentDay `json:"on_this_day"`
	Today     string      `json:"today"`
}

func (h *server) day(r *http.Request) (*Entity, error) {
	return h.dayEntity(r.Context(), r.PathValue("day"))
}

// dayEntity is the resource /days/{day} serves.
func (h *server) dayEntity(ctx context.Context, day string) (*Entity, error) {
	d, err := h.s.Day(ctx, day)
	if err != nil {
		return nil, err
	}
	today := h.localDay()
	var due []occurrenceView
	if day == today { // the open work of the days before, then all of today's
		due, err = h.overdueAndDue(ctx, today)
		var settled []occurrenceView
		if err == nil {
			settled, err = h.dueIn(ctx, today, today, "all", false)
		}
		for _, v := range settled {
			if v.State != core.StateOpen {
				due = append(due, v)
			}
		}
	} else {
		due, err = h.dueIn(ctx, day, day, "all", false)
	}
	if err != nil {
		return nil, err
	}
	moods, err := h.s.Moods(ctx, day, day)
	if err != nil {
		return nil, err
	}
	hs, states, err := h.habitsOn(ctx, day)
	if err != nil {
		return nil, err
	}
	trends, err := h.trends(ctx, day, d.Readings, hs)
	if err != nil {
		return nil, err
	}
	onThisDay, err := h.onThisDay(ctx, day)
	if err != nil {
		return nil, err
	}
	mood := moods[day]
	if mood == nil {
		mood = []float64{}
	}
	t, _ := dayOf(day)
	e := &Entity{
		Class: []string{"day"}, Title: day, Properties: dayView{Day: *d, Due: due, Mood: mood, Habits: hs, Trends: trends, OnThisDay: onThisDay, Today: today},
		Links: []Link{link("self", dayHref(day), day), link("prev", dayHref(t.AddDate(0, 0, -1).Format(time.DateOnly)), "Previous day"),
			link("next", dayHref(t.AddDate(0, 0, 1).Format(time.DateOnly)), "Next day"), link("metrics", "/metrics", "Metrics"), link("index", "/", "Home"),
			link("month", monthHref(t), t.Format(layoutMonth))},
		Actions: []Action{action("capture", map[string]string{"day": day}, nil), h.recordAction(ctx, "", day), action("open-day", nil, nil)},
	}
	if d.PageID != 0 {
		e.Links = append(e.Links, link("page", pageHref(d.PageID), "The day page"))
		p, err := h.s.PageByID(ctx, d.PageID)
		if err != nil {
			return nil, err
		}
		e.Actions = append(e.Actions, saveBodyAction(d.PageID, p.Body, p.Version))
	}
	e.Actions = append(append(e.Actions, checkIns(states, day)...), outcomesOf(due, today)...)
	for _, m := range d.Readings {
		e.Entities = append(e.Entities, link("reading", measurementHref(m.ID), fmt.Sprintf("%s %g", m.Metric, m.Value)))
	}
	return e, nil
}

func (h *server) find(r *http.Request) (*Entity, error) {
	title := r.URL.Query().Get("title")
	if err := required(r.URL.Query(), "title"); err != nil {
		return nil, err
	}
	id, err := h.s.PageID(r.Context(), title)
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, core.NotFound("no page titled %s", title)
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
	ids := idParam(id)
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
	e.Actions = append(e.Actions, saveBodyAction(id, p.Body, p.Version))
	if !p.IsDayPage {
		if p.Type == "page" && !p.SessionKind && !p.TaskProject {
			e.Actions = append(e.Actions, action("promote", ids, nil), action("promote-period", ids, map[string]any{"version": p.Version}))
		}
		e.Actions = append(e.Actions, action("rename", ids, nil))
	}
	if p.Period != nil {
		values := map[string]any{"version": p.Version}
		if p.Period.Start != nil {
			values["start_boundary"] = *p.Period.Start
		}
		if p.Period.End != nil {
			values["end_boundary"] = *p.Period.End
		}
		e.Actions = append(e.Actions, action("edit-period", ids, values))
	}
	if p.Type == "place" {
		values := map[string]any{"radius_m": core.DefaultRadius, "link_days": "1"}
		if pt := p.Point; pt != nil {
			values = map[string]any{"lat": pt.Lat, "lon": pt.Lon, "radius_m": pt.RadiusM, "link_days": boolField(pt.LinkDays)}
		}
		e.Actions = append(e.Actions, action("locate", ids, values))
	}
	kinds, err := h.linkKinds(ctx, p.Type, p.IsDayPage)
	if err != nil {
		return nil, err
	}
	if len(kinds) > 0 {
		e.Actions = append(e.Actions, withOptions(action("link", ids, nil), "kind", kinds))
		e.Actions = append(e.Actions, withOptions(action("unlink", ids, nil), "kind", kinds))
	}
	if core.HasTable(p.Body) {
		e.Actions = append(e.Actions, action("readings-from-table", nil, map[string]any{"page": p.Title}))
	}
	e.Actions = append(e.Actions, action("tombstone", ids, nil))
	return e, nil
}

// linkKinds are the kinds a page of this type may start (link_kinds.from_types), minus the two the save
// contract and renames own; `at` only from a day page (D16).
func (h *server) linkKinds(ctx context.Context, typ string, dayPage bool) ([]string, error) {
	kinds, err := h.s.LinkKindsFrom(ctx, typ)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, k := range kinds {
		if k == "at" && !dayPage {
			continue
		}
		out = append(out, k)
	}
	return out, nil
}

func withOptions(a Action, field string, opts []string) Action {
	if f := a.field(field); f != nil {
		f.Options = opts
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
		a.field("metric").Value = metric
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

// namedRow is a row of the people and places lists: the page and how often the log mentions it.
type namedRow struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Mentions int    `json:"mentions"`
	LastDay  string `json:"last_day,omitempty"`
}

func (h *server) named(typ, plural, create string) func(*http.Request) (*Entity, error) {
	return func(r *http.Request) (*Entity, error) {
		limit, offset, err := pageOf(r, 200)
		if err != nil {
			return nil, err
		}
		e := &Entity{Class: []string{plural}, Title: strings.ToUpper(plural[:1]) + plural[1:],
			Links:   []Link{link("self", "/"+plural, plural), link("index", "/", "Home")},
			Actions: []Action{action(create, nil, nil)}}
		if typ == "person" || typ == "place" { // these rows say how often the log mentions them
			found, err := h.s.NamedMentions(r.Context(), typ, limit+1, offset)
			if err != nil {
				return nil, err
			}
			found = paged(e, found, "/"+plural, nil, limit, offset)
			rows := []namedRow{}
			for _, p := range found {
				rows = append(rows, namedRow{ID: p.ID, Title: p.Title, Mentions: p.Mentions, LastDay: p.LastDay})
				e.Entities = append(e.Entities, Link{Rel: []string{"item"}, Href: pageHref(p.ID), Title: p.Title, Class: []string{typ}})
			}
			e.Properties.(map[string]any)["rows"] = rows
			return e, nil
		}
		list, err := h.s.Named(r.Context(), typ, limit+1, offset)
		if err != nil {
			return nil, err
		}
		list = paged(e, list, "/"+plural, nil, limit, offset)
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
	m := h.metricOf(ctx, name)
	var categories []string
	if m != nil {
		categories = m.Categories
	}
	e := &Entity{Class: []string{"series"}, Title: name,
		Properties: map[string]any{"metric": name, "from": from, "to": to, "readings": rows, "habit_periods": periods, "categories": categories, "scope": scope, "session_id": sessionID, "include_deleted": includeDeleted, "all_history": allHistory},
		Links:      []Link{link("all-readings", allURL.String(), "All readings"), link("self", self, name), link("up", "/metrics", "Metrics"), link("habits", "/habits", "Habits"), link("index", "/", "Home")},
		Actions:    []Action{h.recordAction(ctx, name, today)}}
	if f := e.Actions[0].field("session_id"); scope == "session" && f != nil {
		f.Value = sessionID
	}
	ids := map[string]string{"name": name}
	if len(periods) > 0 && periods[len(periods)-1].End == "" {
		e.Actions = append(e.Actions, action("stop-habit", ids, map[string]any{"day": today}),
			action("check-in", ids, map[string]any{"day": today}))
	} else if h.unitless(ctx, name) {
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

func (h *server) ghosts(r *http.Request) (*Entity, error) {
	limit, offset, err := pageOf(r, 200)
	if err != nil {
		return nil, err
	}
	gs, err := h.s.Ghosts(r.Context(), limit+1, offset)
	if err != nil {
		return nil, err
	}
	e := &Entity{Class: []string{"ghosts"}, Title: "Ghost pages", Links: []Link{link("self", "/ghosts", "Ghost pages"), link("index", "/", "Home")}}
	gs = paged(e, gs, "/ghosts", nil, limit, offset)
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
	limit, offset, err := pageOf(r, 50)
	if err != nil {
		return nil, err
	}
	hits, err := h.s.Search(r.Context(), q, limit+1, offset)
	if err != nil {
		return nil, err
	}
	e.Properties = map[string]any{"q": q}
	hits = paged(e, hits, "/search", url.Values{"q": {q}}, limit, offset)
	e.Properties.(map[string]any)["hits"] = hits
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
	if day == "today" { // the share target of the manifest cannot know the day
		day = h.localDay()
	}
	_, sync, err := h.s.Capture(r.Context(), src, day, v.Get("text"), mood)
	if err != nil {
		return nil, err
	}
	return withResult(h.dayEntity(r.Context(), day))(sync)
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
	id, v, err := idForm(r, "version")
	if err != nil {
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
	id, v, err := idForm(r)
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
		err = core.NotFound("no page titled %s: create it first", v.Get("to"))
	}
	return
}

func (h *server) linkPages(r *http.Request, src string) (*Entity, error) {
	from, to, v, err := h.linkEnds(r)
	if err != nil {
		return nil, err
	}
	if err := h.s.Link(r.Context(), src, from, to, v.Get("kind"), v.Get("note")); err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), from)
}

func (h *server) unlinkPages(r *http.Request, src string) (*Entity, error) {
	from, to, v, err := h.linkEnds(r)
	if err != nil {
		return nil, err
	}
	if err := h.s.Unlink(r.Context(), src, from, to, v.Get("kind")); err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), from)
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

// snapshot is the owner's action (docs/plans/082-snapshot-in-the-catalog.md): the file lands on this machine, in
// the store's folder; the answer is the path and the restore check, with self at the root so a browser form is
// answered home with them as feedback.
func (h *server) snapshot(r *http.Request, _ string) (*Entity, error) {
	path, res, err := h.s.TakeSnapshot(r.Context(), time.Now())
	if err != nil {
		return nil, &core.Error{Status: 422, Msg: err.Error(), Code: "snapshot_refused"}
	}
	props := map[string]any{"snapshot": path, "restore_check": res}
	return &Entity{Class: []string{"snapshot"}, Title: "Snapshot", Properties: props, Result: props,
		Links: []Link{link("self", "/", "Home"), link("index", "/", "Home")}}, nil
}

func withResult(e *Entity, err error) func(any) (*Entity, error) {
	return func(res any) (*Entity, error) {
		if e != nil {
			e.Result = res
		}
		return e, err
	}
}
