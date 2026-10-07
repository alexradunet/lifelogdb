package tests

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
)

// cookbook: every SQL block of the cookbook (docs/cookbook/) prepares and runs on a seeded database, statement by
// statement with its ids carried by RETURNING — on a normal connection and on a hardened one
// (SQLITE_DBCONFIG_DEFENSIVE + trusted_schema=OFF, contract/connections); and every block that only reads runs on
// a reader (mode=ro + trusted_schema=OFF, contract/connections).
func cookbook(s *S) {
	cookbookMeasurementReads(s)
	bl := s.d.CookbookBlocks()
	first, order := s.d.Blocks()
	s.K("the cookbook has at least 24 SQL blocks, one or more per recipe, 24 recipes", len(bl) >= 24 && len(first) == 24 && eq(order, s.d.CookbookOrder()), order)

	seeded := func(hardened bool, path string) (*C, P) {
		c := s.freshWith(F{Hardened: hardened, Path: path})
		dp := c.dayPage("2026-09-28", "Shipped the schema with [[Sam]] in [[Japan]]. [[Lifelog]]")
		wp := c.page("Lifelog")
		cat := c.page("Biomarkers")
		c.link(dp, wp, "wikilink")
		pe := c.named("person", "Sam", M{"name": "Sam"})
		gh := c.page("Ana")
		pl := c.named("place", "Japan")
		c.link(dp, pe, "wikilink")
		c.link(dp, pl, "wikilink")
		c.link(dp, pl, "at")
		w := c.metric("weight", "kg")
		c.metric("vitamin_d", "")
		c.measure(w, "2026-09-29", 71.2)
		c.measure(w, "2026-09-28", 70.9)
		old := c.pageW("Sourdogh", nil, "Feed the starter. [[Lifelog]]")
		c.link(old, wp, "wikilink")
		c.link(old, pl, "about")
		planningProject := c.page("Planning context")
		planningID := planningTask(c, M{"project_page_id": planningProject, "repeat_unit": "month", "anchor_day": "2026-01-31"})
		return c, P{"include_deleted": 0, "old_id": old, "new_title": "Sourdough starter", "new_key": "sourdough starter", "found_id": wp, "target_id": wp, "target_ids": "[]", "place_id": pl, "day_page_id": dp, "mistaken_row_id": 2, "from_day": "2026-01-15", "to_day": "2026-09-10",
			"day": "2026-09-29", "page_id": wp, "person_id": pe, "entity_id": pe, "handle_title": "Bob Sample", "handle_key": "bob sample", "ghost_id": gh,
			"due_day": "2026-10-05", "query": "schema", "key": "newpage", "title": "Newpage", "metric_id": w, "wrong_row_id": 1, "source": "ui",
			"import_key": "notes/sourdough.md", "metric": "vitamin_d", "as_of": "2099-01-01T00:00:00.000Z", "parent_id": cat,
			"task_label": "Garden supplies", "project_page_id": planningProject, "planning_task_id": planningID, "planning_clock": "09:00",
			"planning_task_version": 1, "planning_occurrence_version": 1, "planning_state": "open", "planning_completed_at": nil,
			"planning_reminder_mode": "inherit", "planning_reminder_at": nil, "planning_until": "2026-10-31",
			"planning_from": "2026-10-01", "planning_through": "2026-11-30",
			"sha256": strings.Repeat("ab", 32), "mime": "image/heic", "preview": jpegBytes, "file_title": "2026-09-29 Lake.jpg", "append_embed": 1,
			"file_key": "2026-09-29 lake.jpg", "body": "The lake at dawn with [[Sam]].",
			"lat": 38.7139, "lon": -9.1394, "radius_m": 8000, "link_days": 1, "m_per_deg_lon": 111320 * math.Cos(38.7139*math.Pi/180), "taken_day": "2026-08-15",
			"snapshot": filepath.ToSlash(filepath.Join(s.dir, fmt.Sprintf("cookbook-snapshot-%v.db", hardened)))}
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
