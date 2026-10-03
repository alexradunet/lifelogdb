package core

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Categories file metrics in a tree whose parents never change (D26, docs/cookbook/metrics-by-category.md).
// A category is addressed by its path from the top: "biomarkers/lipids".

// categoryPaths is every category with its path; the path is computed, never stored.
const categoryPaths = `WITH RECURSIVE tree(id, path) AS (
  SELECT id, name FROM metric_categories WHERE parent_id IS NULL
  UNION ALL
  SELECT c.id, t.path || '/' || c.name FROM metric_categories c JOIN tree t ON c.parent_id = t.id
)`

// Category is a registered category: its path, and the owner's note.
type Category struct {
	Path string `json:"path"`
	Note string `json:"note,omitempty"`
}

func (s *Store) Categories(ctx context.Context) ([]Category, error) {
	rows, err := s.DB.R.QueryContext(ctx, categoryPaths+`
		SELECT t.path, coalesce(c.note, '') FROM tree t JOIN metric_categories c ON c.id = t.id ORDER BY t.path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.Path, &c.Note); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// splitPath checks a category path: snake_case names joined by '/'.
func splitPath(path string) ([]string, error) {
	names := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	for _, n := range names {
		if !metricName.MatchString(n) {
			return nil, invalid("category path %q: lowercase snake_case names (a-z 0-9 _) joined by /", path)
		}
	}
	return names, nil
}

// RegisterCategory registers every category of path that is missing, top first; note goes to the last one when
// it is new. A category that exists under another parent is refused: a parent never changes
// (metric_categories_parent_fixed), so the path names another tree.
func (t *Tx) RegisterCategory(path, note string) (added bool, err error) {
	names, err := splitPath(path)
	if err != nil {
		return false, err
	}
	var parent sql.NullInt64
	for i, name := range names {
		var id int64
		var have sql.NullInt64
		err := t.tx.QueryRow(`SELECT id, parent_id FROM metric_categories WHERE name = ?`, name).Scan(&id, &have)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			n := ""
			if i == len(names)-1 {
				n = note
			}
			if err := t.tx.QueryRow(`INSERT INTO metric_categories(name, parent_id, note) VALUES (?, ?, ?) RETURNING id`,
				name, parent, nullIfEmpty(n)).Scan(&id); err != nil {
				return false, err
			}
			added = true
		case err != nil:
			return false, err
		case have != parent:
			return false, conflict("category %s exists under another parent: a parent never changes; register the path it has, or a new category", name)
		}
		parent = sql.NullInt64{Int64: id, Valid: true}
	}
	return added, nil
}

// categoryID is the id of the category at path, which must be registered.
func (t *Tx) categoryID(path string) (int64, error) {
	names, err := splitPath(path)
	if err != nil {
		return 0, err
	}
	var parent sql.NullInt64
	for _, name := range names {
		var id int64
		err := t.tx.QueryRow(`SELECT id FROM metric_categories WHERE name = ? AND parent_id IS ?`, name, parent).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, notFound("no category %q: the owner registers categories", path)
		}
		if err != nil {
			return 0, err
		}
		parent = sql.NullInt64{Int64: id, Valid: true}
	}
	return parent.Int64, nil
}

// FileMetric files a metric in the registered category at path; "" takes it out of every category.
func (t *Tx) FileMetric(metric, path string) (changed bool, err error) {
	id, err := t.metricID(metric)
	if err != nil {
		return false, err
	}
	var cat any
	if strings.TrimSpace(path) != "" {
		c, err := t.categoryID(path)
		if err != nil {
			return false, err
		}
		cat = c
	}
	res, err := t.tx.Exec(`UPDATE metrics SET category_id = ? WHERE id = ? AND category_id IS NOT ?`, cat, id, cat)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (s *Store) RegisterCategory(ctx context.Context, source, path, note string) (added bool, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) { added, e = t.RegisterCategory(path, note); return })
	return
}

func (s *Store) FileMetric(ctx context.Context, source, metric, path string) (changed bool, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) { changed, e = t.FileMetric(metric, path); return })
	return
}
