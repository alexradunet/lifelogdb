package tests

// These probes damage stored data with guards restored before the literal contract
// checks run. Schema-object presence alone cannot discover these failures.
func semanticIntegrity(s *S, checks []string) {
	bypass := func(c *C, trigger string, change func()) {
		definition := c.str("SELECT sql FROM sqlite_schema WHERE type='trigger' AND name=?", trigger)
		c.must("DROP TRIGGER " + trigger)
		change()
		c.must(definition)
	}

	for _, mismatch := range []bool{false, true} {
		c := s.fresh()
		a, b := c.page("One"), c.page("Two")
		var note any
		if mismatch {
			note = "shared"
		}
		c.must("INSERT INTO links(from_id,to_id,kind,note,created_at,source) VALUES(?,?,'related',?,"+NOW+",'ui')", a, b, note)
		c.must("INSERT INTO links(from_id,to_id,kind,note,created_at,source) VALUES(?,?,'related',NULL,"+NOW+",'ui')", a, a)
		s.K("symmetric integrity accepts mirrored and self relationships", c.tab(checks[9]) == "")
		if mismatch {
			bypass(c, "links_mirror_note", func() { c.must("UPDATE links SET note=NULL WHERE from_id=? AND to_id=?", b, a) })
			s.K("symmetric integrity detects unequal shared notes", c.integrityOK() && c.tab(checks[9]) == c.tab("SELECT id FROM links WHERE from_id<>to_id ORDER BY id"))
		} else {
			bypass(c, "links_mirror_delete", func() { c.must("DELETE FROM links WHERE from_id=? AND to_id=?", b, a) })
			s.K("symmetric integrity detects a missing reverse link", c.integrityOK() && c.tab(checks[9]) == c.tab("SELECT id FROM links WHERE from_id<>to_id ORDER BY id"))
		}
	}

	c := s.fresh()
	c.must("INSERT INTO measurements(id,metric_id,day,value,source,created_at) VALUES(200,1,'2026-10-01',3,'ui'," + NOW + ")")
	c.must("INSERT INTO measurements(id,metric_id,day,value,supersedes_id,source,created_at) VALUES(3,1,'2026-10-01',NULL,200,'ui'," + NOW + ")")
	s.K("correction integrity accepts retractions and nonmonotonic ids", c.tab(checks[10]) == "" && c.n("SELECT count(*) FROM measurement_values") == 0)
	bypass(c, "measurements_no_update", func() { c.must("UPDATE measurements SET supersedes_id=3 WHERE id=200") })
	s.K("correction integrity detects a rootless cycle", c.integrityOK() && c.tab(checks[10]) == "3; 200")

	c = s.fresh()
	metric := c.metric("Stretch", "")
	c.must("INSERT INTO habit_periods(metric_id,start_day,end_day,source) VALUES(?,'2026-10-01','2026-10-03','ui'),(?,'2026-10-04',NULL,'ui')", metric, metric)
	s.K("habit integrity accepts adjacent inclusive periods", c.tab(checks[11]) == "")
	bypass(c, "habit_periods_check_update", func() { c.must("UPDATE habit_periods SET end_day='2026-10-04' WHERE start_day='2026-10-01'") })
	s.K("habit integrity detects overlapping periods", c.integrityOK() && c.tab(checks[11]) == "1; 2")

	c = s.fresh()
	metric = c.metric("Stretch", "")
	c.must("INSERT INTO habit_periods(metric_id,start_day,end_day,source) VALUES(?,'2026-10-01','2026-10-03','ui')", metric)
	c.must("INSERT INTO measurements(id,metric_id,day,value,source,created_at) VALUES(10,?,'2026-10-02',2,'ui',"+NOW+")", metric)
	s.K("habit integrity detects current nonbinary check-ins", c.integrityOK() && c.tab(checks[12]) == "10")
	c.must("INSERT INTO measurements(metric_id,day,value,supersedes_id,source,created_at) VALUES(?,'2026-10-02',1,10,'ui',"+NOW+")", metric)
	s.K("habit integrity retains corrected invalid values as history", c.tab(checks[12]) == "")

	for _, journal := range []bool{false, true} {
		c = s.fresh()
		title, alias, key := "Ordinary", "2026-10-01", "2026-10-01"
		if journal {
			title, alias, key = "2026-10-01", "Extra", "extra"
		}
		var owner int64
		if journal {
			owner = c.dayPage(title, "")
		} else {
			owner = c.page(title)
		}
		s.K("journal integrity accepts canonical ownership and ordinary names", c.tab(checks[13]) == "")
		bypass(c, "entity_names_day_insert", func() { c.must("INSERT INTO entity_names(entity_id,title,name_key) VALUES(?,?,?)", owner, alias, key) })
		witness := "journal integrity detects a date name on another owner"
		if journal {
			witness = "journal integrity detects extra journal aliases"
		}
		s.K(witness, c.integrityOK() && c.tab(checks[13]) == c.tab("SELECT id FROM entity_names WHERE name_key=?", key))
	}
}
