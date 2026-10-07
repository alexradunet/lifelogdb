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
// method is the same operation alone. Its SQL uses the context of the owning Do call.
type Tx struct {
	tx     *sql.Tx
	ctx    context.Context
	Source string
}

// Do runs fn in one write transaction, committed when fn returns nil.
func (s *Store) Do(ctx context.Context, source string, fn func(*Tx) error) error {
	if err := CheckSource(source); err != nil {
		return err
	}
	return refused(s.DB.Write(ctx, func(tx *sql.Tx) error { return fn(&Tx{tx: tx, ctx: ctx, Source: source}) }))
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
	if err := t.tx.QueryRowContext(t.ctx, `SELECT preferred_name_key FROM entities WHERE id = ?`, pageID).Scan(&own); err != nil {
		return Sync{}, err
	}
	keys, titles, rejected := text.Targets(body, own)
	r := Sync{Linked: []string{}, Skipped: rejected}
	ids := []int64{}
	seen := map[int64]bool{pageID: true}
	for i, key := range keys {
		title := titles[i]
		if _, err := t.tx.ExecContext(t.ctx, `SAVEPOINT target`); err != nil {
			return r, err
		}
		id, created, revived, err := t.linkTarget(pageID, key, title)
		if err != nil {
			if _, e := t.tx.ExecContext(t.ctx, `ROLLBACK TO target`); e != nil {
				return r, e
			}
			if _, e := t.tx.ExecContext(t.ctx, `RELEASE target`); e != nil {
				return r, e
			}
			r.Skipped = append(r.Skipped, title)
			continue
		}
		if _, err := t.tx.ExecContext(t.ctx, `RELEASE target`); err != nil {
			return r, err
		}
		if seen[id] {
			continue
		}
		seen[id] = true
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
	_, err := t.tx.ExecContext(t.ctx, `DELETE FROM links WHERE from_id = ? AND kind = 'wikilink'
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
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO links(from_id, to_id, kind, created_at, source) VALUES (?, ?, 'wikilink', `+Now+`, ?)
	                    ON CONFLICT(from_id, to_id, kind) DO NOTHING`, pageID, id, t.Source)
	return
}

// dayOfTitle is a link target's day: none, except a day page (entities_day_page).
func dayOfTitle(title string) any {
	if IsDay(title) {
		return title
	}
	return nil
}

// insertPage is the universal insert convention (cookbook/capture.md): entities first, RETURNING id, then the
// owned-name row with that id. With an importKey a re-send inserts nothing and returns the row the key already names
// (cookbook/import-a-row-once.md); existing says so.
func (t *Tx) insertPage(typ, title, key string, day any, body, importKey string) (id int64, existing bool, err error) {
	err = t.tx.QueryRowContext(t.ctx, `INSERT INTO entities(entity_type, preferred_name_key, day, body, created_at, updated_at, source, import_key)
                         VALUES (?, ?, ?, ?, `+Now+`, `+Now+`, ?, ?)
	                     ON CONFLICT(source, import_key) WHERE import_key IS NOT NULL DO NOTHING
	                     RETURNING id`, typ, key, day, body, t.Source, nullIfEmpty(importKey)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = t.tx.QueryRowContext(t.ctx, `SELECT id FROM entities WHERE source = ? AND import_key = ?`, t.Source, importKey).Scan(&id)
		return id, true, err
	}
	if err != nil {
		return 0, false, err
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO entity_names(entity_id, title, name_key) VALUES (?, ?, ?)`, id, title, key)
	return id, false, err
}

// PageRef is what a lookup by title finds.
type PageRef struct {
	ID      int64
	Type    string
	Title   string
	Body    string
	DayPage bool
	Deleted bool
}

// Lookup finds the page that holds a title's key; nil when none does.
func (t *Tx) Lookup(title string) (*PageRef, error) {
	return t.lookupKey(text.TitleKey(title))
}

func (t *Tx) lookupKey(key string) (*PageRef, error) {
	p := &PageRef{}
	var day sql.NullString
	err := t.tx.QueryRowContext(t.ctx, `SELECT e.id, e.entity_type, n.title, e.body, e.day, e.deleted_at IS NOT NULL
                          FROM entities e JOIN entity_names n ON n.entity_id = e.id AND n.name_key = e.preferred_name_key
                         WHERE e.id = (SELECT entity_id FROM entity_names WHERE name_key = ?)`, key).
		Scan(&p.ID, &p.Type, &p.Title, &p.Body, &day, &p.Deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	p.DayPage = day.Valid && day.String == p.Title
	return p, err
}

func (t *Tx) pageByID(id int64) (*PageRef, error) {
	var key string
	err := t.tx.QueryRowContext(t.ctx, `SELECT preferred_name_key FROM entities WHERE id = ?`, id).Scan(&key)
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
	if id, err = t.dayPage(day); err != nil {
		return 0, r, err
	}
	if entry != "" {
		if _, err := t.tx.ExecContext(t.ctx, `UPDATE entities SET body = body || CASE WHEN body = '' THEN '' ELSE char(10, 10) END || ?
		                        WHERE id = ?`, entry, id); err != nil {
			return 0, r, err
		}
	}
	var body string
	if err := t.tx.QueryRowContext(t.ctx, `SELECT body FROM entities WHERE id = ?`, id).Scan(&body); err != nil {
		return 0, r, err
	}
	if r, err = t.syncWikilinks(id, body); err != nil {
		return 0, r, err
	}
	if mood != nil {
		_, err = t.Record(Reading{Metric: "Mood", Day: day, Value: *mood, CapturedWith: id})
	}
	return id, r, err
}

func isMood(v float64) bool { return v >= 1 && v <= 5 && v == math.Trunc(v) }

func validateMeasurementValue(metricID int64, metric string, habit bool, value *float64) error {
	if value == nil {
		return nil
	}
	if math.IsNaN(*value) || math.IsInf(*value, 0) {
		return invalid("value must be a finite number")
	}
	if metricID == 1 && !isMood(*value) {
		return invalid("mood is 1-5")
	}
	if habit && *value != 0 && *value != 1 {
		return invalid("%s is a habit: 1 = done, 0 = not done (D24)", metric)
	}
	return nil
}

// dayPage is the id of a local day's page: found (revived when tombstoned), or created on its first write
// (cookbook/capture.md).
func (t *Tx) dayPage(day string) (int64, error) {
	p, err := t.lookupKey(day)
	switch {
	case err != nil:
		return 0, err
	case p == nil:
		id, _, err := t.insertPage("page", day, day, day, "", "")
		return id, err
	case p.Deleted:
		_, err = t.tx.ExecContext(t.ctx, `UPDATE entities SET deleted_at = NULL WHERE id = ?`, p.ID)
	}
	return p.ID, err
}

// SaveBody replaces a page's body and syncs its wikilinks (cookbook/save-a-body.md). version is the
// entities.revision token the caller read; the clock is audit metadata, not a version.
func (t *Tx) SaveBody(id int64, body, version string) (Sync, error) {
	var currentVersion string
	var deleted sql.NullString
	err := t.tx.QueryRowContext(t.ctx, `SELECT CAST(revision AS TEXT), deleted_at FROM entities WHERE id = ?`, id).
		Scan(&currentVersion, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return Sync{}, notFound("no page %d", id)
	}
	if err != nil {
		return Sync{}, err
	}
	if deleted.Valid {
		return Sync{}, conflict("page %d is deleted: revive it first", id)
	}
	if version != currentVersion {
		return Sync{}, conflict("page %d changed since version %q (now %q): read it again", id, version, currentVersion)
	}
	return t.SetBody(id, body)
}

// SetBody is SaveBody without the version check, for a writer that owns the body it sets (an import).
func (t *Tx) SetBody(id int64, body string) (Sync, error) {
	if _, err := t.tx.ExecContext(t.ctx, `UPDATE entities SET body = ? WHERE id = ? AND body IS NOT ?`, body, id, body); err != nil {
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
	have, err := t.ByImportKey(importKey)
	if err != nil {
		return 0, false, r, err
	}
	if have == 0 {
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

// CreatePerson and CreatePlace are cookbook/person-or-place.md's create: one id, a page, and for a person a
// people row.
func (t *Tx) CreatePerson(title, name, birth, death, importKey string) (int64, bool, error) {
	if name == "" {
		name = title
	}
	return t.createNamed("person", title, importKey, func(id int64) error {
		_, err := t.tx.ExecContext(t.ctx, `INSERT INTO people(id, name, birth_day, death_day) VALUES (?, ?, ?, ?)`,
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
	have, err := t.ByImportKey(importKey)
	if err != nil {
		return 0, false, err
	}
	if have == 0 {
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
	res, err := t.tx.ExecContext(t.ctx, `UPDATE entities SET entity_type = ?, deleted_at = NULL WHERE id = ? AND entity_type = 'page'`, typ, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return conflict("page %d is not a plain page that can be promoted (missing or already typed)", id)
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

// FillPersonDays gives a person the birth_day and death_day it lacks: a NULL column takes the day given, a day
// the row already holds is left alone, and a different one is refused (changing it is a correction, the owner's).
// changed reports whether a column was written.
func (t *Tx) FillPersonDays(id int64, birth, death string) (changed bool, err error) {
	var have [2]sql.NullString
	err = t.tx.QueryRowContext(t.ctx, `SELECT birth_day, death_day FROM people WHERE id = ?`, id).Scan(&have[0], &have[1])
	if errors.Is(err, sql.ErrNoRows) {
		return false, notFound("no person %d", id)
	}
	if err != nil {
		return false, err
	}
	for i, c := range [2]struct{ col, want string }{{"birth_day", birth}, {"death_day", death}} {
		switch {
		case c.want == "" || have[i].Valid && have[i].String == c.want:
		case have[i].Valid:
			return false, conflict("the person's %s is %s, not %s: a correction is the owner's", c.col, have[i].String, c.want)
		case !IsDay(c.want):
			return false, invalid("%s %q is not YYYY-MM-DD", c.col, c.want)
		default:
			if _, err := t.tx.ExecContext(t.ctx, `UPDATE people SET `+c.col+` = ? WHERE id = ?`, c.want, id); err != nil {
				return false, err
			}
			changed = true
		}
	}
	return changed, nil
}

// Tombstone and Revive set or clear entities.deleted_at (D11): nothing is ever deleted.
func (t *Tx) Tombstone(id int64) error {
	return t.setDeleted(id, `UPDATE entities SET deleted_at = `+Now+` WHERE id = ? AND deleted_at IS NULL`)
}

func (t *Tx) Revive(id int64) error {
	return t.setDeleted(id, `UPDATE entities SET deleted_at = NULL WHERE id = ? AND deleted_at IS NOT NULL`)
}

func (t *Tx) setDeleted(id int64, q string) error {
	res, err := t.tx.ExecContext(t.ctx, q, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return conflict("entity %d is missing or already in that state", id)
	}
	return nil
}

// Link adds an edge of a registered kind (D8); added reports whether it was new. wikilink rows belong to the
// save contract; redirect is unsupported, so neither is written here. An `at` link starts at a day page,
// which the app checks (D16).
func (t *Tx) Link(from, to int64, kind, note string) (added bool, err error) {
	if kind == "redirect" {
		return false, invalid("redirect is not a supported link kind")
	}
	if kind == "wikilink" {
		return false, invalid("wikilink links are written by saving a body, not directly")
	}
	if kind == "at" {
		var isDay bool
		if err := t.tx.QueryRowContext(t.ctx, `SELECT coalesce((SELECT preferred_name_key = day FROM entities WHERE id = ?), 0)`, from).Scan(&isDay); err != nil {
			return false, err
		}
		if !isDay {
			return false, invalid("an at link starts at a day page (D16)")
		}
	}
	if kind == "part-of" {
		var invalidSelection bool
		err := t.tx.QueryRowContext(t.ctx, `SELECT EXISTS (
            SELECT 1 FROM entities p JOIN entities category ON category.id=?
            WHERE p.id=? AND p.entity_type='period' AND category.deleted_at IS NOT NULL
              AND NOT EXISTS (SELECT 1 FROM links WHERE from_id=p.id AND to_id=category.id AND kind='part-of')
        )`, to, from).Scan(&invalidSelection)
		if err != nil {
			return false, err
		}
		if invalidSelection {
			return false, invalid("a new period category must be live")
		}
		// A category is never a journal page (D26).
		var odd bool
		if err := t.tx.QueryRowContext(t.ctx, `SELECT coalesce((SELECT preferred_name_key = day FROM entities WHERE id = ?), 0)`, to).Scan(&odd); err != nil {
			return false, err
		}
		if odd {
			return false, invalid("a category is a plain page, never a day page (D26)")
		}
	}
	res, err := t.tx.ExecContext(t.ctx, `INSERT INTO links(from_id, to_id, kind, note, created_at, source) VALUES (?, ?, ?, ?, `+Now+`, ?)
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
	if kind == "redirect" {
		return invalid("redirect is not a supported link kind")
	}
	if kind == "wikilink" {
		return invalid("wikilink links are removed by saving a body, not directly")
	}
	res, err := t.tx.ExecContext(t.ctx, `DELETE FROM links WHERE from_id = ? AND to_id = ? AND kind = ?`, from, to, kind)
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
	ID               int64   `json:"id,omitempty"`
	Metric           string  `json:"metric"`
	Day              string  `json:"day"`
	TakenAt          string  `json:"taken_at,omitempty"`
	TZ               string  `json:"tz,omitempty"`
	Key              string  `json:"import_key,omitempty"`
	Value            float64 `json:"value"`
	CapturedWith     int64   `json:"captured_with_id,omitempty"`
	SessionID        int64   `json:"session_id,omitempty"`
	SessionDeletedAt string  `json:"session_deleted_at,omitempty"`
	Scope            string  `json:"scope,omitempty"`
}

// Record appends a measurement. A Key makes a re-send a no-op (ON CONFLICT ... DO NOTHING, docs/contract/imports.md);
// the id is 0 when the row was recorded before.
func (t *Tx) Record(m Reading) (id int64, err error) {
	metric, err := t.prepareReading(m)
	if err != nil {
		return 0, err
	}
	if m.Key != "" {
		var previous sql.NullInt64
		err := t.tx.QueryRowContext(t.ctx, `SELECT session_id FROM measurements WHERE source=? AND import_key=? AND metric_id=?`, t.Source, m.Key, metric).Scan(&previous)
		if err == nil && previous.Int64 != m.SessionID {
			return 0, conflict("measurement source identity cannot change session scope")
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
	}
	var session any
	if m.SessionID != 0 {
		session = m.SessionID
	}
	var with any
	if m.CapturedWith != 0 {
		with = m.CapturedWith
	}
	err = t.tx.QueryRowContext(t.ctx, `INSERT INTO measurements(metric_id, day, taken_at, tz, value, source, import_key, captured_with_id,session_id, created_at)
	                     VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, `+Now+`)
	                     ON CONFLICT(source, import_key, metric_id) WHERE import_key IS NOT NULL DO NOTHING
	                     RETURNING id`,
		metric, m.Day, nullIfEmpty(m.TakenAt), nullIfEmpty(m.TZ), m.Value, t.Source, nullIfEmpty(m.Key), with, session).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil // sent before
	}
	return id, err
}

func (t *Tx) prepareReading(m Reading) (metric int64, err error) {
	if err := validateMeasurementZone(m.TZ); err != nil {
		return 0, err
	}
	if !IsDay(m.Day) {
		return 0, invalid("day %q is not YYYY-MM-DD", m.Day)
	}
	if m.TakenAt != "" && !IsInstant(m.TakenAt) {
		return 0, invalid("taken_at %q is not a UTC instant like 2026-06-09T21:14:03.482Z", m.TakenAt)
	}
	if err := validateMeasurementValue(0, "", false, &m.Value); err != nil {
		return 0, err
	}
	var metricTitle string
	var habit bool
	err = t.tx.QueryRowContext(t.ctx, `SELECT m.id, m_name.title, EXISTS (SELECT 1 FROM habit_periods h WHERE h.metric_id = m.id) FROM entities m JOIN entity_names m_name ON m_name.entity_id = m.id AND m_name.name_key = m.preferred_name_key AND m.deleted_at IS NULL WHERE m.id = (SELECT entity_id FROM entity_names WHERE name_key = ?) AND m.entity_type = 'metric'`,
		text.TitleKey(m.Metric)).Scan(&metric, &metricTitle, &habit)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, notFound("no metric %q: the owner registers metrics", m.Metric)
	}
	if err != nil {
		return 0, err
	}
	if err := validateMeasurementValue(metric, metricTitle, habit, &m.Value); err != nil {
		return 0, err
	}
	return metric, nil
}

func validateMeasurementZone(zone string) error {
	if len(zone) > 64 {
		return invalid("invalid measurement zone")
	}
	for _, r := range zone {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("_/+-", r)) {
			return invalid("invalid measurement zone")
		}
	}
	return nil
}

// ReadingByKey is the current value of the reading a sender's key names; found is false when there is none.
// A retracted reading is found, with ok false.
func (t *Tx) ReadingByKey(metric, key string) (value float64, ok, found bool, err error) {
	var id int64
	err = t.tx.QueryRowContext(t.ctx, `SELECT me.id FROM measurements me JOIN entities m ON m.id = me.metric_id JOIN entity_names m_name ON m_name.entity_id = m.id AND m_name.name_key = m.preferred_name_key
	                      WHERE me.source = ? AND me.import_key = ? AND m.id = (SELECT entity_id FROM entity_names WHERE name_key = ?)`, t.Source, key, text.TitleKey(metric)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, false, nil
	}
	if err != nil {
		return 0, false, false, err
	}
	// follow the correction chain to its end (a chain never cycles: measurements_supersede_metric)
	var v sql.NullFloat64
	err = t.tx.QueryRowContext(t.ctx, `WITH RECURSIVE chain(id, value, depth) AS (
	                       SELECT id, value, 0 FROM measurements WHERE id = ?
	                       UNION ALL SELECT x.id, x.value, depth + 1 FROM measurements x JOIN chain c ON x.supersedes_id = c.id)
	                     SELECT value FROM chain ORDER BY depth DESC LIMIT 1`, id).Scan(&v)
	return v.Float64, v.Valid, true, err
}

// Correct supersedes a reading with a new value, or retracts it when value is nil (cookbook/correct-a-measurement.md).
func (t *Tx) Correct(wrong int64, value *float64) (id int64, err error) {
	return t.correct(wrong, value, "")
}

// CorrectKeyed is Correct with a writer-generated import_key for durable import-correction replay.
func (t *Tx) CorrectKeyed(wrong int64, value *float64, importKey string) (id int64, err error) {
	return t.correct(wrong, value, importKey)
}

func (t *Tx) correct(wrong int64, value *float64, importKey string) (id int64, err error) {
	var metric int64
	var metricTitle string
	var habit bool
	err = t.tx.QueryRowContext(t.ctx, `SELECT m.id, m_name.title, EXISTS (SELECT 1 FROM habit_periods h WHERE h.metric_id = m.id)
	                       FROM measurements me JOIN entities m ON m.id = me.metric_id JOIN entity_names m_name ON m_name.entity_id = m.id AND m_name.name_key = m.preferred_name_key WHERE me.id = ?`, wrong).Scan(&metric, &metricTitle, &habit)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, notFound("no measurement %d", wrong)
	}
	if err != nil {
		return 0, err
	}
	if err := validateMeasurementValue(metric, metricTitle, habit, value); err != nil {
		return 0, err
	}
	var v any
	if value != nil {
		v = *value
	}
	err = t.tx.QueryRowContext(t.ctx, `INSERT INTO measurements(metric_id, day, taken_at, tz, value, source, import_key, captured_with_id,session_id, supersedes_id, created_at)
	                     SELECT metric_id, day, taken_at, tz, ?, ?, ?, captured_with_id,session_id, id, `+Now+` FROM measurements WHERE id = ?
	                     RETURNING id`, v, t.Source, nullIfEmpty(importKey), wrong).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, notFound("no measurement %d", wrong)
	}
	return id, err
}

// MeasurementRow is the portable identity and value of one measurement row.
type MeasurementRow struct {
	ID         int64
	Source     string
	ImportKey  string
	Metric     string
	Value      *float64
	Supersedes int64
}

func (t *Tx) MeasurementRow(id int64) (MeasurementRow, error) {
	var r MeasurementRow
	var k sql.NullString
	var v sql.NullFloat64
	var supersedes sql.NullInt64
	err := t.tx.QueryRowContext(t.ctx, `SELECT me.id, me.source, me.import_key, m_name.title, me.value, me.supersedes_id
	                       FROM measurements me JOIN entities m ON m.id = me.metric_id JOIN entity_names m_name ON m_name.entity_id = m.id AND m_name.name_key = m.preferred_name_key  WHERE me.id = ?`, id).
		Scan(&r.ID, &r.Source, &k, &r.Metric, &v, &supersedes)
	if errors.Is(err, sql.ErrNoRows) {
		return r, notFound("no measurement %d", id)
	}
	if err != nil {
		return r, err
	}
	r.ImportKey = k.String
	if v.Valid {
		r.Value = &v.Float64
	}
	if supersedes.Valid {
		r.Supersedes = supersedes.Int64
	}
	return r, nil
}

// MeasurementRootAndLeaf returns the oldest ancestor and current leaf of a correction chain.
func (t *Tx) MeasurementRootAndLeaf(id int64) (root, leaf MeasurementRow, err error) {
	var rootID int64
	err = t.tx.QueryRowContext(t.ctx, `WITH RECURSIVE ancestors(id, supersedes_id, depth) AS (
	                       SELECT id, supersedes_id, 0 FROM measurements WHERE id = ?
	                       UNION ALL
	                       SELECT me.id, me.supersedes_id, depth + 1 FROM measurements me JOIN ancestors a ON me.id = a.supersedes_id)
	                     SELECT id FROM ancestors ORDER BY depth DESC LIMIT 1`, id).Scan(&rootID)
	if errors.Is(err, sql.ErrNoRows) {
		return root, leaf, notFound("no measurement %d", id)
	}
	if err != nil {
		return root, leaf, err
	}
	root, err = t.MeasurementRow(rootID)
	if err != nil {
		return root, leaf, err
	}
	leafID, _, _, err := t.CurrentOf(rootID)
	if err != nil {
		return root, leaf, err
	}
	leaf, err = t.MeasurementRow(leafID)
	return root, leaf, err
}

// MeasurementKey is the sender's key of a measurement's oldest ancestor: its source, import_key and metric
// ("" when it has none). A correction keeps its writer's provenance, but import replay records owner corrections
// against the original imported row (docs/guides/importing.md).
func (t *Tx) MeasurementKey(id int64) (source, key, metric string, err error) {
	var k sql.NullString
	err = t.tx.QueryRowContext(t.ctx, `WITH RECURSIVE ancestors(id, source, import_key, metric_id, supersedes_id, depth) AS (
	                       SELECT id, source, import_key, metric_id, supersedes_id, 0 FROM measurements WHERE id = ?
	                       UNION ALL
	                       SELECT me.id, me.source, me.import_key, me.metric_id, me.supersedes_id, depth + 1
	                         FROM measurements me JOIN ancestors a ON me.id = a.supersedes_id)
	                     SELECT a.source, a.import_key, m_name.title FROM ancestors a JOIN entities m ON m.id = a.metric_id JOIN entity_names m_name ON m_name.entity_id = m.id AND m_name.name_key = m.preferred_name_key
	                      ORDER BY depth DESC LIMIT 1`, id).Scan(&source, &k, &metric)
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
