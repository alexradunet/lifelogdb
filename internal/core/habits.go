package core

import (
	"context"
	"database/sql"
	"errors"

	"lifelog/internal/text"
)

// RegisterMetric makes a metric (D27): a page titled name, whose body is the note, and its metrics row with the
// unit, one id. A plain page of that title becomes the metric, as a person is promoted (D20): a ghost, or a note
// the owner wrote about it, whose text is kept — the note is written only to an empty body. Registering one that
// exists with the same unit is a no-op; another unit is refused: a unit never changes (metrics_unit_fixed).
func (t *Tx) RegisterMetric(name, unit, note string) (added bool, err error) {
	if !text.ValidTitle(name) {
		return false, invalid("metric name %q is not a valid title (docs/contract/titles-and-wikilinks.md)", name)
	}
	p, err := t.Lookup(name)
	if err != nil {
		return false, err
	}
	switch {
	case p == nil:
		id, _, err := t.insertPage("metric", name, text.TitleKey(name), nil, "", "")
		if err != nil {
			return false, err
		}
		if _, err = t.tx.ExecContext(t.ctx, `INSERT INTO metrics(id, unit) VALUES (?, ?)`, id, unit); err != nil {
			return false, err
		}
		_, err = t.SetBody(id, note)
		return err == nil, err
	case p.Type == "metric":
		var have string
		if err := t.tx.QueryRowContext(t.ctx, `SELECT unit FROM metrics WHERE id = ?`, p.ID).Scan(&have); err != nil {
			return false, err
		}
		if have != unit {
			return false, conflict("metric %s exists with unit %q, not %q: a unit never changes; register a new metric", p.Title, have, unit)
		}
		return false, nil
	case p.Type != "page" || p.DayPage:
		return false, conflict("%s is taken by a %s or a day page: a metric's title must be free or a plain page (D27)", p.Title, p.Type)
	}
	if _, err := t.tx.ExecContext(t.ctx, `UPDATE entities SET entity_type = 'metric', deleted_at = NULL WHERE id = ?`, p.ID); err != nil {
		return false, err
	}
	if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO metrics(id, unit) VALUES (?, ?)`, p.ID, unit); err != nil {
		return false, err
	}
	if note != "" && p.Body == "" { // the owner's text is never overwritten
		if _, err := t.SetBody(p.ID, note); err != nil {
			return false, err
		}
	}
	return true, nil
}

// metricID finds a live metric by its name, in any case (its title_key).
func (t *Tx) metricID(name string) (int64, error) {
	var id int64
	err := t.tx.QueryRowContext(t.ctx, `SELECT p.id FROM entities p JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key AND p.deleted_at IS NULL
	                       WHERE p.id = (SELECT entity_id FROM entity_names WHERE name_key = ?) AND p.entity_type = 'metric'`, text.TitleKey(name)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, notFound("no metric %q: the owner registers metrics", name)
	}
	return id, err
}

// StartHabit gives a unitless metric a period from start (cookbook/habits.md, D24). end is "" for a habit that is
// still going. A re-sent period inserts nothing and carries its end: a changed end is applied to the period
// that starts on that day.
func (t *Tx) StartHabit(metric, start, end string) error {
	if !IsDay(start) || (end != "" && !IsDay(end)) {
		return invalid("days are YYYY-MM-DD")
	}
	id, err := t.metricID(metric)
	if err != nil {
		return err
	}
	if id == 1 {
		return invalid("the seeded Mood metric cannot be a habit")
	}
	var other bool
	if err := t.tx.QueryRowContext(t.ctx, `SELECT EXISTS (SELECT 1 FROM measurement_values WHERE metric_id = ? AND value NOT IN (0, 1))`, id).
		Scan(&other); err != nil {
		return err
	}
	if other {
		return invalid("%s has readings other than 0 and 1: a habit is 1 = done, 0 = not done (D24)", metric)
	}
	if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO habit_periods(metric_id, start_day, end_day, source) VALUES (?, ?, ?, ?)
	                        ON CONFLICT(metric_id, start_day) DO NOTHING`, id, start, nullIfEmpty(end), t.Source); err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, `UPDATE habit_periods SET end_day = ? WHERE metric_id = ? AND start_day = ? AND end_day IS NOT ?`,
		nullIfEmpty(end), id, start, nullIfEmpty(end))
	return err
}

// StopHabit ends the open period of a habit on day (inclusive).
func (t *Tx) StopHabit(metric, day string) error {
	if !IsDay(day) {
		return invalid("day %q is not YYYY-MM-DD", day)
	}
	id, err := t.metricID(metric)
	if err != nil {
		return err
	}
	res, err := t.tx.ExecContext(t.ctx, `UPDATE habit_periods SET end_day = ? WHERE metric_id = ? AND end_day IS NULL`, day, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("%s has no open period", metric)
	}
	return nil
}

func (s *Store) RegisterMetric(ctx context.Context, source, name, unit, note string) (added bool, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) { added, e = t.RegisterMetric(name, unit, note); return })
	return
}

func (s *Store) StartHabit(ctx context.Context, source, metric, start, end string) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.StartHabit(metric, start, end) })
}

func (s *Store) StopHabit(ctx context.Context, source, metric, day string) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.StopHabit(metric, day) })
}

// HabitState is one habit of a day: done, not done, not recorded or invalid.
type HabitState struct {
	Metric string `json:"metric"`
	State  string `json:"state"`
}

// Habits is cookbook/habits.md: the habits of a day.
func (s *Store) Habits(ctx context.Context, day string) ([]HabitState, error) {
	if !IsDay(day) {
		return nil, invalid("day %q is not YYYY-MM-DD", day)
	}
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT m_name.title,
		       CASE WHEN EXISTS (SELECT 1 FROM measurement_values v WHERE v.metric_id = m.id AND v.day = :day AND v.session_id IS NULL AND v.value NOT IN (0, 1))
		         THEN 'invalid'
		         ELSE CASE (SELECT max(v.value) FROM measurement_values v WHERE v.metric_id = m.id AND v.day = :day AND v.session_id IS NULL)
		           WHEN 1 THEN 'done' WHEN 0 THEN 'not done' ELSE 'not recorded' END END AS state
		  FROM habit_periods h JOIN entities m ON m.id = h.metric_id JOIN entity_names m_name ON m_name.entity_id = m.id AND m_name.name_key = m.preferred_name_key AND m.deleted_at IS NULL
		 WHERE h.start_day <= :day AND coalesce(h.end_day, '9999-12-31') >= :day
		 ORDER BY m.preferred_name_key`, sql.Named("day", day))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HabitState{}
	for rows.Next() {
		var h HabitState
		if err := rows.Scan(&h.Metric, &h.State); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// Completion is one habit's count over a period (cookbook/habits.md).
type Completion struct {
	Metric      string `json:"metric"`
	ActiveDays  int    `json:"active_days"`
	Done        int    `json:"done"`
	NotDone     int    `json:"not_done"`
	NotRecorded int    `json:"not_recorded"`
	Invalid     int    `json:"invalid"`
}

func (s *Store) Completion(ctx context.Context, from, to string) ([]Completion, error) {
	if !IsDay(from) || !IsDay(to) || from > to {
		return nil, invalid("from and to are YYYY-MM-DD days, from <= to")
	}
	rows, err := s.DB.R.QueryContext(ctx, `
		WITH RECURSIVE days(day) AS (
		  SELECT :from_day
		  UNION ALL
		  SELECT date(day, '+1 day') FROM days WHERE day < :to_day
		),
		active AS (
		  SELECT h.metric_id, d.day
		    FROM days d JOIN habit_periods h ON h.start_day <= d.day AND coalesce(h.end_day, '9999-12-31') >= d.day
		)
		SELECT m_name.title, count(*) AS active_days,
		       sum(s.value IS 1 AND s.invalid IS 0) AS done, sum(s.value IS 0 AND s.invalid IS 0) AS not_done,
		       sum(s.value IS NULL) AS not_recorded, sum(s.invalid IS 1) AS invalid
		  FROM active a
		  JOIN entities m ON m.id = a.metric_id JOIN entity_names m_name ON m_name.entity_id = m.id AND m_name.name_key = m.preferred_name_key AND m.deleted_at IS NULL
		  LEFT JOIN (SELECT metric_id, day, max(value) AS value, max(value NOT IN (0, 1)) AS invalid
		             FROM measurement_values WHERE session_id IS NULL GROUP BY metric_id, day) s
		         ON s.metric_id = a.metric_id AND s.day = a.day
		 GROUP BY m.id
		 ORDER BY m.preferred_name_key`, sql.Named("from_day", from), sql.Named("to_day", to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Completion{}
	for rows.Next() {
		var c Completion
		if err := rows.Scan(&c.Metric, &c.ActiveDays, &c.Done, &c.NotDone, &c.NotRecorded, &c.Invalid); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Period is one habit period.
type Period struct {
	Start string `json:"start_day"`
	End   string `json:"end_day,omitempty"`
}

// Periods lists a metric's habit periods, oldest first.
func (s *Store) Periods(ctx context.Context, metric string) ([]Period, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT h.start_day, coalesce(h.end_day, '') FROM habit_periods h
	                                         JOIN entities m ON m.id = h.metric_id JOIN entity_names m_name ON m_name.entity_id = m.id AND m_name.name_key = m.preferred_name_key  WHERE m.id = (SELECT entity_id FROM entity_names WHERE name_key = ?) ORDER BY h.start_day`, text.TitleKey(metric))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Period{}
	for rows.Next() {
		var p Period
		if err := rows.Scan(&p.Start, &p.End); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// MetricID is the id of a live metric, found by its name in any case.
func (t *Tx) MetricID(name string) (int64, error) { return t.metricID(name) }
