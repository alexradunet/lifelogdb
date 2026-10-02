package tests

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// snapshots: cookbook/take-a-snapshot run from the page's own SQL on a live file with synthetic rows (D25): the
// VACUUM INTO block through a read-only connection while the writer holds an open transaction, the rules of its
// target, the restore check (the four checks of contract/integrity-checks, which leave the snapshot as it was), the
// restore, and the old -wal that must not stay beside it.
func snapshots(s *S) {
	page := s.d.Page("cookbook/take-a-snapshot.md")
	var take, restore string
	for _, b := range sqlBlocks(page) {
		if strings.Contains(b, "VACUUM INTO :snapshot") {
			take = b
		}
		if strings.Contains(b, "PRAGMA journal_mode") {
			restore = b
		}
	}
	s.K("cookbook/take-a-snapshot has the VACUUM INTO :snapshot block", take != "")
	s.K("the snapshot block runs on a connection that opened life.db read-only (mode=ro)", strings.Contains(take, "opened read-only (mode=ro)"))
	s.K("cookbook/take-a-snapshot has the restore block", restore != "")
	s.K("the restore check opens the snapshot with the writer's connection settings", strings.Contains(page, "open it with the writer's connection settings"))
	s.K("the shell form of the snapshot opens life.db read-only, with trusted_schema=OFF", strings.Contains(page, `sqlite3 -readonly -cmd "PRAGMA trusted_schema=OFF" life.db "VACUUM INTO`))
	var checks []string
	if ib := sqlBlocks(s.d.Page("contract/integrity-checks.md")); len(ib) > 0 {
		for _, l := range strings.Split(ib[0], "\n") {
			if l = strings.TrimSpace(regexp.MustCompile(`\s*--.*$`).ReplaceAllString(l, "")); l != "" {
				checks = append(checks, strings.TrimSuffix(l, ";"))
			}
		}
	}
	if len(checks) != 4 {
		stop("contract/integrity-checks has not the four checks: %v", checks)
	}
	// fourChecks is the restore check's four results on one connection: integrity_check, foreign_key_check, the
	// orphan query, the FTS5 check (each its rows, or its error).
	fourChecks := func(c *C) []string {
		var out []string
		for _, q := range checks {
			rows, err := c.query(q)
			if err != nil {
				out = append(out, "ERR "+err.Error())
			} else {
				out = append(out, tab(rows))
			}
		}
		return out
	}
	clean := func(r []string) bool { return len(r) == 4 && r[0] == "ok" && r[1] == "" && r[2] == "" && r[3] == "" }
	writer := func(p string) *C { // the writer's settings (contract/connections)
		return s.connect(p, "_defensive=1", "_pragma=foreign_keys(1)", "_pragma=recursive_triggers(1)", "_pragma=synchronous(2)", "_pragma=trusted_schema(0)")
	}
	// dump is every row of every table, the FTS shadow tables included, in a fixed order.
	dump := func(c *C) string {
		var b strings.Builder
		for _, t := range c.col("SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name") {
			fmt.Fprintf(&b, "%s: %s\n", t, c.tab(`SELECT * FROM "`+t+`" ORDER BY 1`))
		}
		return b.String()
	}
	read := func(p string) []byte {
		b, err := os.ReadFile(p)
		if err != nil {
			stop("read %s: %v", p, err)
		}
		return b
	}
	write := func(p string, b []byte) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			stop("write %s: %v", p, err)
		}
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }

	// the live file: synthetic rows, some checkpointed into life.db and some only in life.db-wal
	dir := filepath.Join(s.dir, "snapshots")
	os.MkdirAll(dir, 0o755)
	live := filepath.Join(dir, "life.db")
	w := s.freshWith(F{Path: live})
	w.must("PRAGMA synchronous=FULL")
	w.must("PRAGMA wal_autocheckpoint=0")
	w.must("BEGIN IMMEDIATE")
	for i := range 300 { // enough rows that PRAGMA optimize has statistics to write
		w.pageW(fmt.Sprintf("Note %03d", i), nil, fmt.Sprintf("Walked with [[Sam]] past the lake, note %d.", i))
	}
	dp := w.dayPage("2026-09-28", "Lunch with [[Sam]] at [[Lakeside]].")
	sam := w.named("person", "Sam", M{"name": "Sam"})
	lake := w.named("place", "Lakeside")
	w.link(dp, sam, "wikilink")
	w.link(dp, lake, "wikilink")
	w.link(dp, lake, "at")
	wt := w.metric("weight", "kg")
	w.measure(wt, "2026-09-28", 71.2)
	w.must("COMMIT")
	w.must("PRAGMA wal_checkpoint(TRUNCATE)")
	w.dayPage("2026-10-01", "Written after the checkpoint: only in life.db-wal.")
	w.measure(wt, "2026-10-01", 70.9)
	inWal := "SELECT count(*) FROM pages WHERE title = '2026-10-01'"

	// a copy of life.db alone, while rows wait in its -wal
	plain := filepath.Join(dir, "plain", "life.db")
	write(plain, read(live))
	pc := s.readOnly(plain)
	pn, perr := pc.query(inWal)
	s.K("D25: a copy of life.db alone does not hold the rows still in life.db-wal", exists(live+"-wal") && (perr != nil || tab(pn) == "0"), tab(pn), perr)
	pc.Close()

	// 1. take it: through a read-only connection, while the writer holds an open transaction
	w.must("BEGIN IMMEDIATE")
	w.page("Not committed")
	ro := s.readOnly(live)
	ro.must("PRAGMA trusted_schema = OFF") // the shell form of the recipe: a reader that does not trust the schema
	want := dump(ro)
	snap := filepath.Join(dir, "life-2026-10-02.db")
	_, err := ro.runBlock(take, P{"snapshot": filepath.ToSlash(snap)}, nil)
	s.K("the snapshot block runs on a read-only connection while the writer holds BEGIN IMMEDIATE", err == nil, err)
	s.K("the writer commits after it", w.tryx("COMMIT") == "OK")
	sr := s.readOnly(snap)
	got := dump(sr)
	s.K("the snapshot holds the same rows as the live file, every table (the FTS shadow tables too)", got == want && strings.Contains(got, "Note 299"), clip(got, 200))
	s.K("the snapshot holds the rows still only in life.db-wal", sr.n(inWal) == 1)
	s.K("the snapshot holds nothing of the open transaction", sr.n("SELECT count(*) FROM pages WHERE title = 'Not committed'") == 0)
	s.K("the snapshot is in rollback-journal mode: journal_mode reads delete, and no -wal beside it", sr.str("PRAGMA journal_mode") == "delete" && !exists(snap+"-wal"), sr.str("PRAGMA journal_mode"))
	again := ro.tryx("VACUUM INTO ?", filepath.ToSlash(snap))
	s.K("onto a file that has content: refused (output file already exists)", strings.Contains(again, "output file already exists"), again)
	empty := filepath.Join(dir, "empty.db")
	write(empty, nil)
	ontoEmpty := ro.tryx("VACUUM INTO ?", filepath.ToSlash(empty))
	s.K("onto an existing empty file: SQLite writes it, so the program that names a snapshot checks that the name is free",
		ontoEmpty == "OK" && s.readOnly(empty).n("SELECT count(*) FROM pages") > 300, ontoEmpty)
	qo := s.connect(live, "mode=ro", "_pragma=query_only(1)")
	_, qerr := qo.runBlock(take, P{"snapshot": filepath.ToSlash(filepath.Join(dir, "query-only.db"))}, nil)
	s.K("a connection with PRAGMA query_only = ON cannot take it (attempt to write a readonly database)", qerr != nil && strings.Contains(qerr.Error(), "readonly"), qerr)

	// 2. the restore check
	r := fourChecks(sr)
	s.K("on a mode=ro connection the first three checks pass and the FTS5 check is refused (attempt to write a readonly database)",
		r[0] == "ok" && r[1] == "" && r[2] == "" && strings.Contains(r[3], "readonly"), r)
	sr.Close()
	before := read(snap)
	sc := writer(snap)
	r = fourChecks(sc)
	sc.Close()
	s.K("with the writer's settings the four checks pass on the snapshot", clean(r), r)
	s.K("the restore check leaves the snapshot as it was, byte for byte", bytes.Equal(before, read(snap)))
	opt := filepath.Join(dir, "optimized.db")
	write(opt, before)
	oc := writer(opt)
	fourChecks(oc)
	oc.must("PRAGMA optimize")
	oc.Close()
	s.K("PRAGMA optimize at close can write to it: the restore check closes without it", !bytes.Equal(before, read(opt)))

	// 3. restore: life.db, -wal and -shm moved aside, the snapshot copied in, WAL mode set once
	for i := range 50 { // the live file goes on: more rows only in its -wal
		w.pageW(fmt.Sprintf("Later %03d", i), nil, strings.Repeat("written after the snapshot ", 20))
	}
	back := filepath.Join(dir, "restored", "life.db")
	write(back, before)
	rc := writer(back)
	_, rerr := rc.runBlock(restore, P{}, nil)
	s.K("the restore block runs on the restored life.db", rerr == nil, rerr)
	s.K("the restored life.db is in WAL mode again", rc.str("PRAGMA journal_mode") == "wal", rc.str("PRAGMA journal_mode"))
	s.K("the restored life.db passes the four checks and holds the snapshot's rows", clean(fourChecks(rc)) && dump(rc) == want)
	rc.Close()
	crash := filepath.Join(dir, "beside-old-wal", "life.db")
	write(crash+"-wal", read(live+"-wal"))
	write(crash, before)
	bc := s.readOnly(crash)
	ic, ierr := bc.query("PRAGMA integrity_check")
	s.K("the snapshot copied in beside the old life.db-wal is not the snapshot: database disk image is malformed",
		len(read(crash+"-wal")) > 0 && ((ierr != nil && strings.Contains(ierr.Error(), "malformed")) || (ierr == nil && tab(ic) != "ok")), tab(ic), ierr)
	bc.Close()
}
