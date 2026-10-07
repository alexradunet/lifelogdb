package tests

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// integrity: the four integrity checks of contract/integrity-checks, taken literally from the page and run on the
// LIVE file through a read-only connection: a clean file, a zeroed page, a truncated file, a flipped index entry, a
// flipped value (undetected, as the text says), a reading written with foreign_keys=OFF, an entities row with no
// domain row, a person, a metric or a file with a page but no row of its own, a drifted FTS index.
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
	s.K("contract/integrity-checks has four groups: structure, foreign keys, domain/typed-edge semantics, FTS5",
		len(sts) == 7 && strings.HasPrefix(strings.ToLower(sts[0]), "pragma integrity_check") && strings.HasPrefix(strings.ToLower(sts[1]), "pragma foreign_key_check") &&
			strings.HasPrefix(strings.ToUpper(sts[2]), "SELECT ID FROM ENTITIES") && strings.HasPrefix(sts[3], "SELECT l.id FROM links") && strings.HasPrefix(sts[4], "SELECT s.id FROM sessions") && strings.HasPrefix(sts[5], "SELECT m.id FROM measurements") && strings.Contains(sts[6], "'integrity-check', 1"), sts)
	if len(sts) != 7 {
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
	c.thing("file")
	c.must("COMMIT")
	c.must("PRAGMA wal_checkpoint(TRUNCATE)")
	if err := c.Close(); err != nil {
		stop("close base writer: %v", err)
	}
	sizeConn := s.readOnly(base)
	ps := sizeConn.n("PRAGMA page_size")
	if err := sizeConn.Close(); err != nil {
		stop("close size reader: %v", err)
	}
	if ps < 512 {
		stop("invalid page size %d", ps)
	}

	// sq is what a statement prints on a read-only connection of its own: its rows, or its error.
	sq := func(p, q string) string {
		r := s.readOnly(p)
		rows, err := r.query(q)
		closeErr := r.Close()
		if closeErr != nil {
			stop("close integrity reader: %v", closeErr)
		}
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
		b, err := os.ReadFile(p)
		if err != nil {
			stop("read corruption target: %v", err)
		}
		if at < 0 || at >= len(b) {
			stop("corruption offset %d outside %d bytes", at, len(b))
		}
		b[at] ^= 1
		if err := os.WriteFile(p, b, 0o644); err != nil {
			stop("write corruption target: %v", err)
		}
	}

	r := checks(base)
	s.K("a clean file with a row of every type: integrity ok, foreign_key_check empty, orphan query empty", r == res{"ok", "", ""}, r)

	p := cp("zero")

	root, err := strconv.ParseInt(sq(p, "SELECT rootpage FROM sqlite_schema WHERE name='entities' AND type='table'"), 10, 64)
	if err != nil || root < 1 {
		stop("entity table root: %d, %v", root, err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		stop("read zero target: %v", err)
	}
	start, end := (root-1)*ps, root*ps
	if start < 0 || end > int64(len(b)) {
		stop("table page %d outside file", root)
	}
	clear(b[start:end])
	if err := os.WriteFile(p, b, 0o644); err != nil {
		stop("write zero page: %v", err)
	}
	s.K("a zeroed table page: integrity_check is not ok", checks(p).ic != "ok")

	p = cp("trunc")
	st, err := os.Stat(p)
	if err != nil {
		stop("stat truncation target: %v", err)
	}
	if st.Size() <= 3*ps {
		stop("truncation fixture too short")
	}
	if err := os.Truncate(p, st.Size()-3*ps); err != nil {
		stop("truncate fixture: %v", err)
	}
	s.K("a file truncated by three pages: integrity_check is not ok", checks(p).ic != "ok")

	p = cp("idx")
	reader := s.readOnly(p)
	indexName := reader.str(`SELECT il.name FROM pragma_index_list('entity_names') il WHERE il."unique"=1 AND (SELECT count(*) FROM pragma_index_info(il.name))=1 AND (SELECT name FROM pragma_index_info(il.name))='name_key'`)
	indexRoot := reader.n("SELECT rootpage FROM sqlite_schema WHERE name=? AND type='index'", indexName)
	if err := reader.Close(); err != nil {
		stop("close index reader: %v", err)
	}
	if indexRoot < 1 {
		stop("no live unique registry-key index")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		stop("read index target: %v", err)
	}
	// Follow the actual index b-tree, excluding tables and free/stale pages.
	visited := map[int64]bool{}
	var pages []int64
	var visit func(int64)
	visit = func(page int64) {
		if visited[page] {
			stop("cycle in index fixture at page %d", page)
		}
		visited[page] = true
		lo, hi := (page-1)*ps, page*ps
		if page < 1 || hi > int64(len(data)) {
			stop("index child page outside fixture: %d", page)
		}
		block := data[lo:hi]
		header := 0
		if page == 1 {
			header = 100
		}
		kind := block[header]
		if kind != 2 && kind != 10 {
			stop("non-index page %d type %d", page, kind)
		}
		pages = append(pages, page)
		if kind == 10 {
			return
		}
		count := int(binary.BigEndian.Uint16(block[header+3 : header+5]))
		if header+12+2*count > len(block) {
			stop("bad index pointer array")
		}
		visit(int64(binary.BigEndian.Uint32(block[header+8 : header+12])))
		for i := 0; i < count; i++ {
			pos := int(binary.BigEndian.Uint16(block[header+12+2*i : header+14+2*i]))
			if pos < 0 || pos+4 > len(block) {
				stop("bad index cell offset")
			}
			visit(int64(binary.BigEndian.Uint32(block[pos : pos+4])))
		}
	}
	visit(indexRoot)
	j := -1
	for _, page := range pages {
		lo := (page - 1) * ps
		block := data[lo : lo+ps]
		for _, m := range regexp.MustCompile(`title 012[0-9][0-9]`).FindAllIndex(block, -1) {
			candidate := cp("idx-candidate")
			flip(candidate, int(lo)+m[0]+6)
			if checks(candidate).ic != "ok" {
				p = candidate
				j = int(lo) + m[0]
				break
			}
		}
		if j >= 0 {
			break
		}
	}
	s.K("a flipped byte in a name_key inside the live unique registry index: integrity_check is not ok", j >= 0 && checks(p).ic != "ok", indexName, indexRoot, j)
	s.K("index-only damage preserves authoritative table names", j >= 0 && sq(p, "SELECT count(*) FROM entity_names NOT INDEXED WHERE name_key LIKE 'title 012%'") == sq(base, "SELECT count(*) FROM entity_names NOT INDEXED WHERE name_key LIKE 'title 012%'"))

	// without secure_delete a b-tree split leaves stale copies of cells in free space: flip copies until one is a live row
	j, n := -1, "0"
	baseData, err := os.ReadFile(base)
	if err != nil {
		stop("read body target: %v", err)
	}
	for _, m := range regexp.MustCompile(`lorem ipsum lorem`).FindAllIndex(baseData, -1) {
		p = cp("val")
		flip(p, m[0])
		n = sq(p, "SELECT count(*) FROM entities WHERE body LIKE '%korem ipsum lorem%' OR body LIKE '%morem ipsum lorem%'")
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
	if err := w.Close(); err != nil {
		stop("close FK damage writer: %v", err)
	}
	r = checks(p)
	s.K("a reading of no metric, written with foreign_keys=OFF (STRICT does not enforce FKs): only foreign_key_check sees it", ins == "OK" && r.ic == "ok" && r.fk != "" && r.orph == "", ins, r)

	p = cp("named-plain")
	w = s.connect(p)
	w.must("PRAGMA foreign_keys=ON")
	plain := w.anyIdentity("page")
	if err := w.Close(); err != nil {
		stop("close plain: %v", err)
	}
	r = checks(p)
	s.K("a fully named plain entity has no missing domain row", r == res{"ok", "", ""}, r, plain)

	p = cp("missing-name")
	w = s.connect(p)
	w.must("PRAGMA foreign_keys=OFF")
	oid := w.rows("INSERT INTO entities(entity_type,preferred_name_key,created_at,updated_at,source) VALUES ('page','missing-owner'," + NOW + "," + NOW + ",'ui') RETURNING id")[0][0].(int64)
	if err := w.Close(); err != nil {
		stop("close name damage: %v", err)
	}
	r = checks(p)
	s.K("missing preferred ownership is detected by FK and semantic checks, not structure", r.ic == "ok" && r.fk != "" && r.orph == ids(oid), r)
	for _, typ := range []string{"person", "metric", "file", "period"} {
		p = cp("half-" + typ)
		w = s.connect(p)
		w.must("PRAGMA foreign_keys=ON")
		id := w.identity(typ, "ui", "Missing "+typ+" extension", nil, "")
		if err := w.Close(); err != nil {
			stop("close extension damage: %v", err)
		}
		r = checks(p)
		s.K("a "+typ+" with an owned name but no extension: only the orphan query sees it", r == res{"ok", "", ids(id)}, r)
	}

	// Deliberate typed-edge damage: structural/FK/domain checks remain clean.
	typed := s.fresh()
	target := typed.page("Category")
	source := typed.metric("Weight", "kg")
	if result := typed.link(source, target, "part-of"); result != "OK" {
		stop("typed edge fixture: %s", result)
	}
	typed.must("DROP TRIGGER entities_endpoint_types")
	typed.must("UPDATE entities SET entity_type='place' WHERE id=?", target)
	s.K("typed-edge semantic query detects deliberate endpoint damage", typed.integrityOK() && typed.tab(orphan) == "" && typed.tab(sts[3]) != "")

	// ---- the fourth check: the FTS index against pages (it writes, so a writer connection)
	c = s.fresh()
	c.dayPage("2026-09-30", "alpha beta")
	c.page("Gamma")
	clean := true
	for _, st := range sts {
		clean = clean && c.tryx(st) == "OK"
	}
	s.K("on a clean file every statement of contract/integrity-checks runs without error", clean)
	c.must("INSERT INTO entities_fts(rowid, preferred, all_names, body) VALUES (999, 'ghost', 'ghost', 'drifted')")
	s.K("a drifted FTS index: PRAGMA integrity_check still says ok", c.tab("PRAGMA integrity_check") == "ok")
	s.K("...and the FTS5 integrity-check of contract/integrity-checks fails", strings.HasPrefix(c.tryx(sts[6]), "ERR"))
	c.must("INSERT INTO entities_fts(entities_fts) VALUES('rebuild')")
	s.K("...until 'rebuild' repairs it", c.tryx(sts[6]) == "OK")

	gone := c.page("Gone")
	c.must("PRAGMA foreign_keys=OFF")
	c.must("DROP TRIGGER entities_no_delete")
	c.must("DELETE FROM entities WHERE id=?", gone)
	s.K("an identity deleted past its guard leaves stale FTS content", strings.HasPrefix(c.tryx(sts[6]), "ERR"))
}
