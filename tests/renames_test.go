package tests

import (
	"context"
	"errors"
	"strings"

	"lifelog/internal/core"
	"lifelog/internal/text"
)

// renames: a title never changes, so a rename is a new page and a stub (contract/titles-and-wikilinks "Renames",
// D5), run as cookbook/rename-a-page prints it, statement by statement.
// A  pages with a body, a day or none, and typed links, into free titles: the text, the day and the typed links
//
//	(a mirror included) move; the stub holds its body and its redirect alone; the old title's mentions count one hop;
//
// B  an empty typo ghost into an existing person: no new page, the stub names the person's own spelling;
// C  a page with text into a taken title is refused, and so are the pages that are not renamed; the stub step alone
//
//	changes no row of a page with text, even when a writer skipped the check;
//
// E  a category page (D26): the part-of links that end at it, its metrics', move to the new page;
// D  the writer's own rename (internal/core) writes the same rows as the recipe in A, B, C and E.
func renames(s *S) {
	st := s.renameSteps()
	sv, ok := s.saveSteps()
	if !ok {
		stop("cookbook/save-a-body does not have one statement of each step: %v", sv.counts)
	}
	state := func(c *C) string {
		return c.tab("select title, body, coalesce(day, '-'), entity_type from pages order by title_key") + " / " +
			c.tab(`select f.title, t.title, l.kind, coalesce(l.note, ''), l.source
			         from links l join pages f on f.id = l.from_id join pages t on t.id = l.to_id order by 1, 2, 3`)
	}
	outOf := func(c *C, id int64) []string {
		return sorted(c.col("select l.kind || '|' || p.title from links l join pages p on p.id = l.to_id where l.from_id = ?", id))
	}
	goRename := func(c *C, id int64, title string) (int64, error) {
		return c.store().Rename(context.Background(), "ui", id, title)
	}

	// ---- A  into free titles
	seedA := func(c *C) (old, rye, baking, day int64) {
		japan := c.named("place", "Japan")
		baking = c.pageW("Baking", nil, "Flour and water.")
		old = c.pageW("Sourdogh", "2026-09-20", "Feed the starter. [[Baking]]")
		c.link(old, baking, "wikilink")
		c.link(old, baking, "related")
		c.must("UPDATE links SET note = 'same flour' WHERE from_id = ? AND kind = 'related'", old)
		c.link(old, japan, "about")
		day = c.dayPage("2026-09-29", "baked, see [[Sourdogh]]")
		c.link(day, old, "wikilink")
		rye = c.pageW("Ryee", nil, "Dark.")
		return
	}
	c := s.fresh()
	old, rye, baking, day := seedA(c)
	nw, why := c.docRename(st, sv, old, "Sourdough")
	ry, why2 := c.docRename(st, sv, rye, "Rye")
	s.K("A: cookbook/rename-a-page renames a page with text into a free title", why == "" && nw != 0 && why2 == "" && ry != 0, why, why2)
	s.K("...the new page holds the old page's text and its day", c.tab("select title, body, day from pages where id=?", nw) == "Sourdough|Feed the starter. [[Baking]]|2026-09-20")
	s.K("...and an undated page's new page stays undated", c.tab("select title, body, day from pages where id=?", ry) == "Rye|Dark.|None")
	s.K("...the stub holds the one-line body and its redirect alone", c.str("select body from pages where id=?", old) == "#REDIRECT [[Sourdough]]" &&
		eq(outOf(c, old), []string{"redirect|Sourdough"}), outOf(c, old))
	s.K("...the new page starts the wikilinks of its text and the typed links, notes kept", eq(outOf(c, nw), []string{"about|Japan", "related|Baking", "wikilink|Baking"}) &&
		c.str("select note from links where from_id=? and kind='related'", nw) == "same flour", outOf(c, nw))
	s.K("...and the symmetric link's mirror moved with it", eq(c.col("select to_id from links where from_id=? and kind='related'", baking), []string{ids(nw)}))
	s.K("...the day that wrote [[Sourdogh]] keeps its text and its link to the stub", c.str("select body from pages where id=?", day) == "baked, see [[Sourdogh]]" &&
		eq(c.col("select to_id from links where from_id=?", day), []string{ids(old)}))
	var bl []string
	for _, r := range c.rows(s.d.Block("backlinks"), P{"page_id": nw}) {
		bl = append(bl, val(r[0])+"|"+val(r[3]))
	}
	s.K("...and counts as a backlink of the new page, one hop (cookbook/backlinks)", eq(sorted(bl), []string{"related|Baking", "wikilink|2026-09-29"}), bl)
	s.K("...the database is clean", c.integrityOK())
	c2 := s.fresh()
	o2, r2, _, _ := seedA(c2)
	n2, e1 := goRename(c2, o2, "Sourdough")
	_, e2 := goRename(c2, r2, "Rye")
	s.K("D: the writer's own rename writes the same rows as the recipe (A)", e1 == nil && e2 == nil && n2 == nw && state(c2) == state(c), e1, e2, state(c2), state(c))

	// ---- E  a category's page (D26): its metrics and the part-of links that end at it move too
	filed := func(c *C) string {
		return c.tab(`select mp.title, coalesce(p.title, '-') from metrics m join pages mp on mp.id = m.id
		                 left join links l on l.from_id = m.id and l.kind = 'part-of' left join pages p on p.id = l.to_id order by 1`)
	}
	seedE := func(c *C) (lipds, chol int64) {
		bio := c.page("Biomarkers")
		lipds = c.pageW("Lipds", nil, "Fats in the blood.")
		chol = c.page("Cholesterol")
		c.link(lipds, bio, "part-of")
		c.link(chol, lipds, "part-of")
		c.link(c.metric("Triglycerides", "mg/dL"), lipds, "part-of")
		return
	}
	c = s.fresh()
	lipds, chol := seedE(c)
	lip, why := c.docRename(st, sv, lipds, "Lipids")
	s.K("E: a category's page is renamed; its metrics are filed in the new page", why == "" && filed(c) == "Mood|-; Triglycerides|Lipids", why, filed(c))
	s.K("...its own part-of link moves, and so does its subcategory's", eq(outOf(c, lip), []string{"part-of|Biomarkers"}) && eq(outOf(c, chol), []string{"part-of|Lipids"}), outOf(c, lip), outOf(c, chol))
	s.K("...nothing typed ends at the stub", c.n("select count(*) from links where to_id=? and kind not in ('wikilink','redirect')", lipds) == 0)
	s.K("...the database is clean", c.integrityOK())
	c2 = s.fresh()
	l2, _ := seedE(c2)
	_, e5 := goRename(c2, l2, "Lipids")
	s.K("D: the writer's own rename writes the same rows as the recipe (E)", e5 == nil && state(c2) == state(c) && filed(c2) == filed(c), e5, state(c2), state(c))

	// ---- B  a typo ghost into an existing person
	seedB := func(c *C) (ghost, sam int64) {
		sam = c.named("person", "Sam", M{"name": "Sam Example"})
		tokyo := c.named("place", "Tokyo")
		d := c.dayPage("2026-09-30", "met [[Sm]]")
		ghost = c.page("Sm")
		c.link(d, ghost, "wikilink")
		c.link(ghost, tokyo, "about")
		c.link(ghost, sam, "related")
		return
	}
	c = s.fresh()
	gh, sam := seedB(c)
	np := c.n("select count(*) from pages")
	to, why2 := c.docRename(st, sv, gh, "sam")
	s.K("B: an empty ghost is renamed into the existing person: no new page", why2 == "" && to == sam && c.n("select count(*) from pages") == np, why2)
	s.K("...the stub names the person's own spelling and redirects to it", c.str("select body from pages where id=?", gh) == "#REDIRECT [[Sam]]" &&
		eq(outOf(c, gh), []string{"redirect|Sam"}), outOf(c, gh))
	s.K("...the ghost's typed links move to the person, and the one to the person itself goes, its mirror with it",
		eq(outOf(c, sam), []string{"about|Tokyo"}) && c.n("select count(*) from links where kind='related'") == 0, outOf(c, sam))
	s.K("...the day that wrote [[Sm]] names the person, one hop (cookbook/days-that-name)", eq(c.col(s.d.Block("days-that-name"), P{"entity_id": sam}), []string{"2026-09-30"}))
	s.K("...the database is clean", c.integrityOK())
	c2 = s.fresh()
	g2, _ := seedB(c2)
	t2, e := goRename(c2, g2, "sam")
	s.K("D: the writer's own rename writes the same rows as the recipe (B)", e == nil && t2 == to && state(c2) == state(c), e, state(c2), state(c))

	// ---- C  refusals
	seedC := func(c *C) (a, b, empty, stubbed, person, day int64) {
		a = c.pageW("Sourdough", nil, "Feed the starter.")
		b = c.pageW("Baking", nil, "Flour and water.")
		empty = c.page("Empty one")
		target := c.page("Bread")
		stubbed = c.pageW("Bred", nil, "#REDIRECT [[Bread]]")
		c.link(stubbed, target, "redirect")
		person = c.named("person", "Ana")
		day = c.dayPage("2026-10-01", "")
		return
	}
	c = s.fresh()
	a, b, empty, stubbed, person, dp := seedC(c)
	ghost := c.page("Breed")
	before := state(c)
	refused := func(id int64, title string) bool {
		_, why := c.docRename(st, sv, id, title)
		return why != "" && state(c) == before
	}
	s.K("C: a page with text is not renamed into a title that has text: refused, nothing written", refused(a, "Baking"))
	s.K("...nor into a title whose page is empty: two texts are never merged, and nothing is written", refused(a, "Empty one"))
	s.K("...an empty page is not renamed into a stub's title (one hop)", refused(ghost, "Bred"))
	s.K("...a stub, a person and a day page are not renamed, nor a page into its own key",
		refused(stubbed, "Bready") && refused(person, "Ana Sample") && refused(dp, "Day one") && refused(a, "SOURDOUGH"))
	c.must("BEGIN IMMEDIATE")
	c.must(st.stub, P{"old_id": a, "new_id": b})
	m1 := c.n("select changes()")
	c.must(st.stub, P{"old_id": a, "new_id": empty})
	m2 := c.n("select changes()")
	c.must("ROLLBACK")
	s.K("...and the stub step alone overwrites no text: it changes no row of a page with text whose text is not on the target", m1 == 0 && m2 == 0, m1, m2)
	c2 = s.fresh()
	a2, _, _, s2, p2, d2 := seedC(c2)
	gh2 := c2.page("Breed")
	var ex *core.ExistsError
	_, ea := goRename(c2, a2, "Baking")
	_, eb := goRename(c2, a2, "Empty one")
	all := errors.As(ea, &ex) && errors.As(eb, &ex)
	for _, x := range []struct {
		id    int64
		title string
	}{{gh2, "Bred"}, {s2, "Bready"}, {p2, "Ana Sample"}, {d2, "Day one"}, {a2, "SOURDOUGH"}} {
		_, err := goRename(c2, x.id, x.title)
		all = all && err != nil
	}
	s.K("D: the writer's own rename refuses the same renames and writes nothing (C)", all && state(c2) == before, ea, eb)
}

// The statements of cookbook/rename-a-page, each one statement of its block, in order.
type renameSteps struct {
	begin, selOld, selNew, ent, pg, stub, redirect, move, moveIn, dele, deleIn, commit string
}

func (s *S) renameSteps() renameSteps {
	sts := statements(s.d.Block("rename-a-page"))
	want := []string{"BEGIN IMMEDIATE", "SELECT", "SELECT", "INSERT INTO ENTITIES", "INSERT INTO PAGES", "UPDATE PAGES",
		"INSERT INTO LINKS", "INSERT INTO LINKS", "INSERT INTO LINKS", "DELETE FROM LINKS", "DELETE FROM LINKS", "COMMIT"}
	if len(sts) != len(want) {
		stop("cookbook/rename-a-page has %d statements, not %d", len(sts), len(want))
	}
	for i, w := range want {
		if !strings.HasPrefix(strings.ToUpper(code(sts[i])), w) {
			stop("cookbook/rename-a-page statement %d is not %s: %s", i, w, clip(code(sts[i]), 60))
		}
	}
	return renameSteps{sts[0], sts[1], sts[2], sts[3], sts[4], sts[5], sts[6], sts[7], sts[8], sts[9], sts[10], sts[11]}
}

// docRename is cookbook/rename-a-page run literally, with the refusals its steps 0, 1b and 2 name: the id of the
// page that holds the new title, or why it was refused (rolled back, nothing written). The new page's body goes
// through the link sync of cookbook/save-a-body.
func (c *C) docRename(st renameSteps, sv saveSteps, old int64, title string) (int64, string) {
	p := P{"old_id": old, "new_title": title, "new_key": text.TitleKey(title), "source": "ui"}
	refuse := func(why string) (int64, string) { c.must("ROLLBACK"); return 0, why }
	c.must(st.begin)
	o := c.rows(st.selOld, p) // entity_type, title_key, is_day_page, is_empty, deleted_at, is_stub
	switch {
	case len(o) != 1:
		return refuse("no such page")
	case o[0][0] != "page":
		return refuse("a person's or a place's title is its handle")
	case val(o[0][2]) == "1":
		return refuse("a day page")
	case o[0][4] != nil:
		return refuse("tombstoned")
	case val(o[0][5]) == "1":
		return refuse("a stub")
	case o[0][1] == p["new_key"]:
		return refuse("its own key")
	}
	if f := c.rows(st.selNew, p); len(f) == 0 { // 1a
		p["new_id"] = c.rows(st.ent, p)[0][0]
		c.must(st.pg, p)
		id := p["new_id"].(int64)
		c.docSave(sv, id, c.str("select body from pages where id=?", id), p["new_key"].(string), true)
	} else { // 1b
		switch {
		case val(o[0][3]) != "1":
			return refuse("taken, and the old page has text")
		case f[0][1] != nil:
			return refuse("taken by a tombstoned page")
		case val(f[0][2]) == "1":
			return refuse("taken by a stub")
		}
		p["new_id"] = f[0][0]
	}
	c.must(st.stub, p)
	if c.n("select changes()") != 1 {
		return refuse("the stub step changed no row")
	}
	for _, x := range []string{st.redirect, st.move, st.moveIn, st.dele, st.deleIn, st.commit} {
		c.must(x, p)
	}
	return p["new_id"].(int64), ""
}
