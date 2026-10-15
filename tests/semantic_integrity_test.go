package tests

// These probes damage stored data with guards restored before the literal contract
// checks run. Schema-object presence alone cannot discover these failures.
func semanticIntegrity(s *S, checks []string) {
	c := s.fresh()
	other := c.metric("Other", "kg")
	c.must("INSERT INTO measurements(id,metric_id,day,value,source,created_at) VALUES(100,1,'2026-10-01',3,'ui'," + NOW + ")")
	c.must("INSERT INTO measurements(id,metric_id,day,value,supersedes_id,source,created_at) VALUES(101,1,'2026-10-01',4,100,'ui'," + NOW + ")")
	s.K("correction integrity accepts a correction of the same metric", c.tab(checks[4]) == "")
	bypass(c, "measurements_supersede_metric", func() {
		c.must("INSERT INTO measurements(id,metric_id,day,value,supersedes_id,source,created_at) VALUES(102,?,'2026-10-01',5,101,'ui',"+NOW+")", other)
	})
	s.K("correction integrity detects a correction of another metric", c.integrityOK() && c.tab(checks[4]) == "102")

	c = s.fresh()
	c.must("INSERT INTO measurements(id,metric_id,day,value,source,created_at) VALUES(200,1,'2026-10-01',3,'ui'," + NOW + ")")
	c.must("INSERT INTO measurements(id,metric_id,day,value,supersedes_id,source,created_at) VALUES(3,1,'2026-10-01',NULL,200,'ui'," + NOW + ")")
	s.K("correction integrity accepts retractions and nonmonotonic ids", c.tab(checks[8]) == "" && c.n("SELECT count(*) FROM measurement_values") == 0)
	bypass(c, "measurements_no_update", func() { c.must("UPDATE measurements SET supersedes_id=3 WHERE id=200") })
	s.K("correction integrity detects a rootless cycle", c.integrityOK() && c.tab(checks[8]) == "3; 200")

	c = s.fresh()
	metric := c.metric("Stretch", "")
	c.must("INSERT INTO habit_periods(metric_id,start_day,end_day,source) VALUES(?,'2026-10-01','2026-10-03','ui'),(?,'2026-10-04',NULL,'ui')", metric, metric)
	s.K("habit integrity accepts adjacent inclusive periods", c.tab(checks[9]) == "")
	bypass(c, "habit_periods_check_update", func() { c.must("UPDATE habit_periods SET end_day='2026-10-04' WHERE start_day='2026-10-01'") })
	s.K("habit integrity detects overlapping periods", c.integrityOK() && c.tab(checks[9]) == "1; 2")

	// a period on a metric with a unit (Mood, a scale): the guard is dropped to write it, restored before the check
	c = s.fresh()
	bypass(c, "habit_periods_check_insert", func() { c.must("INSERT INTO habit_periods(metric_id,start_day,source) VALUES(1,'2026-10-01','ui')") })
	s.K("habit integrity detects a period on a metric with a unit", c.integrityOK() && c.tab(checks[9]) == "1")

	c = s.fresh()
	metric = c.metric("Stretch", "")
	c.must("INSERT INTO habit_periods(metric_id,start_day,end_day,source) VALUES(?,'2026-10-01','2026-10-03','ui')", metric)
	bypass(c, "measurements_habit_binary", func() {
		c.must("INSERT INTO measurements(id,metric_id,day,value,source,created_at) VALUES(10,?,'2026-10-02',2,'ui',"+NOW+")", metric)
	})
	s.K("habit integrity detects current nonbinary check-ins", c.integrityOK() && c.tab(checks[10]) == "10")
	c.must("INSERT INTO measurements(metric_id,day,value,supersedes_id,source,created_at) VALUES(?,'2026-10-02',1,10,'ui',"+NOW+")", metric)
	s.K("habit integrity retains corrected invalid values as history", c.tab(checks[10]) == "")

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
		s.K("journal integrity accepts canonical ownership and ordinary names", c.tab(checks[11]) == "")
		bypass(c, "entity_names_day_insert", func() { c.must("INSERT INTO entity_names(entity_id,title,name_key) VALUES(?,?,?)", owner, alias, key) })
		witness := "journal integrity detects a date name on another owner"
		if journal {
			witness = "journal integrity detects extra journal aliases"
		}
		s.K(witness, c.integrityOK() && c.tab(checks[11]) == c.tab("SELECT id FROM entity_names WHERE name_key=?", key))
	}
}
