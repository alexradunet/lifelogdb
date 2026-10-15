package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
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

// dryRun runs fn in a write transaction and always rolls it back: what a write would do, without doing it.
func (s *Store) dryRun(ctx context.Context, source string, fn func(*Tx) error) error {
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
	Added   []string `json:"added,omitempty"` // the linked titles the page did not link before this save
	Created []string `json:"created,omitempty"`
	Revived []string `json:"revived,omitempty"`
	Skipped []string `json:"skipped,omitempty"`
}

// savepoint runs fn inside a savepoint of the transaction: when fn fails, what it wrote is rolled back and the
// transaction goes on; fn's error is returned, a constraint or trigger of the DDL as the 422 that Do would return.
// A wikilink target that cannot be made, a facts write the writer refuses: the rest of the work is unaffected.
func (t *Tx) savepoint(fn func() error) error {
	if _, err := t.tx.ExecContext(t.ctx, `SAVEPOINT step`); err != nil {
		return err
	}
	if err := fn(); err != nil {
		if _, e := t.tx.ExecContext(t.ctx, `ROLLBACK TO step`); e != nil {
			return e
		}
		if _, e := t.tx.ExecContext(t.ctx, `RELEASE step`); e != nil {
			return e
		}
		return refused(err)
	}
	_, err := t.tx.ExecContext(t.ctx, `RELEASE step`)
	return err
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
	rows, err := t.tx.QueryContext(t.ctx, `SELECT to_id FROM links WHERE from_id = ? AND kind = 'wikilink'`, pageID)
	if err != nil {
		return Sync{}, err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return Sync{}, err
		}
		before[id] = true
	}
	if err := rows.Close(); err != nil {
		return Sync{}, err
	}
	if err := rows.Err(); err != nil {
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
                         VALUES (?, ?, ?, ?, `+nowSQL+`, `+nowSQL+`, ?, ?)
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

// pageRef is what a lookup by title finds.
type pageRef struct {
	ID      int64
	Type    string
	Title   string
	Body    string
	DayPage bool
	Deleted bool
}

// lookup finds the page that holds a title's key; nil when none does.
func (t *Tx) lookup(title string) (*pageRef, error) {
	return t.lookupKey(text.TitleKey(title))
}

func (t *Tx) lookupKey(key string) (*pageRef, error) {
	p := &pageRef{}
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

func (t *Tx) pageByID(id int64) (*pageRef, error) {
	var key string
	err := t.tx.QueryRowContext(t.ctx, `SELECT preferred_name_key FROM entities WHERE id = ?`, id).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no page %d", id)
	}
	if err != nil {
		return nil, err
	}
	return t.lookupKey(key)
}

// Capture appends an entry to a day page, creating or reviving it, with an optional mood (cookbook/capture.md).
func (t *Tx) Capture(day, entry string, mood *float64) (id int64, r Sync, err error) {
	if entry == "" && mood == nil {
		return 0, r, Invalid("nothing to capture: give text, a mood or both")
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

// rangeUnit says whether a metric's unit names a scale's range: two numbers of ASCII digits around one hyphen with
// the lower below the upper (`1-5`, `0-10`), and returns the two bounds. The range describes the metric, so a source
// never writes it beside a number: the importer takes the number alone as the evidence of a reading, and the writer
// holds every value of the metric to the whole numbers inside it. Only a 0/1 habit has no unit (D7, D24).
func rangeUnit(unit string) (lo, hi float64, ok bool) {
	a, b, found := strings.Cut(unit, "-")
	if !found || !asciiDigits(a) || !asciiDigits(b) {
		return 0, 0, false
	}
	// compared as digit strings, so a bound too large for a float64 still orders exactly (a unit is at most 32 bytes)
	if a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0"); len(a) > len(b) || len(a) == len(b) && a >= b {
		return 0, 0, false
	}
	lo, _ = strconv.ParseFloat("0"+a, 64)
	hi, _ = strconv.ParseFloat(b, 64)
	return lo, hi, true
}

func asciiDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

// checkValue refuses a NaN: SQLite binds it as NULL, which a correction cannot tell from a retraction (D7). Every
// other rule of a value is the DDL's: finite (measurements_value_finite), inside a scale's range
// (measurements_scale_range), 0 or 1 for a habit (measurements_habit_binary).
func checkValue(value *float64) error {
	if value != nil && math.IsNaN(*value) {
		return Invalid("value must be a number")
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
	if err := checkTitle("title", title); err != nil {
		return 0, false, r, err
	}
	switch {
	case IsDay(title):
		day = title
	case day == "":
		day = Today()
	}
	if id, existing, err = t.createNamed("page", title, body, day, importKey, nil); err != nil || existing {
		return id, existing, r, err
	}
	r, err = t.syncWikilinks(id, body)
	return id, false, r, err
}

// PageForTitle is the id of the page that holds a title, made as an empty page when no page has it: the context a
// task names by its title, like the target of a wikilink, and like it with no day, so it is not an entry of the day it
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

// ExistsError says the title is taken and by which page, so a client can follow it (promote it, edit it).
type ExistsError struct {
	ID    int64
	Title string
}

func (e *ExistsError) Error() string { return "a page titled " + e.Title + " already exists" }

func orExists(err error, p *pageRef, title string) error {
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
// state again changes nothing, as a task's lifecycle answers.
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

// Link adds an edge of a registered kind (D8); added reports whether it was new. wikilink rows belong to the
// save contract; redirect is unsupported, so neither is written here. The endpoint rules, an `at` from a day page
// among them, are links_endpoint_types. A period's category must be live: a rule with no home yet (issue 0068).
func (t *Tx) Link(from, to int64, kind, note string) (added bool, err error) {
	if kind == "redirect" {
		return false, Invalid("redirect is not a supported link kind")
	}
	if kind == "wikilink" {
		return false, Invalid("wikilink links are written by saving a body, not directly")
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
			return false, Invalid("a new period category must be live")
		}
	}
	res, err := t.tx.ExecContext(t.ctx, `INSERT INTO links(from_id, to_id, kind, note, created_at, source) VALUES (?, ?, ?, ?, `+nowSQL+`, ?)
	                       ON CONFLICT(from_id, to_id, kind) DO NOTHING`, from, to, kind, nullIfEmpty(note), t.Source)
	if err != nil {
		return false, refused(err) // the registry's endpoint types refuse the link: a 422, as Do would return it
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Unlink deletes one edge (links rows are the one table whose rows are deleted, D11); a symmetric kind's
// mirror goes with it (links_mirror_delete).
func (t *Tx) Unlink(from, to int64, kind string) error {
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

// Reading is one measurement to record (D7).
type Reading struct {
	ID           int64   `json:"id,omitempty"`
	Metric       string  `json:"metric"`
	Day          string  `json:"day"`
	TakenAt      string  `json:"taken_at,omitempty"`
	TZ           string  `json:"tz,omitempty"`
	ImportKey    string  `json:"import_key,omitempty"`
	Value        float64 `json:"value"`
	CapturedWith int64   `json:"captured_with_id,omitempty"`
}

// Record appends a measurement. An ImportKey makes a re-send a no-op (ON CONFLICT ... DO NOTHING, docs/contract/imports.md);
// the id is 0 when the row was recorded before.
func (t *Tx) Record(m Reading) (id int64, err error) {
	metric, err := t.prepareReading(m)
	if err != nil {
		return 0, err
	}
	var with any
	if m.CapturedWith != 0 {
		with = m.CapturedWith
	}
	err = t.tx.QueryRowContext(t.ctx, `INSERT INTO measurements(metric_id, day, taken_at, tz, value, source, import_key, captured_with_id, created_at)
	                     VALUES (?, ?, ?, ?, ?, ?, ?, ?, `+nowSQL+`)
	                     ON CONFLICT(source, import_key, metric_id) WHERE import_key IS NOT NULL DO NOTHING
	                     RETURNING id`,
		metric, m.Day, nullIfEmpty(m.TakenAt), nullIfEmpty(m.TZ), m.Value, t.Source, nullIfEmpty(m.ImportKey), with).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil // sent before
	}
	return id, err
}

// prepareReading is the live metric the reading names; the day, instant, zone and value are the DDL's to refuse.
func (t *Tx) prepareReading(m Reading) (metric int64, err error) {
	if err := checkValue(&m.Value); err != nil {
		return 0, err
	}
	return t.metricID(m.Metric)
}

// Correct supersedes a reading with a new value, or retracts it when value is nil (cookbook/correct-a-measurement.md).
func (t *Tx) Correct(wrong int64, value *float64) (id int64, err error) {
	if err := checkValue(value); err != nil {
		return 0, err
	}
	var v any
	if value != nil {
		v = *value
	}
	err = t.tx.QueryRowContext(t.ctx, `INSERT INTO measurements(metric_id, day, taken_at, tz, value, source, captured_with_id, supersedes_id, created_at)
	                     SELECT metric_id, day, taken_at, tz, ?, ?, captured_with_id, id, `+nowSQL+` FROM measurements WHERE id = ?
	                     RETURNING id`, v, t.Source, wrong).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, NotFound("no measurement %d", wrong)
	}
	return id, err
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
	if err != nil {
		r = Sync{}
	}
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

func (s *Store) Correct(ctx context.Context, source string, wrong int64, value *float64) (id int64, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) { id, e = t.Correct(wrong, value); return })
	if err != nil {
		id = 0
	}
	return
}

// setDeletedAt sets or clears a row's deleted_at in one of the tables whose rows carry a version: the tombstone of
// a task or an occurrence (D11: nothing is deleted).
func (t *Tx) setDeletedAt(table string, id int64, deleted bool) error {
	switch table {
	case "tasks", "task_occurrences":
	default:
		return fmt.Errorf("setDeletedAt: %s is not a table with a tombstone", table)
	}
	q := `UPDATE ` + table + ` SET deleted_at=NULL WHERE id=?`
	if deleted {
		q = `UPDATE ` + table + ` SET deleted_at=` + nowSQL + ` WHERE id=?`
	}
	_, err := t.tx.ExecContext(t.ctx, q, id)
	return err
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
