package core

import (
	"context"
	"database/sql"
	"errors"

	"lifelog/internal/text"
)

// ByImportKey is the entity a sender's key names under this transaction's source; 0 when none.
func (t *Tx) ByImportKey(importKey string) (int64, error) {
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

// MetricUnit is a registered metric's unit; found is false when no metric has the name.
func (t *Tx) MetricUnit(name string) (unit string, found bool, err error) {
	err = t.tx.QueryRowContext(t.ctx, `SELECT unit FROM metrics WHERE id = (SELECT entity_id FROM entity_names WHERE name_key = ?)`, text.TitleKey(name)).Scan(&unit)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return unit, err == nil, err
}

// Counts are a database's rows by kind, for comparing a trial with its replay.
type Counts struct {
	Pages    int            `json:"pages"`
	ByType   map[string]int `json:"entities_by_type"`
	Readings int            `json:"current_readings"`
	Sessions int            `json:"sessions"`
	Links    map[string]int `json:"links_by_kind"`
	Metrics  int            `json:"metrics"`
	Habits   int            `json:"habit_periods"`
	BySource map[string]int `json:"entities_by_source"`
}

func (s *Store) Counts(ctx context.Context) (*Counts, error) {
	c := &Counts{ByType: map[string]int{}, Links: map[string]int{}, BySource: map[string]int{}}
	q := func(sql string, dest map[string]int) error {
		rows, err := s.DB.R.QueryContext(ctx, sql)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var k string
			var n int
			if err := rows.Scan(&k, &n); err != nil {
				return err
			}
			dest[k] = n
		}
		return rows.Err()
	}
	if err := q(`SELECT entity_type, count(*) FROM entities WHERE deleted_at IS NULL GROUP BY 1`, c.ByType); err != nil {
		return nil, err
	}
	if err := q(`SELECT kind, count(*) FROM links GROUP BY 1`, c.Links); err != nil {
		return nil, err
	}
	if err := q(`SELECT source, count(*) FROM entities GROUP BY 1`, c.BySource); err != nil {
		return nil, err
	}
	err := s.DB.R.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM entities WHERE entity_type <> 'metric'), (SELECT count(*) FROM measurement_values),
	                                           (SELECT count(*) FROM metrics), (SELECT count(*) FROM habit_periods),(SELECT count(*) FROM sessions)`).
		Scan(&c.Pages, &c.Readings, &c.Metrics, &c.Habits, &c.Sessions)
	return c, err
}
