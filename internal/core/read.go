package core

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sort"
	"time"

	"lifelog/internal/text"
)

// Page is one page with what surrounds it. Version is entities.updated_at: pass it back to SaveBody.
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
	IsStub    bool    `json:"is_redirect_stub"`
	Person    *Person `json:"person,omitempty"`
	File      *File   `json:"file,omitempty"`
	Point     *Point  `json:"point,omitempty"` // a place's, when it has one (D21)
	Out       []Edge  `json:"links"`
	In        []Edge  `json:"backlinks"`
}

type Person struct {
	Name  string `json:"name"`
	Birth string `json:"birth_day,omitempty"`
	Death string `json:"death_day,omitempty"`
}

// Edge is a link seen from one end; ID and Title are the other end.
type Edge struct {
	Kind  string `json:"kind"`
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Type  string `json:"entity_type"`
	Note  string `json:"note,omitempty"`
}

// PageByID reads a page, tombstoned or not (the caller decides what a tombstone shows).
func (s *Store) PageByID(ctx context.Context, id int64) (*Page, error) {
	p := &Page{ID: id}
	var day, deleted sql.NullString
	err := s.DB.R.QueryRowContext(ctx, `
		SELECT p.entity_type, p.title, p.day, p.body, e.created_at, e.updated_at, e.deleted_at,
		       EXISTS (SELECT 1 FROM links r WHERE r.from_id = p.id AND r.kind = 'redirect')
		  FROM pages p JOIN entities e ON e.id = p.id WHERE p.id = ?`, id).
		Scan(&p.Type, &p.Title, &day, &p.Body, &p.CreatedAt, &p.Version, &deleted, &p.IsStub)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("no page %d", id)
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
	if p.Type == "place" {
		if p.Point, err = s.PlaceOf(ctx, id); err != nil {
			return nil, err
		}
	}
	if p.Type == "file" {
		p.File = &File{}
		if err := s.DB.R.QueryRowContext(ctx, `SELECT sha256, mime, preview IS NOT NULL FROM files WHERE id = ?`, id).
			Scan(&p.File.SHA256, &p.File.MIME, &p.File.Preview); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	// outgoing edges to live entities; a symmetric kind's mirror is the same edge, so it shows once here
	if p.Out, err = s.edges(ctx, `
		SELECT l.kind, l.to_id, pg.title, e.entity_type, coalesce(l.note, '')
		  FROM links l JOIN entities e ON e.id = l.to_id AND e.deleted_at IS NULL JOIN pages pg ON pg.id = l.to_id
		 WHERE l.from_id = ? ORDER BY l.kind, pg.title`, id); err != nil {
		return nil, err
	}
	// cookbook/backlinks.md: one redirect hop, the redirect row itself is not a backlink; a symmetric edge is
	// already in Out, so it is not listed twice
	if p.In, err = s.edges(ctx, `
		SELECT l.kind, l.from_id, pg.title, e.entity_type, coalesce(l.note, '')
		  FROM links l
		  JOIN entities e  ON e.id = l.from_id AND e.deleted_at IS NULL
		  JOIN pages    pg ON pg.id = l.from_id
		 WHERE l.to_id IN (SELECT ?1 UNION SELECT r.from_id FROM links r WHERE r.to_id = ?1 AND r.kind = 'redirect')
		   AND l.kind <> 'redirect'
		   AND l.kind NOT IN (SELECT kind FROM link_kinds WHERE symmetric = 1)
		 ORDER BY pg.title`, id); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) edges(ctx context.Context, q string, args ...any) ([]Edge, error) {
	rows, err := s.DB.R.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Edge{}
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.Kind, &e.ID, &e.Title, &e.Type, &e.Note); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PageID resolves a title through its title_key; 0 when no page has it.
func (s *Store) PageID(ctx context.Context, title string) (int64, error) {
	var id int64
	err := s.DB.R.QueryRowContext(ctx, `SELECT id FROM pages WHERE title_key = ?`, text.TitleKey(title)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// DayRow is one row of cookbook/day-view.md.
type DayRow struct {
	What   string `json:"what"`
	At     string `json:"at,omitempty"`
	Detail string `json:"detail"`
}

// Day is cookbook/day-view.md for one local day, plus the day page's id (0 when the day has none yet) and the
// day's readings with their ids, so a client can correct one.
type Day struct {
	Day      string    `json:"day"`
	PageID   int64     `json:"page_id,omitempty"`
	Rows     []DayRow  `json:"view"`
	Readings []Reading `json:"readings"`
}

func (s *Store) Day(ctx context.Context, day string) (*Day, error) {
	if !IsDay(day) {
		return nil, invalid("day %q is not YYYY-MM-DD", day)
	}
	d := &Day{Day: day, Rows: []DayRow{}}
	err := s.DB.R.QueryRowContext(ctx, `SELECT p.id FROM pages p JOIN entities e ON e.id = p.id
	                                     WHERE p.title_key = ? AND e.deleted_at IS NULL`, day).Scan(&d.PageID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	rows, err := s.DB.R.QueryContext(ctx, dayView, sql.Named("day", day))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r DayRow
		var at sql.NullString
		if err := rows.Scan(&r.What, &at, &r.Detail); err != nil {
			return nil, err
		}
		r.At = at.String
		d.Rows = append(d.Rows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	d.Readings, err = s.readings(ctx, `WHERE me.day = ? ORDER BY me.taken_at, me.id`, day)
	return d, err
}

// dayView is cookbook/day-view.md verbatim.
const dayView = `
SELECT what, at, detail FROM (
  SELECT 'day page' AS what, NULL AS at, p.body AS detail
    FROM pages p JOIN entities e ON e.id = p.id
   WHERE p.title_key = :day AND e.deleted_at IS NULL
  UNION ALL
  SELECT 'page' || CASE WHEN e.updated_at > e.created_at THEN ' (edited)' ELSE '' END,
         e.updated_at, p.title
    FROM pages p JOIN entities e ON e.id = p.id
   WHERE p.day = :day AND p.title <> :day AND e.deleted_at IS NULL
  UNION ALL
  SELECT 'at', NULL, pl.title
    FROM pages d
    JOIN entities de ON de.id = d.id AND de.deleted_at IS NULL
    JOIN links l    ON l.from_id = d.id AND l.kind = 'at'
    JOIN pages pl   ON pl.id = l.to_id
    JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL
   WHERE d.title_key = :day
  UNION ALL
  SELECT 'habit', NULL, m.title || ': ' ||
         CASE (SELECT max(v.value) FROM measurement_values v WHERE v.metric_id = m.id AND v.day = :day)
           WHEN 1 THEN 'done' WHEN 0 THEN 'not done' ELSE 'not recorded' END
    FROM habit_periods h JOIN pages m ON m.id = h.metric_id
   WHERE h.start_day <= :day AND coalesce(h.end_day, '9999-12-31') >= :day
  UNION ALL
  SELECT p.title, me.taken_at, CAST(me.value AS TEXT) || ' ' || m.unit
    FROM measurement_values me JOIN metrics m ON m.id = me.metric_id JOIN pages p ON p.id = m.id
   WHERE me.day = :day
     AND NOT EXISTS (SELECT 1 FROM habit_periods h WHERE h.metric_id = me.metric_id
                        AND h.start_day <= :day AND coalesce(h.end_day, '9999-12-31') >= :day)
)
ORDER BY (at IS NOT NULL), at;`

func (s *Store) readings(ctx context.Context, where string, args ...any) ([]Reading, error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT me.id, m.title, me.day, coalesce(me.taken_at, ''), coalesce(me.tz, ''), me.value, coalesce(me.captured_with_id, 0)
		  FROM measurement_values me JOIN pages m ON m.id = me.metric_id `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Reading{}
	for rows.Next() {
		var r Reading
		if err := rows.Scan(&r.ID, &r.Metric, &r.Day, &r.TakenAt, &r.TZ, &r.Value, &r.CapturedWith); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Hit is one full-text match (cookbook/full-text-search.md).
type Hit struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

func (s *Store) Search(ctx context.Context, q string, limit int) ([]Hit, error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT p.id, p.title, snippet(pages_fts, 1, '[', ']', '…', 24)
		  FROM pages_fts
		  JOIN pages p    ON p.id = pages_fts.rowid
		  JOIN entities e ON e.id = p.id AND e.deleted_at IS NULL
		 WHERE pages_fts MATCH ?
		 ORDER BY rank LIMIT ?`, q, limit)
	if err != nil {
		return nil, invalid("search %q: %v", q, err)
	}
	defer rows.Close()
	out := []Hit{}
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.ID, &h.Title, &h.Snippet); err != nil {
			return nil, invalid("search %q: %v", q, err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// Metric is a live metric (D27): its page's id, title (its name) and body (its note), its unit; Habit says it has
// a period (D24), Categories are the paths of the categories it is filed in (D26).
type Metric struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Unit       string   `json:"unit"`
	Note       string   `json:"note,omitempty"`
	Habit      bool     `json:"habit"`
	Categories []string `json:"categories,omitempty"`
}

func (s *Store) Metrics(ctx context.Context) ([]Metric, error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT m.id, p.title, m.unit, p.body, EXISTS (SELECT 1 FROM habit_periods h WHERE h.metric_id = m.id)
		  FROM metrics m JOIN pages p ON p.id = m.id JOIN entities e ON e.id = m.id AND e.deleted_at IS NULL
		 ORDER BY p.title_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Metric{}
	at := map[int64]int{}
	for rows.Next() {
		var m Metric
		if err := rows.Scan(&m.ID, &m.Name, &m.Unit, &m.Note, &m.Habit); err != nil {
			return nil, err
		}
		at[m.ID] = len(out)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	cats, err := s.categories(ctx)
	if err != nil {
		return nil, err
	}
	filed, err := s.DB.R.QueryContext(ctx, `SELECT from_id, to_id FROM links WHERE kind = 'part-of'`)
	if err != nil {
		return nil, err
	}
	defer filed.Close()
	for filed.Next() {
		var from, to int64
		if err := filed.Scan(&from, &to); err != nil {
			return nil, err
		}
		if i, ok := at[from]; ok && cats[to] != nil {
			out[i].Categories = append(out[i].Categories, cats[to].Path)
		}
	}
	for i := range out {
		sort.Strings(out[i].Categories)
	}
	return out, filed.Err()
}

// InUse names the metrics with a current reading in the n days up to day, so a view offers the series the
// owner keeps rather than the whole registry (a lab marker read twice a year stays at /metrics).
func (s *Store) InUse(ctx context.Context, day string, n int) ([]string, error) {
	if !IsDay(day) {
		return nil, invalid("day is YYYY-MM-DD")
	}
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT m.title FROM pages m
		 WHERE m.entity_type = 'metric' AND EXISTS (SELECT 1 FROM measurement_values v
		                WHERE v.metric_id = m.id AND v.day > date(?, '-' || ? || ' day') AND v.day <= ?)
		 ORDER BY m.title_key`, day, n, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// Series is cookbook/metric-series.md: the current readings of one metric in (from, to].
func (s *Store) Series(ctx context.Context, metric, from, to string) ([]Reading, error) {
	if !IsDay(from) || !IsDay(to) {
		return nil, invalid("from and to are YYYY-MM-DD days")
	}
	return s.readings(ctx, `WHERE m.title_key = ? AND me.day > ? AND me.day <= ? ORDER BY me.day, me.taken_at`, text.TitleKey(metric), from, to)
}

// Measurement reads one row of the append-only table, current or not, and what corrected it.
type Measurement struct {
	Reading
	Current      bool  `json:"current"`
	Supersedes   int64 `json:"supersedes_id,omitempty"`
	SupersededBy int64 `json:"superseded_by,omitempty"`
	Retraction   bool  `json:"retraction"`
}

func (s *Store) Measurement(ctx context.Context, id int64) (*Measurement, error) {
	m := &Measurement{}
	var value sql.NullFloat64
	err := s.DB.R.QueryRowContext(ctx, `
		SELECT me.id, m.title, me.day, coalesce(me.taken_at, ''), coalesce(me.tz, ''), me.value,
		       coalesce(me.captured_with_id, 0), coalesce(me.supersedes_id, 0),
		       coalesce((SELECT x.id FROM measurements x WHERE x.supersedes_id = me.id), 0),
		       EXISTS (SELECT 1 FROM measurement_values v WHERE v.id = me.id)
		  FROM measurements me JOIN pages m ON m.id = me.metric_id WHERE me.id = ?`, id).
		Scan(&m.ID, &m.Metric, &m.Day, &m.TakenAt, &m.TZ, &value, &m.CapturedWith, &m.Supersedes, &m.SupersededBy, &m.Current)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("no measurement %d", id)
	}
	m.Value, m.Retraction = value.Float64, !value.Valid
	return m, err
}

// Ghost is a row of the ghost_pages view (cookbook/ghost-pages.md).
type Ghost struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
}

func (s *Store) Ghosts(ctx context.Context) ([]Ghost, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, title, created_at FROM ghost_pages ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Ghost{}
	for rows.Next() {
		var g Ghost
		if err := rows.Scan(&g.ID, &g.Title, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// Recent lists the latest day pages, newest first.
func (s *Store) RecentDays(ctx context.Context, limit int) ([]Edge, error) {
	return s.edges(ctx, `SELECT 'day', p.id, p.title, 'page', '' FROM pages p JOIN entities e ON e.id = p.id
	                      WHERE p.title = p.day AND e.deleted_at IS NULL ORDER BY p.day DESC LIMIT ?`, limit)
}

// Named lists the live people or places.
func (s *Store) Named(ctx context.Context, typ string) ([]Edge, error) {
	return s.edges(ctx, `SELECT p.entity_type, p.id, p.title, p.entity_type, coalesce(pe.name, '')
	                       FROM pages p JOIN entities e ON e.id = p.id LEFT JOIN people pe ON pe.id = p.id
	                      WHERE p.entity_type = ? AND e.deleted_at IS NULL ORDER BY p.title`, typ)
}

// Result is the answer to an ad-hoc read-only query.
type Result struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	Truncated bool     `json:"truncated"`
}

// Query runs one statement on the read-only pool (mode=ro, query_only): any SQL may be sent, none can write. The
// statement runs on a connection of its own, which gets query_only and trusted_schema=OFF back before it returns
// to the pool (connections.md): a PRAGMA sent here never reaches the next reader.
func (s *Store) Query(ctx context.Context, q string, maxRows int) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := s.DB.R.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer restoreReader(conn)
	rows, err := conn.QueryContext(ctx, q)
	if err != nil {
		return nil, invalid("%v", err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	res := &Result{Columns: cols, Rows: [][]any{}}
	for rows.Next() {
		if len(res.Rows) == maxRows {
			res.Truncated = true
			break
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, invalid("%v", err)
		}
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		res.Rows = append(res.Rows, vals)
	}
	if err := rows.Err(); err != nil {
		return nil, invalid("%v", err)
	}
	return res, nil
}

// restoreReader sets a read connection's pragmas again and returns it to the pool; one that cannot be restored is
// closed instead, never reused.
func restoreReader(conn *sql.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := conn.ExecContext(ctx, `PRAGMA query_only = ON; PRAGMA trusted_schema = OFF`); err != nil {
		conn.Raw(func(any) error { return driver.ErrBadConn })
	}
	conn.Close()
}
