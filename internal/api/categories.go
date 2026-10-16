package api

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"lifelog/internal/core"
)

// metricGroup is one section of /metrics: a category page with the metrics filed in it (D26), or the metrics
// filed nowhere. Depth is the category's depth in the drawn tree.
type metricGroup struct {
	Title   string        `json:"title"`
	Path    string        `json:"path,omitempty"`
	Page    int64         `json:"page,omitempty"`
	Depth   int           `json:"depth"`
	Metrics []core.Metric `json:"metrics"`
}

// groupMetrics lists a metric under each category it is filed in, in the order of the tree; the rest last. A category is shown when it or a category under it holds a metric.
func groupMetrics(ms []core.Metric, cs []core.Category) []metricGroup {
	filed := map[string][]core.Metric{}
	var none []core.Metric
	for _, m := range ms {
		switch {
		case len(m.Categories) > 0:
			for _, p := range m.Categories {
				filed[p] = append(filed[p], m)
			}
		default:
			none = append(none, m)
		}
	}
	var out []metricGroup
	for _, c := range cs { // by path: a category comes before the categories under it
		held := false
		for p := range filed {
			if p == c.Path || strings.HasPrefix(p, c.Path+"/") {
				held = true
				break
			}
		}
		if !held {
			continue
		}
		out = append(out, metricGroup{Title: c.Title, Path: c.Path, Page: c.ID, Depth: strings.Count(c.Path, "/"),
			Metrics: append([]core.Metric{}, filed[c.Path]...)})
	}
	if len(none) > 0 {
		out = append(out, metricGroup{Title: "Not filed", Metrics: none})
	}
	return out
}

// metricOf is the live metric that name names, by its name or by an alias its renames kept (D27, as a series is
// read); nil when it names none.
func (h *server) metricOf(ctx context.Context, name string) (*core.Metric, error) {
	id, err := h.s.PageID(ctx, name)
	if err != nil || id == 0 {
		return nil, err
	}
	ms, err := h.s.Metrics(ctx)
	if err != nil {
		return nil, err
	}
	for i := range ms {
		if ms[i].ID == id {
			return &ms[i], nil
		}
	}
	return nil, nil
}

// filePages is file-pages: every page named is filed in the category page, in one write (D26). The answer is the
// class filed: the category, whether it was made, the ids, and how many links were added and removed, with an item
// link to each page. A browser form returns to its list (back), with the sentence as its feedback.
func (h *server) filePages(r *http.Request, act spec, src string) (*Entity, error) {
	v, err := fieldsOf(r, act)
	if err != nil {
		return nil, err
	}
	ids, err := idsOf(v["ids"])
	if err != nil {
		return nil, err
	}
	move, err := flag(v, "move")
	if err != nil {
		return nil, err
	}
	f, err := h.s.FilePages(r.Context(), src, ids, v.Get("category"), move)
	if err != nil {
		return nil, err
	}
	e := &Entity{Class: []string{"filed"}, Title: "Filed",
		Properties: map[string]any{"category": f.Category, "created": f.Created, "ids": ids, "count": len(ids),
			"added": f.Added, "moved": f.Moved},
		Links: []Link{link("self", pageHref(f.Category.ID), f.Category.Title), link("category", pageHref(f.Category.ID), f.Category.Title),
			link("index", "/", "Home")}}
	for _, id := range ids {
		e.Entities = append(e.Entities, link("item", pageHref(id), strconv.FormatInt(id, 10)))
	}
	verb := "Put"
	if move {
		verb = "Moved"
	}
	result := verb + " " + strconv.Itoa(len(ids)) + " in " + f.Category.Title
	if f.Created {
		result += " (a new category)"
	}
	e.Result = result + "."
	return e, nil
}

// filedHere is what a category page lists under Filed here: the sub-categories first, then the rest, each in the
// order of the backlinks (by title).
type filedHere struct {
	Subs, Items []Link
}

// Filed is the Filed here of a page: its part-of backlinks, the categories first; nil when nothing is filed in it.
func (v *view) Filed() *filedHere {
	var f filedHere
	for _, l := range v.Embedded("backlink") {
		if !slices.Contains(l.Rel, "part-of") {
			continue
		}
		if slices.Contains(l.Class, "category") {
			f.Subs = append(f.Subs, l)
		} else {
			f.Items = append(f.Items, l)
		}
	}
	if len(f.Subs)+len(f.Items) == 0 {
		return nil
	}
	return &f
}
