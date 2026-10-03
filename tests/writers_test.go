package tests

import (
	"database/sql/driver"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"modernc.org/sqlite"
)

// probeOnce registers lifelog_probe, an application function with no SQLITE_INNOCUOUS flag, for every connection.
var probeOnce sync.Once

// try runs fn and turns a stop into "ERR <why>"; "OK" when fn returns.
func try(fn func()) (r string) {
	defer func() {
		if x := recover(); x != nil {
			r = "ERR " + fmt.Sprint(x)
		}
	}()
	fn()
	return "OK"
}

// mkdb is a new file with the DDL applied, closed again.
func (s *S) mkdb() string {
	p := filepath.Join(s.dir, fmt.Sprintf("life-%d.db", s.next()))
	c := s.connect(p)
	c.must(s.ddl)
	c.Close()
	return p
}

// writerConn is a writing connection with the contract/connections pragmas (and the 5 s busy timeout).
func (s *S) writerConn(p string) *C {
	c := s.connect(p)
	c.must("PRAGMA foreign_keys=ON")
	c.must("PRAGMA recursive_triggers=ON")
	return c
}

// writers: writers and readers (contract/connections, D3, D14) — the BEGIN IMMEDIATE race with real concurrent
// connections, the pragmas and what they do, read-only readers under WAL, the minimum SQLite as data, and a
// hardened connection.
func writers(s *S) {
	const resolve = "SELECT p.id FROM pages p WHERE p.title_key = ?"
	s.K("journal_mode=WAL is stored in the file by the DDL", s.connect(s.mkdb()).str("PRAGMA journal_mode") == "wal")

	// ---- the race of cookbook/save-a-body
	p := s.mkdb()
	A, B := s.writerConn(p), s.writerConn(p)
	A.must("BEGIN")
	s.K("deferred: the resolve finds nothing", A.tab(resolve, "diet") == "")
	B.must("BEGIN IMMEDIATE")
	B.page("Diet")
	B.must("COMMIT")
	out := try(func() { A.page("Diet"); A.must("COMMIT") })
	if out != "OK" {
		A.tryx("ROLLBACK")
	}
	s.K("deferred BEGIN, a read, a rival commit: the write fails at once (busy_timeout does not apply)", strings.Contains(out, "locked"), out)

	p = s.mkdb()
	type result struct {
		what   string
		waited time.Duration
	}
	var mu sync.Mutex
	res := map[string]result{}
	var wg sync.WaitGroup
	writer := func(name string, delay, hold time.Duration) {
		defer wg.Done()
		c := s.writerConn(p)
		time.Sleep(delay)
		t0 := time.Now()
		r := result{}
		out := try(func() {
			c.must("BEGIN IMMEDIATE")
			r.waited = time.Since(t0)
			found := c.tab(resolve, "diet") != ""
			if !found {
				time.Sleep(hold)
				c.page("Diet")
			}
			c.must("COMMIT")
			r.what = map[bool]string{true: "found", false: "created"}[found]
		})
		if out != "OK" {
			r.what = out
			c.tryx("ROLLBACK")
		}
		mu.Lock()
		res[name] = r
		mu.Unlock()
	}
	wg.Add(2)
	go writer("A", 0, 400*time.Millisecond)
	go writer("B", 100*time.Millisecond, 0)
	wg.Wait()
	s.K("BEGIN IMMEDIATE: the first writer creates, the second waits and then finds it", res["A"].what == "created" && res["B"].what == "found", res)
	s.K("...the second really waited for the lock", res["B"].waited > 250*time.Millisecond, res)
	s.K("...one page", s.writerConn(p).n("select count(*) from pages where title_key='diet'") == 1)
	var all []string
	for _, b := range s.d.CookbookBlocks() {
		all = append(all, b.sql)
	}
	sec := strings.Join(all, "\n")
	s.K("cookbook has no bare BEGIN, and its write blocks start with BEGIN IMMEDIATE",
		!regexp.MustCompile(`(?m)^BEGIN;?\s*$`).MatchString(sec) && len(regexp.MustCompile(`(?m)^BEGIN IMMEDIATE;`).FindAllString(sec, -1)) >= 5)

	// ---- pragmas
	c := s.connect(s.mkdb())
	c.must("PRAGMA synchronous=FULL")
	s.K("synchronous=FULL reads back as 2 (what the writer checks)", c.n("PRAGMA synchronous") == 2)
	b26 := sqlBlocks(s.d.Page("contract/connections.md"))
	set := len(b26) > 0
	for _, kv := range [][2]string{{"journal_mode", "WAL"}, {"synchronous", "FULL"}, {"foreign_keys", "ON"}, {"recursive_triggers", "ON"}, {"trusted_schema", "OFF"}} {
		set = set && regexp.MustCompile(`PRAGMA `+kv[0]+`\s*=\s*`+kv[1]).MatchString(b26[0])
	}
	s.K("the contract/connections block sets every pragma the contract names", set, b26)
	i := strings.Index(s.ddl, "PRAGMA application_id")
	if i < 0 {
		stop("the DDL sets no PRAGMA application_id")
	}
	head := s.ddl[:i]
	named := true
	for _, x := range []string{"3.51.3", "foreign_keys = ON", "recursive_triggers = ON", "synchronous = FULL", "trusted_schema = OFF", "BEGIN IMMEDIATE"} {
		named = named && strings.Contains(head, x)
	}
	s.K("the DDL header names SQLite >= 3.51.3, the pragmas and BEGIN IMMEDIATE", named)
	c = s.connect("")
	c.must(s.ddl)
	c.must("BEGIN")
	c.must("PRAGMA foreign_keys=ON")
	inside := c.n("PRAGMA foreign_keys")
	c.must("COMMIT")
	s.K("the DDL marks the file as Lifelog: application_id 0x4C494645 and user_version 1", c.n("PRAGMA application_id") == 0x4C494645 && c.n("PRAGMA user_version") == 1)
	s.K("PRAGMA foreign_keys is a silent no-op inside a transaction", inside == 0)
	meta := c.str("select value from lifelog_meta where key='sqlite'")
	s.K("lifelog_meta.sqlite names 3.51.3 and 3.53", strings.Contains(meta, "3.51.3") && strings.Contains(meta, "3.53"))
	var x, y, z int
	ver := c.str("select sqlite_version()")
	fmt.Sscanf(ver, "%d.%d.%d", &x, &y, &z)
	s.K("this suite runs on such a SQLite", x*1000000+y*1000+z >= 3051003, ver)

	// ---- read-only readers under WAL
	p = s.mkdb()
	w := s.writerConn(p)
	w.must("BEGIN IMMEDIATE")
	w.ent("page")
	ro := s.readOnly(p)
	s.K("a mode=ro reader is not blocked by an open write transaction", ro.n("select count(*) from entities where entity_type = 'page'") == 0)
	w.must("COMMIT")
	s.K("...and sees the commit", ro.n("select count(*) from entities where entity_type = 'page'") == 1)
	for _, st := range []string{"INSERT INTO lifelog_meta VALUES ('x','y')", "DELETE FROM entities", "UPDATE entities SET deleted_at=NULL", "DROP TABLE lifelog_meta"} {
		s.K("mode=ro refuses: "+clip(st, 30), strings.Contains(ro.tryx(st), "readonly"))
	}
	t0 := time.Now()
	w.must("INSERT INTO lifelog_meta VALUES ('k','v')")
	s.K("the writer is not slowed by a connected reader", time.Since(t0) < 500*time.Millisecond)

	// ---- a reader sets trusted_schema=OFF: a view in the file that calls a function not marked side-effect-free
	// (an application function, registered without SQLITE_INNOCUOUS) is refused instead of run
	s.K("contract/connections: readers are read-only and set trusted_schema = OFF",
		regexp.MustCompile("Readers must be \\*\\*read-only\\*\\* and set \\*\\*`PRAGMA trusted_schema = OFF`\\*\\*").MatchString(s.d.Page("contract/connections.md")))
	probeOnce.Do(func() {
		sqlite.MustRegisterScalarFunction("lifelog_probe", 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) {
			return int64(1), nil
		})
	})
	p = s.mkdb()
	s.connect(p, "_pragma=trusted_schema(1)").must("CREATE VIEW probe AS SELECT lifelog_probe() AS x")
	trusting := s.connect(p, "mode=ro", "_pragma=trusted_schema(1)").tryx("SELECT x FROM probe")
	careful := s.connect(p, "mode=ro", "_pragma=trusted_schema(0)").tryx("SELECT x FROM probe")
	s.K("a mode=ro reader with trusted_schema=ON runs the view's function", trusting == "OK", trusting)
	s.K("...with trusted_schema=OFF it refuses it: unsafe use", strings.Contains(careful, "unsafe use"), careful)

	// ---- a hardened connection: DEFENSIVE + the contract/connections pragmas
	c = s.connect("", "_defensive=1")
	var got string
	hard := try(func() {
		if len(b26) > 0 {
			for _, st := range statements(b26[0]) {
				c.must(st)
			}
		}
		c.must(s.ddl)
		pg := c.page("Hardened")
		c.must("UPDATE pages SET body='searchable words' WHERE id=?", pg)
		mm := c.pageW("Another", nil, "another")
		c.link(mm, pg, "wikilink")
		c.named("person", "Ada")
		c.must("DELETE FROM links WHERE from_id=? AND kind='wikilink' AND to_id NOT IN (SELECT value FROM json_each('[]'))", mm)
		got = fmt.Sprint(c.n("SELECT count(*) FROM pages_fts WHERE pages_fts MATCH 'searchable'"), c.n("SELECT count(*) FROM ghost_pages"), c.n("SELECT count(*) FROM measurement_values"))
	})
	s.K("with DEFENSIVE and trusted_schema=OFF: DDL, writes, FTS, views, json_each and a named entity all work", hard == "OK" && got == "1 0 0", hard, got)
	s.K("...and a write to an FTS shadow table is refused", strings.HasPrefix(c.tryx("DELETE FROM pages_fts_data"), "ERR"))
}
