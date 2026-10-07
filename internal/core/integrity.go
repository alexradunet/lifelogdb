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
	FullTextIndexOK          bool     `json:"full_text_index_ok"`
	FullTextError            string   `json:"full_text_error,omitempty"`
}

func (s *Store) Integrity(ctx context.Context) (*IntegrityResult, error) {
	r := &IntegrityResult{IntegrityCheck: []string{}, OrphanEntities: []int64{}, InvalidTypedLinks: []int64{}, InvalidSessionKinds: []int64{}, InvalidMeasurementScopes: []int64{}}
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

	if err := s.integrityRead(ctx, "session kind endpoints", `SELECT s.id FROM sessions s LEFT JOIN entities e ON e.id=s.kind_id WHERE e.id IS NULL OR e.entity_type<>'page' OR (length(e.preferred_name_key)=10 AND date(e.preferred_name_key) IS e.preferred_name_key) ORDER BY s.id`, func(rows *sql.Rows) error {
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
	// the FTS check is an INSERT that writes nothing, so it runs on the writing connection
	if _, err := s.DB.W.ExecContext(ctx, `INSERT INTO entities_fts(entities_fts, rank) VALUES ('integrity-check', 1)`); err != nil {
		r.FullTextError = fmt.Sprint(err)
	} else {
		r.FullTextIndexOK = true
	}
	r.OK = len(r.IntegrityCheck) == 1 && r.IntegrityCheck[0] == "ok" && r.ForeignKeys == 0 &&
		len(r.OrphanEntities) == 0 && len(r.InvalidTypedLinks) == 0 && len(r.InvalidSessionKinds) == 0 && len(r.InvalidMeasurementScopes) == 0 && r.FullTextIndexOK
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
