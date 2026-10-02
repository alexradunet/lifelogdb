package tests

import (
	"fmt"
	"strings"
)

// habits: a unitless metric with active periods (D24) — the CHECKs and triggers of habit_periods (days, order,
// no overlap on insert or update, unitless only, never deleted); cookbook/habits run literally (start, stop, the
// day's habits done / not done / not recorded, completion over a period); the cookbook/day-view day view lists the
// day's habits and not their check-ins again; a day without a check-in is never assumed.
func habits(s *S) {
	err := func(r string) bool { return strings.HasPrefix(r, "ERR") }

	// ---- the table's rules
	c := s.fresh()
	vd, kg := c.metric("vitamin_d", ""), c.metric("weight_x", "kg")
	s.K("a period on a unitless metric is accepted", c.habit(vd, "2026-10-01", "2026-10-31") == "OK")
	for _, bad := range []string{"2026-9-3", "2026-02-31", "today"} {
		s.K(fmt.Sprintf("start_day %q refused", bad), err(c.habit(vd, bad, nil)))
	}
	s.K("an end before the start refused (habit_periods_order)", err(c.habit(vd, "2027-01-10", "2027-01-09")))
	s.K("a period on a metric with a unit refused: a habit is 0/1", strings.Contains(c.habit(kg, "2026-10-01", nil), "unitless"))
	s.K("an overlapping period refused", strings.Contains(c.habit(vd, "2026-10-31", "2026-11-05"), "overlap"))
	s.K("an open period overlapping a closed one refused", strings.Contains(c.habit(vd, "2026-09-01", nil), "overlap"))
	s.K("a period right after the last one accepted (back to back)", c.habit(vd, "2026-11-01", "2026-11-30") == "OK")
	s.K("an open period after them accepted", c.habit(vd, "2027-01-01", nil) == "OK")
	s.K("...and a second open one refused", strings.Contains(c.habit(vd, "2027-06-01", nil), "overlap"))
	s.K("the same start twice is refused by UNIQUE", strings.Contains(c.habit(vd, "2027-01-01", nil), "UNIQUE"))
	s.K("...and ON CONFLICT DO NOTHING makes it a no-op (the trigger leaves it to UNIQUE)",
		c.tryx("INSERT INTO habit_periods(metric_id,start_day,source) VALUES (?, '2027-01-01', 'ui') ON CONFLICT(metric_id, start_day) DO NOTHING", vd) == "OK")
	s.K("a re-run with a changed end_day is a no-op until the UPDATE follows",
		c.tryx("INSERT INTO habit_periods(metric_id,start_day,end_day,source) VALUES (?, '2026-10-01', '2026-10-15', 'ui') ON CONFLICT(metric_id, start_day) DO NOTHING", vd) == "OK" &&
			c.str("select end_day from habit_periods where start_day='2026-10-01'") == "2026-10-31")
	s.K("cookbook/habits documents the idempotent re-run", strings.Contains(s.d.Page("cookbook/habits.md"), "ON CONFLICT(metric_id, start_day) DO NOTHING"))
	// a restarted habit: the re-send of the closed first period must carry its end_day (cookbook/habits)
	c2 := s.fresh()
	rs := c2.metric("stretching", "")
	s.K("a closed period, then a later open one (a restarted habit)", c2.habit(rs, "2026-01-01", "2026-01-31") == "OK" && c2.habit(rs, "2026-03-01", nil) == "OK")
	s.K("cookbook/habits re-send of the closed period WITH its end_day is a no-op beside the later period",
		c2.tryx("INSERT INTO habit_periods(metric_id,start_day,end_day,source) VALUES (?, '2026-01-01', '2026-01-31', 'ui') ON CONFLICT(metric_id, start_day) DO NOTHING", rs) == "OK" &&
			c2.n("select count(*) from habit_periods where metric_id=?", rs) == 2)
	s.K("...re-sent WITHOUT its end_day it is an open period, and the insert trigger refuses the overlap",
		strings.Contains(c2.tryx("INSERT INTO habit_periods(metric_id,start_day,source) VALUES (?, '2026-01-01', 'ui') ON CONFLICT(metric_id, start_day) DO NOTHING", rs), "overlap"))
	s.K("cookbook/habits says a re-sent period carries its end_day", strings.Contains(s.d.Page("cookbook/habits.md"), "carrying the `end_day` it was sent with"))
	s.K("moving a period onto another refused (update)", strings.Contains(c.tryx("UPDATE habit_periods SET end_day='2026-11-15' WHERE start_day='2026-10-01'"), "overlap"))
	s.K("...moving it into a gap accepted", c.tryx("UPDATE habit_periods SET end_day='2026-10-30' WHERE start_day='2026-10-01'") == "OK")
	s.K("moving a period to a metric with a unit refused (update)", strings.Contains(c.tryx("UPDATE habit_periods SET metric_id=? WHERE start_day='2026-10-01'", kg), "unitless"))
	s.K("a period is never deleted", err(c.tryx("DELETE FROM habit_periods")) && c.n("select count(*) from habit_periods") == 3)
	s.K("a period's source never changes", strings.Contains(c.tryx("UPDATE habit_periods SET source='cli' WHERE start_day='2026-10-01'"), "never changed"))
	s.K("...a full-row update that keeps it accepted", c.tryx("UPDATE habit_periods SET source='ui', end_day=end_day WHERE start_day='2026-10-01'") == "OK")
	s.K("mood is not a habit: it has no period", c.n("select count(*) from habit_periods h join metrics m on m.id=h.metric_id where m.name='mood'") == 0)
	s.K("the database is clean", c.integrityOK())

	// ---- cookbook/habits run literally
	B := statements(s.d.Block("habits"))
	var heads []string
	for _, b := range B {
		heads = append(heads, clip(code(b), 30))
	}
	s.K("cookbook/habits has a start, a stop, the day's habits and the completion query", len(B) == 4, heads)
	for len(B) < 4 {
		B = append(B, "select 1")
	}
	start, stopSQL, today, completion := B[0], B[1], B[2], B[3]
	c = s.fresh()
	vd, wbc := c.metric("vitamin_d", ""), c.metric("water_before_coffee", "")
	c.must(start, P{"metric": "vitamin_d", "day": "2026-10-01"})
	c.must(start, P{"metric": "water_before_coffee", "day": "2026-10-03"})
	for _, x := range [][2]any{{"2026-10-01", 1}, {"2026-10-02", 0}, {"2026-10-04", 1}, {"2026-09-30", 1}} {
		c.measure(vd, x[0].(string), x[1])
	}
	c.measure(wbc, "2026-10-03", 1)
	c.measure(wbc, "2026-10-03", 1) // two check-ins on one day count once
	day := func(d string) string { return c.tab(today, P{"day": d}) }
	s.K("cookbook/habits the habits of a day: done", day("2026-10-01") == "vitamin_d|done", day("2026-10-01"))
	s.K("...not done (an explicit 0)", day("2026-10-02") == "vitamin_d|not done", day("2026-10-02"))
	d3 := strings.Split(day("2026-10-03"), "; ")
	s.K("...not recorded (no check-in): never assumed", contains(d3, "vitamin_d|not recorded") && contains(d3, "water_before_coffee|done"), d3)
	s.K("...and before a habit started, it is not a habit of that day (even with a reading)", day("2026-09-30") == "", day("2026-09-30"))
	c.must(stopSQL, P{"metric": "vitamin_d", "day": "2026-10-04"})
	s.K("cookbook/habits stop ends the open period on that day, inclusive", day("2026-10-04") == "vitamin_d|done; water_before_coffee|not recorded" &&
		day("2026-10-05") == "water_before_coffee|not recorded", day("2026-10-04"), day("2026-10-05"))
	s.K("...and a restart is a new period", c.tryx(start, P{"metric": "vitamin_d", "day": "2026-10-10"}) == "OK" && strings.HasPrefix(day("2026-10-10"), "vitamin_d|not recorded"))
	comp := c.tab(completion, P{"from_day": "2026-09-28", "to_day": "2026-10-11"})
	s.K("cookbook/habits completion counts only active days: done, not done, not recorded", comp == "vitamin_d|6|2|1|3; water_before_coffee|9|1|0|8", comp)
	c.must("INSERT INTO measurements(metric_id,day,value,source,supersedes_id,created_at) VALUES (?, '2026-10-02', 1, 'ui', ?, "+NOW+")",
		vd, c.n("select id from measurements where day='2026-10-02'"))
	s.K("a corrected check-in counts as corrected (measurement_values)", day("2026-10-02") == "vitamin_d|done", day("2026-10-02"))

	// ---- cookbook/day-view lists the day's habits, and not their check-ins a second time
	DV := s.d.Block("day-view")
	c.dayPage("2026-10-01", "a day")
	c.measure(c.metric("weight", "kg"), "2026-10-01", 71.0)
	pairs := func(d string) ([]string, [][]any) {
		rows := c.rows(DV, P{"day": d})
		var out []string
		for _, r := range rows {
			out = append(out, val(r[0])+"|"+val(r[2]))
		}
		return out, rows
	}
	got, rows := pairs("2026-10-01")
	s.K("cookbook/day-view shows the habit with its state and the other readings", contains(got, "habit|vitamin_d: done") && contains(got, "weight|71.0 kg"), tab(rows))
	again := false
	for _, r := range rows {
		again = again || r[0] == "vitamin_d"
	}
	s.K("...and not the habit's check-in again as a reading", !again, tab(rows))
	got, rows = pairs("2026-09-30")
	listed := false
	for _, r := range rows {
		listed = listed || r[0] == "habit"
	}
	s.K("...outside every period a 0/1 reading is just a reading, and no habit is listed", contains(got, "vitamin_d|1.0 ") && !listed, tab(rows))
}
