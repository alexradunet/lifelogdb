package core

import (
	"context"
	"database/sql"
	"encoding/json"
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

// ImportedPageIdentity reads the stored handle and nullable day for this source's key.
// found is false when the key has not been applied.
func (t *Tx) ImportedPageIdentity(importKey string) (title string, day *string, found bool, err error) {
	var d sql.NullString
	err = t.tx.QueryRow(`SELECT p_name.title, p.day FROM entities p JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key  JOIN entities e ON e.id = p.id WHERE e.source = ? AND e.import_key = ?`, t.Source, importKey).Scan(&title, &d)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, false, nil
	}
	if d.Valid {
		day = &d.String
	}
	return title, day, err == nil, err
}

// MetricUnit is a registered metric's unit; found is false when no metric has the name.
func (t *Tx) MetricUnit(name string) (unit string, found bool, err error) {
	err = t.tx.QueryRow(`SELECT m.unit FROM metrics m JOIN entities p ON p.id = m.id JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key  WHERE p.id = (SELECT entity_id FROM entity_names WHERE name_key = ?)`, text.TitleKey(name)).Scan(&unit)
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
	err := t.tx.QueryRow(`SELECT body FROM entities WHERE id = ?`, id).Scan(&b)
	return b, err
}

// MeasurementByKey is the id of the reading a sender's key names under a source; 0 when none.
func (t *Tx) MeasurementByKey(source, metric, key string) (int64, error) {
	var id int64
	err := t.tx.QueryRow(`SELECT me.id FROM measurements me JOIN entities m ON m.id = me.metric_id JOIN entity_names m_name ON m_name.entity_id = m.id AND m_name.name_key = m.preferred_name_key
	                       WHERE me.source = ? AND me.import_key = ? AND m.id = (SELECT entity_id FROM entity_names WHERE name_key = ?)`, source, key, text.TitleKey(metric)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// ImportedMeasurementRoot is the portable identity of an imported root reading, for import-key compatibility checks.
type ImportedMeasurementRoot struct {
	ID               int64
	ImportKey        string
	Metric           string
	MetricKey        string
	Day              string
	TakenAt          string
	TZ               string
	CapturedWithKey  string
	MetricKeys       []string
	CapturedWithKeys []string
	Value            *float64
}

// ImportedMeasurementRootsByFile lists root readings for one imported source, source file and canonical metric key.
func (t *Tx) ImportedMeasurementRootsByFile(source, file, metricKey string) ([]ImportedMeasurementRoot, error) {
	rows, err := t.tx.Query(`SELECT me.id, me.import_key, m_name.title, m.preferred_name_key, me.day,
	                              coalesce(me.taken_at, ''), coalesce(me.tz, ''),
	                              coalesce(captured.preferred_name_key, ''), me.value,
                                  (SELECT json_group_array(name_key ORDER BY name_key) FROM entity_names WHERE entity_id = m.id),
                                  (SELECT json_group_array(name_key ORDER BY name_key) FROM entity_names WHERE entity_id = me.captured_with_id)
	                         FROM measurements me
	                         JOIN entities m ON m.id = me.metric_id JOIN entity_names m_name ON m_name.entity_id = m.id AND m_name.name_key = m.preferred_name_key
	                         LEFT JOIN entities captured ON captured.id = me.captured_with_id LEFT JOIN entity_names captured_name ON captured_name.entity_id = captured.id AND captured_name.name_key = captured.preferred_name_key
	                        WHERE me.source = ? AND me.import_key IS NOT NULL
	                          AND m.id = (SELECT entity_id FROM entity_names WHERE name_key = ?) AND me.supersedes_id IS NULL`, source, metricKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	prefix := file + "|reading|"
	var out []ImportedMeasurementRoot
	for rows.Next() {
		var r ImportedMeasurementRoot
		var v sql.NullFloat64
		var metricKeys, withKeys string
		if err := rows.Scan(&r.ID, &r.ImportKey, &r.Metric, &r.MetricKey, &r.Day, &r.TakenAt, &r.TZ, &r.CapturedWithKey, &v, &metricKeys, &withKeys); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(metricKeys), &r.MetricKeys); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(withKeys), &r.CapturedWithKeys); err != nil {
			return nil, err
		}
		if !strings.HasPrefix(r.ImportKey, prefix) {
			continue
		}
		if v.Valid {
			r.Value = &v.Float64
		}
		out = append(out, r)
	}
	return out, rows.Err()
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

// Name is a live page's title (and a person's name), for finding look-alikes.
type Name struct {
	ID      int64    `json:"id"`
	Title   string   `json:"title"`
	Type    string   `json:"entity_type"`
	Name    string   `json:"name,omitempty"`
	Aliases []string `json:"aliases,omitempty"`
}

const namesSQL = `SELECT e.id, n.title, e.entity_type, coalesce(pe.name, ''),
                        (SELECT json_group_array(title ORDER BY name_key) FROM entity_names WHERE entity_id=e.id AND name_key<>e.preferred_name_key)
                    FROM entities e JOIN entity_names n ON n.entity_id=e.id AND n.name_key=e.preferred_name_key LEFT JOIN people pe ON pe.id=e.id
                   WHERE e.deleted_at IS NULL ORDER BY e.id`

func scanNames(rows *sql.Rows, err error) ([]Name, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Name
	for rows.Next() {
		var n Name
		var aliases string
		if err := rows.Scan(&n.ID, &n.Title, &n.Type, &n.Name, &aliases); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(aliases), &n.Aliases); err != nil {
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
	err = t.tx.QueryRow(`SELECT me.value FROM measurements me JOIN entities m ON m.id = me.metric_id JOIN entity_names m_name ON m_name.entity_id = m.id AND m_name.name_key = m.preferred_name_key
	                      WHERE me.source = ? AND me.import_key = ? AND m.id = (SELECT entity_id FROM entity_names WHERE name_key = ?)`, t.Source, key, text.TitleKey(metric)).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return value, err == nil, err
}

// DirectAgentRows counts outside writes, excluding only verified durable measurement events.
func (s *Store) DirectAgentRows(ctx context.Context, verified map[int64]bool) (int, error) {
	var n int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM entities WHERE source LIKE 'agent:%') + (SELECT count(*) FROM links WHERE source LIKE 'agent:%')+(SELECT count(*) FROM sessions WHERE source LIKE 'agent:%')`).Scan(&n); err != nil {
		return 0, err
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id FROM measurements WHERE source LIKE 'agent:%'`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		if !verified[id] {
			n++
		}
	}
	return n, rows.Err()
}
