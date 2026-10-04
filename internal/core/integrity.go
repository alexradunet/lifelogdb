package core

import (
	"context"
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
	rows, err := s.DB.R.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var line string
		rows.Scan(&line)
		r.IntegrityCheck = append(r.IntegrityCheck, line)
	}
	rows.Close()
	rows, err = s.DB.R.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		r.ForeignKeys++
	}
	rows.Close()
	rows, err = s.DB.R.QueryContext(ctx, `SELECT id FROM entities WHERE id NOT IN
	        (SELECT id FROM pages WHERE entity_type IN ('page','place') UNION SELECT id FROM people UNION SELECT id FROM metrics UNION SELECT id FROM files)`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		r.OrphanEntities = append(r.OrphanEntities, id)
	}
	rows.Close()
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
