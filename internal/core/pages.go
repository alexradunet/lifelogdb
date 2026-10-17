package core

import (
	"context"
	"database/sql"
	"errors"

	"lifelog/internal/api/siren"
	"lifelog/internal/text"
)

// saveBody is Store.SaveBody in t. A stale version is a conflict unless the body sent is the body the page holds:
// that is a save sent again after its reply was lost, and it succeeds as the no-op it is. SetBody writes nothing
// when the body is equal, so the page keeps its version.
func (t *Tx) saveBody(id int64, body, version string) (Sync, error) {
	p, err := t.livePage(id)
	if err != nil {
		return Sync{}, err
	}
	if version != p.Version {
		var same bool
		if err := t.tx.QueryRowContext(t.ctx, `SELECT body IS ? FROM entities WHERE id = ?`, body, id).Scan(&same); err != nil {
			return Sync{}, err
		}
		if !same {
			return Sync{}, conflictCode(siren.CodeStaleVersion, "page %d changed since version %q (now %q): read it again", id, version, p.Version)
		}
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
// day makes that day's page.
func (t *Tx) CreatePage(title, body, day string) (id int64, r Sync, err error) {
	switch {
	case text.IsDay(title):
		day = title
	case day == "":
		day = t.today()
	}
	if id, err = t.createNamed("page", title, body, day, nil); err != nil {
		return id, r, err
	}
	r, err = t.syncWikilinks(id, body)
	return id, r, err
}

// CreatePerson and CreatePlace are cookbook/person-or-place.md's create: one id, a page, and for a person a
// people row.
func (t *Tx) CreatePerson(title, name, birth, death string) (int64, error) {
	if name == "" {
		name = title
	}
	return t.createNamed("person", title, "", nil, func(id int64) error {
		_, err := t.tx.ExecContext(t.ctx, `INSERT INTO people(id, name, birth_day, death_day) VALUES (?, ?, ?, ?)`,
			id, name, nullIfEmpty(birth), nullIfEmpty(death))
		return err
	})
}

func (t *Tx) CreatePlace(title string) (int64, error) {
	return t.createNamed("place", title, "", nil, nil)
}

// createNamed creates one entity: its row in entities with the whole body, its preferred name in entity_names, and
// then extra for its typed row. It is the one check of the title for CreatePage, CreatePerson and CreatePlace: the
// title must be valid, and no page, live or deleted, may hold its key (the ExistsError names that page). day is the
// day of a page (for a day page, its date), nil for a person or a place. Creation is one write, so the new entity is
// at revision 1 (lifelog_meta.edit_revisions). The caller syncs the wikilinks of the body after the typed row exists.
func (t *Tx) createNamed(typ, title, body string, day any, extra func(int64) error) (int64, error) {
	if err := checkTitle("title", title); err != nil {
		return 0, err
	}
	if p, err := t.lookup(title); err != nil || p != nil {
		return 0, orExists(err, p, title)
	}
	id, err := t.insertPage(typ, title, text.TitleKey(title), day, body)
	if err != nil || extra == nil {
		return id, err
	}
	return id, extra(id)
}

// promote is Store.Promote in t.
func (t *Tx) promote(id int64, typ, name string) error {
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
	// The schema refuses a person, a place, a metric or a file promoted to another type (entities_promote_from_page).
	// A promotion to the type that the entity has already changes no type, so no rule of the schema sees it: for a
	// place the update below would only revive a tombstone, and for a person the people insert would fail on its key.
	// The writer refuses it with this 409.
	if have == typ {
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

// tombstone and revive are Store.Tombstone and Store.Revive in t.
func (t *Tx) tombstone(id int64) error { return t.setDeleted(id, true) }

func (t *Tx) revive(id int64) error { return t.setDeleted(id, false) }

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

// SaveBody replaces a page's body and syncs its wikilinks (cookbook/save-a-body.md). version is the
// entities.revision token the caller read; the clock is audit metadata, not a version.
func (s *Store) SaveBody(ctx context.Context, source string, id int64, body, version string) (Sync, error) {
	return alone(s, ctx, source, func(t *Tx) (Sync, error) { return t.saveBody(id, body, version) })
}

// made is what a write that makes or writes a page gives, as one value for alone: the page's id and what the save of
// its body did to its wikilinks.
type made struct {
	id   int64
	sync Sync
}

// CreatePage makes the page and, with a category, files it there in the same write, as FilePages does
// (docs/plans/115-categories-to-browse.md): a day as the category refuses the whole write.
func (s *Store) CreatePage(ctx context.Context, source, title, body, category string) (int64, Sync, error) {
	m, err := alone(s, ctx, source, func(t *Tx) (made, error) {
		id, r, err := t.CreatePage(title, body, "")
		if err != nil {
			return made{}, err
		}
		return made{id, r}, t.fileNew(id, category)
	})
	return m.id, m.sync, err
}

// CreatePerson makes the person and, with a category, files it there in the same write, as CreatePage does
// (docs/plans/117-one-browse-view.md, rule 6).
func (s *Store) CreatePerson(ctx context.Context, source, title, name, birth, death, category string) (int64, error) {
	return alone(s, ctx, source, func(t *Tx) (int64, error) {
		id, err := t.CreatePerson(title, name, birth, death)
		if err != nil {
			return 0, err
		}
		return id, t.fileNew(id, category)
	})
}

// CreatePlace makes the place and, with a category, files it there in the same write, as CreatePage does.
func (s *Store) CreatePlace(ctx context.Context, source, title, category string) (int64, error) {
	return alone(s, ctx, source, func(t *Tx) (int64, error) {
		id, err := t.CreatePlace(title)
		if err != nil {
			return 0, err
		}
		return id, t.fileNew(id, category)
	})
}

// fileNew files a thing made in this write in the category titled category ("" for none), as FilePages files it:
// a missing category is made, and a day as the category refuses the whole write: entities_day_page refuses a plain
// page with a day's title, and links_endpoint_types a part-of link to a day page that exists.
func (t *Tx) fileNew(id int64, category string) error {
	if category == "" {
		return nil
	}
	_, err := t.filePages([]int64{id}, category, false)
	return err
}

// Promote makes a plain page a person or a place (cookbook/person-or-place.md): the id and its links stay, a
// tombstoned page is revived. A missing id is 404; a page of another type is the schema's 422
// (entities_promote_from_page), and a promotion to the type that the entity has already is a 409.
func (s *Store) Promote(ctx context.Context, source string, id int64, typ, name string) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.promote(id, typ, name) })
}

// Tombstone and Revive set or clear entities.deleted_at (D11): nothing is ever deleted. A missing id is 404; the same
// state again changes nothing.
func (s *Store) Tombstone(ctx context.Context, source string, id int64) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.tombstone(id) })
}

func (s *Store) Revive(ctx context.Context, source string, id int64) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.revive(id) })
}

// tombstonePages is Store.TombstonePages in t.
func (t *Tx) tombstonePages(ids []int64) error {
	for _, id := range ids {
		if err := t.tombstone(id); err != nil {
			return err
		}
	}
	return nil
}

// TombstonePages tombstones every page named, in one write (D11): a page that is tombstoned already is left as it
// is, and an id no entity has is a 404 that writes nothing.
func (s *Store) TombstonePages(ctx context.Context, source string, ids []int64) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.tombstonePages(ids) })
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
	Cover     *Cover  `json:"cover,omitempty"` // nil for a file page, and for a page with no cover
	// Pictures are the other pictures the body embeds, after the cover, each once, in the order of the body: each
	// may become the cover (SetCoverOf).
	Pictures []Cover `json:"pictures,omitempty"`
	Out      []Edge  `json:"links"`
	In       []Edge  `json:"backlinks"`
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
	} else if p.Cover, p.Pictures, err = picturesOf(ctx, s.DB.R, p.Body); err != nil {
		return nil, err
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
	return list(rows, nil, func(h *Hit) []any { return []any{&h.ID, &h.Title, &h.Snippet} })
}
