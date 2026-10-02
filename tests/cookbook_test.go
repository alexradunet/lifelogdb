package tests

import "path/filepath"

// cookbook: every SQL block of the cookbook (docs/cookbook/) prepares and runs on a seeded database, statement by
// statement with its ids carried by RETURNING — on a normal connection and on a hardened one
// (SQLITE_DBCONFIG_DEFENSIVE + trusted_schema=OFF, contract/connections); and every block that only reads runs on
// a reader (mode=ro + trusted_schema=OFF, contract/connections).
func cookbook(s *S) {
	bl := s.d.CookbookBlocks()
	first, order := s.d.Blocks()
	s.K("the cookbook has at least 16 SQL blocks, one or more per recipe, 16 recipes", len(bl) >= 16 && len(first) == 16 && eq(order, s.d.CookbookOrder()), order)

	seeded := func(hardened bool, path string) (*C, P) {
		c := s.freshWith(F{Hardened: hardened, Path: path})
		dp := c.dayPage("2026-09-28", "Shipped the schema with [[Sam]] in [[Japan]]. [[Lifelog]]")
		wp := c.page("Lifelog")
		c.link(dp, wp, "wikilink")
		pe := c.named("person", "Sam", M{"name": "Sam"})
		gh := c.page("Ana")
		pl := c.named("place", "Japan")
		c.link(dp, pe, "wikilink")
		c.link(dp, pl, "wikilink")
		c.link(dp, pl, "at")
		c.must("INSERT INTO metrics(name,unit) VALUES ('weight','kg')")
		c.must("INSERT INTO metrics(name,unit) VALUES ('vitamin_d','')")
		c.measure(2, "2026-09-29", 71.2)
		c.measure(2, "2026-09-28", 70.9)
		return c, P{"found_id": wp, "target_id": wp, "target_ids": "[]", "place_id": pl, "day_page_id": dp, "mistaken_row_id": 2, "from_day": "2026-01-15", "to_day": "2026-09-10",
			"day": "2026-09-29", "page_id": wp, "person_id": pe, "entity_id": pe, "handle_title": "Bob Sample", "handle_key": "bob sample", "ghost_id": gh,
			"due_day": "2026-10-05", "query": "schema", "key": "newpage", "title": "Newpage", "metric_id": 2, "wrong_row_id": 1, "source": "ui",
			"import_key": "notes/sourdough.md", "metric": "vitamin_d"}
	}

	for _, hardened := range []bool{false, true} {
		tag := map[bool]string{false: "plain", true: "hardened"}[hardened]
		c, p := seeded(hardened, "")
		nprep, nfail := 0, 0
		var why []string
		for _, b := range bl { // every statement prepares against the DDL
			for _, st := range statements(b.sql) {
				switch verb(st) {
				case "BEGIN", "COMMIT", "SAVEPOINT", "RELEASE", "ROLLBACK":
					continue
				}
				nprep++
				nulls := P{}
				for _, m := range paramName.FindAllStringSubmatch(code(st), -1) {
					nulls[m[1]] = nil
				}
				if _, e := c.query("EXPLAIN "+st, nulls); e != nil {
					nfail++
					why = append(why, b.key+": "+e.Error())
				}
			}
		}
		s.K(tag+": all statements of the cookbook prepare", nprep > 0 && nfail == 0, nprep, why)
		for _, b := range bl { // and every block runs, in document order, on one database
			r := "OK"
			if _, e := c.runBlock(b.sql, p, nil); e != nil {
				r = "ERR " + e.Error()
				c.tryx("ROLLBACK")
			}
			s.K(tag+": "+b.key+" runs", r == "OK", r)
		}
		s.K(tag+": the database is clean after the whole cookbook", c.integrityOK())
	}

	// a reader of contract/connections: mode=ro and trusted_schema=OFF, on a file a hardened writer seeded
	path := filepath.Join(s.dir, "cookbook-reader.db")
	_, p := seeded(true, path)
	r := s.connect(path, "mode=ro", "_pragma=trusted_schema(0)")
	nread := 0
	var bad []string
	for _, b := range bl {
		reads := true
		for _, st := range statements(b.sql) {
			reads = reads && (verb(st) == "SELECT" || verb(st) == "WITH")
		}
		if !reads {
			continue
		}
		nread++
		if _, e := r.runBlock(b.sql, p, nil); e != nil {
			bad = append(bad, b.key+": "+e.Error())
		}
	}
	s.K("reader: every block that only reads runs on a mode=ro connection with trusted_schema=OFF",
		nread >= 8 && len(bad) == 0 && r.n("PRAGMA trusted_schema") == 0, nread, bad)
}
