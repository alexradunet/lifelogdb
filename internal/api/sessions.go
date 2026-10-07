package api

import (
	"net/http"
	"net/url"
	"strconv"

	"lifelog/internal/core"
)

var sessionFields = []Field{req("kind", "text", "Kind (existing plain-page name)"), req("day", "date", "Reporting day"), opt("start_at", "text", "Start UTC instant"), opt("start_local", "text", "Start unresolved local clock"), opt("start_offset", "text", "Supplied known start offset"), opt("start_zone_unverified", "text", "Unverified start zone claim"), opt("end_at", "text", "End UTC instant"), opt("end_local", "text", "End unresolved local clock"), opt("end_offset", "text", "Supplied known end offset"), opt("end_zone_unverified", "text", "Unverified end zone claim")}

func sessionInput(v url.Values) core.SessionInput {
	return core.SessionInput{Kind: v.Get("kind"), Day: v.Get("day"), Key: v.Get("import_key"), StartAt: v.Get("start_at"), StartLocal: v.Get("start_local"), StartOffset: v.Get("start_offset"), StartZoneUnverified: v.Get("start_zone_unverified"), EndAt: v.Get("end_at"), EndLocal: v.Get("end_local"), EndOffset: v.Get("end_offset"), EndZoneUnverified: v.Get("end_zone_unverified")}
}
func sessionValues(s *core.Session) map[string]any {
	return map[string]any{"kind": s.Kind, "day": s.Day, "start_at": s.StartAt, "start_local": s.StartLocal, "start_offset": s.StartOffset, "start_zone_unverified": s.StartZoneUnverified, "end_at": s.EndAt, "end_local": s.EndLocal, "end_offset": s.EndOffset, "end_zone_unverified": s.EndZoneUnverified, "version": s.Version}
}
func (h *server) sessionEntity(r *http.Request, id int64) (*Entity, error) {
	s, err := h.s.Session(r.Context(), id)
	if err != nil {
		return nil, err
	}
	ids := map[string]string{"id": strconv.FormatInt(id, 10)}
	e := &Entity{Class: []string{"session"}, Title: s.Kind + " · " + s.Day, Properties: s, Links: []Link{link("self", "/sessions/"+ids["id"], "Session"), link("kind", pageHref(s.KindID), s.Kind), link("index", "/sessions", "Sessions")}}
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
	return h.sessionEntity(r, id)
}
func (h *server) sessions(r *http.Request) (*Entity, error) {
	q := r.URL.Query()
	historical := q.Get("include_deleted")
	if historical != "" && historical != "0" && historical != "1" {
		return nil, &core.Error{Status: 422, Msg: "include_deleted must be 0 or 1"}
	}
	ps, err := h.s.Sessions(r.Context(), q.Get("day"), q.Get("kind"), historical == "1")
	if err != nil {
		return nil, err
	}
	e := &Entity{Class: []string{"sessions"}, Title: "Recorded sessions", Properties: map[string]any{"sessions": ps, "include_deleted": historical == "1", "order": "reporting day then stable id, not an absolute timeline"}, Links: []Link{link("self", r.URL.String(), "Sessions"), link("index", "/", "Home")}, Actions: []Action{action("capture-session", nil, nil)}}
	for _, p := range ps {
		e.Entities = append(e.Entities, link("item", "/sessions/"+strconv.FormatInt(p.ID, 10), p.Kind+" · "+p.Day))
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
	var id int64
	err = h.s.Do(r.Context(), src, func(t *core.Tx) error { var e error; id, _, e = t.CaptureSession(sessionInput(v)); return e })
	if err != nil {
		return nil, err
	}
	return h.sessionEntity(r, id)
}
func (h *server) editSession(r *http.Request, src string) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err = required(v, "version", "kind", "day"); err != nil {
		return nil, err
	}
	err = h.s.Do(r.Context(), src, func(t *core.Tx) error { return t.EditSession(id, v.Get("version"), sessionInput(v)) })
	if err != nil {
		return nil, err
	}
	return h.sessionEntity(r, id)
}
func (h *server) tombstoneSession(r *http.Request, src string) (*Entity, error) {
	return h.sessionLifecycle(r, src, true)
}
func (h *server) reviveSession(r *http.Request, src string) (*Entity, error) {
	return h.sessionLifecycle(r, src, false)
}
func (h *server) sessionLifecycle(r *http.Request, src string, deleted bool) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err = required(v, "version"); err != nil {
		return nil, err
	}
	err = h.s.Do(r.Context(), src, func(t *core.Tx) error { return t.SessionLifecycle(id, v.Get("version"), deleted) })
	if err != nil {
		return nil, err
	}
	return h.sessionEntity(r, id)
}
