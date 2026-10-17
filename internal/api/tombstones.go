package api

import (
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"lifelog/internal/core"
)

// maxTombstoneIDs bounds one tombstone-pages: a list page shows 200 rows at most.
const maxTombstoneIDs = 500

// idsOf reads the field ids of a bulk action: every value of the field (a browser sends one per checked row),
// each split on commas and spaces (a CLI or an agent sends one value), each an id once. Nothing is a 422, as is a
// word that is not an id or more than maxTombstoneIDs of them.
func idsOf(values []string) ([]int64, error) {
	var ids []int64
	seen := map[int64]bool{}
	for _, v := range values {
		for _, w := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
			id, err := strconv.ParseInt(w, 10, 64)
			if err != nil || id < 1 {
				return nil, core.Invalid("ids: %q is not an id", w)
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, core.Invalid("missing field ids")
	}
	if len(ids) > maxTombstoneIDs {
		return nil, core.Invalid("ids: at most %d in one action", maxTombstoneIDs)
	}
	return ids, nil
}

// tombstoneMany is tombstone-pages: every id named is tombstoned in one write, or none is. The answer is the class
// tombstoned: the ids, their count, and an item link to each. A browser form returns to its list (back), with the
// count as its feedback.
func (h *server) tombstoneMany(r *http.Request, act spec, src string) (*Entity, error) {
	v, err := fieldsOf(r, act)
	if err != nil {
		return nil, err
	}
	ids, err := idsOf(v["ids"])
	if err != nil {
		return nil, err
	}
	if err := h.s.TombstonePages(r.Context(), src, ids); err != nil {
		return nil, err
	}
	e := &Entity{Class: []string{"tombstoned"}, Title: "Deleted",
		Properties: map[string]any{"ids": ids, "count": len(ids)},
		Links:      []Link{link("self", "/", "Home"), link("index", "/", "Home")}}
	for _, id := range ids {
		e.Entities = append(e.Entities, link("item", PageHref(id), strconv.FormatInt(id, 10)))
	}
	noun := "page"
	if len(ids) != 1 {
		noun = "pages"
	}
	e.Result = "Deleted " + strconv.Itoa(len(ids)) + " " + noun + "."
	return e, nil
}
