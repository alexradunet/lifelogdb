package core

import (
	"context"
	"sort"
	"strings"

	"lifelog/internal/text"
)

// A category is a page, and anything is filed in it by a part-of link to that page; a category belongs to another
// by the same link (D26, docs/cookbook/metrics-by-category.md). Nothing keeps those links a tree, so the writer
// draws one: each category under one parent (the first by title), and a page it has seen ends the walk.

// Category is a category page and the path the writer draws it at: the titles from the top, joined by '/'.
type Category struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Path  string `json:"path"`
}

// categories are the live pages something is part-of, by id, each with its path.
func (s *Store) categories(ctx context.Context) (map[int64]*Category, error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT l.from_id, p.id, p_name.title, p.preferred_name_key
		  FROM links l JOIN entities p ON p.id = l.to_id JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
		  JOIN entities e ON e.id = p.id AND e.deleted_at IS NULL
		 WHERE l.kind = 'part-of'
		 ORDER BY p.preferred_name_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cats := map[int64]*Category{}
	parent := map[int64]int64{} // the first category a page is part-of, by title
	for rows.Next() {
		var from, id int64
		var title, key string
		if err := rows.Scan(&from, &id, &title, &key); err != nil {
			return nil, err
		}
		cats[id] = &Category{ID: id, Title: title}
		if _, ok := parent[from]; !ok {
			parent[from] = id
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for id, c := range cats {
		titles := []string{c.Title}
		seen := map[int64]bool{id: true}
		for p, ok := parent[id]; ok && !seen[p]; p, ok = parent[p] {
			seen[p] = true
			titles = append([]string{cats[p].Title}, titles...)
		}
		c.Path = strings.Join(titles, "/")
	}
	return cats, nil
}

// Categories are the category pages, by path.
func (s *Store) Categories(ctx context.Context) ([]Category, error) {
	cats, err := s.categories(ctx)
	if err != nil {
		return nil, err
	}
	out := []Category{}
	for _, c := range cats {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return text.TitleKey(out[i].Path) < text.TitleKey(out[j].Path) })
	return out, nil
}

// File files the entity id in the category at path, the titles of its pages from the top joined by '/'
// (Biomarkers/Iron): a page of the path that is missing is made, a plain page with no text, and each is linked
// part-of the one above it, then id to the last. added says whether id was not filed there before.
func (t *Tx) File(id int64, path string) (added bool, err error) {
	var ids []int64
	for _, title := range strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/") {
		title = strings.TrimSpace(title)
		if !text.ValidTitle(title) {
			return false, invalid("category %q in %q is not a valid title (docs/contract/titles-and-wikilinks.md)", title, path)
		}
		p, err := t.Lookup(title)
		if err != nil {
			return false, err
		}
		var c int64
		switch {
		case p == nil && IsDay(title):
			return false, conflict("%s is a day: a category is a plain page (D26)", title)
		case p == nil:
			if c, _, err = t.insertPage("page", title, text.TitleKey(title), nil, "", ""); err != nil {
				return false, err
			}
		case p.Type != "page" || p.DayPage || p.Deleted:
			return false, conflict("%s is not a live plain page: a category is one (D26)", p.Title)
		default:
			c = p.ID
		}
		if len(ids) > 0 {
			if _, err := t.Link(c, ids[len(ids)-1], "part-of", ""); err != nil {
				return false, err
			}
		}
		ids = append(ids, c)
	}
	return t.Link(id, ids[len(ids)-1], "part-of", "")
}
