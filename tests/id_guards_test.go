package tests

import (
	"fmt"
	"path/filepath"
	"strings"
)

// idGuards exercises SQLite's INTEGER PRIMARY KEY aliases on a real hardened file.
func idGuards(s *S) {
	c := s.freshWith(F{Path: filepath.Join(s.dir, "id-guards.db"), Hardened: true})
	a, b := c.named("person", "Alice"), c.named("person", "Bob")
	if r := c.link(a, b, "friend"); r != "OK" {
		stop("friend fixture: %s", r)
	}
	name := c.page("Straße")
	nameID := c.n("SELECT id FROM entity_names WHERE entity_id=?", name)
	linkID := c.n("SELECT id FROM links WHERE from_id=? AND to_id=?", a, b)
	state := func() string {
		return c.tab("SELECT id,entity_type,preferred_name_key,day,body,revision,created_at,updated_at,deleted_at,source,import_key FROM entities ORDER BY id") + " / " + c.tab("SELECT id,entity_id,title,name_key FROM entity_names ORDER BY id") + " / " + c.tab("SELECT id,from_id,to_id,kind,note,created_at,source FROM links ORDER BY id")
	}
	indexed := func() bool {
		return c.tryx("INSERT INTO entities_fts(entities_fts,rank) VALUES('integrity-check',1)") == "OK" && c.n("SELECT count(*) FROM entities_fts WHERE entities_fts MATCH 'straße' AND rowid=?", name) == 1
	}
	for _, table := range []string{"links", "entity_names"} {
		id := linkID
		edit := "note='changed'"
		if table == "entity_names" {
			id = nameID
			edit = "title='STRASSE'"
		}
		for _, alias := range []string{"id", "rowid", "_rowid_", "oid"} {
			for _, combined := range []bool{false, true} {
				before := state()
				c.must("BEGIN IMMEDIATE")
				set := alias + "=id+100000"
				if combined {
					set += "," + edit
				}
				r := c.tryx("UPDATE "+table+" SET "+set+" WHERE id=?", id)
				s.K(fmt.Sprintf("%s %s combined=%v identity refusal is atomic", table, alias, combined), strings.Contains(r, "immutable") && state() == before && indexed(), r)
				c.must("ROLLBACK") // A surviving mutant must not damage the next independent witness.
			}
			before := state()
			r := c.tryx("UPDATE "+table+" SET "+alias+"=id WHERE id=?", id)
			s.K(table+" "+alias+" identity no-op preserves state", r == "OK" && state() == before && indexed(), r)
		}
	}
	before := c.n("SELECT revision FROM entities WHERE id=?", a)
	c.must("UPDATE links SET note='allowed' WHERE id=?", linkID)
	s.K("permitted note edit preserves mirror identities and advances revisions", c.n("SELECT count(*) FROM links WHERE kind='friend' AND note='allowed'") == 2 && c.n("SELECT id FROM links WHERE from_id=? AND to_id=?", a, b) == linkID && c.n("SELECT revision FROM entities WHERE id=?", a) > before)
	before = c.n("SELECT revision FROM entities WHERE id=?", name)
	c.must("UPDATE entity_names SET title='STRASSE' WHERE id=?", nameID)
	s.K("permitted equivalent spelling preserves registry identity and FTS", c.n("SELECT entity_id FROM entity_names WHERE id=?", nameID) == name && c.n("SELECT revision FROM entities WHERE id=?", name) > before && c.tryx("INSERT INTO entities_fts(entities_fts,rank) VALUES('integrity-check',1)") == "OK" && c.n("SELECT count(*) FROM entities_fts WHERE entities_fts MATCH 'strasse' AND rowid=?", name) == 1)
	finalState := state()
	c.must("UPDATE links SET note=note WHERE rowid=?", linkID)
	c.must("UPDATE entity_names SET title=title WHERE oid=?", nameID)
	s.K("allowed edit no-ops preserve identity state and revisions", state() == finalState && c.tryx("INSERT INTO entities_fts(entities_fts,rank) VALUES('integrity-check',1)") == "OK")
	if err := c.Close(); err != nil {
		stop("close identity fixture: %v", err)
	}
	reopened := s.readOnly(filepath.Join(s.dir, "id-guards.db"))
	s.K("identity guards persist after reopen", reopened.n("SELECT count(*) FROM links WHERE kind='friend' AND note='allowed'") == 2 && reopened.n("SELECT entity_id FROM entity_names WHERE id=?", nameID) == name)
}
