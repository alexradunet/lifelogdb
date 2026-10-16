package core

import (
	"context"
	"database/sql"
	"errors"

	"lifelog/internal/text"
)

// SaveBody replaces a page's body and syncs its wikilinks (cookbook/save-a-body.md). version is the
// entities.revision token the caller read; the clock is audit metadata, not a version.
func (t *Tx) SaveBody(id int64, body, version string) (Sync, error) {
	var currentVersion string
	var deleted sql.NullString
	err := t.tx.QueryRowContext(t.ctx, `SELECT CAST(revision AS TEXT), deleted_at FROM entities WHERE id = ?`, id).
		Scan(&currentVersion, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return Sync{}, NotFound("no page %d", id)
	}
	if err != nil {
		return Sync{}, err
	}
	if deleted.Valid {
		return Sync{}, Conflict("page %d is deleted: revive it first", id)
	}
	if version != currentVersion {
		return Sync{}, conflictCode("stale_version", "page %d changed since version %q (now %q): read it again", id, version, currentVersion)
	}
	return t.SetBody(id, body)
}

// SetBody is SaveBody without the version check, for a write that has the body it sets: SaveBody after its check,
// the note of a new metric on an empty body, and the picture mark of a page.
func (t *Tx) SetBody(id int64, body string) (Sync, error) {
	if _, err := t.tx.ExecContext(t.ctx, `UPDATE entities SET body = ? WHERE id = ? AND body IS NOT ?`, body, id, body); err != nil {
		return Sync{}, err
	}
	return t.syncWikilinks(id, body)
}

// CreatePage creates a page written on purpose; day is the day it was written ("" = today). A title that is a
// day makes that day's page. With an importKey, a page the key already names is returned with existing = true.
func (t *Tx) CreatePage(title, body, day, importKey string) (id int64, existing bool, r Sync, err error) {
	if err := checkTitle("title", title); err != nil {
		return 0, false, r, err
	}
	switch {
	case IsDay(title):
		day = title
	case day == "":
		day = t.today()
	}
	if id, existing, err = t.createNamed("page", title, body, day, importKey, nil); err != nil || existing {
		return id, existing, r, err
	}
	r, err = t.syncWikilinks(id, body)
	return id, false, r, err
}

// PageForTitle is the id of the page that holds a title, made as an empty page when no page has it: the category a
// filing names by its title (see FilePages), like the target of a wikilink, and like it with no day, so it is not an entry of the day it
// was made on. A tombstoned page keeps its id and its tombstone.
func (t *Tx) PageForTitle(title string) (id int64, created bool, err error) {
	if err := checkTitle("project", title); err != nil {
		return 0, false, err
	}
	p, err := t.lookup(title)
	if err != nil {
		return 0, false, err
	}
	if p != nil {
		return p.ID, false, nil
	}
	id, _, err = t.insertPage("page", title, text.TitleKey(title), nil, "", "")
	return id, err == nil, err
}

// CreatePerson and CreatePlace are cookbook/person-or-place.md's create: one id, a page, and for a person a
// people row.
func (t *Tx) CreatePerson(title, name, birth, death, importKey string) (int64, bool, error) {
	if name == "" {
		name = title
	}
	return t.createNamed("person", title, "", nil, importKey, func(id int64) error {
		_, err := t.tx.ExecContext(t.ctx, `INSERT INTO people(id, name, birth_day, death_day) VALUES (?, ?, ?, ?)`,
			id, name, nullIfEmpty(birth), nullIfEmpty(death))
		return err
	})
}

func (t *Tx) CreatePlace(title, importKey string) (int64, bool, error) {
	return t.createNamed("place", title, "", nil, importKey, nil)
}

// createNamed inserts the entity with its whole body: creation is one write, so the new page is at revision 1
// (lifelog_meta.edit_revisions). The caller syncs the body's wikilinks once the typed row exists.
// createNamed is one entity with its page: a title no live page has (a sender's key finds the row an earlier send
// made, and writes nothing), its row in entities and entity_names, and extra for its typed row. day is a day page's
// day, nil for every other page.
func (t *Tx) createNamed(typ, title, body string, day any, importKey string, extra func(int64) error) (int64, bool, error) {
	if err := checkTitle("title", title); err != nil {
		return 0, false, err
	}
	have, err := t.byImportKey(importKey)
	if err != nil {
		return 0, false, err
	}
	if have != 0 {
		return have, true, nil
	}
	if p, err := t.lookup(title); err != nil || p != nil {
		return 0, false, orExists(err, p, title)
	}
	id, existing, err := t.insertPage(typ, title, text.TitleKey(title), day, body, importKey)
	if err != nil || existing || extra == nil {
		return id, existing, err
	}
	return id, false, extra(id)
}

// byImportKey is the entity a sender's key names under this transaction's source; 0 when none.
func (t *Tx) byImportKey(importKey string) (int64, error) {
	if importKey == "" {
		return 0, nil
	}
	var id int64
	err := t.tx.QueryRowContext(t.ctx, `SELECT id FROM entities WHERE source = ? AND import_key = ?`, t.Source, importKey).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// Promote makes a plain page a person or a place (cookbook/person-or-place.md): the id and its links stay, a
// tombstoned page is revived. A missing id is 404; a page of another type is a 409.
func (t *Tx) Promote(id int64, typ, name string) error {
	if typ != "person" && typ != "place" {
		return Invalid("promote to person or place, not %q", typ)
	}
	var have string
	err := t.tx.QueryRowContext(t.ctx, `SELECT entity_type FROM entities WHERE id = ?`, id).Scan(&have)
	if errors.Is(err, sql.ErrNoRows) {
		return NotFound("no page %d", id)
	}
	if err != nil {
		return err
	}
	if have != "page" {
		return Conflict("page %d is a %s, not a plain page to promote", id, have)
	}
	if _, err := t.tx.ExecContext(t.ctx, `UPDATE entities SET entity_type = ?, deleted_at = NULL WHERE id = ?`, typ, id); err != nil {
		return err
	}
	if typ == "person" {
		if name == "" {
			if err := t.tx.QueryRowContext(t.ctx, `SELECT title FROM entity_names WHERE entity_id = ? AND name_key = (SELECT preferred_name_key FROM entities WHERE id = entity_id)`, id).Scan(&name); err != nil {
				return err
			}
		}
		_, err = t.tx.ExecContext(t.ctx, `INSERT INTO people(id, name) VALUES (?, ?)`, id, name)
	}
	return err
}

// Tombstone and Revive set or clear entities.deleted_at (D11): nothing is ever deleted. A missing id is 404; the same
// state again changes nothing.
func (t *Tx) Tombstone(id int64) error { return t.setDeleted(id, true) }

func (t *Tx) Revive(id int64) error { return t.setDeleted(id, false) }

func (t *Tx) setDeleted(id int64, deleted bool) error {
	var tombstoned bool
	err := t.tx.QueryRowContext(t.ctx, `SELECT deleted_at IS NOT NULL FROM entities WHERE id = ?`, id).Scan(&tombstoned)
	if errors.Is(err, sql.ErrNoRows) {
		return NotFound("no entity %d", id)
	}
	if err != nil {
		return err
	}
	if tombstoned == deleted {
		return nil
	}
	q := `UPDATE entities SET deleted_at = NULL WHERE id = ?`
	if deleted {
		q = `UPDATE entities SET deleted_at = ` + nowSQL + ` WHERE id = ?`
	}
	_, err = t.tx.ExecContext(t.ctx, q, id)
	return err
}

// ---- the same operations alone, each its own transaction

func (s *Store) SaveBody(ctx context.Context, source string, id int64, body, version string) (r Sync, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) { r, e = t.SaveBody(id, body, version); return })
	if err != nil {
		r = Sync{}
	}
	return
}

// CreatePage makes the page and, with a category, files it there in the same write, as FilePages does
// (docs/plans/115-categories-to-browse.md): a day as the category refuses the whole write.
func (s *Store) CreatePage(ctx context.Context, source, title, body, category string) (id int64, r Sync, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) {
		if id, _, r, e = t.CreatePage(title, body, "", ""); e != nil {
			return e
		}
		return t.fileNew(id, category)
	})
	if err != nil {
		id = 0
	}
	return
}

// CreatePerson makes the person and, with a category, files it there in the same write, as CreatePage does
// (docs/plans/117-one-browse-view.md, rule 6).
func (s *Store) CreatePerson(ctx context.Context, source, title, name, birth, death, category string) (id int64, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) {
		if id, _, e = t.CreatePerson(title, name, birth, death, ""); e != nil {
			return e
		}
		return t.fileNew(id, category)
	})
	if err != nil {
		id = 0
	}
	return
}

// CreatePlace makes the place and, with a category, files it there in the same write, as CreatePage does.
func (s *Store) CreatePlace(ctx context.Context, source, title, category string) (id int64, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) {
		if id, _, e = t.CreatePlace(title, ""); e != nil {
			return e
		}
		return t.fileNew(id, category)
	})
	if err != nil {
		id = 0
	}
	return
}

// fileNew files a thing made in this write in the category titled category ("" for none), as FilePages files it:
// a missing category is made, and a day as the category refuses the whole write: entities_day_page refuses a plain
// page with a day's title, and links_endpoint_types a part-of link to a day page that exists.
func (t *Tx) fileNew(id int64, category string) error {
	if category == "" {
		return nil
	}
	_, err := t.FilePages([]int64{id}, category, false)
	return err
}

func (s *Store) Promote(ctx context.Context, source string, id int64, typ, name string) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.Promote(id, typ, name) })
}

func (s *Store) Tombstone(ctx context.Context, source string, id int64) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.Tombstone(id) })
}

func (s *Store) Revive(ctx context.Context, source string, id int64) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.Revive(id) })
}

// TombstonePages tombstones every page named, in one write (D11): a page that is tombstoned already is left as it
// is, and an id no entity has is a 404 that writes nothing.
func (t *Tx) TombstonePages(ids []int64) error {
	for _, id := range ids {
		if err := t.Tombstone(id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) TombstonePages(ctx context.Context, source string, ids []int64) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.TombstonePages(ids) })
}

// Page is one page with what surrounds it. Version is the decimal entities.revision token: pass it back to SaveBody.
type Page struct {
	ID        int64   `json:"id"`
	Type      string  `json:"entity_type"`
	Title     string  `json:"title"`
	Day       string  `json:"day,omitempty"`
	Body      string  `json:"body"`
	CreatedAt string  `json:"created_at"`
	Version   string  `json:"version"`
	DeletedAt string  `json:"deleted_at,omitempty"`
	IsDayPage bool    `json:"is_day_page"`
	Person    *Person `json:"person,omitempty"`
	File      *File   `json:"file,omitempty"`
	Out       []Edge  `json:"links"`
	In        []Edge  `json:"backlinks"`
}

type Person struct {
	Name  string `json:"name"`
	Birth string `json:"birth_day,omitempty"`
	Death string `json:"death_day,omitempty"`
}

// PageByID reads a page, tombstoned or not (the caller decides what a tombstone shows).
func (s *Store) PageByID(ctx context.Context, id int64) (*Page, error) {
	p := &Page{ID: id}
	var day, deleted sql.NullString
	err := s.DB.R.QueryRowContext(ctx, `
		SELECT e.entity_type, n.title, e.day, e.body, e.created_at, CAST(e.revision AS TEXT), e.deleted_at
		  FROM entities e JOIN entity_names n ON n.entity_id = e.id AND n.name_key = e.preferred_name_key WHERE e.id = ?`, id).
		Scan(&p.Type, &p.Title, &day, &p.Body, &p.CreatedAt, &p.Version, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no page %d", id)
	}
	if err != nil {
		return nil, err
	}
	p.Day, p.DeletedAt = day.String, deleted.String
	p.IsDayPage = p.Day != "" && p.Day == p.Title
	if p.Type == "person" {
		var b, d sql.NullString
		p.Person = &Person{}
		if err := s.DB.R.QueryRowContext(ctx, `SELECT name, birth_day, death_day FROM people WHERE id = ?`, id).
			Scan(&p.Person.Name, &b, &d); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		p.Person.Birth, p.Person.Death = b.String, d.String
	}
	if p.Type == "file" {
		p.File = &File{}
		if err := s.DB.R.QueryRowContext(ctx, `SELECT sha256, mime, preview IS NOT NULL FROM files WHERE id = ?`, id).
			Scan(&p.File.SHA256, &p.File.MIME, &p.File.Preview); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	// outgoing edges to live entities
	if p.Out, err = s.edges(ctx, `
		SELECT l.kind, l.to_id, pg_name.title, e.entity_type, coalesce(l.note, '')
		  FROM links l JOIN entities e ON e.id = l.to_id AND e.deleted_at IS NULL JOIN entity_names pg_name ON pg_name.entity_id = e.id AND pg_name.name_key = e.preferred_name_key
		 WHERE l.from_id = ? ORDER BY l.kind, pg_name.title`, id); err != nil {
		return nil, err
	}
	// cookbook/backlinks.md: owned names resolve to this id
	if p.In, err = s.edges(ctx, `
		SELECT l.kind, l.from_id, pg_name.title, e.entity_type, coalesce(l.note, '')
		  FROM links l
		  JOIN entities e  ON e.id = l.from_id AND e.deleted_at IS NULL
		  JOIN entity_names pg_name ON pg_name.entity_id = e.id AND pg_name.name_key = e.preferred_name_key
		 WHERE l.to_id = ?
		 ORDER BY pg_name.title`, id); err != nil {
		return nil, err
	}
	return p, nil
}

// SnippetOpen and SnippetClose mark a match in a Hit's snippet. They are not brackets, because a body's own text
// holds [[wikilinks]] and check boxes, and a mark must never be confused with the text.
const (
	SnippetOpen  = "⟦"
	SnippetClose = "⟧"
)

// Hit is one full-text match (cookbook/full-text-search.md).
type Hit struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

// Search is the hits of an FTS5 query, best first; limit and offset select one page of them.
func (s *Store) Search(ctx context.Context, q string, limit, offset int) ([]Hit, error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT p.id, p_name.title, snippet(entities_fts, 2, '⟦', '⟧', '…', 24)
		  FROM entities_fts
		  JOIN entities p ON p.id = entities_fts.rowid JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key AND p.deleted_at IS NULL
		 WHERE entities_fts MATCH ?
		 ORDER BY rank, p.id LIMIT ? OFFSET ?`, q, limit, offset)
	if err != nil {
		return nil, invalidCause(err, "search %q: %v", q, err)
	}
	defer rows.Close()
	out := []Hit{}
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.ID, &h.Title, &h.Snippet); err != nil {
			return nil, invalidCause(err, "search %q: %v", q, err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
