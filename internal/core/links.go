package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"lifelog/internal/text"
)

// Sync is what a body save did to the page's wikilinks (docs/contract/titles-and-wikilinks.md: the UI reports
// skipped targets and tells the owner about a revived page).
type Sync struct {
	Linked  []string `json:"linked"`
	Added   []string `json:"added,omitempty"` // the linked titles the page did not link before this save
	Created []string `json:"created,omitempty"`
	Revived []string `json:"revived,omitempty"`
	Skipped []string `json:"skipped,omitempty"`
}

// syncWikilinks is cookbook/save-a-body.md steps 1-4, after the body is written.
func (t *Tx) syncWikilinks(pageID int64, body string) (Sync, error) {
	var own string
	if err := t.tx.QueryRowContext(t.ctx, `SELECT preferred_name_key FROM entities WHERE id = ?`, pageID).Scan(&own); err != nil {
		return Sync{}, err
	}
	keys, titles, rejected := text.Targets(body, own)
	r := Sync{Linked: []string{}, Skipped: rejected}
	before := map[int64]bool{}
	var to int64
	rows, err := t.tx.QueryContext(t.ctx, `SELECT to_id FROM links WHERE from_id = ? AND kind = 'wikilink'`, pageID)
	if err := each(rows, err, []any{&to}, func() { before[to] = true }); err != nil {
		return Sync{}, err
	}
	ids := []int64{}
	seen := map[int64]bool{pageID: true}
	for i, key := range keys {
		title := titles[i]
		var id int64
		var created, revived bool
		if err := t.savepoint(func() (err error) {
			id, created, revived, err = t.linkTarget(pageID, key, title)
			return err
		}); err != nil {
			r.Skipped = append(r.Skipped, title)
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
		r.Linked = append(r.Linked, title)
		if !before[id] {
			r.Added = append(r.Added, title)
		}
		if created {
			r.Created = append(r.Created, title)
		}
		if revived {
			r.Revived = append(r.Revived, title)
		}
	}
	idsJSON, _ := json.Marshal(ids)
	_, err = t.tx.ExecContext(t.ctx, `DELETE FROM links WHERE from_id = ? AND kind = 'wikilink'
	                       AND to_id NOT IN (SELECT value FROM json_each(?))`, pageID, string(idsJSON))
	return r, err
}

func (t *Tx) linkTarget(pageID int64, key, title string) (id int64, created, revived bool, err error) {
	var deleted sql.NullString
	err = t.tx.QueryRowContext(t.ctx, `SELECT id, deleted_at FROM entities WHERE id = (SELECT entity_id FROM entity_names WHERE name_key = ?)`, key).
		Scan(&id, &deleted)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if id, _, err = t.insertPage("page", title, key, dayOfTitle(title), "", ""); err != nil {
			return
		}
		created = true
	case err != nil:
		return
	case id == pageID:
		return id, false, false, nil
	case deleted.Valid:
		if _, err = t.tx.ExecContext(t.ctx, `UPDATE entities SET deleted_at = NULL WHERE id = ?`, id); err != nil {
			return
		}
		revived = true
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO links(from_id, to_id, kind, created_at, source) VALUES (?, ?, 'wikilink', `+nowSQL+`, ?)
	                    ON CONFLICT(from_id, to_id, kind) DO NOTHING`, pageID, id, t.source)
	return
}

// dayOfTitle is a link target's day: none, except a day page (entities_day_page).
func dayOfTitle(title string) any {
	if IsDay(title) {
		return title
	}
	return nil
}

// link is Store.Link in t; added reports whether the link is new.
func (t *Tx) link(from, to int64, kind, note string) (added bool, err error) {
	if kind == "redirect" {
		return false, Invalid("redirect is not a supported link kind")
	}
	if kind == "wikilink" {
		return false, Invalid("wikilink links are written by saving a body, not directly")
	}
	var one int
	err = t.tx.QueryRowContext(t.ctx, `SELECT 1 FROM entities WHERE id = ?`, from).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, NotFound("no page %d", from) // a missing row is a 404, not the foreign key's 422
	}
	if err != nil {
		return false, err
	}
	res, err := t.tx.ExecContext(t.ctx, `INSERT INTO links(from_id, to_id, kind, note, created_at, source) VALUES (?, ?, ?, ?, `+nowSQL+`, ?)
	                       ON CONFLICT(from_id, to_id, kind) DO NOTHING`, from, to, kind, nullIfEmpty(note), t.source)
	if err != nil {
		return false, refused(err) // the registry's endpoint types refuse the link: a 422, as Do would return it
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// unlink is Store.Unlink in t.
func (t *Tx) unlink(from, to int64, kind string) error {
	if kind == "redirect" {
		return Invalid("redirect is not a supported link kind")
	}
	if kind == "wikilink" {
		return Invalid("wikilink links are removed by saving a body, not directly")
	}
	res, err := t.tx.ExecContext(t.ctx, `DELETE FROM links WHERE from_id = ? AND to_id = ? AND kind = ?`, from, to, kind)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return NotFound("no %s link from %d to %d", kind, from, to)
	}
	return nil
}

// Link adds an edge of a registered kind (D8). wikilink rows belong to the save contract; redirect is unsupported,
// so neither is written here. The endpoint rules, an `at` from a day page among them, are links_endpoint_types. A
// from id that no page has is a 404.
func (s *Store) Link(ctx context.Context, source string, from, to int64, kind, note string) error {
	return s.Do(ctx, source, func(t *Tx) error { _, err := t.link(from, to, kind, note); return err })
}

// Unlink deletes one edge (links rows are the one table whose rows are deleted, D11).
func (s *Store) Unlink(ctx context.Context, source string, from, to int64, kind string) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.unlink(from, to, kind) })
}

// Edge is a link seen from one end; ID and Title are the other end.
type Edge struct {
	Kind  string `json:"kind"`
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Type  string `json:"entity_type"`
	Note  string `json:"note,omitempty"`
}

func (s *Store) edges(ctx context.Context, q string, args ...any) ([]Edge, error) {
	rows, err := s.DB.R.QueryContext(ctx, q, args...)
	return list(rows, err, func(e *Edge) []any { return []any{&e.Kind, &e.ID, &e.Title, &e.Type, &e.Note} })
}

// LinkKindsFrom are the registered kinds a page of this type may start (link_kinds.from_types, NULL for any),
// the save contract's wikilink excluded, in kind order.
func (s *Store) LinkKindsFrom(ctx context.Context, typ string) ([]string, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT kind FROM link_kinds WHERE kind <> 'wikilink'
	    AND (from_types IS NULL OR instr(',' || from_types || ',', ',' || ? || ',') > 0) ORDER BY kind`, typ)
	return list(rows, err, func(k *string) []any { return []any{k} })
}
