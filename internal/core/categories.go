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

// Category is a category page and the path the writer draws it at: the titles from the top, joined by '/'. Count
// is how many live things are filed directly in it, the categories under it included: the rows of All in that
// category in Browse (docs/plans/117-one-browse-view.md). A day page is never filed (links_endpoint_types, D26).
type Category struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Path  string `json:"path"`
	Count int    `json:"count,omitempty"`
}

// categories are the live pages something is part-of, by id, each with its path and its count. filed is the ids of
// the categories that each page is part-of, by the page's id, in the order of their titles. It is one read of the
// part-of links for both.
func (s *Store) categories(ctx context.Context) (cats map[int64]*Category, filed map[int64][]int64, err error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT l.from_id, p.id, p_name.title, f.deleted_at IS NULL
		  FROM links l JOIN entities p ON p.id = l.to_id JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key AND p.deleted_at IS NULL
		  JOIN entities f ON f.id = l.from_id
		 WHERE l.kind = 'part-of'
		 ORDER BY p.preferred_name_key`)
	cats, filed = map[int64]*Category{}, map[int64][]int64{}
	var links []categoryLink
	var l categoryLink
	var live bool
	if err := each(rows, err, []any{&l.from, &l.to, &l.title, &live}, func() {
		if cats[l.to] == nil {
			cats[l.to] = &Category{ID: l.to, Title: l.title}
		}
		if live {
			cats[l.to].Count++
		}
		filed[l.from] = append(filed[l.from], l.to)
		links = append(links, l)
	}); err != nil {
		return nil, nil, err
	}
	for id, path := range categoryPaths(links) {
		cats[id].Path = path
	}
	return cats, filed, nil
}

// categoryLink is a part-of link as the categories read it: from a page to a live category, and the category's title.
type categoryLink struct {
	from, to int64
	title    string
}

// categoryPaths are the paths the writer draws the categories at, by id: the titles from the top, joined by '/'.
// links are the part-of links to live categories, in the order of the categories' titles. Each category is drawn
// under one parent, the first by title; a category that the walk up has seen ends it, so a cycle of part-of links
// draws each category once.
func categoryPaths(links []categoryLink) map[int64]string {
	titles, parent := map[int64]string{}, map[int64]int64{}
	for _, l := range links {
		titles[l.to] = l.title
		if _, ok := parent[l.from]; !ok {
			parent[l.from] = l.to
		}
	}
	paths := make(map[int64]string, len(titles))
	for id, title := range titles {
		path := []string{title}
		seen := map[int64]bool{id: true}
		for p, ok := parent[id]; ok && !seen[p]; p, ok = parent[p] {
			seen[p] = true
			path = append([]string{titles[p]}, path...)
		}
		paths[id] = strings.Join(path, "/")
	}
	return paths
}

// Categories are the category pages, by path.
func (s *Store) Categories(ctx context.Context) ([]Category, error) {
	cats, _, err := s.categories(ctx)
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

// Filed is what FilePages did: the category page, whether it was made, the links added and the other part-of links
// removed.
type Filed struct {
	Category Category `json:"category"`
	Created  bool     `json:"created"`
	Added    int      `json:"added"`
	Moved    int      `json:"moved"`
}

// filePages is Store.FilePages in t.
func (t *Tx) filePages(ids []int64, category string, move bool) (Filed, error) {
	if err := checkTitle("category", category); err != nil {
		return Filed{}, err
	}
	// a missing category is made as an empty page, like the target of a wikilink, and like it with no day, so it is
	// not an entry of the day it was made on
	var cat int64
	have, err := t.lookup(category)
	switch {
	case err != nil:
		return Filed{}, err
	case have != nil:
		cat = have.ID
	default:
		if cat, _, err = t.insertPage("page", category, text.TitleKey(category), nil, "", ""); err != nil {
			return Filed{}, err
		}
	}
	p, err := t.livePage(cat)
	if err != nil {
		return Filed{}, err
	}
	f := Filed{Category: Category{ID: cat, Title: p.Title}, Created: have == nil}
	for _, id := range ids {
		if _, err := t.livePage(id); err != nil {
			return Filed{}, err
		}
		if move {
			res, err := t.tx.ExecContext(t.ctx, `DELETE FROM links WHERE from_id = ? AND kind = 'part-of' AND to_id <> ?`, id, cat)
			if err != nil {
				return Filed{}, err
			}
			n, _ := res.RowsAffected()
			f.Moved += int(n)
		}
		added, err := t.link(id, cat, "part-of", "")
		if err != nil {
			return Filed{}, err
		}
		if added {
			f.Added++
		}
	}
	return f, nil
}

// FilePages files every page named in the category page titled category, in one write (D26): a part-of link from
// each. A missing category is made as an empty page with no day. With move, every other
// part-of link of those pages is removed first. A page or a category that is not live is refused as livePage says;
// that writes nothing. What the links registry refuses (a day page as a category, or filed in one) is
// links_endpoint_types.
func (s *Store) FilePages(ctx context.Context, source string, ids []int64, category string, move bool) (Filed, error) {
	return alone(s, ctx, source, func(t *Tx) (Filed, error) { return t.filePages(ids, category, move) })
}
