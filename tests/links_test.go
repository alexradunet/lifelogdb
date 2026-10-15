package tests

import (
	"fmt"
	"strings"
)

// links: the graph (D8, D16, D26) — the closed link-kind registry, endpoint types, immutability, the INSERT OR
// REPLACE trap, and the three rules of at and part-of a registry row cannot say.
func links(s *S) {
	c := s.fresh()
	pa, pb, pl := c.named("person", ""), c.named("person", ""), c.named("place", "")
	m1 := c.dayPage("2026-09-30", "x")
	pw, pc := c.page("Wiki"), c.page("Garden")
	for _, x := range []struct {
		lbl, exp string
		f, t     any
		k        string
	}{
		{"friend is not a kind (RFC 0017)", "ERR", pa, pb, "friend"}, {"an unregistered kind (Wikilink)", "ERR", pa, pb, "Wikilink"}, {"lives-in is not a kind", "ERR", pa, pl, "lives-in"},
		{"attended is not a kind (no events, D22)", "ERR", pa, pw, "attended"}, {"is-a is not a kind (no events, D22)", "ERR", pw, pw, "is-a"},
		{"subtask is not a kind (D23)", "ERR", m1, pw, "subtask"}, {"spawned is not a kind (D23)", "ERR", pw, m1, "spawned"},
		{"wikilink day page->page", "OK", m1, pw, "wikilink"}, {"wikilink day page->person (a person is a page)", "OK", m1, pa, "wikilink"},
		{"wikilink day page->place", "OK", m1, pl, "wikilink"}, {"wikilink person page->page", "OK", pa, pw, "wikilink"},
		{"redirect is not a registered kind", "ERR", m1, pw, "redirect"},
		{"redirect page->person is refused", "ERR", pw, pa, "redirect"},
		{"redirect page->place is refused", "ERR", m1, pl, "redirect"}, {"redirect person->page (a stub is a plain page)", "ERR", pa, pw, "redirect"},
		{"about is not a kind (RFC 0017)", "ERR", m1, pb, "about"}, {"related is not a kind (RFC 0017)", "ERR", pw, pa, "related"},
		{"at day page->place", "OK", m1, pl, "at"}, {"at day page->person (at ends at a place)", "ERR", m1, pa, "at"}, {"at plain page->place (an at starts at a day page, D16)", "ERR", pw, pl, "at"},
		{"part-of metric->page (a category is a plain page, D26)", "OK", 1, pw, "part-of"}, {"part-of page->day page (a category is never a day page, D26)", "ERR", pw, m1, "part-of"},
		{"part-of plain page->page (a plain page is filed, D26)", "OK", pw, pc, "part-of"}, {"part-of person->page", "OK", pa, pc, "part-of"}, {"part-of place->page", "OK", pl, pc, "part-of"},
		{"part-of day page->page (a day page is never filed, D26)", "ERR", m1, pc, "part-of"},
		{"at person->place (at comes from a page)", "ERR", pa, pl, "at"}, {"at page->person", "ERR", pw, pa, "at"},
		{"visited is not a kind (D16)", "ERR", pa, pl, "visited"}, {"a dangling endpoint of a typed kind", "ERR", 9999, pl, "at"},
		{"located-in is not a kind (RFC 0017)", "ERR", pl, pl, "located-in"}, {"parent-of is not a kind (RFC 0017)", "ERR", pa, pb, "parent-of"},
	} {
		r := c.link(x.f, x.t, x.k)
		s.K("link "+x.lbl+": "+x.exp, strings.HasPrefix(r, x.exp), r)
	}
	s.K("a day page keeps its wikilink and its at link: only part-of is refused from it",
		c.n("SELECT count(*) FROM links WHERE from_id=? AND kind IN ('wikilink','at')", m1) == 4 && c.n("SELECT count(*) FROM links WHERE from_id=? AND kind='part-of'", m1) == 0)
	// ---- a filed page or a category never becomes a day page, and a link never moves onto one:
	// entity_names_day_insert refuses a date as an alias of another page, entities_day_identity a date as its
	// preferred key (a named page by its names, a page with no name yet by its part-of links),
	// entity_names_key_ascii a date as a new spelling of its key, and links_fixed any change of an endpoint
	q := c.page("Plans")
	s.K("a filed plain page", c.link(q, pc, "part-of") == "OK")
	s.K("...cannot take a day's name as an alias", strings.Contains(c.tryx("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,?,?)", q, "2026-10-01", "2026-10-01"), "only its canonical date name"))
	s.K("...cannot select a day's name as its preferred key", strings.Contains(c.tryx("UPDATE entities SET preferred_name_key='2026-10-01' WHERE id=?", q), "journal day identity"))
	s.K("...cannot respell its name as a date", strings.HasPrefix(c.tryx("UPDATE entity_names SET title='2026-10-01' WHERE entity_id=?", q), "ERR"))
	s.K("a part-of link cannot move onto a day page", strings.Contains(c.tryx("UPDATE links SET from_id=? WHERE from_id=? AND kind='part-of'", m1, q), "immutable"))
	s.K("...and no part-of link starts at a day page", c.n("SELECT count(*) FROM links l JOIN entities e ON e.id = l.from_id WHERE l.kind='part-of' AND e.is_journal") == 0 &&
		c.n("SELECT is_journal FROM entities WHERE id=?", q) == 0)
	// The entity row comes before its name row (the key's FK is deferred), so in one transaction a page with no
	// name yet is filed, or made a category, and then selects a date as its preferred key. Its names cannot refuse
	// that: it has none.
	nameless := func(title string) int64 {
		return c.n("INSERT INTO entities(entity_type,preferred_name_key,source,created_at,updated_at) VALUES ('page',?,'ui',"+NOW+","+NOW+") RETURNING id", title)
	}
	toDay := func(id int64, day string) string {
		return c.tryx("UPDATE entities SET preferred_name_key=?, day=? WHERE id=?", day, day, id)
	}
	for _, x := range []struct {
		lbl  string
		link func(e int64) string
		exp  string
	}{
		{"a page with no name yet, filed in a category, cannot become a day page (D26)", func(e int64) string { return c.link(e, pc, "part-of") }, "never becomes a day page"},
		{"a category with no name yet cannot become a day page (D26)", func(e int64) string { return c.link(pw, e, "part-of") }, "never becomes a day page"},
		{"a page with no name yet and no part-of link becomes a day page", func(int64) string { return "OK" }, "OK"},
	} {
		c.must("BEGIN IMMEDIATE")
		e := nameless("plans 2")
		linked := x.link(e)
		r := toDay(e, "2026-10-03")
		if r == "OK" {
			r += " " + c.tryx("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,?,?)", e, "2026-10-03", "2026-10-03")
		}
		s.K(x.lbl, linked == "OK" && strings.Contains(r, x.exp) && (x.exp != "OK" || r == "OK OK"), linked, r)
		c.must("ROLLBACK")
	}
	g, t := c.page("Sam Bee"), c.page("Sam B")
	s.K("a wikilink to a plain page", c.link(g, t, "wikilink") == "OK")
	promoted := c.tryx("UPDATE entities SET entity_type='person' WHERE id=?", t)
	if promoted == "OK" {
		c.must("INSERT INTO people(id,name) VALUES (?,?)", t, "Sam B")
	}
	c.must("DELETE FROM links WHERE from_id=? AND kind='wikilink'", g)
	s.K("...survives the promotion of its target: the same row inserts again", promoted == "OK" && c.link(g, t, "wikilink") == "OK")
	s.K("a duplicate edge is refused (UNIQUE from, to, kind)", strings.HasPrefix(c.link(m1, pl, "at"), "ERR"))
	s.K("a link is stored once, in its direction", c.n("select count(*) from links where kind='at'") == 1)
	s.K("links are immutable: kind", strings.Contains(c.tryx("UPDATE links SET kind='wikilink' WHERE kind='at'"), "immutable"))
	s.K("links are immutable: an endpoint", strings.Contains(c.tryx("UPDATE links SET to_id=? WHERE kind='at'", pb), "immutable"))
	s.K("a full-row update that changes only the note passes", c.tryx("UPDATE links SET note='hi', from_id=from_id, to_id=to_id, kind=kind WHERE kind='at'") == "OK")
	s.K("links are immutable: id", strings.Contains(c.tryx("UPDATE links SET id=id+10000 WHERE kind='at'"), "immutable"))
	s.K("links are immutable: created_at", strings.Contains(c.tryx("UPDATE links SET created_at='2020-01-01T00:00:00.000Z' WHERE kind='at'"), "immutable"))
	c.must("BEGIN IMMEDIATE")
	c.must("UPDATE links SET note='rolled back' WHERE kind='at'")
	c.must("ROLLBACK")
	s.K("a note edit rolled back restores the note", c.n("SELECT count(*) FROM links WHERE kind='at' AND note='hi'") == 1)
	c.must("DELETE FROM links WHERE kind='at'")
	s.K("a link is deleted, the one table whose rows are (D11)", c.n("select count(*) from links where kind='at'") == 0)

	// ---- the registry
	s.K("link_kinds: to_types is fixed", strings.Contains(c.tryx("UPDATE link_kinds SET to_types='person' WHERE kind='at'"), "fixed at registration"))
	s.K("link_kinds: from_types is fixed", strings.Contains(c.tryx("UPDATE link_kinds SET from_types='person' WHERE kind='at'"), "fixed at registration"))
	s.K("link_kinds: a note edit with from_types=from_types passes", c.tryx("UPDATE link_kinds SET note='n', from_types=from_types WHERE kind='at'") == "OK")
	s.K("a kind name in upper case is refused", strings.HasPrefix(c.tryx("INSERT INTO link_kinds(kind) VALUES ('Boss')"), "ERR"))
	s.K("a malformed type list is refused", strings.HasPrefix(c.tryx("INSERT INTO link_kinds(kind,from_types,to_types) VALUES ('k2','Person','page')"), "ERR"))
	s.K("a misspelt type token matches nothing: alone, every link of that kind is refused", c.tryx("INSERT INTO link_kinds(kind,from_types,to_types) VALUES ('godparent','persn','person')") == "OK" &&
		strings.HasPrefix(c.link(pa, pb, "godparent"), "ERR"))
	s.K("...beside a valid token, the valid one still admits its type (tokens match one by one)", c.tryx("INSERT INTO link_kinds(kind,from_types,to_types) VALUES ('mixed','person,persn','person')") == "OK" &&
		c.link(pa, pb, "mixed") == "OK" && strings.HasPrefix(c.link(c.named("place", ""), pb, "mixed"), "ERR"))

	// ---- a used kind cannot be replaced past link_kinds_structure_fixed: REPLACE deletes the row first
	cr := s.fresh()
	rd, rp := cr.dayPage("2026-09-30", "x"), cr.named("place", "")
	cr.link(rd, rp, "at")
	graph := func() string {
		return fmt.Sprint(cr.rows("SELECT id, from_id, to_id, kind, note FROM links ORDER BY id"))
	}
	before := graph()
	for _, r := range []struct{ label, sql string }{
		{"the endpoint types", "REPLACE INTO link_kinds(kind, from_types, to_types) VALUES ('at', 'person', 'person')"},
		{"nothing (same definition)", "REPLACE INTO link_kinds(kind, from_types, to_types) VALUES ('at', 'page', 'place')"},
		{"nothing, as INSERT OR REPLACE", "INSERT OR REPLACE INTO link_kinds(kind, from_types, to_types) VALUES ('at', 'page', 'place')"},
	} {
		s.K("REPLACE of a used kind changing "+r.label+" is refused by the links FK", strings.Contains(strings.ToUpper(cr.tryx(r.sql)), "FOREIGN KEY"))
	}
	s.K("...and the registry and graph are unchanged", cr.str("SELECT from_types || to_types FROM link_kinds WHERE kind='at'") == "pageplace" && graph() == before, graph())
	s.K("an unused kind may still be replaced", cr.tryx("REPLACE INTO link_kinds(kind, from_types, to_types) VALUES ('wikilink', 'page', 'page')") == "OK" &&
		cr.str("SELECT from_types FROM link_kinds WHERE kind='wikilink'") == "page")
	cn := s.freshWith(F{FKOff: true})
	s.K("with foreign_keys=OFF, a typed link to an id that does not exist is refused", strings.Contains(cn.link(cn.dayPage("2026-09-30", "x"), 99999, "at"), "endpoint type"))
	c2 := s.freshWith(F{FKOff: true})
	x, y := c2.named("person", ""), c2.named("person", "")
	s.K("with foreign_keys=OFF an unregistered kind is still refused (the trigger, in autocommit)", strings.Contains(c2.link(x, y, "nemesis"), "not registered"))
	c2.link(x, y, "wikilink")
	was := c2.tab("SELECT id FROM links WHERE kind='wikilink' ORDER BY id")
	replaced := c2.tryx("INSERT OR REPLACE INTO links(from_id,to_id,kind,created_at,source) VALUES (?,?,'wikilink',"+NOW+",'ui')", x, y)
	s.K("INSERT OR REPLACE on a link deletes the row and writes it again under a new id",
		replaced == "OK" && c2.n("SELECT count(*) FROM links WHERE kind='wikilink'") == 1 && c2.tab("SELECT id FROM links WHERE kind='wikilink' ORDER BY id") != was, replaced, was)
	s.K("ON CONFLICT DO NOTHING is the way", c2.tryx("INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES (?,?,'wikilink',"+NOW+",'ui') ON CONFLICT(from_id,to_id,kind) DO NOTHING", x, y) == "OK" && c2.n("SELECT count(*) FROM links") == 1)
}
