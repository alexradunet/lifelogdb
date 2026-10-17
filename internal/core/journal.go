package core

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"lifelog/internal/text"
)

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

// dayPage is the id of a local day's page: found (revived when tombstoned), or created on its first write
// (cookbook/capture.md).
func (t *Tx) dayPage(day string) (int64, error) {
	p, err := t.lookupKey(day)
	switch {
	case err != nil:
		return 0, err
	case p == nil:
		id, err := t.insertPage("page", day, day, day, "")
		return id, err
	case p.Deleted:
		_, err = t.tx.ExecContext(t.ctx, `UPDATE entities SET deleted_at = NULL WHERE id = ?`, p.ID)
	}
	return p.ID, err
}

func (s *Store) Capture(ctx context.Context, source, day, entry string, mood *float64) (int64, Sync, error) {
	m, err := alone(s, ctx, source, func(t *Tx) (made, error) {
		id, r, err := t.Capture(day, entry, mood)
		return made{id, r}, err
	})
	return m.id, m.sync, err
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
	var r DayRow
	var at sql.NullString
	rows, err := s.DB.R.QueryContext(ctx, dayView, sql.Named("day", day))
	if err := each(rows, err, []any{&r.What, &at, &r.Detail}, func() {
		r.At = at.String
		d.Rows = append(d.Rows, r)
	}); err != nil {
		return nil, err
	}
	d.Readings, err = s.readings(ctx, `WHERE me.day = ? ORDER BY me.taken_at, me.id`, day)
	return d, err
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
	out := map[string][]float64{}
	var day string
	var v float64
	if err := each(rows, err, []any{&day, &v}, func() { out[day] = append(out[day], v) }); err != nil {
		return nil, err
	}
	return out, nil
}

// DayEntry is a live day page as a list of days shows it: its day, its id, and the first line of its body as plain
// text.
type DayEntry struct {
	Day       string `json:"day"`
	PageID    int64  `json:"page_id"`
	FirstLine string `json:"first_line,omitempty"`
	// Picture is the day page's cover (0 when it has none); the calendar draws it behind the day.
	Picture int64  `json:"-"`
	body    string // read for Picture
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
	if !text.IsDay(day) || limit < 1 {
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
	out := []DayEntry{}
	var e DayEntry
	if err := each(rows, err, []any{&e.Day, &e.PageID, &e.body}, func() {
		e.FirstLine = firstLine(e.body)
		out = append(out, e)
	}); err != nil {
		return nil, err
	}
	return out, setPictures(ctx, s.DB.R, out, func(e *DayEntry) (string, *int64) { return e.body, &e.Picture })
}

// firstLineRunes is how long a first line may be before it is cut.
const firstLineRunes = 160

// firstLine is the first line of a body that has text, as plain text (text.Plain): a heading's or a list item's marker
// cut, the marks of emphasis, code and links cut, an ![[embed]] or an image left out (a picture is not words, and a
// cover goes first in the body), the brackets of a [[wikilink]] cut, and cut with "…" after 160 characters. A line
// with no words is passed over. It is for a list of days, not a rendering.
func firstLine(body string) string {
	for line := range strings.SplitSeq(body, "\n") {
		line = text.Plain(text.BlockMark.ReplaceAllString(strings.TrimSpace(line), ""))
		if line == "" {
			continue
		}
		if r := []rune(line); len(r) > firstLineRunes {
			line = strings.TrimSpace(string(r[:firstLineRunes])) + "…"
		}
		return line
	}
	return ""
}

// RecentDays is one page of the day pages, newest first.
func (s *Store) RecentDays(ctx context.Context, limit, offset int) ([]Edge, error) {
	return s.edges(ctx, `SELECT 'day', p.id, p_name.title, 'page', '' FROM entities p JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key
	                      WHERE p.is_journal AND p.deleted_at IS NULL ORDER BY p.day DESC LIMIT ? OFFSET ?`, limit, offset)
}
