package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"lifelog/internal/core"
)

// SourceHeader names the writer of a request's rows (lifelog_meta.source). Without it a browser form writes as
// "ui" and any other client as "api".
const SourceHeader = "Lifelog-Source"

type server struct {
	s           *core.Store
	feedback    feedbackStore
	localDay    func() string  // the server's local day: what is due, overdue and today is read against it
	zone        *time.Location // the zone of that day: a browser's words for a time use it
	mux         *http.ServeMux // the routes: a refused browser form runs a GET of its page through it
	asker       Asker          // the agent of ask; nil when no model is configured
	transcriber Transcriber    // the words of a voice note sent to capture; nil when none is configured
}

// New returns the whole API as one handler: lifelog serve mounts it on a socket, the CLI and the MCP server
// call it in-process. trustedOrigins are the browser origins a public listener allows to write
// (docs/plans/078-network-server.md); a local listener and the in-process client pass none.
func New(s *core.Store, trustedOrigins ...string) http.Handler {
	return NewOn(s, core.Today, time.Local, nil, nil, trustedOrigins...)
}

// NewWith is New with what the environment configures: the agent of ask
// (docs/plans/102-a-conversation-with-lifelog.md) and the transcriber of a voice note (docs/plans/109-a-voice-note.md).
// A nil asker refuses ask; a nil transcriber refuses a capture with a voice note.
func NewWith(s *core.Store, asker Asker, transcriber Transcriber, trustedOrigins ...string) http.Handler {
	return NewOn(s, core.Today, time.Local, asker, transcriber, trustedOrigins...)
}

// NewOn is New with the clock of the local day and its zone given: a test fixes them, so what is due, overdue and
// today's conversation are read against a fixed day, and a time is written in fixed words.
func NewOn(s *core.Store, localDay func() string, zone *time.Location, asker Asker, transcriber Transcriber, trustedOrigins ...string) http.Handler {
	h := &server{s: s, localDay: localDay, zone: zone, asker: asker, transcriber: transcriber}
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
	get("/media", h.media)
	get("/files", h.files)
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
	get("/categories", h.categoriesView)
	get("/search", h.search)
	get("/titles", h.titles)
	m.HandleFunc("POST /render", h.render)
	m.HandleFunc("GET /assets/fonts/{file}", fonts)
	m.HandleFunc("GET /styleguide", h.styleguide)
	m.HandleFunc("GET /manifest.webmanifest", install("manifest.webmanifest", "application/manifest+json", "public, max-age=3600"))
	m.HandleFunc("GET /assets/icon.svg", install("icon.svg", "image/svg+xml", "public, max-age=31536000, immutable"))
	m.HandleFunc("GET "+worldOutline, install(worldOutline[len("/assets/"):], "image/svg+xml", "public, max-age=31536000, immutable"))
	// The two portraits of a conversation, from the owner's design system Balaur — Basm (docs/plans/104-the-chat-in-basm.md).
	m.HandleFunc("GET /assets/portrait-lifelog.png", install("portrait-lifelog.png", "image/png", "public, max-age=31536000, immutable"))
	m.HandleFunc("GET /assets/portrait-owner.png", install("portrait-owner.png", "image/png", "public, max-age=31536000, immutable"))
	post("/days/{day}/capture", h.capture)
	post("/pages", h.createPage)
	post("/pages/{id}/body", h.saveBody)
	post("/people", h.createPerson)
	post("/places", h.createPlace)
	post("/files", h.addFile)
	post("/pages/{id}/picture", h.setPicture)
	post("/pages/{id}/promote", h.promote)
	post("/pages/{id}/tombstone", h.tombstone)
	post("/pages/{id}/revive", h.revive)
	post("/pages/tombstone", h.tombstoneMany("pages"))
	post("/pages/file", h.filePages)
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
	post("/tasks/tombstone", h.tombstoneMany("tasks"))
	post("/tasks/{id}/occurrences", h.captureOccurrence)
	post("/tasks/{id}/occurrences/{key}/edit", h.editOccurrence)
	post("/tasks/{id}/occurrences/{key}/tombstone", h.occurrenceLifecycle(true))
	post("/tasks/{id}/occurrences/{key}/revive", h.occurrenceLifecycle(false))
	get("/conversations", func(r *http.Request) (*Entity, error) { return h.todayConversation(r.Context()) })
	get("/conversations/{id}", h.conversation)
	post("/conversations", h.ask)
	guard := http.NewCrossOriginProtection()
	for _, origin := range trustedOrigins {
		if err := guard.AddTrustedOrigin(origin); err != nil {
			panic(err) // Public checked every origin before New was called
		}
	}
	guard.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refuse(w, r, http.StatusForbidden, "cross-origin browser write refused", "cross_origin")
	}))
	return guard.Handler(m)
}

// refuse answers a request the guards turn away with the error entity, as every refusal is answered: status, a code
// (the status's general name when code is "") and the message.
func refuse(w http.ResponseWriter, r *http.Request, status int, msg, code string) {
	e, status := errorEntity(&core.Error{Status: status, Msg: msg, Code: code})
	writeEntity(w, r, status, e)
}

// redirect is returned by a handler that answers with another resource's address.
type redirect struct{ to string }

func (r redirect) Error() string { return "see " + r.to }

// ownerOnly refuses an action that is the owner's alone to an agent (a model never registers a metric or approves).
func ownerOnly(f func(*http.Request, string) (*Entity, error)) func(*http.Request, string) (*Entity, error) {
	return func(r *http.Request, src string) (*Entity, error) {
		if !core.IsOwner(src) {
			return nil, core.Forbidden("this action is the owner's: an agent or an import may not run it")
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
