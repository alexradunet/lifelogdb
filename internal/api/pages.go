package api

// The pages: a page by id or title, the people, places, files and ghosts, the search, and the writes on a page.

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"lifelog/internal/core"
)

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
	e, err := h.pageEntity(r.Context(), id)
	if err != nil {
		return nil, err
	}
	// In a browser, the page of a live metric is its metric's one page, which carries the page's text, edges and
	// actions (docs/plans/100-one-page-a-metric.md); a receipt of feedback goes with it. JSON keeps the page.
	if p := e.Properties.(*core.Page); wantsHTML(r) && p.Type == "metric" && p.DeletedAt == "" {
		to := metricHref(p.Title)
		if t := r.URL.Query().Get("feedback"); t != "" && h.feedback.move(t, r.URL.Path, to) {
			to += "?" + url.Values{"feedback": {t}}.Encode()
		}
		return nil, redirect{to}
	}
	// In a browser, a conversation's page is its chat page (docs/plans/103-the-conversation-on-the-screen.md);
	// ?view=page shows the page itself, to edit its text.
	if p := e.Properties.(*core.Page); wantsHTML(r) && p.DeletedAt == "" && isConversation(p) && r.URL.Query().Get("view") != "page" {
		return nil, h.conversationRedirect(r, id)
	}
	return e, nil
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

// namedRow is a row of the people, places and files lists: the page and how often the log mentions it.
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
		if typ == "person" || typ == "place" || typ == "file" { // these rows say how often the log mentions them
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

func (h *server) rename(r *http.Request, src string) (*Entity, error) {
	id, v, err := idForm(r, "title")
	if err != nil {
		return nil, err
	}
	to, err := h.s.Rename(r.Context(), src, id, v.Get("title"))
	if err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), to)
}

// locate gives a place its point (docs/cookbook/place-of-a-photo.md): the owner's answer, or a correction.
func (h *server) locate(r *http.Request, src string) (*Entity, error) {
	id, v, err := idForm(r, "lat", "lon", "radius_m")
	if err != nil {
		return nil, err
	}
	var p core.Point
	for name, to := range map[string]*float64{"lat": &p.Lat, "lon": &p.Lon} {
		f, err := number(v, name)
		if err != nil {
			return nil, err
		}
		*to = *f
	}
	if p.RadiusM, err = radius("radius_m", v.Get("radius_m")); err != nil {
		return nil, err
	}
	linkDays, err := flag(v, "link_days")
	if err != nil {
		return nil, err
	}
	p.LinkDays = linkDays || v.Get("link_days") == "" // the default is to link the days
	if err := h.s.Locate(r.Context(), src, id, p); err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), id)
}
