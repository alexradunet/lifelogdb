package core

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"

	"lifelog/internal/text"
)

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

// PageID resolves an owned name through the registry; 0 when no entity owns it.
func (s *Store) PageID(ctx context.Context, title string) (int64, error) {
	var id int64
	err := s.DB.R.QueryRowContext(ctx, `SELECT entity_id FROM entity_names WHERE name_key = ?`, text.TitleKey(title)).Scan(&id)
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
	if err := checkDay("day", day); err != nil {
		return nil, err
	}
	d := &Day{Day: day, Rows: []DayRow{}}
	err := s.DB.R.QueryRowContext(ctx, `SELECT id FROM entities WHERE id = (SELECT entity_id FROM entity_names WHERE name_key = ?) AND deleted_at IS NULL`, day).Scan(&d.PageID)
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

// Moods are the values of the current Mood readings, by day, from from through through, each day's
// in id order: cookbook/mood-over-time.md read for a window of days. The metric is found by its name key, so a
// renamed Mood is still the mood. A day without a reading is absent.
func (s *Store) Moods(ctx context.Context, from, through string) (map[string][]float64, error) {
	if err := checkWindow(from, through); err != nil {
		return nil, err
	}
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT me.day, me.value
		  FROM measurement_values me
		  JOIN entities e ON e.id = me.metric_id AND e.deleted_at IS NULL
		  JOIN entity_names n ON n.entity_id = me.metric_id AND n.name_key = 'mood'
		 WHERE me.day BETWEEN ? AND ?
		 ORDER BY me.day, me.id`, from, through)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]float64{}
	for rows.Next() {
		var day string
		var v float64
		if err := rows.Scan(&day, &v); err != nil {
			return nil, err
		}
		out[day] = append(out[day], v)
	}
	return out, rows.Err()
}

// DayEntry is a live day page as a list of days shows it: its day, its id, and the first line of its body as plain
// text.
type DayEntry struct {
	Day       string `json:"day"`
	PageID    int64  `json:"page_id"`
	FirstLine string `json:"first_line,omitempty"`
}

// DayEntries are the live day pages from from through through: in day order, or the newest first, and at most limit
// of them.
func (s *Store) DayEntries(ctx context.Context, from, through string, newestFirst bool, limit int) ([]DayEntry, error) {
	if err := checkWindow(from, through); err != nil {
		return nil, err
	}
	if limit < 1 {
		return nil, Invalid("day entries need a limit of 1 or more")
	}
	order := "ASC"
	if newestFirst {
		order = "DESC"
	}
	return s.dayEntries(ctx, `p.day BETWEEN ? AND ?`, order, limit, from, through)
}

// OnThisDay are the live day pages of earlier years that have the month and the day of day, the newest first, and at
// most limit of them. The text from the sixth character is "MM-DD", so 29 February matches 29 February only.
func (s *Store) OnThisDay(ctx context.Context, day string, limit int) ([]DayEntry, error) {
	if !IsDay(day) || limit < 1 {
		return nil, Invalid("on this day needs an exact day and a limit")
	}
	return s.dayEntries(ctx, `p.day < ? AND substr(p.day, 6) = substr(?, 6)`, "DESC", limit, day, day)
}

// dayEntries reads the live day pages that a condition on p.day selects. order is ASC or DESC, never user text.
func (s *Store) dayEntries(ctx context.Context, cond, order string, limit int, args ...any) ([]DayEntry, error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT p.day, p.id, p.body FROM entities p JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
		 WHERE `+cond+` AND p.is_journal AND p.deleted_at IS NULL
		 ORDER BY p.day `+order+` LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DayEntry{}
	for rows.Next() {
		var e DayEntry
		var body string
		if err := rows.Scan(&e.Day, &e.PageID, &body); err != nil {
			return nil, err
		}
		e.FirstLine = firstLine(body)
		out = append(out, e)
	}
	return out, rows.Err()
}

// firstLineRunes is how long a first line may be before it is cut.
const firstLineRunes = 160

// firstLine is the first line of a body that has text, as plain text: a heading's or a list item's marker cut, the
// brackets of a [[wikilink]] cut, and cut with "…" after 160 characters. It is for a list of days, not a rendering.
func firstLine(body string) string {
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(strings.TrimLeft(line, "#>-*+ "))
		if line == "" {
			continue
		}
		line = strings.NewReplacer("![[", "", "[[", "", "]]", "").Replace(line)
		if r := []rune(line); len(r) > firstLineRunes {
			line = strings.TrimSpace(string(r[:firstLineRunes])) + "…"
		}
		return line
	}
	return ""
}

// TitleMatch is a name of a live page that starts with a prefix: the name, and the preferred title, id and type of
// its page. For an alias the name and the title differ.
type TitleMatch struct {
	Name   string `json:"name"`
	Title  string `json:"title"`
	PageID int64  `json:"page_id"`
	Type   string `json:"entity_type"`
}

// TitlesByPrefix are the names of live pages whose name key starts with the title key of prefix, in key order, and
// at most limit of them: what an editor suggests after [[. The read is a range on the unique index of name_key:
// every key with the prefix is at or after it, and before it followed by the byte 0xFF, which no UTF-8 text holds.
func (s *Store) TitlesByPrefix(ctx context.Context, prefix string, limit int) ([]TitleMatch, error) {
	key := text.TitleKey(strings.TrimSpace(prefix))
	if key == "" || limit < 1 {
		return nil, Invalid("a title prefix needs text and a limit")
	}
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT n.title, p.title, e.id, e.entity_type
		  FROM entity_names n
		  JOIN entities e ON e.id = n.entity_id AND e.deleted_at IS NULL
		  JOIN entity_names p ON p.entity_id = e.id AND p.name_key = e.preferred_name_key
		 WHERE n.name_key >= ? AND n.name_key < ?
		 ORDER BY n.name_key LIMIT ?`, key, key+"\xff", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TitleMatch{}
	for rows.Next() {
		var m TitleMatch
		if err := rows.Scan(&m.Name, &m.Title, &m.PageID, &m.Type); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// dayView implements cookbook/day-view.md with registry lookup and preferred display.
const dayView = `
SELECT what, at, detail FROM (
  SELECT 'day page' AS what, NULL AS at, p.body AS detail
    FROM entities p
   WHERE p.id = (SELECT entity_id FROM entity_names WHERE name_key = :day) AND p.deleted_at IS NULL
  UNION ALL
  SELECT 'page' || CASE WHEN p.revision > 1 THEN ' (edited)' ELSE '' END,
         p.updated_at, p_name.title
    FROM entities p JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
   WHERE p.day = :day AND p_name.title <> :day AND p.deleted_at IS NULL
  UNION ALL
  SELECT 'at', NULL, pl_name.title
    FROM entities d
    JOIN links l    ON l.from_id = d.id AND l.kind = 'at'
    JOIN entities pl ON pl.id = l.to_id JOIN entity_names pl_name ON pl_name.entity_id = pl.id AND pl_name.name_key = pl.preferred_name_key AND pl.deleted_at IS NULL
   WHERE d.id = (SELECT entity_id FROM entity_names WHERE name_key = :day) AND d.deleted_at IS NULL
  UNION ALL
  SELECT p_name.title, me.taken_at, CAST(me.value AS TEXT) || ' ' || m.unit
    FROM measurement_values me JOIN metrics m ON m.id = me.metric_id JOIN entities p ON p.id = m.id AND p.deleted_at IS NULL JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
   WHERE me.day = :day
)
ORDER BY (at IS NOT NULL), at;`

// readingColumns are a reading's columns as Reading scans them, from measurements me joined to its metric m with
// its name m_name.
const readingColumns = `me.id, m_name.title, me.day, coalesce(me.taken_at, ''), coalesce(me.tz, ''), me.value, coalesce(me.captured_with_id, 0)`

func (s *Store) readings(ctx context.Context, where string, args ...any) ([]Reading, error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT `+readingColumns+`
		  FROM measurement_values me JOIN entities m ON m.id = me.metric_id  JOIN entity_names m_name ON m_name.entity_id = m.id AND m_name.name_key = m.preferred_name_key `+where, args...)
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

// SnippetOpen and SnippetClose mark a match in a Hit's snippet. They are not brackets, because a body's own text
// holds [[wikilinks]] and task boxes, and a mark must never be confused with the text.
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

// Metric is a live metric (D27): its page's id, title (its name) and body (its note), its unit; Categories are the
// paths of the categories it is filed in (D26).
type Metric struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Unit       string   `json:"unit"`
	Note       string   `json:"note,omitempty"`
	Categories []string `json:"categories,omitempty"`
}

func (s *Store) Metrics(ctx context.Context) ([]Metric, error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT m.id, p_name.title, m.unit, p.body
		  FROM metrics m JOIN entities p ON p.id = m.id JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key AND p.deleted_at IS NULL
		 ORDER BY p.preferred_name_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Metric{}
	at := map[int64]int{}
	for rows.Next() {
		var m Metric
		if err := rows.Scan(&m.ID, &m.Name, &m.Unit, &m.Note); err != nil {
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

// Recent is one of a metric's recent readings: its measurement, its day and its value.
type Recent struct {
	ID    int64   `json:"id"`
	Day   string  `json:"day"`
	Value float64 `json:"value"`
}

// RecentReadings are the newest n current unassociated readings of each live metric, oldest first, in the series'
// order, so the last is the reading a metric's page calls its latest.
// Each metric's readings are read backwards on measurements_series and stop at n, so the work is n a metric, not
// the whole history.
func (s *Store) RecentReadings(ctx context.Context, n int) (map[int64][]Recent, error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT v.metric_id, v.id, v.day, v.value
		  FROM metrics m CROSS JOIN entities e ON e.id = m.id AND e.deleted_at IS NULL -- the metrics drive, not every page
		  JOIN measurement_values v ON v.id IN (
		        SELECT x.id FROM measurement_values x
		         WHERE x.metric_id = m.id
		         ORDER BY x.day DESC, x.taken_at DESC, x.id DESC LIMIT ?)
		 ORDER BY v.metric_id, v.day, v.taken_at, v.id`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]Recent{}
	for rows.Next() {
		var metric int64
		var r Recent
		if err := rows.Scan(&metric, &r.ID, &r.Day, &r.Value); err != nil {
			return nil, err
		}
		out[metric] = append(out[metric], r)
	}
	return out, rows.Err()
}

// InUse names the metrics with a current reading in the n days up to day, so a view offers the series the
// owner keeps rather than the whole registry (a lab marker read twice a year stays at /metrics).
func (s *Store) InUse(ctx context.Context, day string, n int) ([]string, error) {
	if !IsDay(day) {
		return nil, Invalid("day is YYYY-MM-DD")
	}
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT m_name.title FROM entities m JOIN entity_names m_name ON m_name.entity_id = m.id AND m_name.name_key = m.preferred_name_key AND m.deleted_at IS NULL
		 WHERE m.entity_type = 'metric' AND EXISTS (SELECT 1 FROM measurement_values v
		                WHERE v.metric_id = m.id AND v.day > date(?, '-' || ? || ' day') AND v.day <= ?)
		 ORDER BY m.preferred_name_key`, day, n, day)
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
		return nil, Invalid("from and to are YYYY-MM-DD days")
	}
	return s.series(ctx, metric, from, to)
}

// SeriesHistory is Series with no lower day bound; to remains inclusive.
func (s *Store) SeriesHistory(ctx context.Context, metric, to string) ([]Reading, error) {
	if !IsDay(to) {
		return nil, Invalid("to is a YYYY-MM-DD day")
	}
	return s.series(ctx, metric, "", to)
}

// series reads the readings of the metric named metric up to to, and after from unless from is empty.
func (s *Store) series(ctx context.Context, metric, from, to string) ([]Reading, error) {
	predicate := `WHERE m.id=(SELECT entity_id FROM entity_names WHERE name_key=?) AND me.day<=?`
	args := []any{text.TitleKey(metric), to}
	if from != "" {
		predicate += ` AND me.day>?`
		args = append(args, from)
	}
	return s.readings(ctx, predicate+` ORDER BY me.day,me.taken_at,me.id`, args...)
}

// ReadingsCount is how many current readings fall on the days from through through, both included:
// the readings that Series gives, for all metrics. A corrected reading counts once, and a retracted one not at all.
func (s *Store) ReadingsCount(ctx context.Context, from, through string) (int, error) {
	if err := checkWindow(from, through); err != nil {
		return 0, err
	}
	var n int
	err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM measurement_values WHERE day BETWEEN ? AND ?`, from, through).Scan(&n)
	return n, err
}

// ReadingsByDay counts the readings of ReadingsCount for each day from from through through; a day without one is
// absent.
func (s *Store) ReadingsByDay(ctx context.Context, from, through string) (map[string]int, error) {
	if err := checkWindow(from, through); err != nil {
		return nil, err
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT day, count(*) FROM measurement_values WHERE day BETWEEN ? AND ? GROUP BY day`, from, through)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var day string
		var n int
		if err := rows.Scan(&day, &n); err != nil {
			return nil, err
		}
		out[day] = n
	}
	return out, rows.Err()
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
		SELECT `+readingColumns+`, coalesce(me.supersedes_id, 0),
		       coalesce((SELECT x.id FROM measurements x WHERE x.supersedes_id = me.id), 0),
		       EXISTS (SELECT 1 FROM measurement_values v WHERE v.id = me.id)
		  FROM measurements me JOIN entities m ON m.id = me.metric_id JOIN entity_names m_name ON m_name.entity_id = m.id AND m_name.name_key = m.preferred_name_key  WHERE me.id = ?`, id).
		Scan(&m.ID, &m.Metric, &m.Day, &m.TakenAt, &m.TZ, &value, &m.CapturedWith, &m.Supersedes, &m.SupersededBy, &m.Current)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no measurement %d", id)
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

// Ghosts is one page of the ghost_pages view, oldest first.
func (s *Store) Ghosts(ctx context.Context, limit, offset int) ([]Ghost, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, title, created_at FROM ghost_pages ORDER BY created_at, id LIMIT ? OFFSET ?`, limit, offset)
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

// RecentDays is one page of the day pages, newest first.
func (s *Store) RecentDays(ctx context.Context, limit, offset int) ([]Edge, error) {
	return s.edges(ctx, `SELECT 'day', p.id, p_name.title, 'page', '' FROM entities p JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
	                      WHERE p.is_journal AND p.deleted_at IS NULL ORDER BY p.day DESC LIMIT ? OFFSET ?`, limit, offset)
}

// Named is one page of the live people, places or files, by title.
func (s *Store) Named(ctx context.Context, typ string, limit, offset int) ([]Edge, error) {
	return s.edges(ctx, `SELECT p.entity_type, p.id, p_name.title, p.entity_type, coalesce(pe.name, '')
	                       FROM entities p JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key LEFT JOIN people pe ON pe.id = p.id
	                      WHERE p.entity_type = ? AND p.deleted_at IS NULL ORDER BY p_name.title LIMIT ? OFFSET ?`, typ, limit, offset)
}

// LinkKindsFrom are the registered kinds a page of this type may start (link_kinds.from_types, NULL for any),
// the save contract's wikilink excluded, in kind order.
func (s *Store) LinkKindsFrom(ctx context.Context, typ string) ([]string, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT kind FROM link_kinds WHERE kind <> 'wikilink'
	    AND (from_types IS NULL OR instr(',' || from_types || ',', ',' || ? || ',') > 0) ORDER BY kind`, typ)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// NamedRow is a live person or place with how often the log mentions it.
type NamedRow struct {
	ID    int64
	Title string
	// Mentions is the number of live pages that link to this one, of any kind. A page counts once.
	Mentions int
	// LastDay is the day of the newest live day page among those pages. It is "" when there is none.
	LastDay string
	// Picture is the file whose preview stands for a person: the first embed of its body (![[title]]) that names a
	// live file with a preview. It is 0 when there is none, and for a place.
	Picture int64
}

// NamedMentions is one page of the live people or places, in the order and with the paging of Named.
// It reads the mentions in the same query, so the cost does not grow with the number of rows.
func (s *Store) NamedMentions(ctx context.Context, typ string, limit, offset int) ([]NamedRow, error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT p.id, p_name.title, count(DISTINCT lp.id),
		       coalesce(max(CASE WHEN lp_name.title = lp.day THEN lp.day END), ''), p.body
		  FROM entities p
		  JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
		  LEFT JOIN links l ON l.to_id = p.id
		  LEFT JOIN entities lp ON lp.id = l.from_id AND lp.deleted_at IS NULL
		  LEFT JOIN entity_names lp_name ON lp_name.entity_id = lp.id AND lp_name.name_key = lp.preferred_name_key
		 WHERE p.entity_type = ? AND p.deleted_at IS NULL
		 GROUP BY p.id, p_name.title
		 ORDER BY p_name.title LIMIT ? OFFSET ?`, typ, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out, bodies := []NamedRow{}, []string{}
	for rows.Next() {
		var r NamedRow
		var body string
		if err := rows.Scan(&r.ID, &r.Title, &r.Mentions, &r.LastDay, &body); err != nil {
			return nil, err
		}
		out, bodies = append(out, r), append(bodies, body)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if typ != "person" {
		return out, nil
	}
	for i := range out {
		if out[i].Picture, err = firstPicture(ctx, s.DB.R, bodies[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// firstPicture is the id of the first live file with a preview that body embeds, else 0. A title resolves through
// every name of a page, as GET /previews resolves it.
func firstPicture(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, body string) (int64, error) {
	for _, title := range text.EmbeddedTitles(body) {
		var id int64
		err := q.QueryRowContext(ctx, `
			SELECT f.id FROM entity_names n
			  JOIN entities e ON e.id = n.entity_id AND e.deleted_at IS NULL
			  JOIN files f ON f.id = e.id AND f.preview IS NOT NULL
			 WHERE n.name_key = ?`, text.TitleKey(title)).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		return id, err
	}
	return 0, nil
}

// MediaKinds are the kinds of file the media list shows, named by the first part of files.mime, in the order the
// list counts them. A file of any other kind (a PDF, a scan's text) is in the files list (OtherFiles).
var MediaKinds = []string{"image", "video", "audio"}

// FileRow is a live file the owner keeps (docs/cookbook/keep-a-file.md), as the media list and the files list show
// it.
type FileRow struct {
	ID    int64
	Title string
	MIME  string
	// Picture says the file keeps a preview: a photo's own, or the frame a video was sent with.
	Picture bool
	// Day is the file's day (entities.day: the day given, a photo's EXIF day, else the day it was kept), else the day of
	// the newest live day page that links to it (a photo kept with its capture day unknown, then shown in a day page);
	// "" when it has neither.
	Day string
	// Mentions is the number of live pages that link to this one, of any kind. A page counts once.
	Mentions int
}

// mediaKind is the first part of files.mime: 'image' of 'image/jpeg'. files_mime guarantees the slash.
const mediaKind = `substr(f.mime, 1, instr(f.mime, '/') - 1)`

// Media is one page of the live files of the media kinds: all of them, or the one kind named. Newest day first
// (FileRow.Day), then the newest kept.
func (s *Store) Media(ctx context.Context, kind string, limit, offset int) ([]FileRow, error) {
	return s.filesOf(ctx, true, kind, limit, offset)
}

// OtherFiles is one page of the live files of every other kind (a PDF, a text): the files list, in the order of Media.
func (s *Store) OtherFiles(ctx context.Context, limit, offset int) ([]FileRow, error) {
	return s.filesOf(ctx, false, "", limit, offset)
}

// filesOf is one page of the live files of the media kinds (media true) or of every other kind, one media kind alone
// when kind names it.
func (s *Store) filesOf(ctx context.Context, media bool, kind string, limit, offset int) ([]FileRow, error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT p.id, p_name.title, f.mime, f.preview IS NOT NULL,
		       coalesce(p.day, max(CASE WHEN lp_name.title = lp.day THEN lp.day END), '') AS day,
		       count(DISTINCT lp.id)
		  FROM entities p
		  JOIN files f ON f.id = p.id
		  JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
		  LEFT JOIN links l ON l.to_id = p.id
		  LEFT JOIN entities lp ON lp.id = l.from_id AND lp.deleted_at IS NULL
		  LEFT JOIN entity_names lp_name ON lp_name.entity_id = lp.id AND lp_name.name_key = lp.preferred_name_key
		 WHERE p.deleted_at IS NULL
		   AND (`+mediaKind+` IN ('image', 'video', 'audio')) = ?
		   AND (? = '' OR `+mediaKind+` = ?)
		 GROUP BY p.id, p_name.title, f.mime, f.preview IS NOT NULL
		 ORDER BY day DESC, p.created_at DESC, p.id DESC LIMIT ? OFFSET ?`, media, kind, kind, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FileRow{}
	for rows.Next() {
		var r FileRow
		if err := rows.Scan(&r.ID, &r.Title, &r.MIME, &r.Picture, &r.Day, &r.Mentions); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MediaCounts counts the live files of each media kind, by kind; a kind with none is absent.
func (s *Store) MediaCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+mediaKind+`, count(*) FROM files f JOIN entities p ON p.id = f.id
		WHERE p.deleted_at IS NULL AND `+mediaKind+` IN ('image', 'video', 'audio') GROUP BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}
