package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"lifelog/internal/core"
)

// sessionFields are the fields of a session as capture-session and edit-session take them (docs/decisions/D22-events.md).
var sessionFields = []Field{
	req("kind", "text", "Kind (existing plain-page name)"), req("day", "date", "Reporting day"),
	opt("start_at", "text", "Start UTC instant"), opt("start_local", "text", "Start unresolved local clock"),
	opt("start_offset", "text", "Supplied known start offset"), opt("start_zone_unverified", "text", "Unverified start zone claim"),
	opt("end_at", "text", "End UTC instant"), opt("end_local", "text", "End unresolved local clock"),
	opt("end_offset", "text", "Supplied known end offset"), opt("end_zone_unverified", "text", "Unverified end zone claim"),
}

// sessionInput is the session a form describes.
func sessionInput(v url.Values) core.SessionInput {
	return core.SessionInput{Kind: v.Get("kind"), Day: v.Get("day"), ImportKey: v.Get("import_key"),
		StartAt: v.Get("start_at"), StartLocal: v.Get("start_local"), StartOffset: v.Get("start_offset"), StartZoneUnverified: v.Get("start_zone_unverified"),
		EndAt: v.Get("end_at"), EndLocal: v.Get("end_local"), EndOffset: v.Get("end_offset"), EndZoneUnverified: v.Get("end_zone_unverified")}
}

// sessionValues prefill a session's actions with its current values.
func sessionValues(s *core.Session) map[string]any {
	return map[string]any{"kind": s.Kind, "day": s.Day, "start_at": s.StartAt, "start_local": s.StartLocal, "start_offset": s.StartOffset,
		"start_zone_unverified": s.StartZoneUnverified, "end_at": s.EndAt, "end_local": s.EndLocal, "end_offset": s.EndOffset,
		"end_zone_unverified": s.EndZoneUnverified, "version": s.Version}
}

func sessionHref(id int64) string { return "/sessions/" + strconv.FormatInt(id, 10) }

// sessionEntity is the resource /sessions/{id} serves: the session, its kind's page, and the edit or the revive it offers.
func (h *server) sessionEntity(ctx context.Context, id int64) (*Entity, error) {
	s, err := h.s.Session(ctx, id)
	if err != nil {
		return nil, err
	}
	ids := idParam(id)
	e := &Entity{Class: []string{"session"}, Title: s.Kind + " · " + s.Day, Properties: s,
		Links: []Link{link("self", sessionHref(id), "Session"), link("kind", pageHref(s.KindID), s.Kind), link("up", "/sessions", "Sessions"), link("index", "/", "Home")}}
	if s.DeletedAt != "" {
		e.Actions = []Action{action("revive-session", ids, sessionValues(s))}
	} else {
		e.Actions = []Action{action("edit-session", ids, sessionValues(s)), action("tombstone-session", ids, sessionValues(s))}
	}
	return e, nil
}

func (h *server) session(r *http.Request) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	return h.sessionEntity(r.Context(), id)
}

// sessions lists the recorded sessions, by reporting day then id; ?day= and ?kind= narrow them.
func (h *server) sessions(r *http.Request) (*Entity, error) {
	q := r.URL.Query()
	deleted, err := flag(q, "include_deleted")
	if err != nil {
		return nil, err
	}
	ps, err := h.s.Sessions(r.Context(), q.Get("day"), q.Get("kind"), deleted)
	if err != nil {
		return nil, err
	}
	self := url.Values{}
	for _, k := range []string{"day", "kind"} {
		if q.Get(k) != "" {
			self.Set(k, q.Get(k))
		}
	}
	if deleted {
		self.Set("include_deleted", "1")
	}
	href := "/sessions"
	if len(self) > 0 {
		href += "?" + self.Encode()
	}
	e := &Entity{Class: []string{"sessions"}, Title: "Recorded sessions",
		Properties: map[string]any{"sessions": ps, "include_deleted": deleted, "order": "reporting day then stable id, not an absolute timeline"},
		Links:      []Link{link("self", href, "Sessions"), link("index", "/", "Home")}, Actions: []Action{action("capture-session", nil, nil)}}
	for _, p := range ps {
		e.Entities = append(e.Entities, link("item", sessionHref(p.ID), p.Kind+" · "+p.Day))
	}
	return e, nil
}

func (h *server) captureSession(r *http.Request, src string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "kind", "day"); err != nil {
		return nil, err
	}
	id, _, err := h.s.CaptureSession(r.Context(), src, sessionInput(v))
	if err != nil {
		return nil, err
	}
	return h.sessionEntity(r.Context(), id)
}

func (h *server) editSession(r *http.Request, src string) (*Entity, error) {
	id, v, err := idForm(r, "version", "kind", "day")
	if err != nil {
		return nil, err
	}
	if err := h.s.EditSession(r.Context(), src, id, v.Get("version"), sessionInput(v)); err != nil {
		return nil, err
	}
	return h.sessionEntity(r.Context(), id)
}

// sessionLifecycle is tombstone-session (deleted) and revive-session.
func (h *server) sessionLifecycle(deleted bool) func(*http.Request, string) (*Entity, error) {
	return func(r *http.Request, src string) (*Entity, error) {
		id, v, err := idForm(r, "version")
		if err != nil {
			return nil, err
		}
		if err := h.s.SessionLifecycle(r.Context(), src, id, v.Get("version"), deleted); err != nil {
			return nil, err
		}
		return h.sessionEntity(r.Context(), id)
	}
}
