package core

import "context"

// The reads of Browse (docs/plans/117-one-browse-view.md): the lists of each kind in one scope, what is filed
// directly in a category, and the count of each kind in the scope.

// BrowseKinds are the kinds Browse lists, in the order of its chips. A file is media when the first part of its type
// is one of MediaKinds, else a document.
var BrowseKinds = []string{"page", "person", "place", "media", "document", "metric"}

// Scope is where a Browse list looks: everything (the zero Scope), the things filed directly in the category page In,
// or the things in no category (Unfiled).
type Scope struct {
	In      int64
	Unfiled bool
}

// where is the condition of the scope on the entity p, with its arguments. A thing is in no category when no part-of
// link starts at it and ends at a live page. A tombstone keeps its links (D11), but the tree does not draw a
// tombstoned category, so what is filed only there is in no category. A plain page is also not in that list when a
// part-of link ends at it: it is a category itself, as the tree draws it.
func (sc Scope) where() (string, []any) {
	switch {
	case sc.In != 0:
		return `EXISTS (SELECT 1 FROM links fl WHERE fl.kind = 'part-of' AND fl.from_id = p.id AND fl.to_id = ?)`, []any{sc.In}
	case sc.Unfiled:
		return `NOT EXISTS (SELECT 1 FROM links fl JOIN entities fc ON fc.id = fl.to_id AND fc.deleted_at IS NULL
		                     WHERE fl.kind = 'part-of' AND fl.from_id = p.id)
		    AND (p.entity_type <> 'page' OR NOT EXISTS (SELECT 1 FROM links fl WHERE fl.kind = 'part-of' AND fl.to_id = p.id))`, nil
	}
	return `1`, nil
}

// plainPage is the condition on p of a live plain page: a page that is not a day page.
const plainPage = `p.entity_type = 'page' AND NOT p.is_journal AND p.deleted_at IS NULL`

// isCategory is the condition on p of a category: a part-of link ends at it, as Categories reads them.
const isCategory = `EXISTS (SELECT 1 FROM links c WHERE c.kind = 'part-of' AND c.to_id = p.id)`

// browseKind is the Browse kind of the entity p, with its file f (a LEFT JOIN): its type, or media or document for a
// file.
const browseKind = `CASE WHEN p.entity_type <> 'file' THEN p.entity_type WHEN ` + mediaKind + ` IN ('image', 'video', 'audio') THEN 'media' ELSE 'document' END`

// PageRow is a live plain page as Browse lists it. Category says that it is a category: a part-of link ends at it.
type PageRow struct {
	ID       int64
	Title    string
	Category bool
}

// PlainPages is one page of the live plain pages in the scope, by title (the folded title, preferred_name_key, as the
// tree orders the categories). A day page is never in it: the Calendar has
// the days.
func (s *Store) PlainPages(ctx context.Context, sc Scope, limit, offset int) ([]PageRow, error) {
	where, args := sc.where()
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT p.id, p_name.title, `+isCategory+`
		  FROM entities p JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
		 WHERE `+plainPage+` AND `+where+`
		 ORDER BY p.preferred_name_key LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PageRow{}
	for rows.Next() {
		var r PageRow
		if err := rows.Scan(&r.ID, &r.Title, &r.Category); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MetricRow is a live metric as Browse lists it: its page's id, its name and its unit ("" for a count or a habit).
type MetricRow struct {
	ID    int64
	Title string
	Unit  string
}

// MetricRows is one page of the live metrics in the scope, by title. The values are on the board of /metrics.
func (s *Store) MetricRows(ctx context.Context, sc Scope, limit, offset int) ([]MetricRow, error) {
	where, args := sc.where()
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT p.id, p_name.title, m.unit
		  FROM metrics m JOIN entities p ON p.id = m.id
		  JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
		 WHERE p.deleted_at IS NULL AND `+where+`
		 ORDER BY p.preferred_name_key LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MetricRow{}
	for rows.Next() {
		var r MetricRow
		if err := rows.Scan(&r.ID, &r.Title, &r.Unit); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// FiledRow is a live thing filed in a category: its id, title and type, its Browse kind (BrowseKinds), whether it
// is a category itself, and the type of a file's original ("" for any other thing).
type FiledRow struct {
	ID       int64
	Title    string
	Type     string
	Kind     string
	Category bool
	MIME     string
}

// FiledIn is one page of every live thing filed directly in the category page with that id: the categories under
// it first, then the rest, each by title. A day page is never in it: links_endpoint_types refuses to file one (D26).
func (s *Store) FiledIn(ctx context.Context, category int64, limit, offset int) ([]FiledRow, error) {
	where, args := Scope{In: category}.where()
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT p.id, p_name.title, p.entity_type, `+browseKind+`, `+isCategory+` AS category, coalesce(f.mime, '')
		  FROM entities p JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
		  LEFT JOIN files f ON f.id = p.id
		 WHERE p.deleted_at IS NULL AND `+where+`
		 ORDER BY category DESC, p.preferred_name_key LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FiledRow{}
	for rows.Next() {
		var r FiledRow
		if err := rows.Scan(&r.ID, &r.Title, &r.Type, &r.Kind, &r.Category, &r.MIME); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// BrowseCounts counts the live things of each Browse kind in the scope, by kind (a kind with none is 0), with "all",
// the sum of the kinds, and "ghosts", the rows of Ghosts (a ghost page is in no category, so its
// count does not change with the scope). A day page is not counted.
func (s *Store) BrowseCounts(ctx context.Context, sc Scope) (map[string]int, error) {
	out := map[string]int{"all": 0}
	for _, k := range BrowseKinds {
		out[k] = 0
	}
	where, args := sc.where()
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT `+browseKind+`, count(*)
		  FROM entities p LEFT JOIN files f ON f.id = p.id
		 WHERE p.deleted_at IS NULL AND NOT p.is_journal AND `+where+`
		 GROUP BY 1`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k], out["all"] = n, out["all"]+n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var ghosts int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM (`+plainGhosts+`)`).Scan(&ghosts); err != nil {
		return nil, err
	}
	out["ghosts"] = ghosts
	return out, nil
}
