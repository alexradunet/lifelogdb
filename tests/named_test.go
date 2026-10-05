package tests

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"lifelog/internal/core"
	"lifelog/internal/text"
)

// named: a person or a place is a page (schema.sql, D16, D20) — one id with an entities row and a titled pages
// row, and a person's people row, chained people -> pages -> entities.
// A  the foreign keys and CHECKs: what may and may not be built, and what a promotion may and may not do;
// B  cookbook/person-or-place run literally: create, promote a ghost, a taken handle, never a day page;
// C  a day page that writes [[Name]] reaches the person through the writer's save; cookbook/days-that-name,
//
//	cookbook/backlinks, cookbook/everything-about; renames; rebuild;
//
// D  two Sams, two Springfields; ghost_pages leaves named pages alone.
func named(s *S) {
	err := func(r string) bool { return strings.HasPrefix(r, "ERR") }

	// ---- A  structure
	c := s.fresh()
	for i, t := range []string{"person", "place"} {
		id := c.named(t, "H "+t)
		s.K("a "+t+" is one id: entities.entity_type and pages.entity_type agree",
			c.n("select e.entity_type = p.entity_type from entities e join pages p using(id) where id=?", id) == 1 && c.n("select count(*) from entities where source <> 'schema'") == int64(i+1))
	}
	s.K("a person without a page is refused (its FK points at pages)", err(c.tryx("INSERT INTO people(id,name) VALUES (?,?)", c.ent("person"), "x")))
	s.K("a place needs no row of its own: its page is the place, and a places row only gives it a point (D16, D21)",
		c.n("select count(*) from sqlite_schema where name = 'holdings'") == 0 && c.n("select count(*) from places") == 0 &&
			c.n("select count(*) from entities where entity_type = 'place'") == 1)
	s.K("a named page must be titled", err(c.tryx("INSERT INTO pages(id,entity_type,day) VALUES (?, 'person', '2026-09-30')", c.ent("person"))))
	s.K("the page and the entity must agree on the type", err(c.tryx("INSERT INTO pages(id,entity_type,title,title_key) VALUES (?, 'place', 'Mismatch', 'mismatch')", c.ent("person"))))
	s.K("there is no task entity (D23)", err(c.tryx("INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES ('task',"+NOW+","+NOW+",'ui')")))
	s.K("a person row with no people row, only a page, is what the orphan query is for (the FKs allow the page alone)",
		c.tryx("INSERT INTO pages(id,entity_type,title,title_key) VALUES (?, 'person', 'Half', 'half')", c.ent("person")) == "OK")
	s.K("a death before the birth is refused", err(c.tryx("UPDATE people SET birth_day='2000-01-02', death_day='2000-01-01' WHERE id=?", c.named("person", ""))))
	cols := func(t string) []string { return c.col("select name from pragma_table_info(?)", t) }
	s.K("people keep name, and have no nickname and no note", contains(cols("people"), "name") && !contains(cols("people"), "nickname") && !contains(cols("people"), "note"))
	s.K("entities has no page_id column", !contains(cols("entities"), "page_id"))
	// promotion and its limits
	c = s.fresh()
	g := c.page("Ana")
	mm := c.dayPage("2026-09-30", "[[Ana]]")
	c.link(mm, g, "wikilink")
	r1 := c.tryx("UPDATE entities SET entity_type='person' WHERE id=? AND entity_type='page'", g)
	r2 := c.tryx("INSERT INTO people(id,name) VALUES (?, 'Ana Example')", g)
	s.K("a plain page is promoted: UPDATE entities.entity_type cascades to pages.entity_type, then the people row", r1 == "OK" && r2 == "OK" &&
		c.tab("select e.entity_type, p.entity_type from entities e join pages p using(id) where id=?", g) == "person|person", r1, r2)
	s.K("...and the day page's link to it is kept (the id did not change)", c.tab("select to_id from links where from_id=?", mm) == ids(g))
	s.K("a promoted person cannot be turned back into a plain page (the people row's FK)", err(c.tryx("UPDATE entities SET entity_type='page' WHERE id=?", g)))
	s.K("...nor into a place", err(c.tryx("UPDATE entities SET entity_type='place' WHERE id=?", g)))
	dp := c.dayPage("2026-09-29", "x")
	s.K("a day page cannot become a person or a place: the cascade meets pages_day_page_plain",
		strings.Contains(c.tryx("UPDATE entities SET entity_type='person' WHERE id=?", dp), "pages_day_page_plain") &&
			strings.Contains(c.tryx("UPDATE entities SET entity_type='place' WHERE id=?", dp), "pages_day_page_plain"))
	s.K("a person or a place cannot be titled with a day, even with that day", strings.Contains(c.tryx(
		"INSERT INTO pages(id,entity_type,title,title_key,day) VALUES (?, 'place', '2026-09-28', '2026-09-28', '2026-09-28')", c.ent("place")), "pages_day_page_plain"))
	s.K("a page cannot become an unknown type", err(c.tryx("UPDATE entities SET entity_type='task' WHERE id=?", c.page("Tk"))))
	cf := s.freshWith(F{FKOff: true})
	s.K("pages.entity_type is checked even on a connection without foreign keys (pages_entity_type)",
		strings.Contains(cf.tryx("INSERT INTO pages(id,entity_type,title,title_key) VALUES (?, 'task', 'Tk', 'tk')", cf.ent("page")), "pages_entity_type"))
	s.K("the page of a person cannot be deleted", err(c.tryx("DELETE FROM pages WHERE id=?", g)))
	s.K("tombstoning the person keeps its page", c.tryx("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", g) == "OK" && c.str("select title from pages where id=?", g) == "Ana")
	s.K("integrity and foreign keys clean", c.integrityOK())

	// ---- B  cookbook/person-or-place run literally
	sts := statements(s.d.Block("person-or-place"))
	var sel0 []string
	at := -1
	for i, st := range sts {
		if strings.HasPrefix(strings.ToUpper(code(st)), "SELECT P.ID") {
			sel0 = append(sel0, st)
			at = i
		}
	}
	var create, promo []string
	if len(sel0) == 1 {
		create = window(sts, at+1, 5)
		promo = window(sts, at+6, 4)
	}
	firstWord := regexp.MustCompile(`\w+`)
	words := func(xs []string) string {
		var out []string
		for _, x := range xs {
			out = append(out, strings.ToUpper(firstWord.FindString(code(x))))
		}
		return strings.Join(out, " ")
	}
	s.K("cookbook/person-or-place has a resolve, a five-statement create and a four-statement promotion",
		len(sel0) == 1 && len(sts) == 10 && words(create) == "BEGIN INSERT INSERT INSERT COMMIT" && words(promo) == "BEGIN UPDATE INSERT COMMIT", words(sts))
	if len(sel0) == 1 && len(create) == 5 && len(promo) == 4 {
		c = s.fresh()
		p := P{"handle_title": "Bob Sample", "handle_key": "bob sample"}
		s.K("step 0 finds nothing for a new handle", c.tab(sel0[0], P{"handle_key": p["handle_key"]}) == "")
		for _, st := range create {
			if _, e := c.runBlock(st, p, nil); e != nil {
				stop("person-or-place create: %v", e)
			}
		}
		pid := p["person_id"]
		s.K("create: one id, a person with its page titled by the handle", c.tab("select e.entity_type, p.entity_type, p.title, pe.name from entities e join pages p using(id) join people pe using(id)") ==
			"person|person|Bob Sample|Bob Sample" && pid != nil)
		s.K("step 0 now reports the handle as taken (entity_type person)", c.tab(sel0[0], P{"handle_key": "bob sample"}) == val(pid)+"|person|None|0")
		s.K("the same handle cannot be made twice, in any case", strings.Contains(c.tryx("INSERT INTO pages(id,title,title_key) VALUES (?, 'BOB SAMPLE', 'bob sample')", c.ent("page")), "title_key"))
		c.named("place", "Berlin")
		s.K("a place is created as its entity and its page", c.n("select count(*) from pages where entity_type in ('person', 'place')") == 2)
		gp := c.page("Lakeside")
		s.K("a plain page becomes a place by the UPDATE alone, its links kept", c.tryx(strings.Replace(promo[1], "'person'", "'place'", 1), P{"ghost_id": gp}) == "OK" &&
			c.tab("select e.entity_type, p.entity_type from entities e join pages p using(id) where id=?", gp) == "place|place")
		// promote a ghost an earlier day page made
		c = s.fresh()
		mid := c.capture("Today I met [[Ana Example]]", "2026-09-30")
		gid := c.n("select id from pages where title_key='ana example'")
		s.K("step 0 finds the ghost the day page made, as a plain page", c.tab(sel0[0], P{"handle_key": "ana example"}) == ids(gid)+"|page|None|0")
		_, promotionErr := c.runBlock(strings.Join(promo, "\n"), P{"ghost_id": gid}, nil)
		if promotionErr != nil {
			c.tryx("ROLLBACK")
		}
		s.K("the promotion block turns it into a person, one id", promotionErr == nil && c.tab("select e.entity_type, pe.name from entities e join people pe using(id) where id=?", gid) == "person|Ana Example", promotionErr)
		s.K("...and the old day page already names her, no re-save needed (cookbook/days-that-name)", eq(c.col(s.d.Block("days-that-name"), P{"entity_id": gid}), []string{"2026-09-30"}))
		s.K("promoting a page that is already a person changes no row, and the people insert fails", c.tryx(promo[1], P{"ghost_id": gid}) == "OK" &&
			c.n("select changes()") == 0 && err(c.tryx(promo[2], P{"ghost_id": gid})))
		pl := c.named("place", "Lisbon")
		s.K("promoting a place's page into a person changes no row, and the people insert fails on its FK", c.tryx(promo[1], P{"ghost_id": pl}) == "OK" && err(c.tryx(promo[2], P{"ghost_id": pl})))
		s.K("promoting a day page is refused by pages_day_page_plain: the day stays the journal's page", strings.Contains(c.tryx(promo[1], P{"ghost_id": mid}), "pages_day_page_plain") &&
			c.str("select e.entity_type || p.entity_type from entities e join pages p using(id) where id=?", mid) == "pagepage")
		s.K("...nor into a place", strings.Contains(c.tryx(strings.Replace(promo[1], "'person'", "'place'", 1), P{"ghost_id": mid}), "pages_day_page_plain"))
		t := c.page("Cleo Sample")
		c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", t)
		c.tryx(promo[1], P{"ghost_id": t})
		c.tryx(promo[2], P{"ghost_id": t})
		s.K("a tombstoned ghost is promoted and revived", c.n("select entity_type is 'person' and deleted_at is null from entities where id=?", t) == 1)
		nw := c.page("Dana Sample (colleague)")
		stub := c.pageW("Dana", nil, "#REDIRECT [[Dana Sample (colleague)]]")
		c.link(stub, nw, "redirect")
		s.K("a redirect stub is not promoted: the UPDATE changes no row and the people insert fails", c.tryx(promo[1], P{"ghost_id": stub}) == "OK" &&
			c.n("select changes()") == 0 && err(c.tryx(promo[2], P{"ghost_id": stub})))
	}

	// ---- C  the save contract reaches the person
	c = s.fresh()
	bod := c.named("person", "Bob Sample", M{"name": "Bob Sample"})
	m1 := c.capture("Today I met [[Bob Sample]] and went with him for a coffee", "2026-09-28")
	c.capture("Nothing about him today", "2026-09-29")
	m3 := c.capture("[[Bob Sample|Bob]] called", "2026-09-30")
	m4 := c.capture("Bob was here, but I wrote [[Bbo Sample]]", "2026-10-01")
	wiki := c.page("Coffee spots")
	c.editBody(wiki, "Best one: [[Bob Sample]] goes there")
	c.editBody(bod, "Colleague since 2019. Lives in [[Cluj]].")
	rome := c.named("place", "Rome")
	c.link(bod, rome, "about")
	s.K("the day page links to the person's own id, and no new page was made", c.tab("select to_id from links where from_id=? and kind='wikilink'", m1) == ids(bod) &&
		c.n("select count(*) from pages where title_key='bob sample'") == 1)
	s.K("an alias does not change the target", c.tab("select to_id from links where from_id=?", m3) == ids(bod))
	s.K("the person's page body links out like any page", c.tab("select p.title from links l join pages p on p.id=l.to_id where l.from_id=? and l.kind='wikilink'", bod) == "Cluj")
	ark := c.rows(s.d.Block("everything-about"), P{"entity_id": bod})
	arkSet := func(rows [][]any) []string {
		var out []string
		for _, r := range rows {
			out = append(out, val(r[0])+"|"+val(r[2])+"|"+val(r[3]))
		}
		return sorted(out)
	}
	cluj := c.n("select id from pages where title='Cluj'")
	s.K("cookbook/everything-about finds the day pages and the page that name him, what his page links to, and what it is about", eq(arkSet(ark),
		sorted([]string{"wikilink|" + ids(m1) + "|in", "wikilink|" + ids(m3) + "|in", "wikilink|" + ids(wiki) + "|in", "wikilink|" + ids(cluj) + "|out", "about|" + ids(rome) + "|out"})), tab(ark))
	typo := false
	for _, r := range ark {
		typo = typo || r[2] == m4
	}
	s.K("...and not the typo", !typo)
	s.K("cookbook/everything-about is two legs (the third leg through a separate page is gone)", strings.Count(s.d.Block("everything-about"), "UNION ALL") == 1)
	q := s.d.Block("days-that-name")
	s.K("cookbook/days-that-name lists the day pages only, newest day first", eq(c.col(q, P{"entity_id": bod}), []string{"2026-09-30", "2026-09-28"}))
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", m3)
	s.K("...and drops a tombstoned day", eq(c.col(q, P{"entity_id": bod}), []string{"2026-09-28"}))
	bb := c.n("select id from pages where title_key='bbo sample'")
	c.editBody(bb, "#REDIRECT [[Bob Sample]]")
	c.link(bb, bod, "redirect")
	s.K("...and a day that wrote a misspelt name now redirected to him (one hop)", eq(c.col(q, P{"entity_id": bod}), []string{"2026-10-01", "2026-09-28"}))
	bl := c.rows(s.d.Block("backlinks"), P{"page_id": bod})
	var blTitles []string
	for _, r := range bl {
		blTitles = append(blTitles, val(r[3]))
	}
	s.K("cookbook/backlinks labels the backlinks of a person by title, a day page by its day, and counts the day that named him through a redirected misspelling",
		eq(sorted(blTitles), sorted([]string{"Coffee spots", "2026-09-28", "2026-10-01"})), tab(bl))
	ana := c.named("person", "Ana Example", M{"name": "Ana Example"})
	c.link(bod, ana, "friend")
	ark = c.rows(s.d.Block("everything-about"), P{"entity_id": bod})
	var friends []string
	for _, r := range ark {
		if r[0] == "friend" {
			friends = append(friends, val(r[0])+"|"+val(r[2])+"|"+val(r[3]))
		}
	}
	s.K("...and a friend once, not twice", eq(friends, []string{"friend|" + ids(ana) + "|in"}), tab(ark))
	wl := "select from_id, to_id from links where kind='wikilink' order by 1, 2"
	before := c.tab(wl)
	c.must("UPDATE people SET name='Bob S.' WHERE id=?", bod)
	resave := func() {
		for _, r := range c.rows("select id, body from pages") {
			if r[1] != "" {
				id := r[0].(int64)
				if id == bb {
					links := c.tab("select from_id, to_id, kind from links where from_id=? or to_id=? order by 1,2,3", id, id)
					e := c.store().Do(context.Background(), "ui", func(tx *core.Tx) error { _, e := tx.SetBody(id, r[1].(string)); return e })
					var refusal *core.Error
					s.K("a redirect stub refuses resave and preserves its body and links", errors.As(e, &refusal) && refusal.Status == 409 && c.str("select body from pages where id=?", id) == r[1].(string) && c.tab("select from_id, to_id, kind from links where from_id=? or to_id=? order by 1,2,3", id, id) == links)
				} else {
					c.editBody(id, r[1].(string))
				}
			}
		}
	}
	resave()
	s.K("renaming the person (people.name) drops no link: the link is to the id, the handle is the title", c.tab(wl) == before)
	c.must("DELETE FROM links WHERE kind='wikilink'")
	resave()
	s.K("a rebuild from the bodies alone gives back every link, people included", c.tab(wl) == before)

	// ---- D  two Sams, two Springfields, the ghost view
	c = s.fresh()
	s1 := c.named("person", "Sam", M{"name": "Sam"})
	all := true
	for _, t := range []string{"Sam", "SAM", "sam"} {
		all = all && strings.Contains(c.tryx("INSERT INTO pages(id,title,title_key) VALUES (?, ?, ?)", c.ent("page"), t, text.TitleKey(t)), "title_key")
	}
	s.K("a second Sam cannot take the handle Sam, in any case", all)
	s2 := c.named("person", "Sam (barber)", M{"name": "Sam"})
	ma, _ := c.savePage("Met [[Sam]]", "")
	mb, _ := c.savePage("Haircut with [[Sam (barber)]]", "")
	s.K("two people named Sam, told apart in the handle; each page reaches its own", c.tab("select to_id from links where from_id=?", ma) == ids(s1) && c.tab("select to_id from links where from_id=?", mb) == ids(s2))
	c.named("place", "Springfield (IL)")
	c.named("place", "Springfield (MA)")
	s.K("two places are told apart only once: Springfield (IL) and Springfield (MA)", c.n("select count(*) from pages where entity_type='place'") == 2)
	c.page("Typo page")
	tgt := c.page("Renamed target")
	st := c.pageW("Old target name", nil, "#REDIRECT [[Renamed target]]")
	c.link(st, tgt, "redirect")
	c.must("UPDATE entities SET created_at = strftime('%Y-%m-%dT%H:%M:%fZ','now','-40 day')")
	gh := c.col("select title from ghost_pages")
	gnew := c.page("Brand new")
	gdead := c.page("Tombstoned ghost")
	c.must("UPDATE entities SET created_at = strftime('%Y-%m-%dT%H:%M:%fZ','now','-40 day') WHERE id=?", gdead)
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", gdead)
	gh2 := c.col("select id from ghost_pages")
	s.K("ghost_pages leaves a page younger than 30 days alone", !contains(gh2, ids(gnew)), gh2)
	s.K("ghost_pages leaves a tombstoned page alone", !contains(gh2, ids(gdead)), gh2)
	s.K("ghost_pages lists the real ghost and none of the person or place pages, and not a page a rename points at", eq(gh, []string{"Typo page"}), gh)
}

// window is up to n items of xs from i.
func window(xs []string, i, n int) []string {
	if i >= len(xs) {
		return nil
	}
	return xs[i:min(i+n, len(xs))]
}
