package tests

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Isolated SQL exercises the canonical trigger graph, including failures and a
// different trigger creation order. It is not a workload-throughput fixture.
func storageResilience(s *S) {
	for _, reverse := range []bool{false, true} {
		storageFTS(s, reverse)
	}
	storageReaderCheckpoint(s)
}

func storageConnection(s *S, path string) *C {
	return s.connect(path, "_defensive=1", "_pragma=foreign_keys(1)", "_pragma=recursive_triggers(1)",
		"_pragma=synchronous(2)", "_pragma=trusted_schema(0)")
}

func storageFTS(s *S, reverse bool) {
	label := fmt.Sprintf("FTS reverse=%v", reverse)
	path := filepath.Join(s.dir, fmt.Sprintf("search-%v.db", reverse))
	c := storageConnection(s, path)
	c.must(s.ddl)
	if reverse {
		// Only reorder the triggers that share the two searchable source tables.
		// Their SQL and guards remain unchanged; no private SQLite ordering is assumed.
		triggers := c.rows("SELECT name, sql FROM sqlite_schema WHERE type='trigger' AND tbl_name IN ('entities','entity_names') ORDER BY rowid DESC")
		c.must("BEGIN IMMEDIATE")
		for _, row := range triggers {
			c.must(`DROP TRIGGER "` + strings.ReplaceAll(val(row[0]), `"`, `""`) + `"`)
		}
		for _, row := range triggers {
			c.must(val(row[1]))
		}
		c.must("COMMIT")
	}
	c.must("BEGIN IMMEDIATE")
	page := c.pageW("Archive", nil, "alpha")
	person := c.named("person", "Companion", M{"name": "Original full name"})
	c.must("COMMIT")
	c.must("CREATE VIRTUAL TABLE temp.search_terms USING fts5vocab(main, entities_fts, instance)")
	vocabulary := func() string {
		return c.tab("SELECT term,doc,col,offset FROM temp.search_terms ORDER BY term,doc,col,offset")
	}
	hits := func(q string) string {
		return c.tab("SELECT rowid FROM entities_fts WHERE entities_fts MATCH ? ORDER BY rowid", q)
	}
	check := func(stage string, body string) {
		s.K(label+" "+stage+" keeps canonical body", c.str("SELECT body FROM entities WHERE id=?", page) == body)
		r := c.tryx("INSERT INTO entities_fts(entities_fts,rank) VALUES('integrity-check',1)")
		s.K(label+" "+stage+" keeps FTS content consistency", r == "OK", r)
	}

	// The token-superset edit matches the nested touch-trigger scenario reported
	// upstream in August 2026. Revision-only nested updates must not reindex it.
	c.must("BEGIN IMMEDIATE")
	c.must("UPDATE entities SET body='alpha gamma' WHERE id=?", page)
	c.must("UPDATE entities SET body='alpha gamma delta' WHERE id=?", page)
	c.must("COMMIT")
	s.K(label+" nested touches preserve superset tokens", hits("alpha AND gamma AND delta") == ids(page))
	check("superset edit", "alpha gamma delta")

	c.must("BEGIN IMMEDIATE")
	c.must("INSERT INTO entity_names(entity_id,title,name_key) VALUES(?,'Alternate','alternate')", page)
	c.must("UPDATE entities SET preferred_name_key='alternate' WHERE id=?", page)
	c.must("UPDATE entities SET body='gamma delta' WHERE id=?", page)
	c.must("COMMIT")
	s.K(label+" alias selection and removed token agree", hits("alternate") == ids(page) && hits("archive") == ids(page) && hits("alpha") == "" && hits("gamma") == ids(page))
	stable := vocabulary()
	state := c.tab("SELECT id,preferred_name_key,body,source,revision FROM entities ORDER BY id")

	// A BEFORE trigger may already have deleted old index terms before another
	// guard rejects the statement. SQLite must undo all of those derived writes.
	err := c.tryx("UPDATE entities SET body='rejected omega',source='agent:changed' WHERE id=?", page)
	s.K(label+" refused combined edit rolls back index and source rows", strings.HasPrefix(err, "ERR") && vocabulary() == stable && c.tab("SELECT id,preferred_name_key,body,source,revision FROM entities ORDER BY id") == state, err)
	err = c.tryx("UPDATE entity_names SET title='[invalid]' WHERE entity_id=? AND name_key='archive'", page)
	s.K(label+" refused spelling rolls back index", strings.HasPrefix(err, "ERR") && vocabulary() == stable && hits("archive") == ids(page), err)
	check("refused statements", "gamma delta")

	c.must("BEGIN IMMEDIATE")
	c.must("SAVEPOINT draft")
	c.must("INSERT INTO entity_names(entity_id,title,name_key) VALUES(?,'Rollbackword','rollbackword')", page)
	c.must("UPDATE entities SET body='draftword',preferred_name_key='rollbackword' WHERE id=?", page)
	s.K(label+" savepoint exposes its provisional index", hits("draftword") == ids(page) && hits("rollbackword") == ids(page) && hits("gamma") == "")
	c.must("ROLLBACK TO draft")
	c.must("RELEASE draft")
	c.must("COMMIT")
	s.K(label+" savepoint rollback restores all index positions", vocabulary() == stable && hits("draftword OR rollbackword") == "" && c.tab("SELECT id,preferred_name_key,body,source,revision FROM entities ORDER BY id") == state)

	c.must("BEGIN IMMEDIATE")
	c.must("UPDATE entities SET body='transactionword' WHERE id=?", page)
	c.must("UPDATE entity_names SET title='ALTERNATE' WHERE entity_id=? AND name_key='alternate'", page)
	c.must("ROLLBACK")
	s.K(label+" transaction rollback restores index and revisions", vocabulary() == stable && hits("transactionword") == "" && c.tab("SELECT id,preferred_name_key,body,source,revision FROM entities ORDER BY id") == state)
	c.must("BEGIN IMMEDIATE")
	c.must("UPDATE people SET name='Corrected full name' WHERE id=?", person)
	c.must("UPDATE entities SET day='2026-10-07' WHERE id=?", page)
	c.must("COMMIT")
	s.K(label+" metadata-only nested touches leave index positions unchanged", vocabulary() == stable)
	check("rollback and metadata", "gamma delta")
	if err := c.Close(); err != nil {
		stop("close FTS writer: %v", err)
	}
	c = storageConnection(s, path)
	c.must("CREATE VIRTUAL TABLE temp.search_terms USING fts5vocab(main, entities_fts, instance)")
	s.K(label+" reopen retains index positions and exact search results", vocabulary() == stable && hits("gamma AND delta") == ids(page) && hits("alpha OR draftword OR transactionword") == "")
	check("reopen", "gamma delta")
	c.must("BEGIN IMMEDIATE")
	c.must("INSERT INTO entities_fts(entities_fts) VALUES('rebuild')")
	c.must("COMMIT")
	s.K(label+" rebuild equals incremental term positions", vocabulary() == stable)
	check("rebuild", "gamma delta")
}

func storageReaderCheckpoint(s *S) {
	path := filepath.Join(s.dir, "reader-checkpoint.db")
	w := storageConnection(s, path)
	w.must(s.ddl)
	w.must("PRAGMA wal_autocheckpoint=0")
	w.must("PRAGMA busy_timeout=0")
	w.must("BEGIN IMMEDIATE")
	id := w.pageW("Snapshot witness", nil, "oldword")
	w.must("COMMIT")
	ro := s.connect(path, "mode=ro", "_pragma=trusted_schema(0)")
	ro.must("BEGIN")
	s.K("WAL reader establishes original contents", ro.str("SELECT body FROM entities WHERE id=?", id) == "oldword")
	w.must("BEGIN IMMEDIATE")
	w.must("UPDATE entities SET body='newword' WHERE id=?", id)
	w.must("COMMIT")
	s.K("WAL open reader retains body and FTS snapshot after commit",
		ro.str("SELECT body FROM entities WHERE id=?", id) == "oldword" &&
			ro.n("SELECT count(*) FROM entities_fts WHERE entities_fts MATCH 'oldword'") == 1 &&
			ro.n("SELECT count(*) FROM entities_fts WHERE entities_fts MATCH 'newword'") == 0)
	checkpoint := w.rows("PRAGMA wal_checkpoint(TRUNCATE)")
	s.K("WAL retained reader prevents truncate checkpoint", len(checkpoint) == 1 && len(checkpoint[0]) == 3 && checkpoint[0][0] == int64(1), checkpoint)
	ro.must("COMMIT")
	s.K("WAL released reader sees committed body and FTS",
		ro.str("SELECT body FROM entities WHERE id=?", id) == "newword" &&
			ro.n("SELECT count(*) FROM entities_fts WHERE entities_fts MATCH 'newword'") == 1 &&
			ro.n("SELECT count(*) FROM entities_fts WHERE entities_fts MATCH 'oldword'") == 0)
	s.K("WAL truncate checkpoint completes after reader release", w.tab("PRAGMA wal_checkpoint(TRUNCATE)") == "0|0|0")
	if err := ro.Close(); err != nil {
		stop("close WAL reader: %v", err)
	}
	if err := w.Close(); err != nil {
		stop("close WAL writer: %v", err)
	}
	reopened := storageConnection(s, path)
	s.K("WAL checkpoint and reopen retain committed searchable contents", reopened.str("SELECT body FROM entities WHERE id=?", id) == "newword" && reopened.n("SELECT count(*) FROM entities_fts WHERE entities_fts MATCH 'newword'") == 1 && reopened.integrityOK())
}
