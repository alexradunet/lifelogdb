package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// IntegrityResult is the four checks of docs/contract/integrity-checks.md; OK when all four pass.
type IntegrityResult struct {
	OK              bool     `json:"ok"`
	IntegrityCheck  []string `json:"integrity_check"`
	ForeignKeys     int      `json:"foreign_key_violations"`
	OrphanEntities  []int64  `json:"entities_without_domain_row"`
	FullTextIndexOK bool     `json:"full_text_index_ok"`
	FullTextError   string   `json:"full_text_error,omitempty"`
}

func (s *Store) Integrity(ctx context.Context) (*IntegrityResult, error) {
	r := &IntegrityResult{IntegrityCheck: []string{}, OrphanEntities: []int64{}}
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
	if err := s.integrityRead(ctx, "orphan entities", `SELECT id FROM entities WHERE id NOT IN
	        (SELECT id FROM pages WHERE entity_type IN ('page','place') UNION SELECT id FROM people UNION SELECT id FROM metrics UNION SELECT id FROM files)`, func(rows *sql.Rows) error {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		r.OrphanEntities = append(r.OrphanEntities, id)
		return nil
	}); err != nil {
		return nil, err
	}
	// the FTS check is an INSERT that writes nothing, so it runs on the writing connection
	if _, err := s.DB.W.ExecContext(ctx, `INSERT INTO pages_fts(pages_fts, rank) VALUES ('integrity-check', 1)`); err != nil {
		r.FullTextError = fmt.Sprint(err)
	} else {
		r.FullTextIndexOK = true
	}
	r.OK = len(r.IntegrityCheck) == 1 && r.IntegrityCheck[0] == "ok" && r.ForeignKeys == 0 &&
		len(r.OrphanEntities) == 0 && r.FullTextIndexOK
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
