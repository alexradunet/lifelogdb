package tests

import (
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"lifelog/internal/text"
)

// The save contract (contract/titles-and-wikilinks, cookbook/save-a-body, D19). Its vectors live in one place, the
// table of contract/titles-and-wikilinks; the boundary cases under the table are computed here. The extraction is
// the writer's (internal/text), every save is either the writer's own (internal/core) or the cookbook's SQL run
// literally, and both run against the DDL under test.

type vector struct {
	body string
	want []string
}

var (
	a240, a241 = strings.Repeat("a", 240), strings.Repeat("a", 241)
	j80, j81   = strings.Repeat("日", 80), strings.Repeat("日", 81)
	ss81, esz  = strings.Repeat("ss", 81), strings.Repeat("ẞ", 81)
)

// computed is the boundary vectors the line under the table states: 240 and 241 bytes, 80 and 81 CJK characters,
// and 81 x ẞ (243 bytes, invalid) beside 81 x ss, which links the ss spelling.
func computed() []vector {
	return []vector{
		{"[[" + a240 + "]]", []string{a240}}, {"[[" + a241 + "]]", nil},
		{"[[" + j80 + "]]", []string{j80}}, {"[[" + j81 + "]]", nil},
		{"[[" + esz + "]] [[" + ss81 + "]]", []string{ss81}},
	}
}

var (
	vectorTable = regexp.MustCompile(`\| body \| links to.*?\n\s*\|---\|---\|\n((?:\s*\|.*\n)+)`)
	vectorRow   = regexp.MustCompile("^\\| (``? .*? ``?|`[^`]*`) \\| (.*) \\|$")
	uEscape     = regexp.MustCompile(`\\u([0-9a-fA-F]{4})`)
	codeSpanRE  = regexp.MustCompile("`([^`]*)`")
)

// unesc applies the page's escapes: \| \n \r \t and \uXXXX.
func unesc(t string) string {
	t = strings.NewReplacer(`\|`, "|", `\n`, "\n", `\r`, "\r", `\t`, "\t").Replace(t)
	return uEscape.ReplaceAllStringFunc(t, func(m string) string {
		n, _ := strconv.ParseUint(m[2:], 16, 32)
		return string(rune(n))
	})
}

// printedVectors is the vector table of contract/titles-and-wikilinks, and the rows that do not parse.
func (s *S) printedVectors() (rows []vector, bad []string) {
	m := vectorTable.FindStringSubmatch(s.d.Page("contract/titles-and-wikilinks.md"))
	if m == nil {
		return nil, []string{"no vector table"}
	}
	for _, line := range strings.Split(strings.TrimRight(m[1], "\n"), "\n") {
		mm := vectorRow.FindStringSubmatch(strings.TrimSpace(line))
		if mm == nil {
			bad = append(bad, line)
			continue
		}
		body := mm[1]
		if strings.HasPrefix(body, "``") {
			body = body[3 : len(body)-3]
		} else {
			body = body[1 : len(body)-1]
		}
		var want []string
		if mm[2] != "—" {
			for _, t := range codeSpanRE.FindAllStringSubmatch(mm[2], -1) {
				want = append(want, unesc(t[1]))
			}
		}
		rows = append(rows, vector{unesc(body), want})
	}
	return
}

func targets(body string) []string { _, t, _ := text.Targets(body, ""); return t }

// docSaveContract: the save contract as the DOCUMENT prints it.
// A  the vector table of contract/titles-and-wikilinks reproduces with the writer's extraction, row for row, and the
//
//	boundary cases under it hold;
//
// B  the SQL of cookbook/save-a-body, run literally statement by statement, gives the vector results, leaves no
//
//	orphan, carries ids by RETURNING, and equals the writer's own save after 400 random edits;
//
// C  cookbook/backlinks lists a day page's wikilink and not a stub's redirect row.
func docSaveContract(s *S) {
	// ---- A  the table of contract/titles-and-wikilinks
	rows, bad := s.printedVectors()
	s.K("A every row of the contract table parses, and there are at least 60", len(bad) == 0 && len(rows) >= 60, len(rows), bad)
	var wrong []string
	for _, v := range rows {
		if got := targets(v.body); !eq(got, v.want) {
			wrong = append(wrong, fmt.Sprintf("%q: got %q, want %q", v.body, got, v.want))
		}
	}
	s.K("A every printed vector reproduces with the writer's extraction", len(rows) > 0 && len(wrong) == 0, wrong)
	s.K("A 81 x ẞ beside 81 x ss (243 bytes, invalid) links ss, as the line under the table says", eq(targets("[["+esz+"]] [["+ss81+"]]"), []string{ss81}))
	s.K("A the 240/241-byte and 80/81-CJK boundary under the table holds", len(targets("[["+a240+"]]")) == 1 && len(targets("[["+a241+"]]")) == 0 &&
		len(targets("[["+j80+"]]")) == 1 && len(targets("[["+j81+"]]")) == 0)

	// ---- B  cookbook/save-a-body run literally
	st, ok := s.saveSteps()
	s.K("B cookbook/save-a-body has exactly one statement of each step", ok, st.counts)
	sts := statements(s.d.Block("save-a-body"))
	index := func(x string) int {
		for i, y := range sts {
			if y == x {
				return i
			}
		}
		return -1
	}
	s.K("B cookbook/save-a-body opens with BEGIN IMMEDIATE and resolves inside the transaction", ok && len(sts) > 0 && strings.HasPrefix(strings.ToUpper(code(sts[0])), "BEGIN IMMEDIATE") &&
		index(st.sel) < index(st.ent) && index(st.ent) < index(st.commit))
	s.K("B no cookbook/save-a-body statement uses last_insert_rowid()", !strings.Contains(s.d.Block("save-a-body"), "last_insert_rowid"))
	if ok {
		docPage := func(c *C, body, title string) int64 {
			c.must(st.begin)
			pid := c.ent("page")
			c.must("INSERT INTO pages(id,title,title_key,body) VALUES(?, ?, ?, ?)", pid, title, text.TitleKey(title), body)
			c.docSave(st, pid, body, text.TitleKey(title), true)
			c.must(st.commit)
			return pid
		}
		docEdit := func(c *C, pid int64, body string) {
			c.must(st.begin)
			c.must("UPDATE pages SET body=? WHERE id=?", body, pid)
			c.docSave(st, pid, body, c.str("select title_key from pages where id=?", pid), true)
			c.must(st.commit)
		}
		all := append(rows, computed()...)
		okv, orph := true, int64(0)
		var differs []string
		for _, v := range all {
			c := s.fresh()
			pid := docPage(c, v.body, "Vector body")
			orph += c.n("select count(*) from entities e where not exists (select 1 from pages p where p.id=e.id)")
			if got := c.linksOf(pid); !eq(got, sorted(v.want)) {
				okv = false
				differs = append(differs, fmt.Sprintf("%q: %q", v.body, got))
			}
			c.Close()
		}
		s.K(fmt.Sprintf("B the document's SQL, run literally, gives the vector result for all %d vectors", len(all)), okv, differs)
		s.K("B ...and leaves no orphan entities row", orph == 0)
		c := s.fresh()
		c.must("BEGIN IMMEDIATE")
		eid := c.rows(st.ent, P{"source": "ui"})[0][0]
		c.must(st.pg, P{"title": "Zed", "key": "zed", "target_id": eid})
		c.must("COMMIT")
		s.K("B the id step 2b RETURNs is the new page's id", c.one("select id from pages where title='Zed'") == eid)
		frags := []string{"[[Alpha]]", "[[alpha|a]]", "#beta", "`[[code]]`", "[[Bad/Name]]", "~~~\n[[fence]]\n~~~", "[[Ünï]]", "[[UNÏ]]", "text", "#Beta",
			"[[Gamma delta]]", "#12", "[[CON]]", "#REDIRECTED", "#REDIRECT [[Alpha]]"}
		rng := rand.New(rand.NewPCG(10, 10))
		ca, cb := s.fresh(), s.fresh()
		var pa, pb []int64
		for i := range 6 {
			pa = append(pa, docPage(ca, "seed", fmt.Sprintf("Seed %d", i)))
			id, _ := cb.savePage("seed", fmt.Sprintf("Seed %d", i))
			pb = append(pb, id)
		}
		for range 400 {
			i := rng.IntN(6)
			var parts []string
			for range rng.IntN(7) {
				parts = append(parts, frags[rng.IntN(len(frags))])
			}
			body := strings.Join(parts, " ")
			docEdit(ca, pa[i], body)
			cb.editBody(pb[i], body)
		}
		same := true
		for i := range pa {
			same = same && eq(ca.linksOf(pa[i]), cb.linksOf(pb[i]))
		}
		s.K("B the document's SQL equals the writer's own save after 400 random edits", same)
		keys := "select title_key from pages where title_key is not null order by 1"
		s.K("B ...with the same set of pages", eq(ca.col(keys), cb.col(keys)))
	}

	// ---- C  cookbook/backlinks drops redirect rows
	c := s.fresh()
	c.must("BEGIN IMMEDIATE")
	nw := c.page("Diet plan")
	old := c.pageW("Diet", nil, "#REDIRECT [[Diet plan]]")
	mm := c.dayPage("2026-09-30", "[[Diet plan]]")
	c.link(old, nw, "redirect")
	c.link(mm, nw, "wikilink")
	dd := c.dayPage("2026-09-29", "[[Diet]]")
	c.link(dd, old, "wikilink")
	c.must("COMMIT")
	r := c.rows(s.d.Block("backlinks"), P{"page_id": nw})
	var got []string
	for _, x := range r {
		got = append(got, val(x[0])+"|"+val(x[2]))
	}
	s.K("C cookbook/backlinks lists the wikilinks to the page and to its stub, not the stub's redirect row",
		eq(sorted(got), sorted([]string{"wikilink|" + ids(mm), "wikilink|" + ids(dd)})), tab(r))
}

// saveContract: the save contract through the writer's own save, against the DDL under test. Expected outcome in each label.
func saveContract(s *S) {
	orphans := func(c *C) int64 {
		return c.n("SELECT count(*) FROM entities e WHERE entity_type='page' AND NOT EXISTS (SELECT 1 FROM pages p WHERE p.id=e.id)")
	}

	// P1 valid and invalid targets in one page: the page IS saved, valid links made, invalid ones skipped
	c := s.fresh()
	pid, r := c.savePage("Diet: [[Health/Diet]], [[Re: plan]], [[Target|alias]], [[Good page]], #health #con #C", "")
	s.K("P1a the page is saved although two targets are invalid", c.n("select count(*) from pages where id=?", pid) == 1)
	s.K("P1b links = C, Good page, Target, health (#C is a valid one-letter tag)", eq(c.linksOf(pid), []string{"C", "Good page", "Target", "health"}), c.linksOf(pid))
	s.K("P1c skipped (reported, not stored): Health/Diet, Re: plan, con", eq(sorted(r.Skipped), []string{"Health/Diet", "Re: plan", "con"}), r.Skipped)
	s.K("P1d no orphan entities row, DB consistent", orphans(c) == 0 && c.integrityOK())
	s.K("P1e the body keeps the text verbatim", strings.HasPrefix(c.str("select body from pages where id=?", pid), "Diet: [[Health/Diet]]"))

	// P2 backstop: with the title predicate skipped the save survives, and leaves no orphan (cookbook/save-a-body's SAVEPOINT)
	if st, ok := s.saveSteps(); ok {
		c = s.fresh()
		var skipped []string
		var p2 int64
		out := try(func() {
			c.must(st.begin)
			p2 = c.ent("page")
			c.must("INSERT INTO pages(id,title,title_key,body) VALUES (?, 'Backstop', 'backstop', 'x [[Health/Diet]] [[fine]]')", p2)
			_, skipped = c.docSave(st, p2, "x [[Health/Diet]] [[fine]]", "backstop", false)
			c.must(st.commit)
		})
		s.K("P2a predicate off: the page is still committed", out == "OK" && c.n("select count(*) from pages where id=?", p2) == 1, out)
		s.K("P2b predicate off: bad target skipped, good target linked", eq(c.linksOf(p2), []string{"fine"}) && contains(skipped, "Health/Diet"), c.linksOf(p2), skipped)
		s.K("P2c predicate off: no orphan entities row", orphans(c) == 0, orphans(c))
	} else {
		s.K("P2 cookbook/save-a-body has one statement of each step", false, st.counts)
	}

	// P3 sync = set equality: adding, removing, re-saving
	c = s.fresh()
	pid, _ = c.savePage("[[A]] [[B]] #t", "")
	s.K("P3a initial links A, B, t", eq(c.linksOf(pid), []string{"A", "B", "t"}))
	c.editBody(pid, "[[A]] [[C]]")
	s.K("P3b edit: B and t dropped, C added", eq(c.linksOf(pid), []string{"A", "C"}), c.linksOf(pid))
	n1 := c.tab("select count(*), max(id) from links")
	c.editBody(pid, "[[A]] [[C]]")
	n2 := c.tab("select count(*), max(id) from links")
	s.K("P3c re-saving the same body changes nothing (idempotent, no new rows)", n1 == n2, n1, n2)
	c.editBody(pid, "no links at all")
	s.K("P3d body without links: all wikilinks removed", len(c.linksOf(pid)) == 0)
	s.K("P3e dropped targets survive as pages (and may become ghosts, cookbook/ghost-pages)", c.n("select count(*) from pages where id<>? and entity_type = 'page'", pid) == 4)

	// P4 self link, stub
	c = s.fresh()
	c.must("BEGIN IMMEDIATE")
	wid := c.ent("page")
	c.must("INSERT INTO pages(id,title,title_key,body) VALUES(?, 'Diet','diet','')", wid)
	c.must("COMMIT")
	c.editBody(wid, "About [[Diet]] and [[diet]] and [[Food]]")
	s.K("P4a a page never links to itself", eq(c.linksOf(wid), []string{"Food"}), c.linksOf(wid))
	c.must("BEGIN IMMEDIATE")
	old := c.ent("page")
	c.must("INSERT INTO pages(id,title,title_key,body) VALUES(?, 'Old diet','old diet','[[Food]] [[Stuff]]')", old)
	c.must("COMMIT")
	c.editBody(old, "[[Food]] [[Stuff]]")
	nBefore := len(c.linksOf(old))
	c.editBody(old, "#REDIRECT [[Diet]]")
	c.must("BEGIN IMMEDIATE")
	c.must("INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES(?,?, 'redirect', "+NOW+", 'ui')", old, wid)
	c.must("COMMIT")
	s.K(`P4b a stub has no wikilink edges (old links dropped, new not made) and no page "REDIRECT"`, nBefore == 2 && len(c.linksOf(old)) == 0 &&
		c.n("select count(*) from pages where title_key='redirect'") == 0, nBefore, c.linksOf(old))
	s.K("P4c the stub's one edge is the redirect link", c.n("select count(*) from links where from_id=? and kind='redirect'", old) == 1)

	// P5 tombstoned target is revived, not duplicated
	c = s.fresh()
	pid, _ = c.savePage("[[Junk]]", "")
	jid := c.n("select id from pages where title='Junk'")
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", jid)
	pid2, _ := c.savePage("again [[JUNK]]", "")
	s.K("P5a one page, revived", c.n("select count(*) from pages where title_key='junk'") == 1 && c.str("select deleted_at from entities where id=?", jid) == "None")
	s.K("P5b both pages link to it", eq(c.linksOf(pid), []string{"Junk"}) && eq(c.linksOf(pid2), []string{"Junk"}))

	// P6 incremental == rebuild, over random edit sequences
	frags := []string{"[[Alpha]]", "[[alpha|a]]", "#beta", "`[[code]]`", "[[Bad/Name]]", "```\n[[fence]]\n```", "[[Ünï]]", "[[UNÏ]]", "text", "#Beta", "[[Gamma delta]]", "#12", "[[CON]]", "#REDIRECTED"}
	rng := rand.New(rand.NewPCG(6, 6))
	c = s.fresh()
	var pgs []int64
	for range 6 {
		id, _ := c.savePage("seed", "")
		pgs = append(pgs, id)
	}
	for range 400 {
		var parts []string
		for range rng.IntN(7) {
			parts = append(parts, frags[rng.IntN(len(frags))])
		}
		c.editBody(pgs[rng.IntN(len(pgs))], strings.Join(parts, " "))
	}
	c2 := s.fresh()
	same := true
	for _, p := range pgs {
		q, _ := c2.savePage(c.str("select body from pages where id=?", p), "")
		same = same && eq(c.linksOf(p), c2.linksOf(q))
	}
	s.K("P6a incremental maintenance == rebuild from bodies (6 pages, 400 random edits)", same)
	s.K("P6b no orphans, integrity ok after 400 edits", orphans(c) == 0 && c.integrityOK())
	s.K("P6c at most one page per title_key", c.n("select count(*) from (select title_key from pages where title_key is not null group by 1 having count(*)>1)") == 0)

	// P7 four real writers save pages with the same new tag and target at the same time
	path := filepath.Join(s.dir, "p7.db")
	c0 := s.freshWith(F{Path: path})
	c0.must("PRAGMA journal_mode=WAL")
	c0.Close()
	var mu sync.Mutex
	var errs []string
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if out := try(func() { s.writerConn(path).savePage(fmt.Sprintf("writer %d about [[Shared topic]] #shared", i), "") }); out != "OK" {
				mu.Lock()
				errs = append(errs, out)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	cv := s.writerConn(path)
	s.K("P7a four concurrent saves: no errors", len(errs) == 0, errs)
	s.K("P7b one page per shared target, four pages linked to each", cv.n("select count(*) from pages where title_key in ('shared topic','shared')") == 2 &&
		cv.n("select count(*) from links where kind='wikilink'") == 8)

	// P8 the vectors, end to end through the database (links == vector result); P9 the extraction alone
	rows, _ := s.printedVectors()
	all := append(rows, computed()...)
	okDB, okText := len(rows) > 0, len(rows) > 0
	var differs []string
	for _, v := range all {
		c := s.fresh()
		pid, _ := c.savePage(v.body, "")
		if got := c.linksOf(pid); !eq(got, sorted(v.want)) {
			okDB = false
			differs = append(differs, fmt.Sprintf("%q: %q", v.body, got))
		}
		c.Close()
		okText = okText && eq(targets(v.body), v.want)
	}
	s.K(fmt.Sprintf("P8 all %d vectors give the same links through a real save", len(all)), okDB, differs)
	s.K("P9 extraction vectors", okText)
}

// titleFuzz: the writer's title predicate against the DDL's own CHECKs on the same strings. No string the writer
// accepts may be refused by the DB (that would block a save); and, because the predicate mirrors the CHECKs one for
// one, none the other way either, except unassigned code points (category Cn), which only the writer can recognise
// — the alphabet below holds none, and the pages suite tests that difference on its own.
func titleFuzz(s *S) {
	alpha := []string{"a", "b", "c", "X", "Y", "Z", "0", "1", "9", " ", ".", "-", "_", "#", "/", "\\", ":", "*", "?", "\"", "<", ">", "|", "\u0001", "\u0009", "\u000a", "\u001f", "\u007f",
		"\u00a0", "\u3000", "\u2003", "é", "É", "ß", "日", "\U0001f600", "İ", "ſ", "K", "\u0301", "\u200b", "\u202e", "\u00ad", "\ufeff", "\u0085", "\u009f",
		"\u00a0", "\u200c", "\u200d", "\u2066", "\u061c", "\u2060", "\ufe0f"}
	words := []string{"COM¹", "LPT³", "LPT².txt", "con.backup", "Nul.tar.gz", "COM1.x", "CONX.txt", "a.CON", "a.con.b", ".con", "CON.", "COM10.txt", "CON .txt",
		"CON\u00a0.txt", "CON", "con", "Nul", "PRN", "aux", "COM1", "com9", "LPT1", "lpt9", "COM0", "LPT10", "CONSOLE", "NUL.txt", "CON.backup", ".", "..", "...", "a.", ".a",
		"a b", "a  b", " a", "a ", "a\u00a0", "\u00a0a"}
	rng := rand.New(rand.NewPCG(8, 8))
	pick := func(n int) string {
		var b strings.Builder
		for range n {
			b.WriteString(alpha[rng.IntN(len(alpha))])
		}
		return b.String()
	}
	rnd := func() string {
		switch r := rng.Float64(); {
		case r < .15:
			return words[rng.IntN(len(words))]
		case r < .30:
			return pick(1 + rng.IntN(6))
		case r < .40: // the length boundary, mixed byte widths
			n, t := 236+rng.IntN(9), ""
			for len(t) < n {
				t += []string{"a", "é", "日", "😀"}[rng.IntN(4)]
			}
			return t
		}
		return pick(1 + rng.IntN(12))
	}
	cases := append([]string{}, words...)
	cases = append(cases, a240, a241, j80, j81, strings.Repeat("😀", 60), strings.Repeat("😀", 61), strings.Repeat("é", 120), strings.Repeat("é", 121))
	for range 60000 {
		cases = append(cases, rnd())
	}
	c := s.fresh()
	ins, err := c.Prepare("INSERT INTO pages(id,title,title_key,day) VALUES(?, ?, ?, CASE WHEN date(?) IS ? THEN ? END)")
	if err != nil {
		stop("prepare: %v", err)
	}
	defer ins.Close()
	seen := map[string]bool{}
	var block, loose []string
	accepted := 0
	for _, t := range cases {
		if seen[t] {
			continue
		}
		seen[t] = true
		app := text.ValidTitle(t)
		c.must("SAVEPOINT f")
		pid := c.ent("page")
		_, e := ins.Exec(pid, t, text.TitleKey(t), t, t, t)
		c.must("ROLLBACK TO f")
		c.must("RELEASE f")
		db := e == nil
		if db {
			accepted++
		}
		if app && !db {
			block = append(block, fmt.Sprintf("%q (%v)", clip(t, 30), e))
		}
		if db && !app {
			loose = append(loose, fmt.Sprintf("%q", clip(t, 30)))
		}
	}
	s.K("more than 40 000 distinct strings, some accepted by the DB and some refused", len(seen) > 40000 && accepted > 0 && accepted < len(seen), len(seen), accepted)
	s.K("no string the writer accepts is refused by the DB (that would block a save)", len(block) == 0, len(block), block[:min(5, len(block))])
	s.K("no string the DB accepts is refused by the writer (the predicate mirrors the CHECKs)", len(loose) == 0, len(loose), loose[:min(8, len(loose))])
}
