package tests

import (
	"path/filepath"
	"strings"
)

// schemaAdversarial exercises conflict resolution and statement/transaction
// boundaries directly against the hardened canonical schema.
func schemaAdversarial(s *S) {
	// A uniqueness no-op is not validation of every discarded input column.
	c := s.freshWith(F{Path: filepath.Join(s.dir, "adversarial-duplicates.db"), Hardened: true})
	c.must("PRAGMA synchronous=FULL")
	duplicate := "INSERT INTO measurements(metric_id,day,value,captured_with_id,supersedes_id,source,import_key,created_at) VALUES(1,?,3,?,?,'import:retry','existing'," + NOW + ") ON CONFLICT(source,import_key,metric_id) WHERE import_key IS NOT NULL DO NOTHING"
	c.must(duplicate, "2020-01-02", nil, nil)
	before := c.tab("SELECT * FROM measurements")
	r := c.tryx(duplicate, "2020-01-02", int64(999999), int64(999999))
	s.K("duplicate DO NOTHING skips foreign keys and AFTER INSERT guards", r == "OK" && c.n("SELECT changes()") == 0 && c.tab("SELECT * FROM measurements") == before, r)
	r = c.tryx(strings.Replace(duplicate, "'existing'", "'new'", 1), "2020-01-02", int64(999999), int64(999999))
	s.K("new imported row with the same dangling references is refused", strings.HasPrefix(r, "ERR") && c.tab("SELECT * FROM measurements") == before, r)
	r = c.tryx(duplicate, "2020-1-2", nil, nil)
	s.K("duplicate DO NOTHING still checks malformed days before uniqueness", strings.HasPrefix(r, "ERR") && c.tab("SELECT * FROM measurements") == before, r)
	r = c.tryx(strings.Replace(duplicate, "VALUES(1,?,3,", "VALUES(1,?,1e999,", 1), "2020-01-02", nil, nil)
	s.K("duplicate DO NOTHING still checks nonfinite values before uniqueness", strings.HasPrefix(r, "ERR") && c.tab("SELECT * FROM measurements") == before, r)
	s.K("duplicate validation probes retain file integrity", c.integrityOK())

	// RAISE/constraint ABORT reverses its statement, not earlier statements in
	// the transaction. The caller must roll back the complete failed batch.
	c = s.freshWith(F{Path: filepath.Join(s.dir, "adversarial-transaction.db"), Hardened: true})
	c.must("PRAGMA synchronous=FULL")
	c.must("BEGIN IMMEDIATE")
	c.must("INSERT INTO measurements(metric_id,day,value,source,created_at) VALUES(1,'2020-01-02',3,'ui'," + NOW + ")")
	r = c.tryx("INSERT INTO measurements(metric_id,day,value,source,created_at) VALUES(1,'2020-01-03',4,'ui'," + NOW + "),(1,'2020-1-4',5,'ui'," + NOW + ")")
	s.K("statement ABORT leaves earlier transaction writes pending", strings.HasPrefix(r, "ERR") && c.tab("SELECT day,value FROM measurements") == "2020-01-02|3", r)
	c.must("ROLLBACK")
	s.K("explicit rollback discards earlier import transaction writes", c.n("SELECT count(*) FROM measurements") == 0)
	s.K("explicit import rollback retains file integrity", c.integrityOK())
}
