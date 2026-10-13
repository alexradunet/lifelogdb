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
// exists with the same unit is a no-op, and so is one with no unit where the existing metric is a scale: a source
// that names Mood does not write its range (RangeUnit), so the request takes the metric as it is. Another unit is
// refused: a unit never changes (metrics_unit_fixed).
func (t *Tx) RegisterMetric(name, unit, note string) (added bool, err error) {
	if err := checkTitle("metric name", name); err != nil {
		return false, err
	}
	p, err := t.lookup(name)
	if err != nil {
		return false, err
	}
	switch {
	case p == nil:
		// the note is written by the insert: creation is one write, so the metric starts at revision 1
		id, _, err := t.insertPage("metric", name, text.TitleKey(name), nil, note, "")
		if err != nil {
			return false, err
		}
		if _, err = t.tx.ExecContext(t.ctx, `INSERT INTO metrics(id, unit) VALUES (?, ?)`, id, unit); err != nil {
			return false, err
		}
		_, err = t.syncWikilinks(id, note)
		return err == nil, err
	case p.Type == "metric":
		var have string
		if err := t.tx.QueryRowContext(t.ctx, `SELECT unit FROM metrics WHERE id = ?`, p.ID).Scan(&have); err != nil {
			return false, err
		}
		if _, _, scale := rangeUnit(have); have != unit && !(scale && unit == "") {
			return false, Conflict("metric %s exists with unit %q, not %q: a unit never changes; register a new metric", p.Title, have, unit)
		}
		return false, nil
	case p.Type != "page" || p.DayPage:
		return false, Conflict("%s is taken by a %s or a day page: a metric's title must be free or a plain page (D27)", p.Title, p.Type)
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
		return 0, NotFound("no metric %q: the owner registers metrics", name)
	}
	return id, err
}

// StartHabit gives a unitless metric a period from start (cookbook/habits.md, D24): the days, the unit, the overlap
// and the metric's readings being 0 or 1 are the habit_periods CHECKs and triggers. end is "" for a habit that is
// still going. A re-sent period inserts nothing and carries its end: a changed end is applied to the period
// that starts on that day.
func (t *Tx) StartHabit(metric, start, end string) error {
	id, err := t.metricID(metric)
	if err != nil {
		return err
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
	id, err := t.metricID(metric)
	if err != nil {
		return err
	}
	res, err := t.tx.ExecContext(t.ctx, `UPDATE habit_periods SET end_day = ? WHERE metric_id = ? AND end_day IS NULL`, day, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return NotFound("%s has no open period", metric)
	}
	return nil
}

func (s *Store) RegisterMetric(ctx context.Context, source, name, unit, note string) (added bool, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) { added, e = t.RegisterMetric(name, unit, note); return })
	if err != nil {
		added = false
	}
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

// Habits is cookbook/habits.md: the habits of a day, each with its state.
func (s *Store) Habits(ctx context.Context, day string) ([]HabitState, error) {
	if err := checkDay("day", day); err != nil {
		return nil, err
	}
	days, err := s.HabitStates(ctx, day, day)
	if err != nil {
		return nil, err
	}
	out := []HabitState{}
	for _, h := range days {
		out = append(out, h.HabitState)
	}
	return out, nil
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

func (s *Store) Completion(ctx context.Context, from, through string) ([]Completion, error) {
	if err := checkWindow(from, through); err != nil {
		return nil, err
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
		 ORDER BY m.preferred_name_key`, sql.Named("from_day", from), sql.Named("to_day", through))
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

// HabitPeriod is one habit period.
type HabitPeriod struct {
	Start string `json:"start_day"`
	End   string `json:"end_day,omitempty"`
}

// Periods lists a metric's habit periods, oldest first.
func (s *Store) HabitPeriods(ctx context.Context, metric string) ([]HabitPeriod, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT h.start_day, coalesce(h.end_day, '') FROM habit_periods h
	                                         JOIN entities m ON m.id = h.metric_id JOIN entity_names m_name ON m_name.entity_id = m.id AND m_name.name_key = m.preferred_name_key  WHERE m.id = (SELECT entity_id FROM entity_names WHERE name_key = ?) ORDER BY h.start_day`, text.TitleKey(metric))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HabitPeriod{}
	for rows.Next() {
		var p HabitPeriod
		if err := rows.Scan(&p.Start, &p.End); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CheckIn records a habit for a day inside one of its periods (cookbook/habits.md, D24): 1 for done, 0 for not
// done. The period check and the write are one transaction, so a habit stopped between them cannot take a reading.
func (t *Tx) CheckIn(metric, day string, done bool) (int64, error) {
	if err := checkDay("day", day); err != nil {
		return 0, err
	}
	id, err := t.metricID(metric)
	if err != nil {
		return 0, err
	}
	var on bool
	if err := t.tx.QueryRowContext(t.ctx, `SELECT EXISTS (SELECT 1 FROM habit_periods WHERE metric_id = ? AND start_day <= ? AND coalesce(end_day, '9999-12-31') >= ?)`, id, day, day).Scan(&on); err != nil {
		return 0, err
	}
	if !on {
		return 0, Invalid("%s is not a habit on %s: start it first", metric, day)
	}
	value := 0.0
	if done {
		value = 1
	}
	return t.Record(Reading{Metric: metric, Day: day, Value: value})
}

func (s *Store) CheckIn(ctx context.Context, source, metric, day string, done bool) (id int64, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) { id, e = t.CheckIn(metric, day, done); return })
	if err != nil {
		id = 0
	}
	return
}

// HabitDay is one habit's state on one day of a window.
type HabitDay struct {
	Day string `json:"day"`
	HabitState
}

// HabitStates are the states of every habit active on each day from from through through, by day then habit: the
// read of Habits over a window in one statement, so a view of 14 days does not read 14 times. The rule of a state
// (cookbook/habits.md) has this one home.
func (s *Store) HabitStates(ctx context.Context, from, through string) ([]HabitDay, error) {
	if err := checkWindow(from, through); err != nil {
		return nil, err
	}
	rows, err := s.DB.R.QueryContext(ctx, `
		WITH RECURSIVE days(day) AS (SELECT :from_day UNION ALL SELECT date(day, '+1 day') FROM days WHERE day < :through_day)
		SELECT d.day, m_name.title,
		       CASE WHEN EXISTS (SELECT 1 FROM measurement_values v WHERE v.metric_id = m.id AND v.day = d.day AND v.session_id IS NULL AND v.value NOT IN (0, 1))
		         THEN 'invalid'
		         ELSE CASE (SELECT max(v.value) FROM measurement_values v WHERE v.metric_id = m.id AND v.day = d.day AND v.session_id IS NULL)
		           WHEN 1 THEN 'done' WHEN 0 THEN 'not done' ELSE 'not recorded' END END AS state
		  FROM days d
		  JOIN habit_periods h ON h.start_day <= d.day AND coalesce(h.end_day, '9999-12-31') >= d.day
		  JOIN entities m ON m.id = h.metric_id JOIN entity_names m_name ON m_name.entity_id = m.id AND m_name.name_key = m.preferred_name_key AND m.deleted_at IS NULL
		 ORDER BY d.day, m.preferred_name_key`, sql.Named("from_day", from), sql.Named("through_day", through))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HabitDay{}
	for rows.Next() {
		var h HabitDay
		if err := rows.Scan(&h.Day, &h.Metric, &h.State); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// metricUnit is a registered metric's unit; found is false when no metric has the name.
func (t *Tx) metricUnit(name string) (unit string, found bool, err error) {
	err = t.tx.QueryRowContext(t.ctx, `SELECT m.unit FROM metrics m JOIN entities e ON e.id = m.id AND e.deleted_at IS NULL WHERE m.id = (SELECT entity_id FROM entity_names WHERE name_key = ?)`, text.TitleKey(name)).Scan(&unit)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return unit, err == nil, err
}
