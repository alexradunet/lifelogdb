package tests

import (
	"fmt"
	"strings"
	"time"
)

// identity: identity, provenance and deletion (schema.sql, D3, D8, D11) — the supertype and its composite foreign
// keys, ids carried by RETURNING, `source` on every row, `import_key` on entities (a re-run inserts nothing,
// cookbook/import-a-row-once), no hard deletes, updated_at kept by triggers.
func identity(s *S) {
	err := func(r string) bool { return strings.HasPrefix(r, "ERR") }
	entSQL := "INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES (?," + NOW + "," + NOW + ",'ui')"

	// ---- the supertype: a row's type and its table agree
	c := s.fresh()
	s.K("entities.entity_type rejects an unknown type", err(c.tryx(entSQL, "foo")))
	s.K("entities.entity_type has no event (D22)", err(c.tryx(entSQL, "event")))
	e := c.thing("page")
	s.K("entities.entity_type has no task (D23)", err(c.tryx(entSQL, "task")))
	s.K("a people row cannot take a plain page's id", err(c.tryx("INSERT INTO people(id,name) VALUES (?, 'p')", e)))
	s.K("a domain row needs its entities and pages rows", err(c.tryx("INSERT INTO people(id,name) VALUES (9999, 'x')")))
	s.K("a domain row cannot claim another type", err(c.tryx("INSERT INTO people(id,entity_type,name) VALUES (?, 'page', 'x')", e)))
	s.K("pages.entity_type cannot be an unknown type", err(c.tryx("INSERT INTO pages(id,entity_type,title,title_key) VALUES (?, 'task', 'T', 't')", c.ent("page"))))
	var odd []string
	for _, t := range c.col("select name from sqlite_schema where type='table' and name not like 'pages_fts%'") {
		if strings.Join(c.col("select type from pragma_table_info(?) where pk > 0 order by pk", t), ",") != "INTEGER" {
			odd = append(odd, t)
		}
	}
	s.K("the tables without a single INTEGER primary key are exactly the two registries", eq(sorted(odd), []string{"lifelog_meta", "link_kinds"}), odd)

	// ---- ids by RETURNING
	c = s.fresh()
	for i := range 10 { // entity ids run ahead of link ids
		c.page(fmt.Sprintf("Seed %d", i))
	}
	m := c.ent("page")
	c.must("INSERT INTO pages(id,title,title_key,day) VALUES (?, '2026-09-30', '2026-09-30', '2026-09-30')", m)
	g := c.page("Target")
	c.link(m, g, "wikilink")
	s.K("last_insert_rowid() moves: after a link insert it is the link's id, not the page's", c.n("select last_insert_rowid()") != m)
	var uses []string
	for _, b := range s.d.CookbookBlocks() {
		if strings.Contains(b.sql, "last_insert_rowid") {
			uses = append(uses, b.key)
		}
	}
	s.K("no cookbook block uses last_insert_rowid()", len(uses) == 0, uses)
	c = s.fresh()
	for i := range 10 {
		c.page(fmt.Sprintf("Seed %d", i))
	}
	p := P{}
	sync := func(st string) { // the cookbook/save-a-body link sync, inside cookbook/capture's transaction
		if strings.HasPrefix(strings.ToUpper(st), "UPDATE PAGES SET BODY") {
			c.link(p["page_id"], c.page("Lifelog"), "wikilink")
		}
	}
	_, e2 := c.runBlock(s.d.Block("capture"), p, sync)
	s.K("cookbook/capture run literally, with a link sync inside: the mood reading points at the day page the RETURNING gave",
		e2 == nil && c.tab("select captured_with_id from measurements") == val(p["page_id"]) && c.str("select title from pages where id=?", p["page_id"]) == "2026-09-29", e2, p)

	// ---- source on every row
	c = s.fresh()
	for _, t := range []string{"entities", "links", "measurements"} {
		s.K(t+".source is TEXT NOT NULL with no default", c.tab("select type, \"notnull\", dflt_value from pragma_table_info(?) where name='source'", t) == "TEXT|1|None")
	}
	s.K("an entities row without a source is refused", err(c.tryx("INSERT INTO entities(entity_type,created_at,updated_at) VALUES ('page',"+NOW+","+NOW+")")))
	entSrc := "INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES ('page'," + NOW + "," + NOW + ",?)"
	for _, v := range []string{"ui", "cli", "api", "agent:claude", "import:bank_csv", "import:health-2026.v2"} {
		s.K(fmt.Sprintf("source %q accepted on entities and measurements", v), c.tryx(entSrc, v) == "OK" && c.measure(1, "2026-01-01", 3, M{"source": v}) == "OK")
	}
	for _, v := range []any{"", "UI", "agent claude", strings.Repeat("x", 65), "a/b", "manual ", nil} {
		s.K(fmt.Sprintf("source %.20v rejected on entities and measurements", v), err(c.tryx(entSrc, v)) && err(c.measure(1, "2026-01-01", 3, M{"source": v})))
	}
	hab := c.metric("hab", "")
	s.K("links.source takes the same GLOB as entities.source", err(c.link(c.named("person", ""), c.named("person", ""), "friend", "UI")))
	s.K("habit_periods.source takes the same GLOB as entities.source", err(c.habit(hab, "2026-01-01", nil, "Import:x")))
	a, b := c.named("person", ""), c.named("person", "")
	s.K("entities.source cannot change", err(c.tryx("UPDATE entities SET source='cli' WHERE id=?", a)))
	s.K("a no-op full-row update and a tombstone still pass", c.tryx("UPDATE entities SET source=source, deleted_at="+NOW+" WHERE id=?", a) == "OK")
	c.link(a, b, "friend", "agent:claude")
	s.K("the mirror of a symmetric link copies its source", c.str("select source from links where from_id=? and to_id=?", b, a) == "agent:claude")
	s.K("links.source cannot change", err(c.tryx("UPDATE links SET source='ui' WHERE from_id=?", a)))
	s.K("a link note edit still passes", c.tryx("UPDATE links SET note='n', source=source WHERE from_id=?", a) == "OK")

	// Probe endpoints independently of provenance, with valid alternate pages.
	endpointConn := s.fresh()
	from, to, other := endpointConn.page("From"), endpointConn.page("To"), endpointConn.page("Other")
	endpointConn.link(from, to, "wikilink")
	s.K("a link's endpoints cannot change", err(endpointConn.tryx("UPDATE links SET from_id=? WHERE from_id=? AND to_id=?", other, from, to)) &&
		err(endpointConn.tryx("UPDATE links SET to_id=? WHERE from_id=? AND to_id=?", other, from, to)) &&
		endpointConn.tab("SELECT from_id,to_id FROM links") == ids(from)+"|"+ids(to))

	// ---- no hard deletes
	c = s.fresh()
	byType := map[string]int64{"page": c.thing("page"), "person": c.thing("person"), "place": c.thing("place"), "metric": c.thing("metric"), "file": c.thing("file")}
	n0 := c.n("select count(*) from entities where source <> 'schema'")
	for _, x := range [][2]string{{"page", "pages"}, {"person", "people"}, {"place", "pages"}, {"metric", "metrics"}, {"file", "files"}} {
		s.K("DELETE FROM "+x[1]+" is refused", err(c.tryx("DELETE FROM "+x[1]+" WHERE id=?", byType[x[0]])))
		s.K("DELETE of the "+x[0]+" entities row is refused", err(c.tryx("DELETE FROM entities WHERE id=?", byType[x[0]])))
	}
	s.K("the page rows of named entities cannot be deleted either", err(c.tryx("DELETE FROM pages WHERE id=?", byType["person"])))
	s.K("nothing was removed (five entities, one id each)", c.n("select count(*) from entities where source <> 'schema'") == n0 && n0 == 5)
	s.K("REPLACE INTO pages is blocked under recursive_triggers=ON",
		err(c.tryx(fmt.Sprintf("REPLACE INTO pages(id,entity_type,title,title_key,body) VALUES (%d,'page','Replaced','replaced','overwritten')", byType["page"]))) &&
			c.str("select body from pages where id=?", byType["page"]) == "x")
	o := c.ent("page")
	s.K("an orphan entities row cannot be deleted either (only the entities trigger can stop it)", err(c.tryx("DELETE FROM entities WHERE id=?", o)))
	c.must("PRAGMA foreign_keys=OFF")
	s.K("with foreign_keys=OFF: DELETE FROM entities still refused (the trigger, not the FK)", err(c.tryx("DELETE FROM entities WHERE id=?", byType["person"])))
	s.K("with foreign_keys=OFF: DELETE FROM people still refused", err(c.tryx("DELETE FROM people WHERE id=?", byType["person"])))
	c.must("PRAGMA foreign_keys=ON")
	c.link(byType["person"], byType["place"], "about")
	s.K("links rows may be hard-deleted (the one such table)", c.tryx("DELETE FROM links WHERE from_id=?", byType["person"]) == "OK" && c.n("select count(*) from links") == 0)
	c.must("INSERT INTO link_kinds(kind) VALUES ('spare')")
	s.K("an unreferenced registry row may be deleted (registries are administrative)", c.tryx("DELETE FROM link_kinds WHERE kind='spare'") == "OK")
	c.link(byType["person"], byType["place"], "about")
	s.K("a referenced link kind is refused by its foreign key, not by a trigger", strings.Contains(strings.ToUpper(c.tryx("DELETE FROM link_kinds WHERE kind='about'")), "FOREIGN KEY"))

	// ---- updated_at, kept by triggers
	c = s.fresh()
	byType = map[string]int64{"page": c.thing("page"), "person": c.thing("person"), "place": c.thing("place")}
	c.must("UPDATE entities SET updated_at='2000-01-01T00:00:00.000Z', created_at='2000-01-01T00:00:00.000Z'")
	for _, x := range [][2]string{{"page", "UPDATE pages SET body='y' WHERE id=?"}, {"person", "UPDATE people SET birth_day='1990-01-01' WHERE id=?"}, {"place", "UPDATE pages SET body='a café' WHERE id=?"}} {
		c.must(x[1], byType[x[0]])
		s.K("updating a "+x[0]+" bumps entities.updated_at", c.n("select updated_at > created_at from entities where id=?", byType[x[0]]) == 1)
	}

	// ---- entities.import_key: a re-run, a replay or a retry inserts nothing (cookbook/import-a-row-once)
	c = s.fresh()
	imp := "INSERT INTO entities(entity_type,created_at,updated_at,source,import_key) VALUES ('page'," + NOW + "," + NOW + ",'import:x','k1')"
	s.K("a second entity with the same (source, import_key) is refused", c.tryx(imp) == "OK" && strings.Contains(c.tryx(imp), "UNIQUE"))
	c = s.fresh()
	var B []string
	for _, st := range statements(s.d.Block("import-a-row-once")) {
		if v := verb(st); v != "BEGIN" && v != "COMMIT" {
			B = append(B, st)
		}
	}
	if len(B) != 3 {
		stop("cookbook/import-a-row-once has %d statements besides BEGIN and COMMIT, not 3", len(B))
	}
	insEnt, insPage, upd := B[0], B[1], B[2]
	K := "notes/sourdough.md"
	runImport := func(key string) [][]any {
		got := c.rows(insEnt, P{"import_key": key})
		if len(got) > 0 {
			c.must(insPage, P{"page_id": got[0][0]})
		}
		return got
	}
	first, importErr := c.query(insEnt, P{"import_key": K})
	s.K("cookbook/import-a-row-once: the first insert returns an id", importErr == nil && len(first) == 1, importErr)
	if importErr == nil && len(first) == 1 {
		c.must(insPage, P{"page_id": first[0][0]})
		again := runImport(K)
		fid := first[0][0]
		body := func() string { return c.str("select body from pages where id=?", fid) }
		s.K("cookbook/import-a-row-once: the first run returns an id, the re-run returns none and adds no page",
			len(first) == 1 && len(again) == 0 && c.n("select count(*) from pages where title='Sourdough'") == 1 && c.integrityOK())
		s.K("the same import_key under another source is another row (per-source namespace)",
			c.tryx("INSERT INTO entities(entity_type,created_at,updated_at,source,import_key) VALUES ('page',"+NOW+","+NOW+",'import:health',?)", K) == "OK")
		twice := c.tryx("INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES ('page',"+NOW+","+NOW+",'import:vault')") == "OK" &&
			c.tryx("INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES ('page',"+NOW+","+NOW+",'import:vault')") == "OK"
		s.K("rows without an import_key never collide", twice)
		s.K("a plain INSERT of a known key is refused (the index is unique)",
			strings.Contains(c.tryx("INSERT INTO entities(entity_type,created_at,updated_at,source,import_key) VALUES ('page',"+NOW+","+NOW+",'import:vault',?)", K), "UNIQUE"))
		keyFixed := s.K("entities.import_key cannot change", strings.Contains(c.tryx("UPDATE entities SET import_key='notes/other.md' WHERE id=?", fid), "never changed"))
		keyNotCleared := s.K("...nor be cleared", strings.Contains(c.tryx("UPDATE entities SET import_key=NULL WHERE id=?", fid), "never changed"))
		if keyFixed && keyNotCleared {
			c.must(upd, P{"import_key": K})
			s.K("cookbook/import-a-row-once: a changed note is updated in its own page", body() == "Feed the starter the night before; 75% water.", body())
			c.must("UPDATE pages SET body='old' WHERE id=?", fid)
			c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", fid)
			c.must(upd, P{"import_key": K})
			again = runImport(K)
			s.K("cookbook/import-a-row-once: a tombstoned import is neither updated nor inserted again",
				body() == "old" && len(again) == 0 && c.n("select count(*) from pages where title='Sourdough'") == 1)
		}
	}

	c = s.fresh()
	pg := c.thing("page")
	c.thing("person")
	c.thing("place")
	c.must("UPDATE entities SET updated_at='2000-01-01T00:00:00.000Z'")
	time.Sleep(2 * time.Millisecond)
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", pg)
	r := c.rows("select updated_at, deleted_at from entities where id=?", pg)[0]
	s.K("a tombstone bumps updated_at to the tombstone instant, and the trigger does not re-fire itself", r[0] == r[1] && val(r[0]) > "2000", r)
}
