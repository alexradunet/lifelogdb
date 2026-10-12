package core

import "context"

// counts are a database's rows by kind: a test compares them before and after a write that must change nothing.
type counts struct {
	Pages    int
	ByType   map[string]int
	Readings int
	Sessions int
	Links    map[string]int
	Metrics  int
	Habits   int
	BySource map[string]int
}

func countRows(ctx context.Context, s *Store) (*counts, error) {
	c := &counts{ByType: map[string]int{}, Links: map[string]int{}, BySource: map[string]int{}}
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
