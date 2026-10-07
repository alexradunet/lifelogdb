package tests

import (
	"fmt"
	"strings"
)

func editRevisions(s *S) {
	c := s.fresh()
	page := c.page("Revision witness")
	revision := func(id int64) int64 { return c.n("SELECT revision FROM entities WHERE id=?", id) }
	stamp := func(id int64) string { return c.str("SELECT updated_at FROM entities WHERE id=?", id) }
	s.K("an edit revision starts positive", revision(page) > 0)

	// ---- creation is not an edit: the entity, its owned preferred name and its typed row are one write
	created := true
	for i := range 25 { // cookbook/capture's two statements, the clock supplied by the statement
		id := c.addPage(fmt.Sprintf("Created page %d", i), nil, "body", autoKey)
		created = created && id != 0 && revision(id) == 1 && c.str("SELECT updated_at = created_at FROM entities WHERE id=?", id) == "1"
	}
	s.K("creation is not an edit: a page made by cookbook/capture's entity and owned-name statements is at revision 1 with updated_at = created_at", created)
	var edited []string
	for _, typ := range []string{"page", "person", "place", "metric", "file", "period"} {
		id := c.identity(typ, "ui", "Created "+typ, nil, "")
		switch typ {
		case "person", "metric", "file":
			c.domain(typ, id, nil)
		case "place":
			c.must("INSERT INTO places(id,lat,lon,radius_m) VALUES(?,1,2,30)", id)
		case "period":
			c.must("INSERT INTO periods(id) VALUES(?)", id)
		}
		if revision(id) != 1 || c.str("SELECT updated_at = created_at FROM entities WHERE id=?", id) != "1" {
			edited = append(edited, typ)
		}
	}
	s.K("creation is not an edit: a page, person, place with a point, metric, file and period stay at revision 1 with updated_at = created_at after their typed row", len(edited) == 0, edited)
	renamed := c.page("Rename witness")
	renameBefore := revision(renamed)
	c.must("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,'Renamed witness','renamed witness')", renamed)
	c.must("UPDATE entities SET preferred_name_key='renamed witness' WHERE id=?", renamed)
	s.K("a rename, the new spelling and then its selection, advances revision", revision(renamed) > renameBefore)
	nameBefore := revision(page)
	c.must("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,'Revision alias','revision alias')", page)
	s.K("owned alias insertion advances revision", revision(page) > nameBefore)
	nameBefore = revision(page)
	c.must("UPDATE entity_names SET title='REVISION ALIAS' WHERE entity_id=? AND name_key='revision alias'", page)
	s.K("equivalent owned spelling edit advances revision", revision(page) > nameBefore)
	nameBefore = revision(page)
	c.must("UPDATE entities SET preferred_name_key='revision alias' WHERE id=?", page)
	s.K("already-owned preferred selection advances revision", revision(page) > nameBefore)
	nameBefore = revision(page)
	c.must("UPDATE entity_names SET title=title WHERE entity_id=?", page)
	c.must("UPDATE entities SET preferred_name_key=preferred_name_key WHERE id=?", page)
	c.must("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,'REVISION ALIAS','revision alias') ON CONFLICT(name_key) DO NOTHING", page)
	s.K("owned spelling selection and conflict no-ops preserve revision", revision(page) == nameBefore)
	c.must("BEGIN IMMEDIATE")
	c.must("INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,'Rollback alias','rollback alias')", page)
	c.must("UPDATE entity_names SET title='Revision Alias' WHERE name_key='revision alias'")
	c.must("UPDATE entities SET preferred_name_key='rollback alias' WHERE id=?", page)
	c.must("ROLLBACK")
	s.K("owned name changes rollback spelling selection and revision", revision(page) == nameBefore && c.str("SELECT preferred_name_key FROM entities WHERE id=?", page) == "revision alias" && c.str("SELECT title FROM entity_names WHERE name_key='revision alias'") == "REVISION ALIAS" && c.n("SELECT count(*) FROM entity_names WHERE name_key='rollback alias'") == 0)
	_, zeroErr := c.tryIdentity("page", "Zero revision", M{"revision": 0})
	s.K("zero revision refused", zeroErr != nil && strings.Contains(zeroErr.Error(), "entities_revision"))
	c.must("UPDATE entities SET revision=5 WHERE id=?", page)
	s.K("revision decrease refused", strings.HasPrefix(c.tryx("UPDATE entities SET revision=4 WHERE id=?", page), "ERR"))
	before := revision(page)
	c.must("UPDATE entities SET body=body WHERE id=?", page)
	s.K("body no-op preserves revision", revision(page) == before)
	c.must("BEGIN IMMEDIATE")
	c.must("UPDATE entities SET body='rollback' WHERE id=?", page)
	c.must("ROLLBACK")
	s.K("rollback restores revision", revision(page) == before)
	c.must("UPDATE entities SET body='committed' WHERE id=?", page)
	s.K("body changes advance revision", revision(page) > before)
	before = revision(page)
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", page)
	s.K("lifecycle changes advance revision", revision(page) > before)
	before = revision(page)
	c.must("UPDATE entities SET updated_at='2020-01-01T00:00:00.000Z' WHERE id=?", page)
	s.K("clock alone does not advance revision", revision(page) == before)
	for _, typ := range []string{"person", "metric", "file"} {
		id := c.identity(typ, "ui", "Detail "+typ, nil, "")
		c.domain(typ, id, nil)
		if typ == "person" {
			before = revision(id)
			c.must("UPDATE people SET name='changed' WHERE id=?", id)
			s.K("person details advance revision", revision(id) > before)
		}
		if typ == "file" {
			before = revision(id)
			c.must("UPDATE files SET preview=? WHERE id=?", jpegBytes, id)
			s.K("file details advance revision", revision(id) > before)
		}
	}
	place := c.named("place", "")
	c.must("INSERT INTO places(id,lat,lon,radius_m) VALUES(?,1,2,30)", place)
	before = revision(place)
	c.must("UPDATE places SET lat=3 WHERE id=?", place)
	s.K("place details advance revision", revision(place) > before)
	metric := c.metric("Revision habit", "")
	before = revision(metric)
	c.must("INSERT INTO habit_periods(metric_id,start_day,source) VALUES(?,'2026-01-01','ui')", metric)
	s.K("habit insertion advances revision", revision(metric) > before)
	before = revision(metric)
	c.must("UPDATE habit_periods SET end_day='2026-01-02' WHERE metric_id=?", metric)
	s.K("habit edits advance revision", revision(metric) > before)
	other := c.page("Other endpoint")
	// a link is an edit of the page it leaves and of no page it points at
	before, otherBefore, otherStamp := revision(page), revision(other), stamp(other)
	c.must("INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES(?,?,'part-of',"+NOW+",'ui')", page, other)
	s.K("a link insertion advances the page it leaves", revision(page) > before)
	s.K("a link insertion advances no page it points at", revision(other) == otherBefore && stamp(other) == otherStamp)
	before = revision(page)
	c.must("UPDATE links SET note='note' WHERE from_id=? AND to_id=?", page, other)
	s.K("a link note edit advances the page it leaves", revision(page) > before)
	s.K("a link note edit advances no page it points at", revision(other) == otherBefore && stamp(other) == otherStamp)
	before = revision(page)
	c.must("DELETE FROM links WHERE from_id=? AND to_id=?", page, other)
	s.K("a link deletion advances the page it leaves", revision(page) > before)
	s.K("a link deletion advances no page it points at", revision(other) == otherBefore && stamp(other) == otherStamp)
	// a wikilink is derived from the body, whose own change advanced the page
	before, otherBefore = revision(page), revision(other)
	pageStamp, otherStamp := stamp(page), stamp(other)
	untouched := func() bool {
		return revision(page) == before && stamp(page) == pageStamp && revision(other) == otherBefore && stamp(other) == otherStamp
	}
	c.must("INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES(?,?,'wikilink',"+NOW+",'ui')", page, other)
	s.K("a wikilink insertion advances no page: it is derived from the body", untouched())
	c.must("DELETE FROM links WHERE from_id=? AND to_id=? AND kind='wikilink'", page, other)
	s.K("a wikilink deletion advances no page: it is derived from the body", untouched())
	// the two rows of a symmetric link are two inserts, two note updates, two deletes: both pages advance
	ana, bob := c.named("person", "Revision Ana"), c.named("person", "Revision Bob")
	ra, rb := revision(ana), revision(bob)
	c.must("INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES(?,?,'friend',"+NOW+",'ui')", ana, bob)
	s.K("a symmetric link insertion advances both pages: the mirror row is its own insert", revision(ana) > ra && revision(bob) > rb)
	ra, rb = revision(ana), revision(bob)
	c.must("UPDATE links SET note='shared' WHERE from_id=? AND to_id=? AND kind='friend'", ana, bob)
	s.K("a symmetric link note edit advances both pages: the mirror note is its own update", revision(ana) > ra && revision(bob) > rb)
	ra, rb = revision(ana), revision(bob)
	c.must("DELETE FROM links WHERE from_id=? AND to_id=? AND kind='friend'", ana, bob)
	s.K("a symmetric link deletion advances both pages: the mirror row is its own delete", revision(ana) > ra && revision(bob) > rb)
	c.must("UPDATE entities SET revision=9223372036854775807 WHERE id=?", page)
	s.K("revision exhaustion refuses edit", strings.HasPrefix(c.tryx("UPDATE entities SET body='overflow' WHERE id=?", page), "ERR") && c.str("SELECT body FROM entities WHERE id=?", page) == "committed" && revision(page) == 9223372036854775807)
}
