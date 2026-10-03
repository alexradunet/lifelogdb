package tests

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// facts: measurements and metrics (schema.sql, D6, D7, D26) — the registry and its categories, append-only rows, supersede chains and
// retraction, finite values and NaN, the read view and its index, and why never OR IGNORE / OR REPLACE.
func facts(s *S) {
	err := func(r string) bool { return strings.HasPrefix(r, "ERR") }

	// ---- metrics
	c := s.fresh()
	w := c.metric("weight", "kg")
	s.K("mood is seeded (D6)", c.n("select count(*) from metrics where name='mood'") == 1)
	c.measure(w, "2026-06-01", 70)
	s.K("metrics.unit cannot change", err(c.tryx("UPDATE metrics SET unit='lb' WHERE name='weight'")))
	s.K("a no-op SET unit=unit with a note edit passes", c.tryx("UPDATE metrics SET unit=unit, note='body weight' WHERE name='weight'") == "OK")
	s.K("a name typo can be fixed", c.tryx("UPDATE metrics SET name='body_weight' WHERE name='weight'") == "OK")
	s.K("a metric name is registered once", err(c.tryx("INSERT INTO metrics(name,unit) VALUES ('mood','x')")))
	for _, nm := range []string{"Blood Pressure", "bp sys", "bp-sys", "", "Weight", "MOOD", "x(y)", "ünï"} {
		s.K(fmt.Sprintf("metric name %q rejected", nm), err(c.tryx("INSERT INTO metrics(name,unit) VALUES (?, 'x')", nm)))
	}
	for _, nm := range []string{"bp_sys", "x1", "_a", "steps_walked"} {
		s.K(fmt.Sprintf("metric name %q accepted", nm), c.tryx("INSERT INTO metrics(name,unit) VALUES (?, 'x')", nm) == "OK")
	}

	// ---- metric categories (D26): a tree whose parents are fixed
	c = s.fresh()
	cat := func(name string) int64 { return c.n("select id from metric_categories where name=?", name) }
	s.K("the top-level categories are seeded", c.tab("select name from metric_categories where parent_id is null order by name") == "biomarkers; body; self_report; substances")
	s.K("mood is seeded in self_report", c.str("select c.name from metrics m join metric_categories c on c.id=m.category_id where m.name='mood'") == "self_report")
	addCat := func(name string, parent any) string {
		return c.tryx("INSERT INTO metric_categories(name,parent_id) VALUES (?,?)", name, parent)
	}
	s.K("a subcategory is registered under an existing parent", addCat("hormones", cat("biomarkers")) == "OK")
	s.K("nesting has no depth limit", addCat("thyroid", cat("hormones")) == "OK" && addCat("thyroid_antibodies", cat("thyroid")) == "OK")
	s.K("a category name is registered once", err(addCat("thyroid", nil)))
	for _, nm := range []string{"Lipids", "blood count", "blood-count", "", "ünï"} {
		s.K(fmt.Sprintf("category name %q rejected", nm), err(addCat(nm, nil)))
	}
	s.K("a parent that does not exist is refused", err(addCat("lipids", 999)))
	s.K("a category cannot be its own parent (explicit id)", err(c.tryx("INSERT INTO metric_categories(id,name,parent_id) VALUES (500,'loop',500)")))
	s.K("a category cannot be its own parent (the id it is about to be given)",
		err(c.tryx("INSERT INTO metric_categories(name,parent_id) VALUES ('loop',(SELECT max(id)+1 FROM metric_categories))")))
	s.K("the parent cannot change, so no cycle can form", err(c.tryx("UPDATE metric_categories SET parent_id=? WHERE name='hormones'", cat("thyroid"))))
	s.K("a top-level category cannot be given a parent", err(c.tryx("UPDATE metric_categories SET parent_id=? WHERE name='body'", cat("biomarkers"))))
	s.K("a full-row update with the same parent and a new note passes", c.tryx("UPDATE metric_categories SET parent_id=parent_id, note='endocrine' WHERE name='hormones'") == "OK")
	tsh := c.metric("tsh", "µUI/mL")
	s.K("a metric is filed in a category", c.tryx("UPDATE metrics SET category_id=? WHERE id=?", cat("thyroid"), tsh) == "OK")
	s.K("a metric cannot be filed in a category that does not exist", err(c.tryx("UPDATE metrics SET category_id=999 WHERE id=?", tsh)))
	s.K("a category is renamed by one UPDATE; its children and metrics follow by id",
		c.tryx("UPDATE metric_categories SET name='endocrine' WHERE name='hormones'") == "OK" &&
			c.str("select p.name from metric_categories c join metric_categories p on p.id=c.parent_id where c.name='thyroid'") == "endocrine" &&
			c.str("select c.name from metrics m join metric_categories c on c.id=m.category_id where m.id=?", tsh) == "thyroid")
	s.K("a category with subcategories is not deleted", err(c.tryx("DELETE FROM metric_categories WHERE name='endocrine'")))
	c.must("DELETE FROM metric_categories WHERE name='thyroid_antibodies'")
	s.K("a category with metrics is not deleted", err(c.tryx("DELETE FROM metric_categories WHERE name='thyroid'")))
	s.K("a metric is re-filed by UPDATE, and the emptied category may be deleted",
		c.tryx("UPDATE metrics SET category_id=? WHERE id=?", cat("biomarkers"), tsh) == "OK" && c.tryx("DELETE FROM metric_categories WHERE name='thyroid'") == "OK")

	// ---- cookbook/metrics-by-category, executed
	c = s.fresh()
	c.must("INSERT INTO metric_categories(name,parent_id) SELECT 'lipids', id FROM metric_categories WHERE name='biomarkers'")
	c.must("INSERT INTO metrics(name,unit,category_id) SELECT 'ldl_cholesterol','mg/dL', id FROM metric_categories WHERE name='lipids'")
	c.must("INSERT INTO metrics(name,unit,category_id) SELECT 'weight','kg', id FROM metric_categories WHERE name='body'")
	c.metric("steps", "n")
	vd := c.metric("vitamin_d", "")
	c.habit(vd, "2026-01-01", nil)
	out, e := c.runBlock(s.d.Block("metrics-by-category"), P{"category": "biomarkers", "subcategory": "vitamins", "metric": "vitamin_d"}, nil)
	if e != nil {
		stop("metrics-by-category: %v", e)
	}
	_, e = c.runBlock(s.d.Block("metrics-by-category"), P{"category": "biomarkers", "subcategory": "vitamins", "metric": "vitamin_d"}, nil)
	s.K("cookbook/metrics-by-category runs twice: the second registers nothing", e == nil && c.n("select count(*) from metric_categories where name='vitamins'") == 1, e)
	sub, all := tab(out[2]), tab(out[3])
	s.K("cookbook/metrics-by-category: the subtree of a category holds its subcategories' metrics", sub == "biomarkers/lipids|ldl_cholesterol|mg/dL; biomarkers/vitamins|vitamin_d|", sub)
	s.K("cookbook/metrics-by-category: habits first, then by category path, the metrics not filed last",
		all == "1|biomarkers/vitamins|vitamin_d|; 0|biomarkers/lipids|ldl_cholesterol|mg/dL; 0|body|weight|kg; 0|self_report|mood|; 0||steps|n", all)

	// ---- append-only, supersede, retract
	c = s.fresh()
	w = c.metric("weight", "kg")
	sup := func(v any, sid any, metric any, id any) string {
		m := M{"supersedes_id": sid}
		if id != nil {
			m["id"] = id
		}
		if metric == nil {
			metric = w
		}
		return c.measure(metric, "2026-06-01", v, m)
	}
	c.measure(w, "2026-06-01", 70)
	s.K("a measurement without created_at is refused", err(c.tryx("INSERT INTO measurements(metric_id,day,value,source) VALUES (1,'2026-06-01',3,'ui')")))
	s.K("UPDATE of a value refused", err(c.tryx("UPDATE measurements SET value=1")))
	s.K("UPDATE of supersedes_id or captured_with_id refused", err(c.tryx("UPDATE measurements SET supersedes_id=NULL")) && err(c.tryx("UPDATE measurements SET captured_with_id=NULL")))
	s.K("DELETE refused", err(c.tryx("DELETE FROM measurements")))
	s.K("a correction supersedes a row", sup(71, 1, nil, nil) == "OK")
	s.K("a second correction of the same row is refused", err(sup(72, 1, nil, nil)))
	s.K("the correction can be corrected (a chain)", sup(73, 2, nil, nil) == "OK")
	s.K("a correction of another metric is refused", err(sup(3, 3, 1, nil)))
	s.K("a dangling supersedes_id is refused", err(sup(3, 999, nil, nil)))
	s.K("a row cannot supersede itself", err(sup(3, 50, nil, 50)))
	s.K("the view shows only the last of the chain", c.tab("select value from measurement_values where metric_id=?", w) == "73")
	s.K("a first reading with NULL value is refused", err(c.measure(w, "2026-06-02", nil)))
	c.measure(1, "2026-06-01", 3)
	tap := c.n("select max(id) from measurements") // a mood tap logged by mistake
	s.K("retract the mis-tap with a NULL correction", c.measure(1, "2026-06-01", nil, M{"supersedes_id": tap}) == "OK")
	s.K("the tap and its retraction are hidden", c.n("select count(*) from measurement_values where metric_id=1") == 0)
	s.K("correcting the retraction brings a value back", c.measure(1, "2026-06-01", 4, M{"supersedes_id": tap + 1}) == "OK" && c.tab("select value from measurement_values where metric_id=1") == "4")
	s.K("nothing was deleted: all six rows remain", c.n("select count(*) from measurements") == 6)
	c.measure(w, "2026-06-05", 70.1)
	c.measure(w, "2026-06-05", 70.4)
	s.K("two independent readings on one day are both returned", c.n("select count(*) from measurement_values where day='2026-06-05'") == 2)

	// ---- the correction story of cookbook/correct-a-measurement, executed
	c = s.fresh()
	w = c.metric("weight", "kg")
	var shown []string
	for _, x := range [][2]any{{71.2, nil}, {70.8, 1}, {nil, 2}, {71.4, 3}} {
		c.measure(w, "2026-09-30", x[0], M{"supersedes_id": x[1]})
		shown = append(shown, "["+c.tab("select value from measurement_values where metric_id=?", w)+"]")
	}
	s.K("corrected, retracted, restored: the view shows 71.2, 70.8, nothing, 71.4", strings.Join(shown, "") == "[71.2][70.8][][71.4]", shown)

	// ---- foreign_keys=OFF: the trigger alone refuses a correction of a row that does not exist
	c = s.freshWith(F{FKOff: true})
	w = c.metric("weight", "kg")
	s.K("with foreign_keys=OFF, a correction of a row that does not exist is refused by the trigger", strings.Contains(c.measure(w, "2026-06-01", 1, M{"supersedes_id": 99999}), "same metric"))

	// ---- the cookbook reads skip superseded and retracted readings
	c = s.fresh()
	w = c.metric("weight", "kg")
	for _, m := range []int64{1, w} {
		c.measure(m, "2026-09-01", 3)
		a := c.n("select max(id) from measurements")
		c.measure(m, "2026-09-01", 4, M{"supersedes_id": a})
		c.measure(m, "2026-09-02", 5)
		b := c.n("select max(id) from measurements")
		c.measure(m, "2026-09-02", nil, M{"supersedes_id": b})
	}
	mot := c.tab(s.d.Block("mood-over-time"))
	s.K("cookbook/mood-over-time skips superseded and retracted readings", mot == "2026-09-01|4", mot)
	ms := c.tab(s.d.Block("metric-series"), P{"day": "2026-10-02"})
	s.K("cookbook/metric-series skips superseded and retracted readings", ms == "2026-09-01|4", ms)

	// ---- finite values, NaN
	c = s.fresh()
	s.K("+Infinity refused", err(c.measure(1, "2026-01-01", math.Inf(1))))
	s.K("-Infinity refused", err(c.measure(1, "2026-01-01", math.Inf(-1))))
	s.K("the largest finite double, -0.0 and ordinary values accepted", c.measure(1, "2026-01-01", math.MaxFloat64) == "OK" &&
		c.measure(1, "2026-01-01", math.Copysign(0, -1)) == "OK" && c.measure(1, "2026-01-01", 70.5) == "OK")
	s.K("NaN binds as NULL: refused as a first reading", err(c.measure(1, "2026-01-01", math.NaN())))
	first := c.n("select max(id) from measurements")
	s.K("...but on a correction it is a retraction the DB cannot tell apart (why the writer never binds NaN)",
		c.measure(1, "2026-01-01", math.NaN(), M{"supersedes_id": first}) == "OK" && c.tab("select value from measurements where supersedes_id=?", first) == "None")

	// ---- import idiom: ON CONFLICT DO NOTHING, never OR IGNORE, never OR REPLACE
	c = s.fresh()
	st := c.metric("steps", "n")
	imp := fmt.Sprintf("INSERT INTO measurements(metric_id,day,value,source,import_key,created_at) VALUES (%d,?,?,?,?,%s) ON CONFLICT(source,import_key,metric_id) WHERE import_key IS NOT NULL DO NOTHING", st, NOW)
	s.K("first import row", c.tryx(imp, "2026-06-09", 8000, "import:apple", "1001") == "OK")
	c.must(imp, "2026-06-09", 8000, "import:apple", "1001")
	s.K("the same source, id and metric again inserts nothing", c.n("select changes()") == 0 && c.n("select count(*) from measurements where import_key='1001'") == 1)
	s.K("the same import_key from another importer is kept", c.tryx(imp, "2026-06-10", 9000, "import:garmin", "1001") == "OK" && c.n("select count(*) from measurements where import_key='1001'") == 2)
	s.K("ON CONFLICT DO NOTHING still raises on a malformed day", err(c.tryx(imp, "2026-6-9", 1, "import:apple", "2002")))
	n := c.n("select count(*) from measurements")
	s.K("OR IGNORE swallows a malformed day silently (the documented hole)", c.tryx(fmt.Sprintf("INSERT OR IGNORE INTO measurements(metric_id,day,value,source,import_key,created_at) VALUES (%d,'2026-6-9',1,'import:apple','2002',%s)", st, NOW)) == "OK" &&
		c.n("select count(*) from measurements") == n)
	s.K("OR IGNORE also swallows a CHECK violation (a NULL first value)", c.tryx(fmt.Sprintf("INSERT OR IGNORE INTO measurements(metric_id,day,value,source,created_at) VALUES (%d,'2026-06-11',NULL,'ui',%s)", st, NOW)) == "OK" &&
		c.n("select count(*) from measurements") == n)
	replace := "INSERT OR REPLACE INTO measurements(metric_id,day,value,source,import_key,created_at) VALUES (2,'2026-01-01',99,'import:s','k'," + NOW + ")"
	c0 := s.freshWith(F{RTOff: true})
	c0.metric("w", "kg")
	c0.measure(2, "2026-01-01", 70, M{"source": "import:s", "import_key": "k"})
	s.K("with recursive_triggers=OFF, OR REPLACE rewrites history (why the pragma is mandatory)", c0.tryx(replace) == "OK" && c0.tab("select value from measurements") == "99")
	c1 := s.fresh()
	c1.metric("w", "kg")
	c1.measure(2, "2026-01-01", 70, M{"source": "import:s", "import_key": "k"})
	s.K("with recursive_triggers=ON the same REPLACE is refused and history is intact", err(c1.tryx(replace)) && c1.tab("select value from measurements") == "70")

	// ---- cookbook/metric-series: the 90 days ending on :day
	c = s.fresh()
	w = c.metric("weight", "kg")
	for _, d := range []string{"2026-07-04", "2026-07-05", "2026-10-02", "2026-10-03"} {
		c.measure(w, d, 70)
	}
	s.K("cookbook/metric-series: the 90 days ending on :day, none after it", eq(c.col(s.d.Block("metric-series"), P{"day": "2026-10-02"}), []string{"2026-07-05", "2026-10-02"}))

	// ---- the read view is index-served
	c = s.fresh()
	c.metric("w", "kg")
	c.must("BEGIN")
	ins, e := c.Prepare("INSERT INTO measurements(metric_id,day,value,source,created_at) VALUES (2,?,?,'ui'," + NOW + ")")
	if e != nil {
		stop("prepare: %v", e)
	}
	d0 := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 20000 {
		if _, e := ins.Exec(d0.AddDate(0, 0, i%9000).Format(time.DateOnly), float64(i)*0.001); e != nil {
			stop("insert: %v", e)
		}
	}
	ins.Close()
	c.must("COMMIT")
	plan := c.plan("SELECT count(*) FROM measurement_values")
	s.K("measurement_values uses measurements_one_correction for its NOT EXISTS", strings.Contains(plan, "measurements_one_correction"), plan)
	s.K("...and counts 20 000 rows", c.n("select count(*) from measurement_values") == 20000)
}
