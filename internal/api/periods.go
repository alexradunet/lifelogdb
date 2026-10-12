package api

import (
	"net/http"
	"net/url"
)

// boundary is a period's boundary as a form sends it: "" is unknown (nil).
func boundary(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// periods lists the recorded periods (docs/decisions/D22-events.md); ?day= selects the ones a day falls in, ?as_of= the
// ones known at a day.
func (h *server) periods(r *http.Request) (*Entity, error) {
	q := r.URL.Query()
	deleted, err := flag(q, "include_deleted")
	if err != nil {
		return nil, err
	}
	ps, err := h.s.LifePeriods(r.Context(), q.Get("day"), q.Get("as_of"), deleted)
	if err != nil {
		return nil, err
	}
	self := url.Values{}
	for _, k := range []string{"day", "as_of"} {
		if q.Get(k) != "" {
			self.Set(k, q.Get(k))
		}
	}
	if deleted {
		self.Set("include_deleted", "1")
	}
	href := "/periods"
	if len(self) > 0 {
		href += "?" + self.Encode()
	}
	e := &Entity{Class: []string{"periods"}, Title: "Recorded periods",
		Properties: map[string]any{"periods": ps, "day": q.Get("day"), "as_of": q.Get("as_of"), "membership_basis": "supplied calendar day, not physical interval overlap", "include_deleted": deleted},
		Links:      []Link{link("self", href, "Periods"), link("index", "/", "Home")}, Actions: []Action{action("create-period", nil, nil)}}
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
	if err := required(v, "title"); err != nil {
		return nil, err
	}
	id, err := h.s.CreatePeriod(r.Context(), src, v.Get("title"), v.Get("body"), boundary(v.Get("start_boundary")), boundary(v.Get("end_boundary")))
	if err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), id)
}

// writePeriod is edit-period, and promote-period when promote is set: both take the page's version and its boundaries.
func (h *server) writePeriod(promote bool) func(*http.Request, string) (*Entity, error) {
	return func(r *http.Request, src string) (*Entity, error) {
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
		start, end := boundary(v.Get("start_boundary")), boundary(v.Get("end_boundary"))
		if promote {
			err = h.s.PromotePeriod(r.Context(), src, id, v.Get("version"), start, end)
		} else {
			err = h.s.EditPeriod(r.Context(), src, id, v.Get("version"), start, end)
		}
		if err != nil {
			return nil, err
		}
		return h.pageEntity(r.Context(), id)
	}
}
