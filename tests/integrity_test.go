package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// integrity: the four integrity checks of contract/integrity-checks, taken literally from the page and run on the
// LIVE file through a read-only connection: a clean file, a zeroed page, a truncated file, a flipped index entry, a
// flipped value (undetected, as the text says), a reading written with foreign_keys=OFF, an entities row with no
// domain row, a person with a page but no people row, a drifted FTS index.
func integrity(s *S) {
	blk := sqlBlocks(s.d.Page("contract/integrity-checks.md"))
	var sts []string
	if len(blk) > 0 {
		for _, l := range strings.Split(blk[0], "\n") {
			if l = strings.TrimSpace(regexp.MustCompile(`\s*--.*$`).ReplaceAllString(l, "")); l != "" {
				sts = append(sts, l)
			}
		}
	}
	s.K("contract/integrity-checks has exactly the four checks: integrity_check, foreign_key_check, the orphan query, the FTS5 integrity-check",
		len(sts) == 4 && strings.HasPrefix(strings.ToLower(sts[0]), "pragma integrity_check") && strings.HasPrefix(strings.ToLower(sts[1]), "pragma foreign_key_check") &&
			strings.HasPrefix(strings.ToUpper(sts[2]), "SELECT ID FROM ENTITIES") && strings.Contains(sts[3], "'integrity-check', 1"), sts)
	if len(sts) != 4 {
		return
	}
	orphan := strings.TrimSuffix(sts[2], ";")

	base := filepath.Join(s.dir, "base.db")
	c := s.freshWith(F{Path: base})
	c.must("BEGIN IMMEDIATE")
	for i := range 3000 { // enough pages that the table and pages_title span many pages
		if i%2 == 1 {
			c.pageW(fmt.Sprintf("Title %05d", i), "2026-09-30", fmt.Sprintf("BODYMARK%05d ", i)+strings.Repeat("lorem ipsum ", 20))
		} else {
			c.pageW(fmt.Sprintf("Note %05d", i), nil, fmt.Sprintf("BODYMARK%05d ", i)+strings.Repeat("dolor sit amet ", 20))
		}
	}
	c.thing("person") // one row of every domain type, so a query that forgets one table reports a false orphan
	c.thing("place")
	c.thing("metric")
	c.must("COMMIT")
	c.must("PRAGMA wal_checkpoint(TRUNCATE)")
	c.Close()
	ps := s.connect(base).n("PRAGMA page_size")

	// sq is what a statement prints on a read-only connection of its own: its rows, or its error.
	sq := func(p, q string) string {
		r := s.readOnly(p)
		defer r.Close()
		rows, err := r.query(q)
		if err != nil {
			return "Error: " + err.Error()
		}
		var out []string
		for _, row := range rows {
			out = append(out, strings.ReplaceAll(tab([][]any{row}), "; ", "\n"))
		}
		return strings.Join(out, "\n")
	}
	type res struct{ ic, fk, orph string }
	checks := func(p string) res {
		ic, _, _ := strings.Cut(sq(p, sts[0]), "\n")
		return res{ic, sq(p, sts[1]), sq(p, orphan)}
	}
	cp := func(name string) string {
		p := filepath.Join(s.dir, name+".db")
		b, err := os.ReadFile(base)
		if err != nil {
			stop("read base: %v", err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			stop("copy: %v", err)
		}
		return p
	}
	flip := func(p string, at int) {
		b, _ := os.ReadFile(p)
		b[at] ^= 1
		os.WriteFile(p, b, 0o644)
	}

	r := checks(base)
	s.K("a clean file with a row of every type: integrity ok, foreign_key_check empty, orphan query empty", r == res{"ok", "", ""}, r)

	p := cp("zero")
	var root int64
	fmt.Sscan(sq(p, "SELECT rootpage FROM sqlite_master WHERE name='pages'"), &root)
	b, _ := os.ReadFile(p)
	copy(b[(root-1)*ps:root*ps], make([]byte, ps))
	os.WriteFile(p, b, 0o644)
	s.K("a zeroed table page: integrity_check is not ok", root > 0 && checks(p).ic != "ok")

	p = cp("trunc")
	st, _ := os.Stat(p)
	os.Truncate(p, st.Size()-3*ps)
	s.K("a file truncated by three pages: integrity_check is not ok", checks(p).ic != "ok")

	p = cp("idx")
	data, _ := os.ReadFile(p)
	j := -1
	tableCell := regexp.MustCompile(`^Title \d{5}$`)
	for _, m := range regexp.MustCompile(`title 012\d\d`).FindAllIndex(data, -1) {
		if m[0] >= 11 && !tableCell.Match(data[m[0]-11:m[0]]) { // not the title_key beside its title in the table row
			j = m[0]
			break
		}
	}
	if j >= 0 {
		flip(p, j+6)
	}
	s.K("a flipped byte in a title_key inside the pages_title index: integrity_check is not ok", j >= 0 && checks(p).ic != "ok")

	// without secure_delete a b-tree split leaves stale copies of cells in free space: flip copies until one is a live row
	j, n := -1, "0"
	baseData, _ := os.ReadFile(base)
	for _, m := range regexp.MustCompile(`lorem ipsum lorem`).FindAllIndex(baseData, -1) {
		p = cp("val")
		flip(p, m[0])
		n = sq(p, "SELECT count(*) FROM pages WHERE body LIKE '%korem ipsum lorem%' OR body LIKE '%morem ipsum lorem%'")
		if n == "1" {
			j = m[0]
			break
		}
	}
	s.K("a flipped byte inside a body: the text changed and integrity_check is STILL ok", j >= 0 && n == "1" && checks(p).ic == "ok", j, n)

	p = cp("fk")
	w := s.connect(p)
	w.must("PRAGMA foreign_keys=OFF")
	ins := w.tryx("INSERT INTO measurements(metric_id,day,value,created_at,source) VALUES (99999,'2026-09-30',100,'2026-09-30T10:00:00.000Z','ui')")
	w.Close()
	r = checks(p)
	s.K("a reading of no metric, written with foreign_keys=OFF (STRICT does not enforce FKs): only foreign_key_check sees it", ins == "OK" && r.ic == "ok" && r.fk != "" && r.orph == "", ins, r)

	p = cp("orph")
	w = s.connect(p)
	oid := w.ent("page")
	w.Close()
	r = checks(p)
	s.K("an entities row with no domain row: only the orphan query sees it", r == res{"ok", "", ids(oid)}, r, oid)

	p = cp("half")
	w = s.connect(p)
	w.must("PRAGMA foreign_keys=ON")
	hid := w.ent("person")
	w.must("INSERT INTO pages(id,entity_type,title,title_key) VALUES (?, 'person', 'Half person', 'half person')", hid)
	w.Close()
	r = checks(p)
	s.K("a person with a page but no people row: only the orphan query sees it", r == res{"ok", "", ids(hid)}, r, hid)

	p = cp("half-metric")
	w = s.connect(p)
	w.must("PRAGMA foreign_keys=ON")
	mid := w.ent("metric")
	w.must("INSERT INTO pages(id,entity_type,title,title_key) VALUES (?, 'metric', 'Half metric', 'half metric')", mid)
	w.Close()
	r = checks(p)
	s.K("a metric with a page but no metrics row: only the orphan query sees it", r == res{"ok", "", ids(mid)}, r, mid)

	// ---- the fourth check: the FTS index against pages (it writes, so a writer connection)
	c = s.fresh()
	c.dayPage("2026-09-30", "alpha beta")
	c.page("Gamma")
	clean := true
	for _, st := range sts {
		clean = clean && c.tryx(st) == "OK"
	}
	s.K("on a clean file every statement of contract/integrity-checks runs without error", clean)
	c.must("INSERT INTO pages_fts(rowid, title, body) VALUES (999, 'ghost', 'drifted')")
	s.K("a drifted FTS index: PRAGMA integrity_check still says ok", c.tab("PRAGMA integrity_check") == "ok")
	s.K("...and the FTS5 integrity-check of contract/integrity-checks fails", strings.HasPrefix(c.tryx(sts[3]), "ERR"))
	c.must("INSERT INTO pages_fts(pages_fts) VALUES('rebuild')")
	s.K("...until 'rebuild' repairs it", c.tryx(sts[3]) == "OK")
	gone := c.page("Gone")
	c.must("DROP TRIGGER pages_no_delete")
	c.must("DELETE FROM pages WHERE id=?", gone)
	s.K("a page deleted past pages_no_delete (no trigger keeps the index in step): the FTS5 integrity-check fails", strings.HasPrefix(c.tryx(sts[3]), "ERR"))
}
