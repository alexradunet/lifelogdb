package tests

import "strings"

// names owns storage identity/name protections independently of transport policy.
func names(s *S) {
	c := s.fresh()
	probe := func(q string, args ...any) string {
		c.must("SAVEPOINT name_refusal")
		r := c.tryx(q, args...)
		c.must("ROLLBACK TO name_refusal")
		c.must("RELEASE name_refusal")
		return r
	}
	id := c.pageW("Original", nil, "body once")
	c.must("INSERT INTO entities_fts(entities_fts) VALUES('rebuild')")
	c.must("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,'Alias','alias')", id)
	s.K("name insertion indexes an alias on the existing document", c.n("SELECT count(*) FROM entities_fts WHERE entities_fts MATCH 'alias' AND rowid=?", id) == 1)
	s.K("name insertion preserves the original indexed content", c.n("SELECT count(*) FROM entities_fts WHERE entities_fts MATCH 'original' AND rowid=?", id) == 1 && c.tryx("INSERT INTO entities_fts(entities_fts,rank) VALUES('integrity-check',1)") == "OK")
	s.K("names own immutable registry ids", strings.Contains(probe("UPDATE entity_names SET id=id+100 WHERE entity_id=?", id), "immutable"))
	other := c.page("Other")
	s.K("names cannot transfer owners", strings.Contains(probe("UPDATE entity_names SET entity_id=? WHERE name_key='alias'", other), "immutable"))
	s.K("names cannot change normalized keys", strings.Contains(probe("UPDATE entity_names SET name_key='alternate',title='Alternate' WHERE name_key='alias'"), "immutable"))
	s.K("owned names cannot be deleted", strings.HasPrefix(probe("DELETE FROM entity_names WHERE name_key='alias'"), "ERR"))
	s.K("an unowned preferred selection refuses", strings.HasPrefix(c.tryx("UPDATE entities SET preferred_name_key='other' WHERE id=?", id), "ERR"))
	_, e := c.tryIdentity("page", "Alias", nil)
	s.K("all names reserve the global namespace", e != nil && strings.Contains(e.Error(), "UNIQUE"))
	// Isolate each maintenance witness from earlier deliberate index faults.
	c.must("INSERT INTO entities_fts(entities_fts) VALUES('rebuild')")
	c.must("UPDATE entities SET preferred_name_key='alias' WHERE id=?", id)
	s.K("preferred selection reindexes without losing old aliases", c.tryx("INSERT INTO entities_fts(entities_fts,rank) VALUES('integrity-check',1)") == "OK" && c.n("SELECT count(*) FROM entities_fts WHERE entities_fts MATCH 'original' AND rowid=?", id) == 1)
	spelling := c.page("Straße")
	c.must("INSERT INTO entities_fts(entities_fts) VALUES('rebuild')")
	c.must("UPDATE entity_names SET title='STRASSE' WHERE name_key='strasse'")
	s.K("case-equivalent spelling update keeps FTS consistent", c.tryx("INSERT INTO entities_fts(entities_fts,rank) VALUES('integrity-check',1)") == "OK" && c.n("SELECT count(*) FROM entities_fts WHERE entities_fts MATCH 'strasse' AND rowid=?", spelling) == 1 && c.n("SELECT count(*) FROM entities_fts WHERE entities_fts MATCH 'straße' AND rowid=?", spelling) == 0)
	c.must("INSERT INTO entities_fts(entities_fts) VALUES('rebuild')")
	c.must("UPDATE entities SET body='new content' WHERE id=?", id)
	s.K("body reindex deletes old tokens", c.n("SELECT count(*) FROM entities_fts WHERE entities_fts MATCH 'once' AND rowid=?", id) == 0 && c.tryx("INSERT INTO entities_fts(entities_fts,rank) VALUES('integrity-check',1)") == "OK")
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", id)
	_, e = c.tryIdentity("page", "Original", nil)
	s.K("tombstones reserve old aliases", e != nil && strings.Contains(e.Error(), "UNIQUE"))
	day := c.dayPage("2026-09-29", "")
	s.K("journal ownership permits no extra names", strings.HasPrefix(probe("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,'Journal alias','journal alias')", day), "ERR"))
	s.K("date spelling cannot belong to another identity", strings.HasPrefix(probe("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,'2026-09-28','2026-09-28')", other), "ERR"))
	s.K("journal preferred selection is fixed", strings.HasPrefix(c.tryx("UPDATE entities SET preferred_name_key='other' WHERE id=?", day), "ERR"))
	s.K("journal spelling is fixed", strings.HasPrefix(probe("UPDATE entity_names SET title='Other date é' WHERE entity_id=?", day), "ERR"))
	damaged := s.fresh()
	reserved := damaged.dayPage("2026-09-27", "")
	// Isolate the preferred-selection guard from the independent alias-insert guard.
	insertGuard := damaged.str("SELECT sql FROM sqlite_schema WHERE name='entity_names_day_insert'")
	damaged.must("DROP TRIGGER entity_names_day_insert")
	// This deliberate semantic fixture does not exercise derived-index maintenance.
	damaged.must("DROP TRIGGER entity_names_fts_insert")
	damaged.must("DROP TRIGGER entities_fts_before_update")
	damaged.must("DROP TRIGGER entities_fts_update")
	damaged.must("INSERT INTO entities_fts(entities_fts) VALUES('rebuild')")
	damaged.must("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,'Damaged journal alias','damaged journal alias')", reserved)
	damaged.must(insertGuard)
	s.K("journal selection refuses a deliberately damaged owned alias", strings.HasPrefix(damaged.tryx("UPDATE entities SET preferred_name_key='damaged journal alias' WHERE id=?", reserved), "ERR"))
}

// nameOwnership probes deferred completion without constructing another identity
// after an intentionally broken timing rule.
func nameOwnership(s *S) {
	c := s.fresh()
	missing := c.tryx("INSERT INTO entities(entity_type,preferred_name_key,created_at,updated_at,source) VALUES ('page',NULL," + NOW + "," + NOW + ",'ui')")
	s.K("preferred ownership is mandatory", strings.Contains(missing, "NOT NULL"), missing)
	c.must("BEGIN IMMEDIATE")
	missing = c.tryx("INSERT INTO entities(entity_type,preferred_name_key,created_at,updated_at,source) VALUES ('page','missing-name'," + NOW + "," + NOW + ",'ui')")
	s.K("entity insertion defers actual owned-name completion", missing == "OK", missing)
	commit := c.tryx("COMMIT")
	s.K("commit refuses incomplete name ownership", strings.HasPrefix(commit, "ERR"), commit)
	if commit != "OK" {
		c.must("ROLLBACK")
	}
	s.K("a name cannot refer to an absent owner", strings.HasPrefix(c.tryx("INSERT INTO entity_names(entity_id,title,name_key) VALUES (9999,'Absent owner','absent owner')"), "ERR"))
}
