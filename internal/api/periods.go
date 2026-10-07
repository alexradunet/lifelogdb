package api

import (
	"net/http"

	"lifelog/internal/core"
)

func boundary(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func (h *server) periods(r *http.Request) (*Entity, error) {
	q := r.URL.Query()
	historical := q.Get("include_deleted")
	if historical != "" && historical != "0" && historical != "1" {
		return nil, &core.Error{Status: 422, Msg: "include_deleted must be 0 or 1"}
	}
	ps, err := h.s.LifePeriods(r.Context(), q.Get("day"), q.Get("as_of"), historical == "1")
	if err != nil {
		return nil, err
	}
	e := &Entity{Class: []string{"periods"}, Title: "Recorded periods", Properties: map[string]any{"periods": ps, "day": q.Get("day"), "as_of": q.Get("as_of"), "membership_basis": "supplied calendar day, not physical interval overlap", "include_deleted": historical == "1"}, Links: []Link{link("self", r.URL.String(), "Periods"), link("index", "/", "Home")}, Actions: []Action{action("create-period", nil, nil)}}
	for _, p := range ps {
		e.Entities = append(e.Entities, link("item", pageHref(p.ID), p.Title))
	}
	return e, nil
}
func (h *server) createPeriod(r *http.Request, src string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err = required(v, "title"); err != nil {
		return nil, err
	}
	var id int64
	err = h.s.Do(r.Context(), src, func(t *core.Tx) error {
		var e error
		id, e = t.CreatePeriod(v.Get("title"), v.Get("body"), boundary(v.Get("start_boundary")), boundary(v.Get("end_boundary")))
		return e
	})
	if err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), id)
}
func (h *server) editPeriod(r *http.Request, src string) (*Entity, error) {
	return h.writePeriod(r, src, false)
}
func (h *server) promotePeriod(r *http.Request, src string) (*Entity, error) {
	return h.writePeriod(r, src, true)
}
func (h *server) writePeriod(r *http.Request, src string, promote bool) (*Entity, error) {
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
	err = h.s.Do(r.Context(), src, func(t *core.Tx) error {
		if promote {
			return t.PromotePeriod(id, v.Get("version"), boundary(v.Get("start_boundary")), boundary(v.Get("end_boundary")))
		}
		return t.EditPeriod(id, v.Get("version"), boundary(v.Get("start_boundary")), boundary(v.Get("end_boundary")))
	})
	if err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), id)
}
