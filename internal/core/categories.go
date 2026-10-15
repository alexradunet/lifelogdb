package core

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"

	"lifelog/internal/text"
)

// A category is a page, and anything is filed in it by a part-of link to that page; a category belongs to another
// by the same link (D26, docs/cookbook/metrics-by-category.md). Nothing keeps those links a tree, so the writer
// draws one: each category under one parent (the first by title), and a page it has seen ends the walk.

// Category is a category page and the path the writer draws it at: the titles from the top, joined by '/'. Count
// is how many live things are filed directly in it, the categories under it included.
type Category struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Path  string `json:"path"`
	Count int    `json:"count,omitempty"`
}

// categories are the live pages something is part-of, by id, each with its path and its count.
func (s *Store) categories(ctx context.Context) (map[int64]*Category, error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT l.from_id, p.id, p_name.title, f.deleted_at IS NULL
		  FROM links l JOIN entities p ON p.id = l.to_id JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key AND p.deleted_at IS NULL
		  JOIN entities f ON f.id = l.from_id
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
		var title string
		var live bool
		if err := rows.Scan(&from, &id, &title, &live); err != nil {
			return nil, err
		}
		if cats[id] == nil {
			cats[id] = &Category{ID: id, Title: title}
		}
		if live {
			cats[id].Count++
		}
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

// Unfiled are the live plain pages in no category that are not categories themselves, by title: what the
// Categories view lists after the tree (docs/plans/115-categories-to-browse.md). A day page is never filed, and a
// metric, a person, a place and a file have lists of their own.
func (s *Store) Unfiled(ctx context.Context, limit, offset int) ([]Edge, error) {
	return s.edges(ctx, `SELECT p.entity_type, p.id, p_name.title, p.entity_type, ''
	                       FROM entities p JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
	                      WHERE p.entity_type = 'page' AND NOT p.is_journal AND p.deleted_at IS NULL
	                        AND NOT EXISTS (SELECT 1 FROM links l WHERE l.kind = 'part-of' AND (l.from_id = p.id OR l.to_id = p.id))
	                      ORDER BY p_name.title LIMIT ? OFFSET ?`, limit, offset)
}

// Filed is what FilePages did: the category page, whether it was made, the links added and the other part-of links
// removed.
type Filed struct {
	Category Category `json:"category"`
	Created  bool     `json:"created"`
	Added    int      `json:"added"`
	Moved    int      `json:"moved"`
}

// FilePages files every page named in the category page titled category, in one write (D26): a part-of link from
// each. A missing category is made as an empty page, as PageForTitle makes a task's project. With move, every other
// part-of link of those pages is removed first. An id no page has is a 404 and a tombstone a 409; either writes
// nothing. What the links registry refuses (a day page as a category) is links_endpoint_types.
func (t *Tx) FilePages(ids []int64, category string, move bool) (Filed, error) {
	if err := checkTitle("category", category); err != nil {
		return Filed{}, err
	}
	cat, created, err := t.PageForTitle(category)
	if err != nil {
		return Filed{}, err
	}
	p, err := t.pageByID(cat)
	if err != nil {
		return Filed{}, err
	}
	if p.Deleted {
		return Filed{}, Conflict("the category %s is deleted: revive it first", p.Title)
	}
	f := Filed{Category: Category{ID: cat, Title: p.Title}, Created: created}
	for _, id := range ids {
		var deleted bool
		err := t.tx.QueryRowContext(t.ctx, `SELECT deleted_at IS NOT NULL FROM entities WHERE id = ?`, id).Scan(&deleted)
		if errors.Is(err, sql.ErrNoRows) {
			return Filed{}, NotFound("no page %d", id)
		}
		if err != nil {
			return Filed{}, err
		}
		if deleted {
			return Filed{}, Conflict("page %d is deleted", id)
		}
		if move {
			res, err := t.tx.ExecContext(t.ctx, `DELETE FROM links WHERE from_id = ? AND kind = 'part-of' AND to_id <> ?`, id, cat)
			if err != nil {
				return Filed{}, err
			}
			n, _ := res.RowsAffected()
			f.Moved += int(n)
		}
		added, err := t.Link(id, cat, "part-of", "")
		if err != nil {
			return Filed{}, err
		}
		if added {
			f.Added++
		}
	}
	return f, nil
}

func (s *Store) FilePages(ctx context.Context, source string, ids []int64, category string, move bool) (f Filed, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) { f, e = t.FilePages(ids, category, move); return })
	if err != nil {
		f = Filed{}
	}
	return
}
