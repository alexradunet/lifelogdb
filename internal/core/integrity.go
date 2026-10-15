package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// IntegrityResult is the four check groups of docs/contract/integrity-checks.md; OK when all pass.
type IntegrityResult struct {
	OK                            bool     `json:"ok"`
	IntegrityCheck                []string `json:"integrity_check"`
	ForeignKeys                   int      `json:"foreign_key_violations"`
	OrphanEntities                []int64  `json:"entities_without_domain_row"`
	InvalidTypedLinks             []int64  `json:"invalid_typed_links"`
	InvalidMeasurementCorrections []int64  `json:"invalid_measurement_corrections"`
	InvalidTaskProjects           []int64  `json:"invalid_task_projects"`
	InvalidOneOffTasks            []int64  `json:"invalid_one_off_tasks"`
	InvalidTaskOccurrences        []int64  `json:"invalid_task_occurrences"`
	InvalidMeasurementChains      []int64  `json:"invalid_measurement_chains"`
	InvalidJournalNames           []int64  `json:"invalid_journal_names"`
	FullTextIndexOK               bool     `json:"full_text_index_ok"`
	FullTextError                 string   `json:"full_text_error,omitempty"`
}

// idCheck is one semantic check: a query that selects the id of every row that breaks a rule, and the list of the
// result it fills. An empty list is a passed check.
type idCheck struct {
	name  string
	query string
	dst   *[]int64
}

// idChecks are the semantic checks, in the order the result lists them.
func (r *IntegrityResult) idChecks() []idCheck {
	return []idCheck{
		{"orphan entities", `SELECT e.id FROM entities e WHERE
             NOT EXISTS (SELECT 1 FROM entity_names n WHERE n.entity_id = e.id AND n.name_key = e.preferred_name_key)
             OR (e.entity_type = 'person' AND NOT EXISTS (SELECT 1 FROM people p WHERE p.id = e.id))
             OR (e.entity_type = 'metric' AND NOT EXISTS (SELECT 1 FROM metrics m WHERE m.id = e.id))
             OR (e.entity_type = 'file' AND NOT EXISTS (SELECT 1 FROM files f WHERE f.id = e.id))`, &r.OrphanEntities},
		{"typed link endpoints", `SELECT l.id FROM links l
 LEFT JOIN link_kinds k ON k.kind=l.kind
 LEFT JOIN entities f ON f.id=l.from_id LEFT JOIN entities t ON t.id=l.to_id
 WHERE k.kind IS NULL OR f.id IS NULL OR t.id IS NULL
 OR (k.from_types IS NOT NULL AND instr(',' || k.from_types || ',', ',' || f.entity_type || ',')=0)
 OR (k.to_types IS NOT NULL AND instr(',' || k.to_types || ',', ',' || t.entity_type || ',')=0)
 ORDER BY l.id`, &r.InvalidTypedLinks},
		{"measurement corrections", `SELECT m.id FROM measurements m LEFT JOIN measurements p ON p.id=m.supersedes_id WHERE m.supersedes_id IS NOT NULL AND (p.id IS NULL OR p.metric_id IS NOT m.metric_id) ORDER BY m.id`, &r.InvalidMeasurementCorrections},
		{"task project endpoints", `SELECT t.id FROM tasks t LEFT JOIN entities e ON e.id=t.project_page_id WHERE t.project_page_id IS NOT NULL AND (e.id IS NULL OR e.entity_type<>'page' OR e.is_journal) ORDER BY t.id`, &r.InvalidTaskProjects},
		{"one-off task ownership", `SELECT t.id FROM tasks t WHERE t.repeat_unit IS NULL AND (SELECT count(*) FROM task_occurrences o WHERE o.task_id=t.id AND o.occurrence_key='once')<>1 ORDER BY t.id`, &r.InvalidOneOffTasks},
		// The recurrence rule in SQL, independent of the Go calendar: an occurrence is on a slot of its series.
		{"task occurrence membership", `SELECT o.id FROM task_occurrences o WHERE NOT EXISTS
(SELECT 1 FROM tasks t WHERE t.id=o.task_id AND
   ((t.repeat_unit IS NULL AND o.occurrence_key='once') OR
    (t.repeat_unit IS NOT NULL AND o.occurrence_key>=t.anchor_day
     AND CASE t.repeat_unit
       WHEN 'day' THEN CAST(julianday(o.occurrence_key)-julianday(t.anchor_day) AS INTEGER)%t.repeat_every=0
       WHEN 'week' THEN CAST(julianday(o.occurrence_key)-julianday(t.anchor_day) AS INTEGER)%7=0
         AND (CAST(julianday(o.occurrence_key)-julianday(t.anchor_day) AS INTEGER)/7)%t.repeat_every=0
       WHEN 'month' THEN ((CAST(substr(o.occurrence_key,1,4) AS INTEGER)-CAST(substr(t.anchor_day,1,4) AS INTEGER))*12
         + CAST(substr(o.occurrence_key,6,2) AS INTEGER)-CAST(substr(t.anchor_day,6,2) AS INTEGER))%t.repeat_every=0
       WHEN 'year' THEN (CAST(substr(o.occurrence_key,1,4) AS INTEGER)-CAST(substr(t.anchor_day,1,4) AS INTEGER))%t.repeat_every=0
         AND substr(o.occurrence_key,6,2)=substr(t.anchor_day,6,2)
     END
     AND (t.repeat_unit IN ('day','week') OR CAST(substr(o.occurrence_key,9,2) AS INTEGER)=min(CAST(substr(t.anchor_day,9,2) AS INTEGER),
       CASE WHEN substr(o.occurrence_key,6,2)='02' THEN 28+
         (CAST(substr(o.occurrence_key,1,4) AS INTEGER)%4=0 AND
          (CAST(substr(o.occurrence_key,1,4) AS INTEGER)%100<>0 OR CAST(substr(o.occurrence_key,1,4) AS INTEGER)%400=0))
       WHEN substr(o.occurrence_key,6,2) IN ('04','06','09','11') THEN 30 ELSE 31 END))))) OR (o.state='open' AND EXISTS (SELECT 1 FROM tasks t WHERE t.id=o.task_id AND t.repeat_until_day IS NOT NULL AND o.occurrence_key>t.repeat_until_day)) ORDER BY o.id`, &r.InvalidTaskOccurrences},
		{"measurement correction chains", `WITH RECURSIVE rooted(id) AS (
 SELECT id FROM measurements WHERE supersedes_id IS NULL
 UNION
 SELECT m.id FROM measurements m JOIN rooted r ON m.supersedes_id=r.id
)
SELECT id FROM measurements EXCEPT SELECT id FROM rooted ORDER BY id`, &r.InvalidMeasurementChains},
		{"journal name ownership", `SELECT n.id FROM entity_names n JOIN entities e ON e.id=n.entity_id
 WHERE (length(n.name_key)=10 AND date(n.name_key) IS n.name_key
   AND (e.entity_type<>'page' OR e.preferred_name_key IS NOT n.name_key OR e.day IS NOT n.name_key OR n.title IS NOT n.name_key))
 OR (e.is_journal
   AND (n.name_key IS NOT e.preferred_name_key OR n.title IS NOT e.preferred_name_key)) ORDER BY n.id`, &r.InvalidJournalNames},
	}
}

// Integrity runs the checks: SQLite's own two, the semantic checks of idChecks, and the full-text index's.
func (s *Store) Integrity(ctx context.Context) (*IntegrityResult, error) {
	r := &IntegrityResult{IntegrityCheck: []string{}}
	if err := s.integrityRead(ctx, "integrity_check", `PRAGMA integrity_check`, func(rows *sql.Rows) error {
		var line string
		if err := rows.Scan(&line); err != nil {
			return err
		}
		r.IntegrityCheck = append(r.IntegrityCheck, line)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := s.integrityRead(ctx, "foreign_key_check", `PRAGMA foreign_key_check`, func(rows *sql.Rows) error {
		var table, parent string
		var rowID sql.NullInt64 // WITHOUT ROWID tables report NULL.
		var foreignKey int64
		if err := rows.Scan(&table, &rowID, &parent, &foreignKey); err != nil {
			return err
		}
		r.ForeignKeys++
		return nil
	}); err != nil {
		return nil, err
	}
	semanticOK := true
	for _, c := range r.idChecks() {
		*c.dst = []int64{}
		if err := s.integrityRead(ctx, c.name, c.query, func(rows *sql.Rows) error {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			*c.dst = append(*c.dst, id)
			return nil
		}); err != nil {
			return nil, err
		}
		semanticOK = semanticOK && len(*c.dst) == 0
	}
	// the FTS check is an INSERT that writes nothing, so it runs on the writing connection
	if _, err := s.DB.W.ExecContext(ctx, `INSERT INTO entities_fts(entities_fts, rank) VALUES ('integrity-check', 1)`); err != nil {
		r.FullTextError = fmt.Sprint(err)
	} else {
		r.FullTextIndexOK = true
	}
	r.OK = len(r.IntegrityCheck) == 1 && r.IntegrityCheck[0] == "ok" && r.ForeignKeys == 0 && semanticOK && r.FullTextIndexOK
	return r, nil
}

// integrityRead distinguishes an incomplete check from a completed diagnostic result.
func (s *Store) integrityRead(ctx context.Context, check, query string, scan func(*sql.Rows) error) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("%s: %w", check, err)
		}
	}()
	rows, err := s.DB.R.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
