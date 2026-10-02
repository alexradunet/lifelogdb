package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"

	"lifelog/internal/text"
)

// Tx is one BEGIN IMMEDIATE write transaction and the writer whose rows it writes (lifelog_meta.source).
// Every write operation is a method on it, so an import composes several into one transaction; each Store
// method is the same operation alone.
type Tx struct {
	tx     *sql.Tx
	Source string
}

// Do runs fn in one write transaction, committed when fn returns nil.
func (s *Store) Do(ctx context.Context, source string, fn func(*Tx) error) error {
	if err := CheckSource(source); err != nil {
		return err
	}
	return refused(s.DB.Write(ctx, func(tx *sql.Tx) error { return fn(&Tx{tx, source}) }))
}

// errDryRun rolls a DryRun back after fn succeeded.
var errDryRun = errors.New("dry run")

// DryRun runs fn in a write transaction and always rolls it back: what a write would do, without doing it.
func (s *Store) DryRun(ctx context.Context, source string, fn func(*Tx) error) error {
	err := s.Do(ctx, source, func(t *Tx) error {
		if err := fn(t); err != nil {
			return err
		}
		return errDryRun
	})
	if errors.Is(err, errDryRun) {
		return nil
	}
	return err
}

// Sync is what a body save did to the page's wikilinks (docs/contract/titles-and-wikilinks.md: the UI reports
// skipped targets and tells the owner about a revived page).
type Sync struct {
	Linked  []string `json:"linked"`
	Created []string `json:"created,omitempty"`
	Revived []string `json:"revived,omitempty"`
	Skipped []string `json:"skipped,omitempty"`
}

// syncWikilinks is cookbook/save-a-body.md steps 1-4, after the body is written.
func (t *Tx) syncWikilinks(pageID int64, body string) (Sync, error) {
	var own string
	if err := t.tx.QueryRow(`SELECT title_key FROM pages WHERE id = ?`, pageID).Scan(&own); err != nil {
		return Sync{}, err
	}
	keys, titles, rejected := text.Targets(body, own)
	r := Sync{Linked: []string{}, Skipped: rejected}
	ids := []int64{}
	for i, key := range keys {
		title := titles[i]
		if _, err := t.tx.Exec(`SAVEPOINT target`); err != nil {
			return r, err
		}
		id, created, revived, err := t.linkTarget(pageID, key, title)
		if err != nil {
			if _, e := t.tx.Exec(`ROLLBACK TO target`); e != nil {
				return r, e
			}
			if _, e := t.tx.Exec(`RELEASE target`); e != nil {
				return r, e
			}
			r.Skipped = append(r.Skipped, title)
			continue
		}
		if _, err := t.tx.Exec(`RELEASE target`); err != nil {
			return r, err
		}
		ids = append(ids, id)
		r.Linked = append(r.Linked, title)
		if created {
			r.Created = append(r.Created, title)
		}
		if revived {
			r.Revived = append(r.Revived, title)
		}
	}
	idsJSON, _ := json.Marshal(ids)
	_, err := t.tx.Exec(`DELETE FROM links WHERE from_id = ? AND kind = 'wikilink'
	                       AND to_id NOT IN (SELECT value FROM json_each(?))`, pageID, string(idsJSON))
	return r, err
}

func (t *Tx) linkTarget(pageID int64, key, title string) (id int64, created, revived bool, err error) {
	var deleted sql.NullString
	err = t.tx.QueryRow(`SELECT p.id, e.deleted_at FROM pages p JOIN entities e ON e.id = p.id WHERE p.title_key = ?`, key).
		Scan(&id, &deleted)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if id, _, err = t.insertPage("page", title, key, dayOfTitle(title), "", ""); err != nil {
			return
		}
		created = true
	case err != nil:
		return
	case deleted.Valid:
		if _, err = t.tx.Exec(`UPDATE entities SET deleted_at = NULL WHERE id = ?`, id); err != nil {
			return
		}
		revived = true
	}
	_, err = t.tx.Exec(`INSERT INTO links(from_id, to_id, kind, created_at, source) VALUES (?, ?, 'wikilink', `+Now+`, ?)
	                    ON CONFLICT(from_id, to_id, kind) DO NOTHING`, pageID, id, t.Source)
	return
}

// dayOfTitle is a link target's day: none, except a day page, whose day is its title (pages_day_page).
func dayOfTitle(title string) any {
	if IsDay(title) {
		return title
	}
	return nil
}

// insertPage is the universal insert convention (cookbook/capture.md): entities first, RETURNING id, then the
// pages row with that id. With an importKey a re-send inserts nothing and returns the row the key already names
// (cookbook/import-a-row-once.md); existing says so.
func (t *Tx) insertPage(typ, title, key string, day any, body, importKey string) (id int64, existing bool, err error) {
	err = t.tx.QueryRow(`INSERT INTO entities(entity_type, created_at, updated_at, source, import_key) VALUES (?, `+Now+`, `+Now+`, ?, ?)
	                     ON CONFLICT(source, import_key) WHERE import_key IS NOT NULL DO NOTHING
	                     RETURNING id`, typ, t.Source, nullIfEmpty(importKey)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = t.tx.QueryRow(`SELECT id FROM entities WHERE source = ? AND import_key = ?`, t.Source, importKey).Scan(&id)
		return id, true, err
	}
	if err != nil {
		return 0, false, err
	}
	_, err = t.tx.Exec(`INSERT INTO pages(id, entity_type, title, title_key, day, body) VALUES (?, ?, ?, ?, ?, ?)`,
		id, typ, title, key, day, body)
	return id, false, err
}

// PageRef is what a lookup by title finds.
type PageRef struct {
	ID      int64
	Type    string
	Title   string
	Body    string
	DayPage bool
	Stub    bool
	Deleted bool
}

// Lookup finds the page that holds a title's key; nil when none does.
func (t *Tx) Lookup(title string) (*PageRef, error) {
	return t.lookupKey(text.TitleKey(title))
}

func (t *Tx) lookupKey(key string) (*PageRef, error) {
	p := &PageRef{}
	var day sql.NullString
	err := t.tx.QueryRow(`SELECT p.id, p.entity_type, p.title, p.body, p.day, e.deleted_at IS NOT NULL,
	                             EXISTS (SELECT 1 FROM links r WHERE r.from_id = p.id AND r.kind = 'redirect')
	                        FROM pages p JOIN entities e ON e.id = p.id WHERE p.title_key = ?`, key).
		Scan(&p.ID, &p.Type, &p.Title, &p.Body, &day, &p.Deleted, &p.Stub)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	p.DayPage = day.Valid && day.String == p.Title
	return p, err
}

func (t *Tx) pageByID(id int64) (*PageRef, error) {
	var key string
	err := t.tx.QueryRow(`SELECT title_key FROM pages WHERE id = ?`, id).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("no page %d", id)
	}
	if err != nil {
		return nil, err
	}
	return t.lookupKey(key)
}

// Capture appends an entry to a day page, creating or reviving it, with an optional mood (cookbook/capture.md).
func (t *Tx) Capture(day, entry string, mood *float64) (id int64, r Sync, err error) {
	if !IsDay(day) {
		return 0, r, invalid("day %q is not YYYY-MM-DD", day)
	}
	if entry == "" && mood == nil {
		return 0, r, invalid("nothing to capture: give text, a mood or both")
	}
	if mood != nil && !isMood(*mood) {
		return 0, r, invalid("mood is 1-5")
	}
	p, err := t.lookupKey(day)
	switch {
	case err != nil:
		return 0, r, err
	case p == nil:
		if id, _, err = t.insertPage("page", day, day, day, "", ""); err != nil {
			return 0, r, err
		}
	default:
		id = p.ID
		if p.Deleted {
			if _, err := t.tx.Exec(`UPDATE entities SET deleted_at = NULL WHERE id = ?`, id); err != nil {
				return 0, r, err
			}
		}
	}
	if entry != "" {
		if _, err := t.tx.Exec(`UPDATE pages SET body = body || CASE WHEN body = '' THEN '' ELSE char(10, 10) END || ?
		                        WHERE id = ?`, entry, id); err != nil {
			return 0, r, err
		}
	}
	var body string
	if err := t.tx.QueryRow(`SELECT body FROM pages WHERE id = ?`, id).Scan(&body); err != nil {
		return 0, r, err
	}
	if r, err = t.syncWikilinks(id, body); err != nil {
		return 0, r, err
	}
	if mood != nil {
		_, err = t.tx.Exec(`INSERT INTO measurements(metric_id, day, value, source, captured_with_id, created_at)
		                    SELECT id, ?, ?, ?, ?, `+Now+` FROM metrics WHERE name = 'mood'`, day, *mood, t.Source, id)
	}
	return id, r, err
}

func isMood(v float64) bool { return v >= 1 && v <= 5 && v == math.Trunc(v) }

// SaveBody replaces a page's body and syncs its wikilinks (cookbook/save-a-body.md). version is the
// entities.updated_at the caller read; a newer one means someone else saved in between.
func (t *Tx) SaveBody(id int64, body, version string) (Sync, error) {
	var updated string
	var deleted sql.NullString
	err := t.tx.QueryRow(`SELECT e.updated_at, e.deleted_at FROM entities e JOIN pages p ON p.id = e.id WHERE e.id = ?`, id).
		Scan(&updated, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return Sync{}, notFound("no page %d", id)
	}
	if err != nil {
		return Sync{}, err
	}
	if deleted.Valid {
		return Sync{}, conflict("page %d is deleted: revive it first", id)
	}
	if version != updated {
		return Sync{}, conflict("page %d changed since version %q (now %q): read it again", id, version, updated)
	}
	return t.SetBody(id, body)
}

// SetBody is SaveBody without the version check, for a writer that owns the body it sets (an import).
func (t *Tx) SetBody(id int64, body string) (Sync, error) {
	if _, err := t.tx.Exec(`UPDATE pages SET body = ? WHERE id = ? AND body IS NOT ?`, body, id, body); err != nil {
		return Sync{}, err
	}
	return t.syncWikilinks(id, body)
}

// CreatePage creates a page written on purpose; day is the day it was written ("" = today). A title that is a
// day makes that day's page. With an importKey, a page the key already names is returned with existing = true.
func (t *Tx) CreatePage(title, body, day, importKey string) (id int64, existing bool, r Sync, err error) {
	if !text.ValidTitle(title) {
		return 0, false, r, invalid("title %q is not a valid title (docs/contract/titles-and-wikilinks.md)", title)
	}
	switch {
	case IsDay(title):
		day = title
	case day == "":
		day = Today()
	}
	if importKey == "" || !t.keyExists(importKey) {
		if p, err := t.Lookup(title); err != nil || p != nil {
			return 0, false, r, orExists(err, p, title)
		}
	}
	if id, existing, err = t.insertPage("page", title, text.TitleKey(title), day, body, importKey); err != nil || existing {
		return id, existing, r, err
	}
	r, err = t.syncWikilinks(id, body)
	return id, false, r, err
}

func (t *Tx) keyExists(importKey string) bool {
	var n int
	t.tx.QueryRow(`SELECT count(*) FROM entities WHERE source = ? AND import_key = ?`, t.Source, importKey).Scan(&n)
	return n > 0
}

// CreatePerson and CreatePlace are cookbook/person-or-place.md's create: one id, a page, and for a person a
// people row.
func (t *Tx) CreatePerson(title, name, birth, death, importKey string) (int64, bool, error) {
	if name == "" {
		name = title
	}
	return t.createNamed("person", title, importKey, func(id int64) error {
		_, err := t.tx.Exec(`INSERT INTO people(id, name, birth_day, death_day) VALUES (?, ?, ?, ?)`,
			id, name, nullIfEmpty(birth), nullIfEmpty(death))
		return err
	})
}

func (t *Tx) CreatePlace(title, importKey string) (int64, bool, error) {
	return t.createNamed("place", title, importKey, nil)
}

func (t *Tx) createNamed(typ, title, importKey string, extra func(int64) error) (int64, bool, error) {
	if !text.ValidTitle(title) {
		return 0, false, invalid("title %q is not a valid title (docs/contract/titles-and-wikilinks.md)", title)
	}
	if importKey == "" || !t.keyExists(importKey) {
		if p, err := t.Lookup(title); err != nil || p != nil {
			return 0, false, orExists(err, p, title)
		}
	}
	id, existing, err := t.insertPage(typ, title, text.TitleKey(title), nil, "", importKey)
	if err != nil || existing || extra == nil {
		return id, existing, err
	}
	return id, false, extra(id)
}

// ExistsError says the title is taken and by which page, so a client can follow it (promote it, edit it).
type ExistsError struct {
	ID    int64
	Title string
}

func (e *ExistsError) Error() string { return "a page titled " + e.Title + " already exists" }

func orExists(err error, p *PageRef, title string) error {
	if err != nil {
		return err
	}
	return &ExistsError{p.ID, title}
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Promote makes a plain page a person or a place (cookbook/person-or-place.md): the id and its links stay, a
// tombstoned page is revived.
func (t *Tx) Promote(id int64, typ, name string) error {
	if typ != "person" && typ != "place" {
		return invalid("promote to person or place, not %q", typ)
	}
	res, err := t.tx.Exec(`UPDATE entities SET entity_type = ?, deleted_at = NULL
	                        WHERE id = ? AND entity_type = 'page'
	                          AND NOT EXISTS (SELECT 1 FROM links WHERE from_id = ? AND kind = 'redirect')`, typ, id, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return conflict("page %d is not a plain page that can be promoted (missing, already named, or a redirect stub)", id)
	}
	if typ == "person" {
		if name == "" {
			if err := t.tx.QueryRow(`SELECT title FROM pages WHERE id = ?`, id).Scan(&name); err != nil {
				return err
			}
		}
		_, err = t.tx.Exec(`INSERT INTO people(id, name) VALUES (?, ?)`, id, name)
	}
	return err
}

// Tombstone and Revive set or clear entities.deleted_at (D11): nothing is ever deleted.
func (t *Tx) Tombstone(id int64) error {
	return t.setDeleted(id, `UPDATE entities SET deleted_at = `+Now+` WHERE id = ? AND deleted_at IS NULL`)
}

func (t *Tx) Revive(id int64) error {
	return t.setDeleted(id, `UPDATE entities SET deleted_at = NULL WHERE id = ? AND deleted_at IS NOT NULL`)
}

func (t *Tx) setDeleted(id int64, q string) error {
	res, err := t.tx.Exec(q, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return conflict("entity %d is missing or already in that state", id)
	}
	return nil
}

// Link adds an edge of a registered kind (D8); added reports whether it was new. wikilink rows belong to the
// save contract and redirect rows to a rename, so neither is written here. An `at` link starts at a day page,
// which the app checks (D16).
func (t *Tx) Link(from, to int64, kind, note string) (added bool, err error) {
	if kind == "wikilink" || kind == "redirect" {
		return false, invalid("%s links are written by saving a body or a rename, not directly", kind)
	}
	if kind == "at" {
		var isDay bool
		if err := t.tx.QueryRow(`SELECT coalesce((SELECT title = day FROM pages WHERE id = ?), 0)`, from).Scan(&isDay); err != nil {
			return false, err
		}
		if !isDay {
			return false, invalid("an at link starts at a day page (D16)")
		}
	}
	res, err := t.tx.Exec(`INSERT INTO links(from_id, to_id, kind, note, created_at, source) VALUES (?, ?, ?, ?, `+Now+`, ?)
	                       ON CONFLICT(from_id, to_id, kind) DO NOTHING`, from, to, kind, nullIfEmpty(note), t.Source)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Unlink deletes one edge (links rows are the one table whose rows are deleted, D11); a symmetric kind's
// mirror goes with it (links_mirror_delete).
func (t *Tx) Unlink(from, to int64, kind string) error {
	if kind == "wikilink" || kind == "redirect" {
		return invalid("%s links are removed by saving a body, not directly", kind)
	}
	res, err := t.tx.Exec(`DELETE FROM links WHERE from_id = ? AND to_id = ? AND kind = ?`, from, to, kind)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("no %s link from %d to %d", kind, from, to)
	}
	return nil
}

// Reading is one measurement to record (D7).
type Reading struct {
	ID           int64   `json:"id,omitempty"`
	Metric       string  `json:"metric"`
	Day          string  `json:"day"`
	TakenAt      string  `json:"taken_at,omitempty"`
	TZ           string  `json:"tz,omitempty"`
	Key          string  `json:"import_key,omitempty"`
	Value        float64 `json:"value"`
	CapturedWith int64   `json:"captured_with_id,omitempty"`
}

// Record appends a measurement. A Key makes a re-send a no-op (ON CONFLICT ... DO NOTHING, docs/contract/imports.md);
// the id is 0 when the row was recorded before.
func (t *Tx) Record(m Reading) (id int64, err error) {
	if !IsDay(m.Day) {
		return 0, invalid("day %q is not YYYY-MM-DD", m.Day)
	}
	if m.TakenAt != "" && !IsInstant(m.TakenAt) {
		return 0, invalid("taken_at %q is not a UTC instant like 2026-06-09T21:14:03.482Z", m.TakenAt)
	}
	if math.IsNaN(m.Value) || math.IsInf(m.Value, 0) {
		return 0, invalid("value must be a finite number")
	}
	var metric int64
	var habit bool
	err = t.tx.QueryRow(`SELECT id, EXISTS (SELECT 1 FROM habit_periods h WHERE h.metric_id = m.id) FROM metrics m WHERE name = ?`,
		m.Metric).Scan(&metric, &habit)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, notFound("no metric %q: the owner registers metrics", m.Metric)
	}
	if err != nil {
		return 0, err
	}
	if m.Metric == "mood" && !isMood(m.Value) {
		return 0, invalid("mood is 1-5")
	}
	if habit && m.Value != 0 && m.Value != 1 {
		return 0, invalid("%s is a habit: 1 = done, 0 = not done (D24)", m.Metric)
	}
	var with any
	if m.CapturedWith != 0 {
		with = m.CapturedWith
	}
	err = t.tx.QueryRow(`INSERT INTO measurements(metric_id, day, taken_at, tz, value, source, import_key, captured_with_id, created_at)
	                     VALUES (?, ?, ?, ?, ?, ?, ?, ?, `+Now+`)
	                     ON CONFLICT(source, import_key, metric_id) WHERE import_key IS NOT NULL DO NOTHING
	                     RETURNING id`,
		metric, m.Day, nullIfEmpty(m.TakenAt), nullIfEmpty(m.TZ), m.Value, t.Source, nullIfEmpty(m.Key), with).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil // sent before
	}
	return id, err
}

// ReadingByKey is the current value of the reading a sender's key names; found is false when there is none.
// A retracted reading is found, with ok false.
func (t *Tx) ReadingByKey(metric, key string) (value float64, ok, found bool, err error) {
	var id int64
	err = t.tx.QueryRow(`SELECT me.id FROM measurements me JOIN metrics m ON m.id = me.metric_id
	                      WHERE me.source = ? AND me.import_key = ? AND m.name = ?`, t.Source, key, metric).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, false, nil
	}
	if err != nil {
		return 0, false, false, err
	}
	// follow the correction chain to its end (a chain never cycles: measurements_supersede_metric)
	var v sql.NullFloat64
	err = t.tx.QueryRow(`WITH RECURSIVE chain(id, value, depth) AS (
	                       SELECT id, value, 0 FROM measurements WHERE id = ?
	                       UNION ALL SELECT x.id, x.value, depth + 1 FROM measurements x JOIN chain c ON x.supersedes_id = c.id)
	                     SELECT value FROM chain ORDER BY depth DESC LIMIT 1`, id).Scan(&v)
	return v.Float64, v.Valid, true, err
}

// Correct supersedes a reading with a new value, or retracts it when value is nil (cookbook/correct-a-measurement.md).
func (t *Tx) Correct(wrong int64, value *float64) (id int64, err error) {
	if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0)) {
		return 0, invalid("value must be a finite number")
	}
	var v any
	if value != nil {
		v = *value
	}
	err = t.tx.QueryRow(`INSERT INTO measurements(metric_id, day, taken_at, tz, value, source, captured_with_id, supersedes_id, created_at)
	                     SELECT metric_id, day, taken_at, tz, ?, ?, captured_with_id, id, `+Now+` FROM measurements WHERE id = ?
	                     RETURNING id`, v, t.Source, wrong).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, notFound("no measurement %d", wrong)
	}
	return id, err
}

// MeasurementKey is the sender's key of a measurement: its source, import_key and metric ("" when it has none).
func (t *Tx) MeasurementKey(id int64) (source, key, metric string, err error) {
	var k sql.NullString
	err = t.tx.QueryRow(`SELECT me.source, me.import_key, m.name FROM measurements me JOIN metrics m ON m.id = me.metric_id
	                      WHERE me.id = ?`, id).Scan(&source, &k, &metric)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", notFound("no measurement %d", id)
	}
	return source, k.String, metric, err
}

// ---- the same operations alone, each its own transaction

func (s *Store) Capture(ctx context.Context, source, day, entry string, mood *float64) (id int64, r Sync, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) { id, r, e = t.Capture(day, entry, mood); return })
	if err != nil {
		id = 0 // rolled back
	}
	return
}

func (s *Store) SaveBody(ctx context.Context, source string, id int64, body, version string) (r Sync, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) { r, e = t.SaveBody(id, body, version); return })
	return
}

func (s *Store) CreatePage(ctx context.Context, source, title, body string) (id int64, r Sync, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) { id, _, r, e = t.CreatePage(title, body, "", ""); return })
	if err != nil {
		id = 0
	}
	return
}

func (s *Store) CreatePerson(ctx context.Context, source, title, name, birth, death string) (id int64, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) { id, _, e = t.CreatePerson(title, name, birth, death, ""); return })
	if err != nil {
		id = 0
	}
	return
}

func (s *Store) CreatePlace(ctx context.Context, source, title string) (id int64, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) { id, _, e = t.CreatePlace(title, ""); return })
	if err != nil {
		id = 0
	}
	return
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

func (s *Store) Link(ctx context.Context, source string, from, to int64, kind, note string) error {
	return s.Do(ctx, source, func(t *Tx) error { _, err := t.Link(from, to, kind, note); return err })
}

func (s *Store) Unlink(ctx context.Context, source string, from, to int64, kind string) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.Unlink(from, to, kind) })
}

func (s *Store) Record(ctx context.Context, source string, m Reading) (id int64, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) { id, e = t.Record(m); return })
	if err != nil {
		id = 0
	}
	return
}

// Correct also reports the corrected row's sender key, so an import workspace can carry the correction to a replay.
func (s *Store) Correct(ctx context.Context, source string, wrong int64, value *float64) (id int64, key CorrectedKey, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) {
		if key.Source, key.Key, key.Metric, e = t.MeasurementKey(wrong); e != nil {
			return e
		}
		id, e = t.Correct(wrong, value)
		return
	})
	if err != nil {
		id = 0
	}
	key.Value = value
	return
}

// CorrectedKey names a corrected reading by its sender's key, and the value it now has (nil = retracted).
type CorrectedKey struct {
	Source string   `json:"source"`
	Key    string   `json:"import_key"`
	Metric string   `json:"metric"`
	Value  *float64 `json:"value"`
}

// IsImport reports a row written by an importer (lifelog_meta.source: import:<name>).
func IsImport(source string) bool { return strings.HasPrefix(source, "import:") }
