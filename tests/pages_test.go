package tests

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/text"

	"modernc.org/sqlite"
)

// autoKey asks addPage for the title_key of the title.
const autoKey = "\x00auto"

// addPage is an entities + pages row inside a savepoint: the id, or 0 when the DDL refused it.
func (c *C) addPage(title any, day any, body string, key any) int64 {
	if key == autoKey {
		key = nil
		if t, ok := title.(string); ok {
			key = text.TitleKey(t)
		}
	}
	c.must("SAVEPOINT a")
	i := c.ent("page")
	if c.tryx("INSERT INTO pages(id,title,title_key,day,body) VALUES (?,?,?,?,?)", i, title, key, day, body) != "OK" {
		c.must("ROLLBACK TO a")
		c.must("RELEASE a")
		return 0
	}
	c.must("RELEASE a")
	return i
}

func (c *C) add(title string) int64 { return c.addPage(title, nil, "", autoKey) }

// pages: pages and titles (contract/titles-and-wikilinks, D5) — every page titled, the day rule, one title
// namespace, filename-safe titles, title_key and its vectors, lookups by key, fixed titles, link first and write
// later, full-text search, and why titles are not unique by a collation. The day page itself is in journal.
func pages(s *S) {
	// ---- every page titled; the day rule
	c := s.fresh()
	s.K("a page accepted", c.add("Diet") != 0)
	s.K("a page with a day accepted", c.addPage("Trip report", "2026-06-09", "", autoKey) != 0)
	s.K("a page without a day accepted", c.add("Reference") != 0)
	s.K("a malformed day rejected", c.addPage("Bad day", "2026-6-9", "", autoKey) == 0)
	s.K("a page without a title rejected", c.addPage(nil, nil, "", nil) == 0)
	s.K("a page with a key but no title rejected", c.addPage(nil, nil, "", "orphan") == 0)
	s.K("a page with a title but no key rejected", c.addPage("Keyless", nil, "", nil) == 0)

	// ---- one title namespace
	c = s.fresh()
	s.K("first page accepted", c.addPage("Diet", "2026-06-09", "", autoKey) != 0)
	s.K("DIET without a day collides with Diet with a day", c.add("DIET") == 0)
	s.K("NFD Café collides with NFC Café", c.add("Cafe\u0301") != 0 && c.add("Café") == 0)
	s.K("Straße and STRASSE are one page", c.add("Straße") != 0 && c.add("STRASSE") == 0)
	s.K("a different title is fine", c.add("Cafe") != 0)
	c.tryx("UPDATE entities SET deleted_at=" + NOW + " WHERE id=(select id from pages where title='Diet')")
	s.K("a tombstoned page still holds its title (the index covers tombstones)", c.add("diet") == 0)

	// ---- filename-safe titles (pages_title_len, pages_title_safe)
	c = s.fresh()
	for _, ch := range []string{"/", `\`, ":", "*", "?", `"`, "<", ">", "|", "\x01", "\t", "\n", "\x1f", "\x7f", "\u0085", "\u009f"} {
		s.K(fmt.Sprintf("character %q rejected", ch), c.addPage("a"+ch+"b", nil, "", "ab") == 0)
	}
	for _, t := range []string{".hidden", "..", "trail.", " pad", "pad ", "CON", "con", "Nul", "prn", "AUX", "COM1", "lpt9", "con.txt", "CON.backup", "NUL.tar.gz", "Prn.a.b",
		"COM¹", "com².x", "COM³", "LPT¹", "lpt².txt", "LPT³.a.b", "CON.", ".CON", "CON ", " CON"} {
		k := strings.TrimSpace(strings.ToLower(t))
		if k == "" {
			k = "x"
		}
		s.K(fmt.Sprintf("unsafe or device title %q rejected", t), c.addPage(t, nil, "", k) == 0)
	}
	for _, t := range []string{"CONSOLE", "CONSOLE.txt", "a.CON", "x.NUL", "COM10", "com10.x", "LPT0", "Com0.x", "CON1", "my.con.note", "Lifelog v1.2", "日本語 ノート", "C#", strings.Repeat("a", 240)} {
		s.K(fmt.Sprintf("title %.14q accepted", t), c.add(t) != 0)
	}
	s.K("241 bytes rejected (é × 121 = 242 bytes)", c.add(strings.Repeat("é", 121)) == 0 && c.add(strings.Repeat("a", 241)) == 0)
	s.K("80 × 日 = 240 bytes accepted, 81 rejected", c.add(strings.Repeat("日", 80)) != 0 && c.add(strings.Repeat("日", 81)) == 0)
	for _, cp := range []rune{0xAD, 0x61C, 0x200B, 0x200E, 0x200F, 0x202A, 0x202E, 0x2060, 0x2064, 0x2066, 0x2069, 0xFEFF} {
		t := "Diet" + string(cp) + "plan"
		s.K(fmt.Sprintf("U+%04X in a title is rejected by the DB and by the writer", cp), c.add(t) == 0 && !text.ValidTitle(t))
	}
	for _, t := range []string{"Café", "\U0001F389 Party", "می\u200cخواهم", "\U0001F468\u200d\U0001F469\u200d\U0001F467", "İstanbul", "❤\ufe0f"} {
		s.K(fmt.Sprintf("%q accepted by both (ZWNJ/ZWJ and variation selectors carry meaning)", t), c.add(t) != 0 && text.ValidTitle(t))
	}
	for _, t := range []string{"Diet\u0378", "\U000E0080x"} {
		s.K(fmt.Sprintf("unassigned code point %q: the writer rejects it, the DB cannot tell", t), !text.ValidTitle(t) && c.add(t) != 0)
	}
	s.K("ASCII title with a wrong key rejected", c.addPage("Diet2", nil, "", "dyet2") == 0)
	s.K("a key with an ASCII capital rejected", c.addPage("Diet3", nil, "", "Diet3") == 0)
	s.K("a key with a leading space rejected", c.addPage("Diet4", nil, "", " diet4") == 0)
	s.K("a non-ASCII title's key may not hold an ASCII capital", c.addPage("Café Q", nil, "", "Café Q") == 0)
	s.K("a title with leading space refused (a key the writer folded to match)", c.addPage(" Pädded", nil, "", "pädded") == 0)
	s.K("a title with a NUL byte refused", c.addPage("a\x00é", nil, "", text.TitleKey("a\x00é")) == 0)
	s.K("a non-ASCII title with a plausible key accepted (the writer owns the fold)", c.addPage("Über", nil, "", "über") != 0)

	// ---- title_key vectors of contract/titles-and-wikilinks
	m := regexp.MustCompile("\\| title \\| `title_key` \\|\n\\|---\\|---\\|\n((?:\\|.*\n)+)").FindStringSubmatch(s.d.Page("contract/titles-and-wikilinks.md"))
	span := regexp.MustCompile("`([^`]+)`")
	type pair struct{ in, out string }
	var vec []pair
	if m != nil {
		for _, line := range strings.Split(strings.TrimRight(m[1], "\n"), "\n") {
			parts := strings.Split(strings.Trim(line, "|"), "|")
			if len(parts) < 2 {
				continue
			}
			var ins, outs []string
			for _, x := range span.FindAllStringSubmatch(parts[0], -1) {
				ins = append(ins, strings.ReplaceAll(x[1], `\u0301`, "\u0301"))
			}
			for _, x := range span.FindAllStringSubmatch(parts[1], -1) {
				outs = append(outs, x[1])
			}
			for i, in := range ins {
				o := ""
				switch {
				case len(outs) == 1:
					o = outs[0]
				case i < len(outs):
					o = outs[i]
				}
				vec = append(vec, pair{in, o})
			}
		}
	}
	var wrong []string
	for _, p := range vec {
		if text.TitleKey(p.in) != p.out {
			wrong = append(wrong, fmt.Sprintf("%q→%q (want %q)", p.in, text.TitleKey(p.in), p.out))
		}
	}
	s.K("the title_key table of contract/titles-and-wikilinks has at least 12 pairs and every one is what the writer's function gives", len(vec) >= 12 && len(wrong) == 0, len(vec), wrong)

	// ---- fixed title
	c = s.fresh()
	p := c.add("Fixed")
	s.K("a title cannot change", strings.Contains(c.tryx("UPDATE pages SET title='Changed' WHERE id=?", p), "immutable"))
	s.K("a no-op SET title = title passes", c.tryx("UPDATE pages SET title=title, body='b' WHERE id=?", p) == "OK")

	// ---- lookups by key
	c = s.fresh()
	for i := range 500 {
		c.addPage(fmt.Sprintf("Note %d", i), nil, fmt.Sprintf("note %d", i), autoKey)
	}
	for _, t := range []string{"Diet", "Food", "Zürich"} {
		c.add(t)
	}
	var sel []string
	for _, st := range statements(s.d.Block("save-a-body")) {
		if strings.HasPrefix(strings.ToUpper(code(st)), "SELECT") {
			sel = append(sel, st)
		}
	}
	s.K("cookbook/save-a-body has one resolve", len(sel) == 1, sel)
	if len(sel) == 1 {
		plan := c.plan(sel[0], P{"key": "diet"})
		s.K("...and is a SEARCH on pages_title", strings.Contains(plan, "SEARCH") && strings.Contains(plan, "pages_title") && !strings.Contains(plan, "SCAN p"), plan)
		r, e := c.query(sel[0], P{"key": "zürich"})
		s.K("...and finds the page", e == nil && len(r) == 1 && r[0][1] == "Zürich", e)
	}
	s.K("control: a lookup by lower(title) is a SCAN", strings.Contains(c.plan("SELECT id FROM pages WHERE lower(title)='diet'"), "SCAN"))

	// ---- link first, write later
	c = s.fresh()
	mm, _ := c.savePage("Planning #japan-trip for spring", "Plans")
	row := c.rows("select id, day, body from pages where title_key='japan-trip'")
	s.K("a link made its target a page, empty, with no day", len(row) == 1 && tab([][]any{row[0][1:]}) == "None|", tab(row))
	if len(row) == 1 {
		s.K("writing the ghost needs no new page and no redirect", c.tryx("UPDATE pages SET body='Trip report' WHERE id=?", row[0][0]) == "OK" && c.n("select count(*) from pages where title_key='japan-trip'") == 1)
		s.K("the page still links to it", c.n("select count(*) from links where from_id=? and to_id=? and kind='wikilink'", mm, row[0][0]) == 1)
	}

	// ---- full-text search
	c = s.fresh()
	c.add("Notes")
	mo := c.addPage("Words", nil, "first words about Zürich", autoKey)
	hits := func(q string) int64 { return c.n("select count(*) from pages_fts where pages_fts match ?", q) }
	s.K("an insert is indexed; unicode61 folds accents (zurich finds Zürich)", hits("first") == 1 && hits("zurich") == 1)
	before := c.n("select total_changes()")
	c.must("UPDATE pages SET day='2026-06-09' WHERE id=?", mo)
	d := c.n("select total_changes()") - before
	s.K("a new day touches pages and entities only, not the index (pages_fts_update watches title and body)", d == 2, d)
	c.must("UPDATE pages SET body='second words' WHERE id=?", mo)
	s.K("a body change re-indexes: old word gone, new word found", hits("first") == 0 && hits("second") == 1)
	c.addPage("CJK", nil, "日本語のノートを書く", autoKey)
	s.K("a CJK run is one token (the known limit of unicode61)", hits("日本語のノートを書く") == 1 && hits("本語") == 0)
	c.must("INSERT INTO pages_fts(pages_fts) VALUES('rebuild')")
	s.K("the index rebuilds from pages and passes its integrity-check", hits("second") == 1 && c.tryx("INSERT INTO pages_fts(pages_fts, rank) VALUES('integrity-check', 1)") == "OK")
	s.K("cookbook/full-text-search finds a page by a word of its body", eq(c.col(s.d.Block("full-text-search"), P{"query": "second"}), []string{ids(mo)}))

	// ---- why not a collation: an index on a collation only one program has. Another process (this test binary,
	// run again) registers the collation and builds the file; this process has no such collation.
	cp := filepath.Join(s.dir, "coll.db")
	out, e := collationChild(cp)
	if e != nil {
		stop("the collation helper failed: %v %s", e, out)
	}
	o := s.connect(cp)
	r1 := o.tryx("INSERT INTO t VALUES ('x')")
	r2 := o.tabOrErr("PRAGMA integrity_check")
	s.K("a database whose index needs a collation one program registered cannot be written by another program", strings.Contains(r1, "no such collation sequence"), r1)
	s.K("...nor integrity-checked", strings.Contains(r2, "no such collation sequence"), r2)
	s.K("...though it can be read", o.n("SELECT count(*) FROM t") == 1)
}

// tabOrErr is the rows as text, or the error.
func (c *C) tabOrErr(q string, args ...any) string {
	r, err := c.query(q, args...)
	if err != nil {
		return err.Error()
	}
	return tab(r)
}

// collationChild runs TestCollationHelper in a new process of this test binary: it registers a case-folding
// collation, builds a file with a unique index on it, and exits.
func collationChild(path string) ([]byte, error) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestCollationHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), "LIFELOG_COLLATION_DB="+path)
	return cmd.CombinedOutput()
}

// TestCollationHelper is the other program of the collation expectations in pages; on its own it does nothing.
func TestCollationHelper(t *testing.T) {
	p := os.Getenv("LIFELOG_COLLATION_DB")
	if p == "" {
		t.Skip("run by the pages suite in its own process")
	}
	if err := sqlite.RegisterCollationUtf8("UFOLD", func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	}); err != nil {
		t.Fatal(err)
	}
	s := &S{}
	defer s.close()
	c := s.connect(p)
	c.must("CREATE TABLE t (title TEXT)")
	c.must("CREATE UNIQUE INDEX t_title ON t(title COLLATE UFOLD)")
	c.must("INSERT INTO t VALUES ('Café')")
}
