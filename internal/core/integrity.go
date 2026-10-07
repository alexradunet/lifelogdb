package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// IntegrityResult is the four check groups of docs/contract/integrity-checks.md; OK when all pass.
type IntegrityResult struct {
	OK                       bool     `json:"ok"`
	IntegrityCheck           []string `json:"integrity_check"`
	ForeignKeys              int      `json:"foreign_key_violations"`
	OrphanEntities           []int64  `json:"entities_without_domain_row"`
	InvalidTypedLinks        []int64  `json:"invalid_typed_links"`
	InvalidSessionKinds      []int64  `json:"invalid_session_kinds"`
	InvalidMeasurementScopes []int64  `json:"invalid_measurement_scopes"`
	InvalidTaskProjects      []int64  `json:"invalid_task_projects"`
	InvalidOneOffTasks       []int64  `json:"invalid_one_off_tasks"`
	InvalidTaskOccurrences   []int64  `json:"invalid_task_occurrences"`
	InvalidSymmetricLinks    []int64  `json:"invalid_symmetric_links"`
	InvalidMeasurementChains []int64  `json:"invalid_measurement_chains"`
	InvalidHabitPeriods      []int64  `json:"invalid_habit_periods"`
	InvalidHabitReadings     []int64  `json:"invalid_habit_readings"`
	InvalidJournalNames      []int64  `json:"invalid_journal_names"`
	FullTextIndexOK          bool     `json:"full_text_index_ok"`
	FullTextError            string   `json:"full_text_error,omitempty"`
}

func (s *Store) Integrity(ctx context.Context) (*IntegrityResult, error) {
	r := &IntegrityResult{
		IntegrityCheck: []string{}, OrphanEntities: []int64{}, InvalidTypedLinks: []int64{},
		InvalidSessionKinds: []int64{}, InvalidMeasurementScopes: []int64{},
		InvalidTaskProjects: []int64{}, InvalidOneOffTasks: []int64{}, InvalidTaskOccurrences: []int64{},
		InvalidSymmetricLinks: []int64{}, InvalidMeasurementChains: []int64{},
		InvalidHabitPeriods: []int64{}, InvalidHabitReadings: []int64{}, InvalidJournalNames: []int64{},
	}
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
	if err := s.integrityRead(ctx, "orphan entities", `SELECT e.id FROM entities e WHERE
             NOT EXISTS (SELECT 1 FROM entity_names n WHERE n.entity_id = e.id AND n.name_key = e.preferred_name_key)
             OR (e.entity_type = 'person' AND NOT EXISTS (SELECT 1 FROM people p WHERE p.id = e.id))
             OR (e.entity_type = 'metric' AND NOT EXISTS (SELECT 1 FROM metrics m WHERE m.id = e.id))
             OR (e.entity_type = 'period' AND NOT EXISTS (SELECT 1 FROM periods p WHERE p.id=e.id))
             OR (e.entity_type = 'file' AND NOT EXISTS (SELECT 1 FROM files f WHERE f.id = e.id))`, func(rows *sql.Rows) error {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		r.OrphanEntities = append(r.OrphanEntities, id)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := s.integrityRead(ctx, "typed link endpoints", `SELECT l.id FROM links l
 LEFT JOIN link_kinds k ON k.kind=l.kind
 LEFT JOIN entities f ON f.id=l.from_id LEFT JOIN entities t ON t.id=l.to_id
 WHERE k.kind IS NULL OR f.id IS NULL OR t.id IS NULL
 OR (k.from_types IS NOT NULL AND instr(',' || k.from_types || ',', ',' || f.entity_type || ',')=0)
 OR (k.to_types IS NOT NULL AND instr(',' || k.to_types || ',', ',' || t.entity_type || ',')=0)
 ORDER BY l.id`, func(rows *sql.Rows) error {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		r.InvalidTypedLinks = append(r.InvalidTypedLinks, id)
		return nil
	}); err != nil {
		return nil, err
	}

	if err := s.integrityRead(ctx, "session kind endpoints", `SELECT s.id FROM sessions s LEFT JOIN entities e ON e.id=s.kind_id WHERE e.id IS NULL OR e.entity_type<>'page' OR e.is_journal ORDER BY s.id`, func(rows *sql.Rows) error {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		r.InvalidSessionKinds = append(r.InvalidSessionKinds, id)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := s.integrityRead(ctx, "measurement scopes", `SELECT m.id FROM measurements m LEFT JOIN sessions s ON s.id=m.session_id LEFT JOIN measurements p ON p.id=m.supersedes_id WHERE (m.session_id IS NOT NULL AND s.id IS NULL) OR (m.supersedes_id IS NOT NULL AND (p.id IS NULL OR p.metric_id IS NOT m.metric_id OR p.session_id IS NOT m.session_id)) ORDER BY m.id`, func(rows *sql.Rows) error {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		r.InvalidMeasurementScopes = append(r.InvalidMeasurementScopes, id)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := s.integrityRead(ctx, "task project endpoints", `SELECT t.id FROM tasks t LEFT JOIN entities e ON e.id=t.project_page_id WHERE t.project_page_id IS NOT NULL AND (e.id IS NULL OR e.entity_type<>'page' OR e.is_journal) ORDER BY t.id`, func(rows *sql.Rows) error {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		r.InvalidTaskProjects = append(r.InvalidTaskProjects, id)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := s.integrityRead(ctx, "one-off task ownership", `SELECT t.id FROM tasks t WHERE t.repeat_unit IS NULL AND (SELECT count(*) FROM task_occurrences o WHERE o.task_id=t.id AND o.occurrence_key='once')<>1 ORDER BY t.id`, func(rows *sql.Rows) error {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		r.InvalidOneOffTasks = append(r.InvalidOneOffTasks, id)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := s.integrityRead(ctx, "task occurrence membership", `SELECT o.id FROM task_occurrences o WHERE NOT EXISTS
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
       WHEN substr(o.occurrence_key,6,2) IN ('04','06','09','11') THEN 30 ELSE 31 END))))) OR (o.state='open' AND EXISTS (SELECT 1 FROM tasks t WHERE t.id=o.task_id AND t.repeat_until_day IS NOT NULL AND o.occurrence_key>t.repeat_until_day)) ORDER BY o.id`, func(rows *sql.Rows) error {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		r.InvalidTaskOccurrences = append(r.InvalidTaskOccurrences, id)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := s.integrityRead(ctx, "symmetric link pairs", `SELECT l.id FROM links l JOIN link_kinds k ON k.kind=l.kind
 LEFT JOIN links r ON r.from_id=l.to_id AND r.to_id=l.from_id AND r.kind=l.kind
 WHERE k.symmetric=1 AND (r.id IS NULL OR r.note IS NOT l.note) ORDER BY l.id`, func(rows *sql.Rows) error {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		r.InvalidSymmetricLinks = append(r.InvalidSymmetricLinks, id)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := s.integrityRead(ctx, "measurement correction chains", `WITH RECURSIVE rooted(id) AS (
 SELECT id FROM measurements WHERE supersedes_id IS NULL
 UNION
 SELECT m.id FROM measurements m JOIN rooted r ON m.supersedes_id=r.id
)
SELECT id FROM measurements EXCEPT SELECT id FROM rooted ORDER BY id`, func(rows *sql.Rows) error {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		r.InvalidMeasurementChains = append(r.InvalidMeasurementChains, id)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := s.integrityRead(ctx, "habit period semantics", `SELECT h.id FROM habit_periods h LEFT JOIN metrics m ON m.id=h.metric_id
 WHERE m.id IS NULL OR m.unit<>'' OR EXISTS
 (SELECT 1 FROM habit_periods p WHERE p.metric_id=h.metric_id AND p.id<>h.id
  AND p.start_day<=coalesce(h.end_day,'9999-12-31') AND coalesce(p.end_day,'9999-12-31')>=h.start_day)
 ORDER BY h.id`, func(rows *sql.Rows) error {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		r.InvalidHabitPeriods = append(r.InvalidHabitPeriods, id)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := s.integrityRead(ctx, "habit reading values", `SELECT v.id FROM measurement_values v WHERE v.value NOT IN (0,1)
 AND EXISTS (SELECT 1 FROM habit_periods h WHERE h.metric_id=v.metric_id) ORDER BY v.id`, func(rows *sql.Rows) error {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		r.InvalidHabitReadings = append(r.InvalidHabitReadings, id)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := s.integrityRead(ctx, "journal name ownership", `SELECT n.id FROM entity_names n JOIN entities e ON e.id=n.entity_id
 WHERE (length(n.name_key)=10 AND date(n.name_key) IS n.name_key
   AND (e.entity_type<>'page' OR e.preferred_name_key IS NOT n.name_key OR e.day IS NOT n.name_key OR n.title IS NOT n.name_key))
 OR (e.is_journal
   AND (n.name_key IS NOT e.preferred_name_key OR n.title IS NOT e.preferred_name_key)) ORDER BY n.id`, func(rows *sql.Rows) error {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		r.InvalidJournalNames = append(r.InvalidJournalNames, id)
		return nil
	}); err != nil {
		return nil, err
	}
	// the FTS check is an INSERT that writes nothing, so it runs on the writing connection
	if _, err := s.DB.W.ExecContext(ctx, `INSERT INTO entities_fts(entities_fts, rank) VALUES ('integrity-check', 1)`); err != nil {
		r.FullTextError = fmt.Sprint(err)
	} else {
		r.FullTextIndexOK = true
	}
	r.OK = len(r.IntegrityCheck) == 1 && r.IntegrityCheck[0] == "ok" && r.ForeignKeys == 0 &&
		len(r.OrphanEntities) == 0 && len(r.InvalidTypedLinks) == 0 && len(r.InvalidSessionKinds) == 0 && len(r.InvalidMeasurementScopes) == 0 &&
		len(r.InvalidTaskProjects) == 0 && len(r.InvalidOneOffTasks) == 0 && len(r.InvalidTaskOccurrences) == 0 &&
		len(r.InvalidSymmetricLinks) == 0 && len(r.InvalidMeasurementChains) == 0 && len(r.InvalidHabitPeriods) == 0 && len(r.InvalidHabitReadings) == 0 && len(r.InvalidJournalNames) == 0 && r.FullTextIndexOK
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
