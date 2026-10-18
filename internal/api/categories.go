package api

import (
	"net/http"
	"strconv"

	"lifelog/internal/api/siren"
)

// filePages is file-pages: every page named is filed in the category page, in one write (D26). The answer is the
// class filed: the category, whether it was made, the ids, and how many links were added and removed (with from, how many were removed from it), with an item
// link to each page. A browser form returns to its list (back), with the sentence as its feedback.
func (h *server) filePages(r *http.Request, act spec, src string) (*siren.Entity, error) {
	v, err := fieldsOf(r, act)
	if err != nil {
		return nil, err
	}
	ids, err := idsOf(v["ids"])
	if err != nil {
		return nil, err
	}
	from := v.Get("from")
	move, err := flag(v, "move")
	if err != nil {
		return nil, err
	}
	f, err := h.s.FilePages(r.Context(), src, ids, v.Get("category"), from, move)
	if err != nil {
		return nil, err
	}
	e := &siren.Entity{Class: []string{"filed"}, Title: "Filed",
		Properties: map[string]any{"category": f.Category, "created": f.Created, "ids": ids, "count": len(ids),
			"added": f.Added, "moved": f.Moved, "removed": f.Removed},
		Links: []siren.Link{link("self", siren.PageHref(f.Category.ID), f.Category.Title), link("category", siren.PageHref(f.Category.ID), f.Category.Title),
			link("index", "/", "Home")}}
	for _, id := range ids {
		e.Entities = append(e.Entities, link("item", siren.PageHref(id), strconv.FormatInt(id, 10)))
	}
	if from != "" {
		e.Result = "Removed " + strconv.Itoa(f.Removed) + " from " + f.Category.Title + "."
		return e, nil
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
