package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"lifelog/internal/api/siren"
	"lifelog/internal/api/view"
	"lifelog/internal/core"
)

// server is the API on one store. The store's clock is the server's one clock: today is core.Store.Today, the words
// for a time and the name of a snapshot are in the zone of core.Store.Local, and the life of a feedback receipt is
// measured with core.Store.Now.
type server struct {
	s        *core.Store
	feedback feedbackStore
	mux      *http.ServeMux // the routes: a refused browser form runs a GET of its page through it
}

// New returns the whole API as one handler: lifelog serve mounts it on a socket, the CLI and the MCP server
// call it in-process. It trusts no browser origin other than its own. A public listener uses Public, which builds the
// API with the origins that the owner allows (docs/plans/078-network-server.md). A test gives a store with a fixed
// clock, so today is a fixed day and a time is written in fixed words.
func New(s *core.Store) http.Handler {
	return newOn(s, http.NewCrossOriginProtection())
}

// newOn builds the API behind guard, which refuses a browser write from an origin that it does not trust. Public
// gives the guard that trusts the allowed origins; New gives one that trusts no other origin.
func newOn(s *core.Store, guard *http.CrossOriginProtection) http.Handler {
	h := newServer(s)
	guard.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, crossOriginRefusal())
	}))
	return guard.Handler(limitBody(h.mux))
}

// newServer is the server with its routes: mount gives each action of the catalog its route, then the routes that are
// not actions follow.
func newServer(s *core.Store) *server {
	h := &server{s: s, feedback: feedbackStore{now: s.Now}}
	m := http.NewServeMux()
	h.mux = m
	h.mount(m, catalog, map[string]actionFunc{
		"capture": h.capture, "create-page": h.createPage, "save-body": h.saveBody, "create-person": h.createPerson,
		"create-place": h.createPlace, "add-file": h.addFile, "set-picture": h.setPicture, "promote": h.promote,
		"tombstone": h.tombstone, "revive": h.revive, "tombstone-pages": h.tombstoneMany, "link": h.linkPages,
		"unlink": h.unlinkPages, "file-pages": h.filePages, "record": h.record, "correct": h.correct,
		"retract": h.retract, "rename": h.rename, "register-metric": h.registerMetric, "integrity-check": h.integrity,
		"search": h.search, "browse": h.browse, "find": h.find, "open-day": h.openDay, "snapshot": h.snapshot,
	})
	// The routes that are not actions: the resources, the old lists, the editor's helpers and the files of the browser.
	m.HandleFunc("GET /{$}", h.serve(h.root))
	m.HandleFunc("GET /actions", h.serve(h.actions))
	m.HandleFunc("GET /days", h.serve(h.days))
	m.HandleFunc("GET /days/today", h.serve(h.today))
	m.HandleFunc("GET /days/{day}", h.serve(h.day))
	m.HandleFunc("GET /months/current", h.serve(h.currentMonth))
	m.HandleFunc("GET /months/{month}", h.serve(h.month))
	m.HandleFunc("GET /weeks/current", h.serve(h.currentWeek))
	m.HandleFunc("GET /weeks/{week}", h.serve(h.week))
	m.HandleFunc("GET /years/current", h.serve(h.currentYear))
	m.HandleFunc("GET /years/{year}", h.serve(h.year))
	m.HandleFunc("GET /pages/{id}", h.serve(h.page))
	m.HandleFunc("GET /pages/{id}/preview", picture(func(r *http.Request) ([]byte, error) {
		id, err := idOf(r)
		if err != nil {
			return nil, err
		}
		return h.s.Preview(r.Context(), id)
	}, "private, max-age=31536000, immutable")) // a preview by id never changes
	m.HandleFunc("GET /previews", picture(func(r *http.Request) ([]byte, error) {
		return h.s.PreviewByTitle(r.Context(), r.URL.Query().Get("title"))
	}, ""))
	m.HandleFunc("GET /metrics", h.serve(h.metrics))
	m.HandleFunc("GET /metrics/{name}", h.serve(h.series))
	m.HandleFunc("GET /measurements/{id}", h.serve(h.measurement))
	for path := range oldLists {
		m.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, toBrowse(path, r.URL.Query()), http.StatusPermanentRedirect)
		})
	}
	m.HandleFunc("POST /render", h.render)
	m.HandleFunc("GET /assets/fonts/{file}", fonts)
	m.HandleFunc("GET /styleguide", h.styleguide)
	m.HandleFunc("GET /manifest.webmanifest", install("manifest.webmanifest", "application/manifest+json", "public, max-age=3600"))
	m.HandleFunc("GET /assets/icon.svg", install("icon.svg", "image/svg+xml", "public, max-age=31536000, immutable"))
	return h
}

// crossOriginRefusal is the refusal of a browser write from an origin that the server does not trust: the guard of
// the API and the login forms of a public listener write it. Each call gives a new error, so no caller can change the
// refusal of another.
func crossOriginRefusal() error {
	return &core.Error{Status: http.StatusForbidden, Msg: "cross-origin browser write refused", Code: "cross_origin"}
}

// authorityRefusal is the refusal of a request whose authority the listener does not take: the local listener and the
// public one write it. Each call gives a new error, as crossOriginRefusal does.
func authorityRefusal() error {
	return &core.Error{Status: http.StatusForbidden, Msg: "HTTP authority refused"}
}

// redirect is returned by a handler that answers with another resource's address.
type redirect struct{ to string }

func (r redirect) Error() string { return "see " + r.to }

// actionFunc runs one action of the catalog. act is the action's row of the catalog, which mount gives: the handler
// reads its fields with it (fieldsOf), so a handler does not write its own name. src is the writer of the request
// (lifelog_meta.source); a read that is not the owner's alone gets "".
type actionFunc func(r *http.Request, act spec, src string) (*siren.Entity, error)

// mount gives each action of cat a route to its handler, with the method and the path of the action, and gives the
// handler the action's row. An action that writes reads its writer. An Owner action reads its writer and refuses a
// writer that is not the owner (an agent or an import). Any other read reads no writer, so its source header cannot
// refuse it. mount panics when an action has no handler or a handler has no action: the server does not start when
// the catalog and the routes disagree.
func (h *server) mount(m *http.ServeMux, cat []spec, handlers map[string]actionFunc) {
	named := map[string]bool{}
	for _, s := range cat {
		named[s.Name] = true
		if handlers[s.Name] == nil {
			panic("api: the action " + s.Name + " has no handler")
		}
	}
	for name := range handlers {
		if !named[name] {
			panic("api: the handler " + name + " has no action in the catalog")
		}
	}
	for _, s := range cat {
		f := handlers[s.Name]
		m.HandleFunc(s.Method+" "+s.Path, h.serve(func(r *http.Request) (*siren.Entity, error) {
			var src string
			if s.Method != "GET" || s.Owner {
				src = source(r)
			}
			if s.Owner {
				if err := requireOwner(src); err != nil {
					return nil, err
				}
			}
			return f(r, s, src)
		}))
	}
}

func (h *server) serve(f func(*http.Request) (*siren.Entity, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		e, err := f(r)
		status := http.StatusOK
		var rd redirect
		switch {
		case errors.As(err, &rd):
			http.Redirect(w, r, rd.to, http.StatusSeeOther)
			return
		case err != nil:
			e, status = errorEntity(r, err)
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
		answer(w, r, status, e, frame{shell: h.s})
	}
}

// refusedOnItsPage answers a browser form that failed with the page it came from, filled in, and reports whether it
// did. The server runs its own GET of the back path through its routes, on the context and the headers of the POST,
// and catches the entity that the route answers. answer then draws that entity with the refusal and the error's
// status. When the GET answers with no entity, or with an error (a 404, a redirect, a picture), nothing is sent from
// it and the plain error view answers, as for a POST with no back path. The GET gets no body: only the refusal
// carries what the form sent.
func (h *server) refusedOnItsPage(w http.ResponseWriter, r *http.Request, e *siren.Entity, status int) bool {
	back := backOf(r)
	if r.Method != "POST" || !wantsHTML(r) || back == "" {
		return false
	}
	get, err := http.NewRequestWithContext(r.Context(), "GET", back, nil)
	if err != nil {
		return false
	}
	get.Header = r.Header.Clone()
	get.Header.Del("Content-Type")
	get.Header.Del("Content-Length")
	get.Host, get.RemoteAddr, get.TLS = r.Host, r.RemoteAddr, r.TLS
	page := &caught{header: http.Header{}}
	h.mux.ServeHTTP(page, get)
	if page.entity == nil || page.status >= 400 {
		return false
	}
	for k, v := range page.header {
		w.Header()[k] = v
	}
	w.Header().Set("Cache-Control", "no-store")
	get.Pattern = r.Pattern // a fault of the answer is logged under the route of the POST that the client sent (logFault)
	answer(w, get, status, page.entity, frame{shell: h.s, refused: &view.Refusal{Entity: e, Href: r.URL.Path, Sent: r.PostForm}})
	return true
}

// caught takes the answer of the GET that refusedOnItsPage runs: answer gives it the entity and the status, not their
// bytes, and the route's headers stay in header. A route that writes bytes of its own (a redirect, a picture, a 404
// of the routes) leaves entity nil, and its bytes are dropped.
type caught struct {
	header http.Header
	entity *siren.Entity
	status int
}

func (c *caught) Header() http.Header         { return c.header }
func (c *caught) Write(p []byte) (int, error) { return len(p), nil }
func (c *caught) WriteHeader(int)             {}

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

// errorEntity is the one error representation: status, a code a client branches on, and the message. An error that
// core did not type (not a core.Error, not an ExistsError) is a fault of the server: a defect, or the disk or the
// driver failed. Its text can name a file or show the driver, so the client gets the message "internal error", and
// the server's log gets the cause (logFault). A cancelled request is not logged: its client went away, or the server
// stopped it, and no fault caused it. A timeout keeps its text.
func errorEntity(r *http.Request, err error) (*siren.Entity, int) {
	status, code, msg := http.StatusInternalServerError, "", err.Error()
	e := &siren.Entity{Class: []string{"error"}, Title: "Error", Links: []siren.Link{{Rel: []string{"index"}, Href: "/", Title: "Home"}}}
	var ce *core.Error
	var ex *core.ExistsError
	switch {
	case errors.Is(err, context.DeadlineExceeded): // before the typed errors: a refusal that wraps a timeout is a timeout
		status = http.StatusGatewayTimeout
	case errors.As(err, &ex):
		status, code = http.StatusConflict, "exists"
		e.Links = append(e.Links, siren.Link{Rel: []string{"existing"}, Href: siren.PageHref(ex.ID), Title: ex.Title})
	case errors.As(err, &ce):
		status, code = ce.Status, ce.Code
	default:
		msg = "internal error"
		if !errors.Is(err, context.Canceled) {
			logFault(r, "internal error", err)
		}
	}
	if code == "" {
		code = codeOf(status)
	}
	e.Properties = map[string]any{"status": status, "code": code, "message": msg}
	return e, status
}

// logFault writes a fault of the server to the standard log, which the owner reads on the server's stderr: the route,
// what failed and the cause. A client never gets the cause. The route is the pattern that the request matched, not
// its path, because a path can name a metric or a title. A request that a guard answers before the routes has no
// pattern: its method stands for the route.
func logFault(r *http.Request, what string, err error) {
	route := r.Pattern
	if route == "" {
		route = r.Method
	}
	log.Printf("lifelog: %s: %s: %v", route, what, err)
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

// writeError answers with the error entity of err, as JSON or as a page with no shell. The guards, the pictures and
// /render use it. A route that serve wraps answers its error in serve: there a browser gets the page in the shell, or
// the page that a refused form came from.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	e, status := errorEntity(r, err)
	answer(w, r, status, e, frame{})
}

// frame says how answer draws an entity as a page, and for whom. The zero frame gives a program JSON and a browser a
// page with no sidebar: the answer of a guard, of a picture's error and of a refusal before the routes.
type frame struct {
	shell    *core.Store   // the store that the sidebar reads; nil: no sidebar, so the page shows nothing of the database
	refused  *view.Refusal // the refused form that the page shows; nil: none
	pageOnly bool          // a page for every client, a program too: the answer is presentation and has no JSON form
}

// answer is the one way an entity leaves the API: the answer of every route, guard and error. A program gets Siren
// JSON; a browser, and every client of a pageOnly frame, gets a page, and only then is the shell read. The GET that
// refusedOnItsPage runs gets the entity itself (caught). The answer is held whole before its first byte, so a fault
// found while it is made can still answer 500. A failed write is logged: the client went away, and nothing else can
// be done.
func answer(w http.ResponseWriter, r *http.Request, status int, e *siren.Entity, fr frame) {
	if c, ok := w.(*caught); ok {
		c.entity, c.status = e, status
		return
	}
	typ, b := "application/vnd.siren+json", []byte(nil)
	if fr.pageOnly || wantsHTML(r) {
		typ = "text/html; charset=utf-8"
		status, b = pageAnswer(r, status, e, fr)
	} else {
		status, b = sirenAnswer(r, status, e)
	}
	w.Header().Set("Content-Type", typ)
	w.WriteHeader(status)
	if _, err := w.Write(b); err != nil {
		logFault(r, "the answer was not sent", err)
	}
}

// pageAnswer is the status and the HTML of the page of e. When the page cannot be drawn, a defect of this program,
// the owner reads the cause in the log and a client never does: the page is DrawFailed, with 500.
func pageAnswer(r *http.Request, status int, e *siren.Entity, fr frame) (int, []byte) {
	f := view.Facts{URL: r.URL}
	if fr.shell != nil {
		f.Shell = shellOf(r.Context(), fr.shell)
	}
	if status >= 400 && r.Method == "POST" {
		f.Submitted = r.PostForm.Get("body")
		if f.Submitted == "" {
			f.Submitted = r.PostForm.Get("text")
		}
		if ref, err := url.Parse(r.Referer()); err == nil && ref.Host == r.Host && strings.HasPrefix(ref.Path, "/") {
			f.Back = ref.RequestURI()
		}
	}
	b, err := view.Draw(e, f, fr.refused)
	if err != nil {
		logFault(r, "the view "+view.TemplateOf(e)+" cannot be drawn", err)
		return http.StatusInternalServerError, []byte(view.DrawFailed)
	}
	return status, b
}

// sirenAnswer is the status and the Siren JSON of e. A value that JSON cannot carry (a NaN, an infinity) is a fault
// of this program: the answer is then the error entity of the fault, with 500.
func sirenAnswer(r *http.Request, status int, e *siren.Entity) (int, []byte) {
	b, err := sirenJSON(e)
	if err == nil {
		return status, b
	}
	e, status = errorEntity(r, fmt.Errorf("the answer cannot be encoded as JSON: %w", err))
	if b, err = sirenJSON(e); err != nil { // the error entity holds a number and strings only: a defect
		logFault(r, "the error cannot be encoded as JSON", err)
		return status, nil
	}
	return status, b
}

// sirenJSON is the Siren JSON of e, indented, with no HTML escapes. The answer is held whole before its first byte,
// as a page is. An entity is small: a list is paged, and this writer takes a body in a request of at most
// maxRequestBody bytes.
func sirenJSON(e *siren.Entity) ([]byte, error) {
	var b bytes.Buffer
	if err := siren.WriteJSON(&b, e, "  "); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func wantsHTML(r *http.Request) bool {
	a := r.Header.Get("Accept")
	return strings.Contains(a, "text/html") && !strings.Contains(a, "json")
}

// source is the writer of a request: its header, else "ui" for a browser and "api". The name is not checked here.
func source(r *http.Request) string {
	s := r.Header.Get(siren.SourceHeader)
	switch {
	case s != "":
	case wantsHTML(r):
		s = "ui"
	default:
		s = "api"
	}
	return s
}

// writerRE is the grammar of a writer name: entities_source, measurements_source and links_source.
var writerRE = regexp.MustCompile(`^[a-z0-9_:.-]{1,64}$`)

// requireOwner refuses a writer that is not the owner: an agent (agent:*) or an import (import:*). The owner-only
// actions (register-metric, snapshot, integrity-check) are refused to any other writer, on every surface.
// It checks the grammar of the name too, which repeats the CHECKs on purpose. An owner-only action can read and meet
// no CHECK, and the prefix tests below would pass "Agent:x" as the owner. Any other write leaves the name to the
// schema.
func requireOwner(source string) error {
	if !writerRE.MatchString(source) {
		return core.Invalid("source %q: 1-64 characters of a-z 0-9 _ : . -", source)
	}
	if strings.HasPrefix(source, "agent:") || strings.HasPrefix(source, "import:") {
		return &core.Error{Status: http.StatusForbidden, Msg: "this action is the owner's: an agent or an import may not run it"}
	}
	return nil
}

func (h *server) actions(*http.Request) (*siren.Entity, error) {
	e := &siren.Entity{Class: []string{"actions"}, Title: "Actions", Links: []siren.Link{link("self", "/actions", "Actions"), link("index", "/", "Home")}}
	for _, s := range catalog {
		e.Actions = append(e.Actions, templated(s))
	}
	return e, nil
}
