package core

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
)

var metricName = regexp.MustCompile(`^[a-z0-9_]+$`)

// RegisterMetric adds a metric to the registry (the owner's administrative row, D7). Registering one that exists
// with the same unit is a no-op; another unit is refused: a unit never changes (metrics_unit_fixed).
func (t *Tx) RegisterMetric(name, unit, note string) (added bool, err error) {
	if !metricName.MatchString(name) {
		return false, invalid("metric %q: lowercase snake_case (a-z 0-9 _)", name)
	}
	res, err := t.tx.Exec(`INSERT INTO metrics(name, unit, note) VALUES (?, ?, ?) ON CONFLICT(name) DO NOTHING`,
		name, unit, nullIfEmpty(note))
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return true, nil
	}
	var have string
	if err := t.tx.QueryRow(`SELECT unit FROM metrics WHERE name = ?`, name).Scan(&have); err != nil {
		return false, err
	}
	if have != unit {
		return false, conflict("metric %s exists with unit %q, not %q: a unit never changes; register a new metric", name, have, unit)
	}
	return false, nil
}

func (t *Tx) metricID(name string) (int64, error) {
	var id int64
	err := t.tx.QueryRow(`SELECT id FROM metrics WHERE name = ?`, name).Scan(&id)
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
	var other bool
	if err := t.tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM measurement_values WHERE metric_id = ? AND value NOT IN (0, 1))`, id).
		Scan(&other); err != nil {
		return err
	}
	if other {
		return invalid("%s has readings other than 0 and 1: a habit is 1 = done, 0 = not done (D24)", metric)
	}
	if _, err := t.tx.Exec(`INSERT INTO habit_periods(metric_id, start_day, end_day, source) VALUES (?, ?, ?, ?)
	                        ON CONFLICT(metric_id, start_day) DO NOTHING`, id, start, nullIfEmpty(end), t.Source); err != nil {
		return err
	}
	_, err = t.tx.Exec(`UPDATE habit_periods SET end_day = ? WHERE metric_id = ? AND start_day = ? AND end_day IS NOT ?`,
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
	res, err := t.tx.Exec(`UPDATE habit_periods SET end_day = ? WHERE metric_id = ? AND end_day IS NULL`, day, id)
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

// HabitState is one habit of a day: done, not done or not recorded.
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
		SELECT m.name,
		       CASE (SELECT max(v.value) FROM measurement_values v WHERE v.metric_id = m.id AND v.day = :day)
		         WHEN 1 THEN 'done' WHEN 0 THEN 'not done' ELSE 'not recorded' END AS state
		  FROM habit_periods h JOIN metrics m ON m.id = h.metric_id
		 WHERE h.start_day <= :day AND coalesce(h.end_day, '9999-12-31') >= :day
		 ORDER BY m.name`, sql.Named("day", day))
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
		SELECT m.name, count(*) AS active_days,
		       sum(s.value IS 1) AS done, sum(s.value IS 0) AS not_done, sum(s.value IS NULL) AS not_recorded
		  FROM active a
		  JOIN metrics m ON m.id = a.metric_id
		  LEFT JOIN (SELECT metric_id, day, max(value) AS value FROM measurement_values GROUP BY metric_id, day) s
		         ON s.metric_id = a.metric_id AND s.day = a.day
		 GROUP BY m.id
		 ORDER BY m.name`, sql.Named("from_day", from), sql.Named("to_day", to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Completion{}
	for rows.Next() {
		var c Completion
		if err := rows.Scan(&c.Metric, &c.ActiveDays, &c.Done, &c.NotDone, &c.NotRecorded); err != nil {
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
	                                         JOIN metrics m ON m.id = h.metric_id WHERE m.name = ? ORDER BY h.start_day`, metric)
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
