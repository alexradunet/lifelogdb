package api

// The pages: a page by id or title, the search, and the writes on a page.

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"

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
	for _, l := range p.Out {
		e.Entities = append(e.Entities, Link{Rel: []string{l.Kind}, Href: pageHref(l.ID), Title: l.Title, Class: []string{l.Type}})
	}
	// The categories, read once: the options of file-pages, and which of the pages filed here are categories
	// themselves (docs/plans/115-categories-to-browse.md).
	var cats []core.Category
	if !p.IsDayPage {
		if cats, err = h.s.Categories(ctx); err != nil {
			return nil, err
		}
	}
	isCategory := func(id int64) bool {
		return slices.ContainsFunc(cats, func(c core.Category) bool { return c.ID == id })
	}
	filed := 0
	for _, l := range p.In {
		class := []string{l.Type}
		if l.Kind == "part-of" {
			filed++
			if isCategory(l.ID) {
				class = append(class, "category")
			}
		}
		e.Entities = append(e.Entities, Link{Rel: []string{"backlink", l.Kind}, Href: pageHref(l.ID), Title: l.Title, Class: class})
	}
	if filed > 0 && p.DeletedAt == "" { // a category page: a new page is made and filed here in one write
		e.Actions = append(e.Actions, action("create-page", nil, map[string]any{"category": p.Title}))
	}
	if p.DeletedAt != "" {
		e.Class = append(e.Class, "deleted")
		e.Actions = []Action{action("revive", ids, nil)}
		return e, nil
	}
	e.Actions = append(e.Actions, saveBodyAction(id, p.Body, p.Version))
	if !p.IsDayPage {
		if p.Type == "page" {
			e.Actions = append(e.Actions, action("promote", ids, nil))
		}
		e.Actions = append(e.Actions, action("rename", ids, nil))
	}
	if p.Type == "person" {
		e.Actions = append(e.Actions, action("set-picture", ids, nil))
	}
	kinds, err := h.linkKinds(ctx, p.Type, p.IsDayPage)
	if err != nil {
		return nil, err
	}
	if len(kinds) > 0 {
		e.Actions = append(e.Actions, withOptions(action("link", ids, nil), "kind", kinds))
		e.Actions = append(e.Actions, withOptions(action("unlink", ids, nil), "kind", kinds))
	}
	if slices.Contains(kinds, "part-of") { // its categories, as badges (docs/plans/114-one-way-to-do-a-thing.md)
		var titles []string
		for _, c := range cats {
			if c.ID != p.ID {
				titles = append(titles, c.Title)
			}
		}
		e.Actions = append(e.Actions, withOptions(action("file-pages", nil, map[string]any{"ids": strconv.FormatInt(p.ID, 10), "move": "0"}), "category", titles))
	}
	e.Actions = append(e.Actions, action("tombstone", ids, nil))
	return e, nil
}

// linkKinds are the kinds a page of this type may start (link_kinds.from_types), minus the two the save
// contract and renames own; `at` only from a day page (D16), `part-of` never from one (D26). links_endpoint_types
// holds both rules: these are only the options a form offers.
func (h *server) linkKinds(ctx context.Context, typ string, dayPage bool) ([]string, error) {
	kinds, err := h.s.LinkKindsFrom(ctx, typ)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, k := range kinds {
		if k == "at" && !dayPage || k == "part-of" && dayPage {
			continue
		}
		out = append(out, k)
	}
	return out, nil
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
	// The pages whose title starts with the words come first, as the names after [[ once did: the search dialog of
	// the browser is the one way to a page by its title (docs/plans/108-the-browser-made-current.md).
	titles, err := h.s.TitlesByPrefix(r.Context(), q, 8)
	if err != nil {
		return nil, err
	}
	e.Properties = map[string]any{"q": q, "titles": titles}
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
	id, sync, err := h.s.CreatePage(r.Context(), src, v.Get("title"), v.Get("body"), v.Get("category"))
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
	id, err := h.s.CreatePerson(r.Context(), src, v.Get("title"), v.Get("name"), v.Get("birth_day"), v.Get("death_day"), v.Get("category"))
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
	id, err := h.s.CreatePlace(r.Context(), src, v.Get("title"), v.Get("category"))
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
