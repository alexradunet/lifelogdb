package core

import (
	"context"
	"database/sql"
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
	var line string
	if err := s.integrityRead(ctx, "integrity_check", `PRAGMA integrity_check`, []any{&line}, func() {
		r.IntegrityCheck = append(r.IntegrityCheck, line)
	}); err != nil {
		return nil, err
	}
	var table, parent string
	var rowID sql.NullInt64 // WITHOUT ROWID tables report NULL.
	var foreignKey int64
	if err := s.integrityRead(ctx, "foreign_key_check", `PRAGMA foreign_key_check`, []any{&table, &rowID, &parent, &foreignKey}, func() {
		r.ForeignKeys++
	}); err != nil {
		return nil, err
	}
	semanticOK := true
	for _, c := range r.idChecks() {
		*c.dst = []int64{}
		var id int64
		if err := s.integrityRead(ctx, c.name, c.query, []any{&id}, func() { *c.dst = append(*c.dst, id) }); err != nil {
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

// integrityRead reads every row of one check into dst and runs row, as each does. Its error names the check, so an
// incomplete check is not taken for a completed diagnostic result.
func (s *Store) integrityRead(ctx context.Context, check, query string, dst []any, row func()) error {
	rows, err := s.DB.R.QueryContext(ctx, query)
	if err := each(rows, err, dst, row); err != nil {
		return fmt.Errorf("%s: %w", check, err)
	}
	return nil
}
