package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
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
	localDay func() string  // the server's local day: today, and the words for a day, are read against it
	zone     *time.Location // the zone of that day: a browser's words for a time use it
	mux      *http.ServeMux // the routes: a refused browser form runs a GET of its page through it
}

// New returns the whole API as one handler: lifelog serve mounts it on a socket, the CLI and the MCP server
// call it in-process. trustedOrigins are the browser origins a public listener allows to write
// (docs/plans/078-network-server.md); a local listener and the in-process client pass none.
func New(s *core.Store, trustedOrigins ...string) http.Handler {
	return NewOn(s, core.Today, time.Local, trustedOrigins...)
}

// NewOn is New with the clock of the local day and its zone given: a test fixes them, so today is a fixed day, and a
// time is written in fixed words.
func NewOn(s *core.Store, localDay func() string, zone *time.Location, trustedOrigins ...string) http.Handler {
	h := &server{s: s, localDay: localDay, zone: zone}
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
	m.HandleFunc("GET /titles", h.serve(h.titles))
	m.HandleFunc("POST /render", h.render)
	m.HandleFunc("GET /assets/fonts/{file}", fonts)
	m.HandleFunc("GET /styleguide", h.styleguide)
	m.HandleFunc("GET /manifest.webmanifest", install("manifest.webmanifest", "application/manifest+json", "public, max-age=3600"))
	m.HandleFunc("GET /assets/icon.svg", install("icon.svg", "image/svg+xml", "public, max-age=31536000, immutable"))
	guard := http.NewCrossOriginProtection()
	for _, origin := range trustedOrigins {
		if err := guard.AddTrustedOrigin(origin); err != nil {
			panic(err) // Public checked every origin before New was called
		}
	}
	guard.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, errCrossOrigin)
	}))
	return guard.Handler(m)
}

// oldLists are the lists that Browse replaced (docs/plans/117-one-browse-view.md, rule 8), each with the filters of
// Browse that give its rows. A GET of one answers 308 to Browse, so a bookmark or a client keeps working; the POST
// routes of the same paths do not change.
var oldLists = map[string]url.Values{
	"/people":     {"kind": {"person"}},
	"/places":     {"kind": {"place"}},
	"/media":      {"kind": {"media"}},
	"/files":      {"kind": {"document"}},
	"/ghosts":     {"kind": {"page"}, "ghosts": {"1"}},
	"/categories": {},
}

// toBrowse is the Browse address of a GET of an old list: its filters, and the limit, the offset and the asked form
// (new) of the query. The kind of the media list (image, video, audio) is the field media of Browse.
func toBrowse(path string, q url.Values) string {
	v := url.Values{}
	for k, vs := range oldLists[path] {
		v[k] = slices.Clone(vs)
	}
	for _, k := range []string{"limit", "offset", "new"} {
		if x, ok := q[k]; ok {
			v[k] = x
		}
	}
	if path == "/media" && q.Get("kind") != "" {
		v.Set("media", q.Get("kind"))
	}
	return browseHref(v)
}

// The refusals that two guards write: a browser write from an origin that the server does not trust (the guard of
// the API and the login forms of a public listener), and a request whose authority the listener does not take (the
// local listener and the public one).
var (
	errCrossOrigin = &core.Error{Status: http.StatusForbidden, Msg: "cross-origin browser write refused", Code: "cross_origin"}
	errAuthority   = &core.Error{Status: http.StatusForbidden, Msg: "HTTP authority refused"}
)

// redirect is returned by a handler that answers with another resource's address.
type redirect struct{ to string }

func (r redirect) Error() string { return "see " + r.to }

// actionFunc runs one action of the catalog. src is the writer of the request (lifelog_meta.source); a read that
// is not the owner's alone gets "".
type actionFunc func(r *http.Request, src string) (*Entity, error)

// mount gives each action of cat a route to its handler, with the method and the path of the action. An action that
// writes reads its writer. An Owner action reads its writer and refuses a writer that is not the owner (an agent or
// an import). Any other read reads no writer, so its source header cannot refuse it. mount panics when an action has
// no handler or a handler has no action: the server does not start when the catalog and the routes disagree.
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
		m.HandleFunc(s.Method+" "+s.Path, h.serve(func(r *http.Request) (*Entity, error) {
			var src string
			if s.Method != "GET" || s.Owner {
				var err error
				if src, err = source(r); err != nil {
					return nil, err
				}
			}
			if s.Owner && !core.IsOwner(src) {
				return nil, core.Forbidden("this action is the owner's: an agent or an import may not run it")
			}
			return f(r, src)
		}))
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

// errorEntity is the one error representation: status, a code a client branches on, and the message.
func errorEntity(err error) (*Entity, int) {
	status, code := http.StatusInternalServerError, ""
	e := &Entity{Class: []string{"error"}, Title: "Error", Links: []Link{{Rel: []string{"index"}, Href: "/", Title: "Home"}}}
	var ce *core.Error
	var ex *core.ExistsError
	switch {
	case errors.Is(err, context.DeadlineExceeded): // before the typed errors: a refusal that wraps a timeout is a timeout
		status = http.StatusGatewayTimeout
	case errors.As(err, &ex):
		status, code = http.StatusConflict, "exists"
		e.Links = append(e.Links, Link{Rel: []string{"existing"}, Href: pageHref(ex.ID), Title: ex.Title})
	case errors.As(err, &ce):
		status, code = ce.Status, ce.Code
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

// writeError answers with the error entity of err, as JSON or as a page with no shell. The guards, the pictures and
// /render use it. A route that serve wraps answers its error in serve: there a browser gets the page in the shell, or
// the page that a refused form came from.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	e, status := errorEntity(err)
	writeEntity(w, r, status, e)
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

func (h *server) actions(*http.Request) (*Entity, error) {
	e := &Entity{Class: []string{"actions"}, Title: "Actions", Links: []Link{link("self", "/actions", "Actions"), link("index", "/", "Home")}}
	for _, s := range catalog {
		e.Actions = append(e.Actions, templated(s))
	}
	return e, nil
}
