package core

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"lifelog/internal/text"
)

// CreateImported creates a page an importer writes: day is its day (nil for none; a day title's day is always the
// title). importKey makes a re-send return the page the key already names, with existing = true.
func (t *Tx) CreateImported(title string, day any, importKey string) (id int64, existing bool, err error) {
	if !text.ValidTitle(title) {
		return 0, false, invalid("title %q is not a valid title (docs/contract/titles-and-wikilinks.md)", title)
	}
	if IsDay(title) {
		day = title
	}
	if !t.keyExists(importKey) {
		if p, err := t.Lookup(title); err != nil || p != nil {
			return 0, false, orExists(err, p, title)
		}
	}
	return t.insertPage("page", title, text.TitleKey(title), day, "", importKey)
}

// ByImportKey is the entity a sender's key names under this transaction's source; 0 when none.
func (t *Tx) ByImportKey(importKey string) (int64, error) {
	var id int64
	err := t.tx.QueryRow(`SELECT id FROM entities WHERE source = ? AND import_key = ?`, t.Source, importKey).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// MetricUnit is a registered metric's unit; found is false when no metric has the name.
func (t *Tx) MetricUnit(name string) (unit string, found bool, err error) {
	err = t.tx.QueryRow(`SELECT unit FROM metrics WHERE name = ?`, name).Scan(&unit)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return unit, err == nil, err
}

// LinkEnds are the entity types a registered kind accepts at each end (link_kinds.from_types / to_types); nil is
// any type. found is false when no kind has the name.
func (t *Tx) LinkEnds(kind string) (from, to []string, found bool, err error) {
	var f, g sql.NullString
	err = t.tx.QueryRow(`SELECT from_types, to_types FROM link_kinds WHERE kind = ?`, kind).Scan(&f, &g)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, false, nil
	}
	split := func(s sql.NullString) []string {
		if !s.Valid {
			return nil
		}
		return strings.Split(s.String, ",")
	}
	return split(f), split(g), err == nil, err
}

// Body is a page's body.
func (t *Tx) Body(id int64) (string, error) {
	var b string
	err := t.tx.QueryRow(`SELECT body FROM pages WHERE id = ?`, id).Scan(&b)
	return b, err
}

// MeasurementByKey is the id of the reading a sender's key names under a source; 0 when none.
func (t *Tx) MeasurementByKey(source, metric, key string) (int64, error) {
	var id int64
	err := t.tx.QueryRow(`SELECT me.id FROM measurements me JOIN metrics m ON m.id = me.metric_id
	                       WHERE me.source = ? AND me.import_key = ? AND m.name = ?`, source, key, metric).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// CurrentOf follows a reading's corrections to the last one: its id and value (ok false = retracted).
func (t *Tx) CurrentOf(id int64) (last int64, value float64, ok bool, err error) {
	var v sql.NullFloat64
	err = t.tx.QueryRow(`WITH RECURSIVE chain(id, value, depth) AS (
	                       SELECT id, value, 0 FROM measurements WHERE id = ?
	                       UNION ALL SELECT x.id, x.value, depth + 1 FROM measurements x JOIN chain c ON x.supersedes_id = c.id)
	                     SELECT id, value FROM chain ORDER BY depth DESC LIMIT 1`, id).Scan(&last, &v)
	return last, v.Float64, v.Valid, err
}

// Counts are a database's rows by kind, for comparing a trial with its replay.
type Counts struct {
	Pages    int            `json:"pages"`
	ByType   map[string]int `json:"entities_by_type"`
	Readings int            `json:"current_readings"`
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
	err := s.DB.R.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM pages), (SELECT count(*) FROM measurement_values),
	                                           (SELECT count(*) FROM metrics), (SELECT count(*) FROM habit_periods)`).
		Scan(&c.Pages, &c.Readings, &c.Metrics, &c.Habits)
	return c, err
}

// Name is a live page's title (and a person's name), for finding look-alikes.
type Name struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Type  string `json:"entity_type"`
	Name  string `json:"name,omitempty"`
}

const namesSQL = `SELECT p.id, p.title, p.entity_type, coalesce(pe.name, '')
                    FROM pages p JOIN entities e ON e.id = p.id LEFT JOIN people pe ON pe.id = p.id
                   WHERE e.deleted_at IS NULL`

func scanNames(rows *sql.Rows, err error) ([]Name, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Name
	for rows.Next() {
		var n Name
		if err := rows.Scan(&n.ID, &n.Title, &n.Type, &n.Name); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// Names lists every live page inside the transaction (rows it wrote included).
func (t *Tx) Names() ([]Name, error) { return scanNames(t.tx.Query(namesSQL)) }

func (s *Store) Names(ctx context.Context) ([]Name, error) {
	return scanNames(s.DB.R.QueryContext(ctx, namesSQL))
}

// KeyedValue is the value the reading a sender's key names was first written with (not its correction).
func (t *Tx) KeyedValue(metric, key string) (value float64, found bool, err error) {
	err = t.tx.QueryRow(`SELECT me.value FROM measurements me JOIN metrics m ON m.id = me.metric_id
	                      WHERE me.source = ? AND me.import_key = ? AND m.name = ?`, t.Source, key, metric).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return value, err == nil, err
}
