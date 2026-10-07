package tests

import (
	"context"
	"strings"
	"time"
)

// links: the graph (D8, D16) — the closed link-kind registry, endpoint types, symmetric mirrors, immutability,
// containment (cookbook/inside-a-place) with its cycle guard, and the INSERT OR REPLACE trap.
func links(s *S) {
	c := s.fresh()
	pa, pb, pl := c.named("person", ""), c.named("person", ""), c.named("place", "")
	m1 := c.dayPage("2026-09-30", "x")
	pw := c.page("Wiki")
	for _, x := range []struct {
		lbl, exp string
		f, t     any
		k        string
	}{
		{"friend person-person", "OK", pa, pb, "friend"}, {"an unregistered kind (Friend)", "ERR", pa, pb, "Friend"}, {"lives-in is not a kind", "ERR", pa, pl, "lives-in"},
		{"attended is not a kind (no events, D22)", "ERR", pa, pw, "attended"}, {"is-a is not a kind (no events, D22)", "ERR", pw, pw, "is-a"}, {"friend person->place", "ERR", pa, pl, "friend"},
		{"subtask is not a kind (D23)", "ERR", m1, pw, "subtask"}, {"spawned is not a kind (D23)", "ERR", pw, m1, "spawned"},
		{"wikilink day page->page", "OK", m1, pw, "wikilink"}, {"wikilink day page->person (a person is a page)", "OK", m1, pa, "wikilink"},
		{"wikilink day page->place", "OK", m1, pl, "wikilink"}, {"wikilink person page->page", "OK", pa, pw, "wikilink"},
		{"redirect is not a registered kind", "ERR", m1, pw, "redirect"},
		{"redirect page->person is refused", "ERR", pw, pa, "redirect"},
		{"redirect page->place is refused", "ERR", m1, pl, "redirect"}, {"redirect person->page (a stub is a plain page)", "ERR", pa, pw, "redirect"},
		{"about day page->person", "OK", m1, pb, "about"}, {"about person->place", "OK", pa, pl, "about"},
		{"about day page->page", "ERR", m1, pw, "about"}, {"related page-person", "OK", pw, pa, "related"},
		{"at day page->place", "OK", m1, pl, "at"}, {"at person->place (at comes from a page)", "ERR", pa, pl, "at"}, {"at page->person", "ERR", pw, pa, "at"},
		{"visited is not a kind (D16)", "ERR", pa, pl, "visited"}, {"a dangling endpoint of a typed kind", "ERR", 9999, pl, "at"},
		{"located-in place->person", "ERR", pl, pa, "located-in"}, {"parent-of person->place", "ERR", pa, pl, "parent-of"},
	} {
		r := c.link(x.f, x.t, x.k)
		s.K("link "+x.lbl+": "+x.exp, strings.HasPrefix(r, x.exp), r)
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
	s.K("a symmetric kind is stored in both directions", c.n("select count(*) from links where kind='friend'") == 2)
	s.K("links are immutable: kind", strings.Contains(c.tryx("UPDATE links SET kind='related' WHERE kind='friend'"), "immutable"))
	s.K("links are immutable: an endpoint", strings.Contains(c.tryx("UPDATE links SET to_id=? WHERE kind='at'", pb), "immutable"))
	s.K("a full-row update that changes only the note passes", c.tryx("UPDATE links SET note='hi', from_id=from_id, to_id=to_id, kind=kind WHERE kind='at'") == "OK")
	s.K("links are immutable: id", strings.Contains(c.tryx("UPDATE links SET id=id+10000 WHERE kind='friend'"), "immutable"))
	s.K("links are immutable: created_at", strings.Contains(c.tryx("UPDATE links SET created_at='2020-01-01T00:00:00.000Z' WHERE kind='friend'"), "immutable"))
	c.must("UPDATE links SET note='shared' WHERE kind='friend' AND from_id=?", pa)
	s.K("symmetric note edit mirrors forward", c.n("SELECT count(*) FROM links WHERE kind='friend' AND note='shared'") == 2)
	c.must("UPDATE links SET note=NULL WHERE kind='friend' AND from_id=?", pb)
	s.K("symmetric note edit mirrors reverse NULL", c.n("SELECT count(*) FROM links WHERE kind='friend' AND note IS NULL") == 2)
	s.K("symmetric note no-op terminates", c.tryx("UPDATE links SET note=note WHERE kind='friend'") == "OK")
	c.must("BEGIN IMMEDIATE")
	c.must("UPDATE links SET note='rolled back' WHERE kind='friend' AND from_id=?", pa)
	c.must("ROLLBACK")
	s.K("symmetric note rollback restores both directions", c.n("SELECT count(*) FROM links WHERE kind='friend' AND note IS NULL") == 2)
	c.must("DELETE FROM links WHERE kind='friend' AND from_id=?", pa)
	s.K("deleting one side of a symmetric edge deletes its mirror", c.n("select count(*) from links where kind='friend'") == 0)

	// ---- the registry
	s.K("link_kinds: symmetric is fixed (by the trigger: located-in could be symmetric by its CHECK)", strings.Contains(c.tryx("UPDATE link_kinds SET symmetric=1 WHERE kind='located-in'"), "fixed at registration"))
	s.K("link_kinds: to_types is fixed", strings.Contains(c.tryx("UPDATE link_kinds SET to_types='person' WHERE kind='about'"), "fixed at registration"))
	s.K("link_kinds: a note edit with symmetric=symmetric passes", c.tryx("UPDATE link_kinds SET note='n', symmetric=symmetric, from_types=from_types WHERE kind='at'") == "OK")
	s.K("link_kinds.symmetric is 0 or 1", strings.HasPrefix(c.tryx("INSERT INTO link_kinds(kind, symmetric) VALUES ('x-test', 2)"), "ERR"))
	s.K("a kind name in upper case is refused", strings.HasPrefix(c.tryx("INSERT INTO link_kinds(kind,symmetric) VALUES ('Boss',0)"), "ERR"))
	s.K("a symmetric kind with different endpoint types is refused", strings.HasPrefix(c.tryx("INSERT INTO link_kinds(kind,symmetric,from_types,to_types) VALUES ('mentor',1,'person','place')"), "ERR"))
	s.K("a malformed type list is refused", strings.HasPrefix(c.tryx("INSERT INTO link_kinds(kind,symmetric,from_types,to_types) VALUES ('k2',0,'Person','page')"), "ERR"))
	s.K("a misspelt type token fails closed: every link of that kind is refused", c.tryx("INSERT INTO link_kinds(kind,symmetric,from_types,to_types) VALUES ('godparent',0,'persn','person')") == "OK" &&
		strings.HasPrefix(c.link(pa, pb, "godparent"), "ERR"))
	cn := s.freshWith(F{FKOff: true})
	s.K("with foreign_keys=OFF, a typed link to an id that does not exist is refused", strings.Contains(cn.link(cn.dayPage("2026-09-30", "x"), 99999, "at"), "endpoint type"))
	c2 := s.freshWith(F{FKOff: true})
	x, y := c2.named("person", ""), c2.named("person", "")
	s.K("with foreign_keys=OFF an unregistered kind is still refused (the trigger, in autocommit)", strings.Contains(c2.link(x, y, "nemesis"), "not registered"))
	s.K("the mirrors terminate under recursive_triggers=ON", c2.link(x, y, "family") == "OK" && c2.n("select count(*) from links where kind='family'") == 2 &&
		c2.tryx("DELETE FROM links WHERE kind='family' AND from_id=?", x) == "OK" && c2.n("select count(*) from links") == 0)
	c2.link(x, y, "friend")
	s.K("INSERT OR REPLACE on a symmetric link: too many levels of trigger recursion", strings.Contains(
		c2.tryx("INSERT OR REPLACE INTO links(from_id,to_id,kind,created_at,source) VALUES (?,?,'friend',"+NOW+",'ui')", x, y), "too many levels of trigger recursion"))
	s.K("ON CONFLICT DO NOTHING is the way", c2.tryx("INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES (?,?,'friend',"+NOW+",'ui') ON CONFLICT(from_id,to_id,kind) DO NOTHING", x, y) == "OK")

	// ---- containment (cookbook/inside-a-place) and one-way kinds
	c = s.fresh()
	japan, kanto, tokyo := c.named("place", "Japan"), c.named("place", "Kanto"), c.named("place", "Tokyo")
	s.K("Tokyo located-in Kanto located-in Japan", c.link(tokyo, kanto, "located-in") == "OK" && c.link(kanto, japan, "located-in") == "OK")
	s.K("located-in is one-way (no mirror)", c.n("select count(*) from links where kind='located-in'") == 2)
	kid, par := c.named("person", ""), c.named("person", "")
	s.K("parent-of keeps its direction", c.link(par, kid, "parent-of") == "OK" && c.n("select count(*) from links where kind='parent-of'") == 1)
	d1 := c.dayPage("2019-04-02", "landed in Tokyo")
	c.link(d1, tokyo, "at")
	d2 := c.dayPage("2019-04-05", "a day in Japan and Kanto")
	c.link(d2, japan, "at")
	c.link(d2, kanto, "at")
	d3 := c.dayPage("2018-12-31", "Tokyo again")
	c.link(d3, tokyo, "at")
	osaka := c.named("place", "Osaka")
	d4 := c.dayPage("2019-06-01", "not linked to Japan: Osaka")
	c.link(d4, osaka, "at")
	d5 := c.dayPage("2019-07-01", "planning [[Tokyo]]")
	c.link(d5, tokyo, "wikilink")
	p := P{"place_id": japan, "from_day": "2019-01-01", "to_day": "2019-12-31"}
	const want = "2019-04-02|Tokyo; 2019-04-05|Japan; 2019-04-05|Kanto"
	q := s.d.Block("inside-a-place")
	s.K("cookbook/inside-a-place finds the 2019 days at a place in Japan, not another year, a place outside or a day that only names it", c.tab(q, p) == want, c.tab(q, p))
	typo := c.named("place", "Tokio (typo)")
	c.link(typo, japan, "located-in")
	d6 := c.dayPage("2019-04-09", "mistyped place")
	c.link(d6, typo, "at")
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", typo)
	s.K("cookbook/inside-a-place leaves out a tombstoned place and its days", c.tab(q, p) == want, c.tab(q, p))
	s.K("cookbook/inside-a-place asked about a tombstoned place itself lists nothing", c.tab(q, P{"place_id": typo, "from_day": "2019-01-01", "to_day": "2019-12-31"}) == "")
	c.link(japan, tokyo, "located-in")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second) // a walk that never ends is interrupted
	defer cancel()
	r, e := c.queryCtx(ctx, q, p)
	s.K("cookbook/inside-a-place terminates on a cycle (UNION)", e == nil && tab(r) == want, e, tab(r))
}
