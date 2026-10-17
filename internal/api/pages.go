package api

// The pages: a page by id or title, the search, and the writes on a page.

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"lifelog/internal/api/siren"
	"lifelog/internal/core"
)

func (h *server) find(r *http.Request, act spec, _ string) (*siren.Entity, error) {
	v, err := fieldsOf(r, act)
	if err != nil {
		return nil, err
	}
	title := v.Get("title")
	id, err := h.s.PageID(r.Context(), title)
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, core.NotFound("no page titled %s", title)
	}
	return nil, redirect{siren.PageHref(id)}
}

// randomPage answers with the address of a thing of the kind picked at random (docs/plans/130-the-explore-pass.md,
// part 5); the kind is page when the request names none.
func (h *server) randomPage(r *http.Request, act spec, _ string) (*siren.Entity, error) {
	v, err := fieldsOf(r, act)
	if err != nil {
		return nil, err
	}
	kind := v.Get("kind")
	if kind == "" {
		kind = "page"
	}
	id, err := h.s.RandomThing(r.Context(), kind)
	if err != nil {
		return nil, err
	}
	return nil, redirect{siren.PageHref(id)}
}

func (h *server) page(r *http.Request) (*siren.Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	p, e, err := h.loadPage(r.Context(), id)
	if err != nil {
		return nil, err
	}
	// In a browser, the page of a live metric is its metric's one page, which carries the page's text, edges and
	// actions (docs/plans/100-one-page-a-metric.md); a receipt of feedback goes with it. JSON keeps the page.
	if wantsHTML(r) && p.Type == "metric" && p.DeletedAt == "" {
		return nil, h.redirectKeepingFeedback(r, siren.MetricHref(p.Title))
	}
	return e, nil
}

func (h *server) pageEntity(ctx context.Context, id int64) (*siren.Entity, error) {
	_, e, err := h.loadPage(ctx, id)
	return e, err
}

// loadPage is the page with that id, and the entity that serves it: the page is the properties of the entity.
func (h *server) loadPage(ctx context.Context, id int64) (*core.Page, *siren.Entity, error) {
	p, err := h.s.PageByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	e := &siren.Entity{Class: []string{p.Type}, Title: p.Title, Properties: p,
		Links: []siren.Link{link("self", siren.PageHref(id), p.Title), link("index", "/", "Home")}}
	if p.IsDayPage {
		e.Class = append(e.Class, "day-page")
		e.Links = append(e.Links, link("day", siren.DayHref(p.Day), "Day view"))
	}
	if p.File != nil && p.File.Preview {
		e.Links = append(e.Links, link("preview", siren.PageHref(id)+"/preview", "Picture"))
	}
	e.Links = append(e.Links, coverLinks(p)...)
	for _, l := range p.Out {
		e.Entities = append(e.Entities, siren.Link{Rel: []string{l.Kind}, Href: siren.PageHref(l.ID), Title: l.Title, Class: []string{l.Type}})
	}
	// The categories, read once: the options of file-pages, and which of the pages filed here are categories
	// themselves (docs/plans/115-categories-to-browse.md).
	var cats []core.Category
	if !p.IsDayPage {
		if cats, err = h.s.Categories(ctx); err != nil {
			return nil, nil, err
		}
	}
	isCategory := func(id int64) bool {
		return slices.ContainsFunc(cats, func(c core.Category) bool { return c.ID == id })
	}
	for _, l := range p.In {
		class := []string{l.Type}
		if l.Kind == "part-of" && isCategory(l.ID) {
			class = append(class, "category")
		}
		e.Entities = append(e.Entities, siren.Link{Rel: []string{"backlink", l.Kind}, Href: siren.PageHref(l.ID), Title: l.Title, Class: class})
	}
	var kinds []string
	if p.DeletedAt != "" {
		e.Class = append(e.Class, "deleted")
	} else if kinds, err = h.linkKinds(ctx, p.Type, p.IsDayPage); err != nil {
		return nil, nil, err
	}
	e.Actions = pageActions(p, kinds, cats)
	return p, e, nil
}

// pageActions are the actions a page offers, by its type, its state and its links. kinds are the link kinds it may
// start (linkKinds), and cats are the categories that file-pages offers. A tombstoned page offers revive alone. A
// page that something is part-of is a category page: a new page is made and filed there in one write.
func pageActions(p *core.Page, kinds []string, cats []core.Category) []siren.Action {
	ids := idParam(p.ID)
	if p.DeletedAt != "" {
		return []siren.Action{action("revive", ids, nil)}
	}
	var out []siren.Action
	if slices.ContainsFunc(p.In, func(l core.Edge) bool { return l.Kind == "part-of" }) {
		out = append(out, action("create-page", nil, map[string]any{"category": p.Title}))
	}
	out = append(out, saveBodyAction(p.ID, p.Body, p.Version))
	if !p.IsDayPage {
		if p.Type == "page" {
			out = append(out, action("promote", ids, nil))
		}
		out = append(out, action("rename", ids, nil))
	}
	if p.Type != "file" {
		out = append(out, action("set-cover", ids, nil))
	}
	if len(kinds) > 0 {
		out = append(out, withOptions(action("link", ids, nil), "kind", kinds))
		out = append(out, withOptions(action("unlink", ids, nil), "kind", kinds))
	}
	if slices.Contains(kinds, "part-of") { // its categories, as badges (docs/plans/114-one-way-to-do-a-thing.md)
		var titles []string
		for _, c := range cats {
			if c.ID != p.ID {
				titles = append(titles, c.Title)
			}
		}
		out = append(out, withOptions(action("file-pages", nil, map[string]any{"ids": strconv.FormatInt(p.ID, 10), "move": "0"}), "category", titles))
	}
	return append(out, action("tombstone", ids, nil))
}

// coverLinks are the links of a page's cover: cover, its picture, when it has one; choose-cover, the pictures it may
// take, when it may take one (a live page that is not a file).
func coverLinks(p *core.Page) []siren.Link {
	var out []siren.Link
	if p.Cover != nil {
		out = append(out, link("cover", siren.PageHref(p.Cover.ID)+"/preview", p.Cover.Title))
	}
	if p.Type != "file" && p.DeletedAt == "" {
		out = append(out, link("choose-cover", siren.PageHref(p.ID)+"/cover", "Set cover photo"))
	}
	return out
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

// searchProps are the properties of the search answer: the words, their order, the pages whose title starts with
// them, and a page of the hits.
type searchProps struct {
	pageProps
	Q      string            `json:"q"`
	Order  string            `json:"order"`
	Titles []core.TitleMatch `json:"titles"`
	Hits   []core.Hit        `json:"hits"`
}

// searchHref is the address of the search for these words in this order. The best match is the default, so its
// address has no order.
func searchHref(q, order string) string {
	v := url.Values{"q": {q}}
	if order != core.SearchBest {
		v.Set("order", order)
	}
	return "/search?" + v.Encode()
}

func (h *server) search(r *http.Request, _ spec, _ string) (*siren.Entity, error) {
	q := r.URL.Query().Get("q")
	order, err := core.SearchOrderOf(r.URL.Query().Get("order"))
	if err != nil {
		return nil, err
	}
	e := &siren.Entity{Class: []string{"search"}, Title: "Search",
		Links:   []siren.Link{link("self", searchHref(q, order), "Search"), link("index", "/", "Home")},
		Actions: []siren.Action{action("search", nil, map[string]any{"q": q, "order": order})}}
	if strings.TrimSpace(q) == "" { // a q of only blanks has no words: it answers as the empty search does
		return e, nil
	}
	limit, offset, err := pageOf(r, 50)
	if err != nil {
		return nil, err
	}
	// The pages whose title starts with the words come first, as the names after [[ once did: the search dialog of
	// the browser is the one way to a page by its title (docs/plans/108-the-browser-made-current.md). A page is in one
	// group only, so the hits leave these out.
	titles, err := h.s.TitlesByPrefix(r.Context(), q, 8)
	if err != nil {
		return nil, err
	}
	omit := make([]int64, len(titles))
	for i, m := range titles {
		omit[i] = m.PageID
	}
	pageQuery := url.Values{"q": {q}}
	other, otherTitle := core.SearchNewest, "Newest first"
	if order != core.SearchBest {
		pageQuery.Set("order", order)
		other, otherTitle = core.SearchBest, "Best match"
	}
	hits, props, err := readPage(e, "/search", pageQuery, limit, offset, func(limit, offset int) ([]core.Hit, error) {
		return h.s.SearchWith(r.Context(), q, core.SearchOptions{Order: order, Omit: omit}, limit, offset)
	})
	if err != nil {
		return nil, err
	}
	e.Links = append(e.Links, link("order", searchHref(q, other), otherTitle))
	e.Properties = searchProps{pageProps: props, Q: q, Order: order, Titles: titles, Hits: hits}
	for _, x := range hits {
		e.Entities = append(e.Entities, link("item", siren.PageHref(x.ID), x.Title))
	}
	return e, nil
}

func (h *server) createPage(r *http.Request, act spec, src string) (*siren.Entity, error) {
	v, err := fieldsOf(r, act)
	if err != nil {
		return nil, err
	}
	id, sync, err := h.s.CreatePage(r.Context(), src, v.Get("title"), v.Get("body"), v.Get("category"))
	if err != nil {
		return nil, err
	}
	return withResult(h.pageEntity(r.Context(), id))(sync)
}

func (h *server) saveBody(r *http.Request, act spec, src string) (*siren.Entity, error) {
	id, v, err := idForm(r, act)
	if err != nil {
		return nil, err
	}
	sync, err := h.s.SaveBody(r.Context(), src, id, v.Get("body"), v.Get("version"))
	if err != nil {
		return nil, err
	}
	return withResult(h.pageEntity(r.Context(), id))(sync)
}

func (h *server) createPerson(r *http.Request, act spec, src string) (*siren.Entity, error) {
	v, err := fieldsOf(r, act)
	if err != nil {
		return nil, err
	}
	id, err := h.s.CreatePerson(r.Context(), src, v.Get("title"), v.Get("name"), h.dayNamed(v.Get("birth_day")), h.dayNamed(v.Get("death_day")), v.Get("category"))
	if err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), id)
}

func (h *server) createPlace(r *http.Request, act spec, src string) (*siren.Entity, error) {
	v, err := fieldsOf(r, act)
	if err != nil {
		return nil, err
	}
	id, err := h.s.CreatePlace(r.Context(), src, v.Get("title"), v.Get("category"))
	if err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), id)
}

func (h *server) promote(r *http.Request, act spec, src string) (*siren.Entity, error) {
	id, v, err := idForm(r, act)
	if err != nil {
		return nil, err
	}
	if err := h.s.Promote(r.Context(), src, id, v.Get("to"), v.Get("name")); err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), id)
}

func (h *server) tombstone(r *http.Request, _ spec, src string) (*siren.Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	if err := h.s.Tombstone(r.Context(), src, id); err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), id)
}

func (h *server) revive(r *http.Request, _ spec, src string) (*siren.Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	if err := h.s.Revive(r.Context(), src, id); err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), id)
}

// linkEnds resolves the page of the path and the page titled `to`, for the action act (link or unlink).
func (h *server) linkEnds(r *http.Request, act spec) (from, to int64, v url.Values, err error) {
	if from, v, err = idForm(r, act); err != nil {
		return
	}
	if to, err = h.s.PageID(r.Context(), v.Get("to")); err == nil && to == 0 {
		err = core.NotFound("no page titled %s: create it first", v.Get("to"))
	}
	return
}

func (h *server) linkPages(r *http.Request, act spec, src string) (*siren.Entity, error) {
	from, to, v, err := h.linkEnds(r, act)
	if err != nil {
		return nil, err
	}
	if err := h.s.Link(r.Context(), src, from, to, v.Get("kind"), v.Get("note")); err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), from)
}

func (h *server) unlinkPages(r *http.Request, act spec, src string) (*siren.Entity, error) {
	from, to, v, err := h.linkEnds(r, act)
	if err != nil {
		return nil, err
	}
	if err := h.s.Unlink(r.Context(), src, from, to, v.Get("kind")); err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), from)
}

func (h *server) rename(r *http.Request, act spec, src string) (*siren.Entity, error) {
	id, v, err := idForm(r, act)
	if err != nil {
		return nil, err
	}
	to, err := h.s.Rename(r.Context(), src, id, v.Get("title"))
	if err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), to)
}
