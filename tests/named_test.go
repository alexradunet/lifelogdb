package tests

import (
	"context"
	"regexp"
	"strings"
)

// named: people and places use one named identity; direct typed details and
// canonical promotion/lookup recipes preserve that identity (D16, D20).
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

	for i, typ := range []string{"person", "place"} {
		id := c.named(typ, "H "+typ)
		s.K("a "+typ+" is one id with its actual owned handle", c.n("SELECT count(*) FROM entities e JOIN entity_names n ON n.entity_id=e.id AND n.name_key=e.preferred_name_key WHERE e.id=? AND e.entity_type=?", id, typ) == 1 && c.n("SELECT count(*) FROM entities WHERE source<>'schema'") == int64(i+1))
	}
	s.K("a person detail without its entity is refused", err(c.tryx("INSERT INTO people(id,name) VALUES (9999,'x')")))
	plainOwner := c.page("Wrong extension owner")
	s.K("a person extension requires a person owner", err(c.tryx("INSERT INTO people(id,name) VALUES (?,'Wrong type')", plainOwner)))
	c.must("UPDATE entities SET body='not a cleanup ghost' WHERE id=?", plainOwner)
	s.K("a place needs no detail row without a point", c.n("SELECT count(*) FROM places") == 0 && c.n("SELECT count(*) FROM entities WHERE entity_type='place'") == 1)
	_, untitled := c.tryIdentity("person", "", nil)
	s.K("a named entity must be titled", untitled != nil)
	s.K("a person detail cannot claim another type", err(c.tryx("INSERT INTO people(id,entity_type,name) VALUES (?,'place','Mismatch')", c.anyIdentity("person"))))
	_, unknown := c.tryIdentity("task", "Task witness", nil)
	s.K("there is no task entity (D23)", unknown != nil && strings.Contains(unknown.Error(), "entities_entity_type"))
	half := c.anyIdentity("person")
	integrityBlocks := sqlBlocks(s.d.Page("contract/integrity-checks.md"))
	if len(integrityBlocks) != 1 {
		stop("missing literal integrity block")
	}
	integrityStatements := statements(integrityBlocks[0])
	if len(integrityStatements) != 7 {
		stop("unexpected integrity statement count")
	}
	s.K("a named person missing its extension is found by the semantic query", c.n("SELECT count(*) FROM people WHERE id=?", half) == 0 && contains(c.col(integrityStatements[2]), ids(half)))
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
		c.tab("select e.entity_type, pe.entity_type from entities e join people pe using(id) where id=?", g) == "person|person", r1, r2)
	s.K("...and the day page's link to it is kept (the id did not change)", c.tab("select to_id from links where from_id=?", mm) == ids(g))
	s.K("a promoted person cannot be turned back into a plain page (the people row's FK)", err(c.tryx("UPDATE entities SET entity_type='page' WHERE id=?", g)))
	s.K("...nor into a place", err(c.tryx("UPDATE entities SET entity_type='place' WHERE id=?", g)))
	dp := c.dayPage("2026-09-29", "x")
	s.K("a day page cannot become a person or a place: the cascade meets entities_day_page_plain",
		strings.Contains(c.tryx("UPDATE entities SET entity_type='person' WHERE id=?", dp), "entities_day_page_plain") &&
			strings.Contains(c.tryx("UPDATE entities SET entity_type='place' WHERE id=?", dp), "entities_day_page_plain"))

	_, datedPlace := c.tryIdentity("place", "2026-09-28", M{"day": "2026-09-28"})
	s.K("a place cannot own a canonical journal date", datedPlace != nil && strings.Contains(datedPlace.Error(), "entities_day_page_plain"))
	s.K("a page cannot become an unknown type", err(c.tryx("UPDATE entities SET entity_type='task' WHERE id=?", c.page("Tk"))))
	cf := s.freshWith(F{FKOff: true})
	_, badType := cf.tryIdentity("task", "Unknown kind", nil)
	s.K("entities.entity_type is checked without foreign keys", badType != nil && strings.Contains(badType.Error(), "entities_entity_type"))
	s.K("the owned name of a person cannot be deleted", err(c.tryx("DELETE FROM entity_names WHERE entity_id=?", g)))
	s.K("tombstoning a person retains its name", c.tryx("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", g) == "OK" && c.str("SELECT title FROM entity_names WHERE entity_id=?", g) == "Ana")

	// ---- B  cookbook/person-or-place run literally
	sts := statements(s.d.Block("person-or-place"))
	var sel0 []string
	at := -1
	for i, st := range sts {
		if strings.HasPrefix(strings.ToUpper(code(st)), "SELECT E.ID") {
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
		s.K("create: one id, a person with its page titled by the handle", c.tab("select e.entity_type,pe.entity_type,n.title,pe.name from entities e join entity_names n on n.entity_id=e.id and n.name_key=e.preferred_name_key join people pe on pe.id=e.id") ==
			"person|person|Bob Sample|Bob Sample" && pid != nil)
		s.K("step 0 now reports the handle as taken (entity_type person)", c.tab(sel0[0], P{"handle_key": "bob sample"}) == val(pid)+"|person|None")
		_, dupe := c.tryIdentity("page", "BOB SAMPLE", nil)
		s.K("the same handle cannot be made twice, in any case", dupe != nil && strings.Contains(dupe.Error(), "UNIQUE"))
		c.named("place", "Berlin")
		s.K("a place is created as its entity and its page", c.n("select count(*) from entities where entity_type in ('person','place')") == 2)
		gp := c.page("Lakeside")
		s.K("a plain page becomes a place by the UPDATE alone, its links kept", c.tryx(strings.Replace(promo[1], "'person'", "'place'", 1), P{"ghost_id": gp}) == "OK" &&
			c.str("SELECT entity_type FROM entities WHERE id=?", gp) == "place")
		// promote a ghost an earlier day page made
		c = s.fresh()
		mid := c.capture("Today I met [[Ana Example]]", "2026-09-30")
		gid := c.n("select entity_id from entity_names where name_key='ana example'")
		s.K("step 0 finds the ghost the day page made, as a plain page", c.tab(sel0[0], P{"handle_key": "ana example"}) == ids(gid)+"|page|None")
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
		s.K("promoting a day page is refused by entities_day_page_plain: the day stays the journal's page", strings.Contains(c.tryx(promo[1], P{"ghost_id": mid}), "entities_day_page_plain") &&
			c.str("SELECT entity_type FROM entities WHERE id=?", mid) == "page")
		s.K("...nor into a place", strings.Contains(c.tryx(strings.Replace(promo[1], "'person'", "'place'", 1), P{"ghost_id": mid}), "entities_day_page_plain"))
		t := c.page("Cleo Sample")
		c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", t)
		c.must(promo[1], P{"ghost_id": t})
		c.must(promo[2], P{"ghost_id": t})
		s.K("a tombstoned ghost is promoted and revived", c.n("select entity_type is 'person' and deleted_at is null from entities where id=?", t) == 1)

		prose := c.pageW("Dana", nil, "#REDIRECT [[Dana Sample (colleague)]]")
		_, proseErr := c.runBlock(strings.Join(promo, "\n"), P{"ghost_id": prose}, nil)
		s.K("REDIRECT prose is ordinary text during promotion", proseErr == nil && c.str("SELECT body FROM entities WHERE id=?", prose) == "#REDIRECT [[Dana Sample (colleague)]]", proseErr)

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
		c.n("select count(*) from entity_names where name_key='bob sample'") == 1)
	s.K("an alias does not change the target", c.tab("select to_id from links where from_id=?", m3) == ids(bod))
	s.K("the person's page body links out like any page", c.tab("select n.title from links l join entities e on e.id=l.to_id join entity_names n on n.entity_id=e.id and n.name_key=e.preferred_name_key where l.from_id=? and l.kind='wikilink'", bod) == "Cluj")
	ark := c.rows(s.d.Block("everything-about"), P{"entity_id": bod})
	arkSet := func(rows [][]any) []string {
		var out []string
		for _, r := range rows {
			out = append(out, val(r[0])+"|"+val(r[2])+"|"+val(r[3]))
		}
		return sorted(out)
	}
	cluj := c.n("select entity_id from entity_names where name_key='cluj'")
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

	renameID, renameErr := c.store().Rename(context.Background(), "ui", bod, "Bob S")
	s.K("a preferred-handle rename retains the person and original incident prose", renameErr == nil && renameID == bod && c.str("SELECT body FROM entities WHERE id=?", m1) == "Today I met [[Bob Sample]] and went with him for a coffee", renameErr)
	c.capture("Called [[Bob S]] and [[Bob Sample]]", "2026-10-02")
	s.K("both owned names resolve directly, without adopting another owner's typo", eq(c.col(q, P{"entity_id": bod}), []string{"2026-10-02", "2026-09-28"}) && c.n("SELECT count(*) FROM links WHERE from_id=? AND to_id=?", m4, bod) == 0)

	bl := c.rows(s.d.Block("backlinks"), P{"page_id": bod})
	var blTitles []string
	for _, r := range bl {
		blTitles = append(blTitles, val(r[3]))
	}
	s.K("cookbook/backlinks labels the backlinks of a person by title, a day page by its day, and counts the day that used both owned aliases",
		eq(sorted(blTitles), sorted([]string{"Coffee spots", "2026-09-28", "2026-10-02"})), tab(bl))
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
		for _, r := range c.rows("SELECT id,body FROM entities") {
			if r[1] != "" {
				c.editBody(r[0].(int64), r[1].(string))
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
		_, e := c.tryIdentity("page", t, nil)
		all = all && e != nil && strings.Contains(e.Error(), "UNIQUE")
	}
	s.K("a second Sam cannot take the handle Sam, in any case", all)
	s2 := c.named("person", "Sam (barber)", M{"name": "Sam"})
	ma, _ := c.savePage("Met [[Sam]]", "")
	mb, _ := c.savePage("Haircut with [[Sam (barber)]]", "")
	s.K("two people named Sam, told apart in the handle; each page reaches its own", c.tab("select to_id from links where from_id=?", ma) == ids(s1) && c.tab("select to_id from links where from_id=?", mb) == ids(s2))
	c.named("place", "Springfield (IL)")
	c.named("place", "Springfield (MA)")
	s.K("two places are told apart only once: Springfield (IL) and Springfield (MA)", c.n("select count(*) from entities where entity_type='place'") == 2)
	c.page("Typo page")
	tgt := c.page("Renamed target")
	st := c.pageW("Old target name", nil, "#REDIRECT [[Renamed target]]")
	if c.link(st, tgt, "wikilink") != "OK" {
		stop("ordinary redirect-text graph setup")
	}
	c.must("UPDATE entities SET created_at = strftime('%Y-%m-%dT%H:%M:%fZ','now','-40 day')")
	gh := c.col("select title from ghost_pages")
	gnew := c.page("Brand new")
	c.must("UPDATE entities SET created_at="+NOW+" WHERE id=?", gnew)
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
