package tests

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

// facts: measurements and metrics (schema.sql, D6, D7, D26) — the registry and its categories, append-only rows, supersede chains and
// retraction, finite values and NaN, the read view and its index, and why never OR IGNORE / OR REPLACE.
func facts(s *S) {
	err := func(r string) bool { return strings.HasPrefix(r, "ERR") }

	// ---- metrics: a metric is a page (D27)
	c := s.fresh()
	w := c.metric("Weight", "kg")
	s.K("Mood is seeded (D6): a metric, its page and its entity, one id", c.tab(`select e.entity_type, n.title, m.entity_type, m.unit
 from metrics m join entities e on e.id=m.id join entity_names n on n.entity_id=e.id and n.name_key=e.preferred_name_key where n.name_key='mood'`) == "metric|Mood|metric|")
	c.measure(w, "2026-06-01", 70)
	s.K("metrics.unit cannot change", err(c.tryx("UPDATE metrics SET unit='lb' WHERE id=?", w)))
	s.K("a no-op SET unit=unit passes", c.tryx("UPDATE metrics SET unit=unit WHERE id=?", w) == "OK")
	s.K("a metric's note is its page's body", c.tryx("UPDATE entities SET body='body weight, morning' WHERE id=?", w) == "OK")
	renamed, renameErr := c.store().Rename(context.Background(), "ui", w, "Body weight")
	s.K("a metric rename preserves its id, readings and owned prior handle", renameErr == nil && renamed == w && c.n("SELECT count(*) FROM entity_names WHERE entity_id=?", w) == 2 && c.n("SELECT count(*) FROM measurements WHERE metric_id=?", w) == 1, renameErr)
	_, duplicateErr := c.tryIdentity("metric", "WEIGHT", nil)
	s.K("a metric name is reserved once across owned aliases, whatever its case", duplicateErr != nil && strings.Contains(duplicateErr.Error(), "UNIQUE"), duplicateErr)
	pg := c.page("Steps")
	s.K("a metrics row needs a page of type metric", err(c.tryx("INSERT INTO metrics(id,unit) VALUES (?,'n')", pg)))
	s.K("...and cannot claim another type to hang off a person's page", err(c.tryx("INSERT INTO metrics(id,entity_type,unit) VALUES (?,'person','n')", c.named("person", "Ann"))))
	s.K("a page becomes a metric by the UPDATE that promotes any page (D20), then its metrics row",
		c.tryx("UPDATE entities SET entity_type='metric' WHERE id=?", pg) == "OK" && c.tryx("INSERT INTO metrics(id,unit) VALUES (?,'n')", pg) == "OK")
	s.K("a metric is never deleted: it is tombstoned", err(c.tryx("DELETE FROM metrics WHERE id=?", pg)) &&
		c.tryx("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", pg) == "OK")
	s.K("[[Weight]] in the journal links to the metric", c.link(c.dayPage("2026-06-01", "[[Weight]]"), w, "wikilink") == "OK")

	// ---- metric categories (D26): a category is a page, filing is a part-of link
	c = s.fresh()
	tsh := c.metric("TSH", "µUI/mL")
	thy, bio := c.page("Thyroid"), c.page("Biomarkers")
	s.K("a metric is filed in a category by a part-of link to its page", c.link(tsh, thy, "part-of") == "OK")
	s.K("a category is nested by the same link between pages", c.link(thy, bio, "part-of") == "OK")
	s.K("a metric may be filed in two categories", c.link(tsh, bio, "part-of") == "OK")
	sam := c.named("person", "Sam", M{"name": "Sam"})
	s.K("anything may be filed in a category: a person", c.link(sam, c.page("Family"), "part-of") == "OK")
	s.K("...but a category is a plain page: never a person or a metric", err(c.link(thy, sam, "part-of")) && err(c.link(thy, tsh, "part-of")))

	// ---- cookbook/metrics-by-category, executed
	c = s.fresh()
	bio, lip, vit := c.page("Biomarkers"), c.page("Lipids"), c.page("Vitamins")
	c.link(lip, bio, "part-of")
	c.link(c.metric("LDL cholesterol", "mg/dL"), lip, "part-of")
	c.link(c.metric("Weight", "kg"), c.page("Body"), "part-of")
	c.metric("Steps", "n")
	vd := c.metric("Vitamin D", "")
	c.habit(vd, "2026-01-01", nil)
	p := P{"metric_id": vd, "page_id": vit, "parent_id": bio, "source": "ui"}
	out, e := c.runBlock(s.d.Block("metrics-by-category"), p, nil)
	if e != nil {
		stop("metrics-by-category: %v", e)
	}
	_, e = c.runBlock(s.d.Block("metrics-by-category"), p, nil)
	s.K("cookbook/metrics-by-category runs twice: the second files and nests nothing",
		e == nil && c.n("select count(*) from links where kind='part-of' and from_id in (?, ?)", vit, vd) == 2, e)
	sub, all := tab(out[2]), tab(out[3])
	s.K("cookbook/metrics-by-category: a category holds the metrics of every category under it", sub == "Lipids|LDL cholesterol|mg/dL; Vitamins|Vitamin D|", sub)
	s.K("cookbook/metrics-by-category: habits first, then by category, the metrics filed nowhere last",
		all == "1|Vitamins|Vitamin D|; 0|Body|Weight|kg; 0|Lipids|LDL cholesterol|mg/dL; 0||Mood|; 0||Steps|n", all)
	walk := statements(s.d.Block("metrics-by-category"))[2]
	c.link(bio, lip, "part-of") // a cycle: Biomarkers part-of Lipids part-of Biomarkers
	cyc, e := c.query(walk, P{"parent_id": bio})
	s.K("cookbook/metrics-by-category: a cycle of part-of links ends the walk", e == nil && tab(cyc) == sub, e, tab(cyc))
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", vit)
	s.K("cookbook/metrics-by-category: a deleted category's metrics read as filed nowhere",
		strings.Contains(tab(c.rows(statements(s.d.Block("metrics-by-category"))[3])), "1||Vitamin D|"))

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
	plainImport := fmt.Sprintf("INSERT INTO measurements(metric_id,day,value,source,import_key,created_at) VALUES (%d,'2026-06-09',8000,'import:probe','unique-probe',%s)", st, NOW)
	s.K("a duplicate measurement import is refused by its unique index", c.tryx(plainImport) == "OK" && strings.Contains(c.tryx(plainImport), "UNIQUE"))
	imp := fmt.Sprintf("INSERT INTO measurements(metric_id,day,value,source,import_key,created_at) VALUES (%d,?,?,?,?,%s) ON CONFLICT(source,import_key,metric_id) WHERE import_key IS NOT NULL DO NOTHING", st, NOW)
	s.K("first import row", c.tryx(imp, "2026-06-09", 8000, "import:apple", "1001") == "OK")
	resent := c.tryx(imp, "2026-06-09", 8000, "import:apple", "1001")
	s.K("the same source, id and metric again inserts nothing", resent == "OK" && c.n("select changes()") == 0 && c.n("select count(*) from measurements where import_key='1001'") == 1)
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
