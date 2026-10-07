package tests

import (
	"context"
	"fmt"
	"strings"

	"lifelog/internal/core"
)

func schemaSafeguards(s *S) {
	schemaTextNUL(s)
	placePointOwnership(s)
	referencedGhosts(s)
	fixedDetailOwnership(s)
	fixedEntityCreation(s)
	searchAccentSemantics(s)
	schemaBounds(s)
}

func schemaTextNUL(s *S) {
	c := s.fresh()
	owner := c.page("Name owner")
	file := c.identity("file", "ui", "Hash witness", nil, "")
	habit := c.metric("Habit source witness", "")
	type probe struct {
		constraint, valid, query string
		args                     []any
	}
	probes := []probe{
		{"entities_source", "cli", "INSERT INTO entities(entity_type,preferred_name_key,source,created_at,updated_at) VALUES('page','source witness',?," + NOW + "," + NOW + ")", nil},
		{"entity_names_key_folded", "café", "INSERT INTO entity_names(entity_id,title,name_key) VALUES(?,'Café',?)", []any{owner}},
		{"files_sha256", strings.Repeat("a", 64), "INSERT INTO files(id,sha256,mime) VALUES(?,?,'image/jpeg')", []any{file}},
		{"files_mime", "image/jpeg", "INSERT INTO files(id,sha256,mime) VALUES(?,'" + strings.Repeat("a", 64) + "',?)", []any{file}},
		{"measurements_tz", "UTC", "INSERT INTO measurements(metric_id,day,value,source,created_at,tz) VALUES(1,'2020-01-01',3,'ui'," + NOW + ",?)", nil},
		{"measurements_source", "cli", "INSERT INTO measurements(metric_id,day,value,created_at,source) VALUES(1,'2020-01-01',3," + NOW + ",?)", nil},
		{"habit_periods_source", "cli", "INSERT INTO habit_periods(metric_id,start_day,source) VALUES(?,'2020-01-01',?)", []any{habit}},
		{"links_source", "cli", "INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES(?,1,'related'," + NOW + ",?)", []any{owner}},
		{"link_kinds_kind", "custom", "INSERT INTO link_kinds(kind) VALUES(?)", nil},
		{"link_kinds_from_types", "page", "INSERT INTO link_kinds(kind,from_types) VALUES('custom',?)", nil},
		{"link_kinds_to_types", "page", "INSERT INTO link_kinds(kind,to_types) VALUES('custom',?)", nil},
	}
	for _, p := range probes {
		for _, suffix := range []string{"", "\x00", "\x00hidden"} {
			c.must("BEGIN IMMEDIATE")
			args := append(append([]any{}, p.args...), p.valid+suffix)
			result := c.tryx(p.query, args...)
			c.must("ROLLBACK")
			if suffix == "" {
				s.K(p.constraint+" valid control accepted", result == "OK", result)
			} else {
				label := p.constraint + " rejects embedded NUL"
				if suffix == "\x00" {
					label = p.constraint + " rejects trailing NUL"
				}
				s.K(label, strings.Contains(result, p.constraint), result)
			}
		}
	}
	s.K("SQLite TEXT length and GLOB ignore a NUL suffix", c.n("SELECT length('UTC'||char(0)||'!')=3 AND ('UTC'||char(0)||'!') NOT GLOB '*[^A-Za-z0-9_/+-]*'") == 1)
}

func placePointOwnership(s *S) {
	c := s.fresh()
	a, b := c.named("place", "Point owner"), c.named("place", "Unlocated place")
	c.must("INSERT INTO places(id,lat,lon,radius_m) VALUES(?,1,2,30)", a)
	state := func() string {
		return c.tab("SELECT id,lat,lon,radius_m,link_days FROM places ORDER BY id") + " / " + c.tab("SELECT id,revision,updated_at FROM entities ORDER BY id")
	}
	for _, alias := range []string{"id", "rowid", "_rowid_", "oid"} {
		for _, combined := range []bool{false, true} {
			before := state()
			c.must("BEGIN IMMEDIATE")
			q := "UPDATE places SET " + alias + "=?"
			if combined {
				q += ",lat=3"
			}
			r := c.tryx(q+" WHERE id=?", b, a)
			s.K(fmt.Sprintf("place %s combined=%v ownership refusal is atomic", alias, combined), strings.Contains(r, "immutable") && state() == before, r)
			c.must("ROLLBACK")
		}
		before := state()
		r := c.tryx("UPDATE places SET "+alias+"=id WHERE id=?", a)
		s.K("place "+alias+" ownership no-op preserves revisions", r == "OK" && state() == before, r)
	}
	before := c.n("SELECT revision FROM entities WHERE id=?", a)
	c.must("UPDATE places SET lat=3 WHERE id=?", a)
	s.K("place point edits still advance owner revision", c.n("SELECT revision FROM entities WHERE id=?", a) > before && c.n("SELECT lat=3 FROM places WHERE id=?", a) == 1)
}

func referencedGhosts(s *S) {
	c := s.fresh()
	store := c.store()
	ctx := context.Background()
	// Insert already-old synthetic identities, then capture through the writer.
	kind := c.page("Sleep")
	c.dayPage("2020-01-02", "")
	err := store.Do(ctx, "cli", func(tx *core.Tx) error {
		_, _, e := tx.CaptureSession(core.SessionInput{Kind: "Sleep", Day: "2020-01-01", StartAt: "2020-01-01T00:00:00.000Z"})
		return e
	})
	if err != nil {
		stop("ghost session: %v", err)
	}
	mood := float64(3)
	day, _, err := store.Capture(ctx, "cli", "2020-01-02", "", &mood)
	if err != nil {
		stop("ghost mood capture: %v", err)
	}
	unused := c.page("Unused typo")
	s.K("ghost pages exclude retained session kinds", c.n("SELECT count(*) FROM ghost_pages WHERE id=?", kind) == 0)
	s.K("ghost pages exclude measurement capture provenance", c.n("SELECT count(*) FROM ghost_pages WHERE id=?", day) == 0)
	s.K("ghost pages retain unused empty page control", c.n("SELECT count(*) FROM ghost_pages WHERE id=?", unused) == 1)
	c.must("UPDATE sessions SET deleted_at="+NOW+" WHERE kind_id=?", kind)
	reading := c.n("SELECT id FROM measurements WHERE captured_with_id=?", day)
	if _, _, err := store.Correct(ctx, "cli", reading, nil); err != nil {
		stop("ghost retraction: %v", err)
	}
	s.K("ghost pages retain historical session references", c.n("SELECT count(*) FROM ghost_pages WHERE id=?", kind) == 0)
	s.K("ghost pages retain retracted capture provenance", c.n("SELECT count(*) FROM ghost_pages WHERE id=?", day) == 0)
}

// Each probe uses a matching typed destination with no detail row, so refusal
// must come from the identity guard rather than a duplicate key or foreign key.
func fixedDetailOwnership(s *S) {
	for _, tc := range []struct{ table, typ, edit string }{
		{"people", "person", ",name='Changed'"},
		{"files", "file", ",preview=x'FFD8FF01'"},
		{"metrics", "metric", ",unit=unit"},
		{"habit_periods", "metric", ",end_day='2026-12-31'"},
	} {
		c := s.fresh()
		a := c.named(tc.typ, "Original owner")
		b := c.identity(tc.typ, "ui", "Other owner", nil, "")
		if tc.table == "habit_periods" {
			c.must("INSERT INTO habit_periods(id,metric_id,start_day,source) VALUES(?,?,'2026-01-01','ui')", a, a)
		}
		state := func() string {
			return c.tab("SELECT * FROM "+tc.table+" ORDER BY id") + " / " + c.tab("SELECT id,revision,updated_at FROM entities ORDER BY id")
		}
		for _, alias := range []string{"id", "rowid", "_rowid_", "oid"} {
			for _, combined := range []bool{false, true} {
				before := state()
				c.must("BEGIN IMMEDIATE")
				q := "UPDATE " + tc.table + " SET " + alias + "=?"
				if combined {
					q += tc.edit
				}
				r := c.tryx(q+" WHERE id=?", b, a)
				s.K(fmt.Sprintf("%s %s combined=%v identity refusal is atomic", tc.table, alias, combined), r != "OK" && state() == before, r)
				c.must("ROLLBACK")
			}
			before := state()
			r := c.tryx("UPDATE "+tc.table+" SET "+alias+"=id WHERE id=?", a)
			s.K(tc.table+" "+alias+" identity no-op preserves revisions", r == "OK" && state() == before, r)
		}
	}
}

func fixedEntityCreation(s *S) {
	c := s.fresh()
	id := c.page("Creation evidence")
	before := c.tab("SELECT * FROM entities ORDER BY id")
	for _, extra := range []string{"", ",body='Changed'"} {
		r := c.tryx("UPDATE entities SET created_at='2000-01-01T00:00:00.000Z'"+extra+" WHERE id=?", id)
		s.K("entity creation time immutable"+extra, strings.Contains(r, "never changed") && c.tab("SELECT * FROM entities ORDER BY id") == before, r)
	}
	s.K("entity creation no-op allowed", c.tryx("UPDATE entities SET created_at=created_at WHERE id=?", id) == "OK" && c.tab("SELECT * FROM entities ORDER BY id") == before)
	c.must("UPDATE entities SET body='Allowed edit' WHERE id=?", id)
	s.K("ordinary body edit preserves creation time", c.str("SELECT created_at FROM entities WHERE id=?", id) == "2026-01-01T00:00:00.000Z" && c.n("SELECT revision FROM entities WHERE id=?", id) > 1)
}

func searchAccentSemantics(s *S) {
	c := s.fresh()
	id := c.pageW("Search witness", nil, "ộ ș ț ă â î")
	s.K("FTS strips compound Latin diacritics", c.n("SELECT count(*) FROM entities_fts WHERE entities_fts MATCH 'o' AND rowid=?", id) == 1)
	s.K("FTS strips Romanian diacritics", c.n("SELECT count(*) FROM entities_fts WHERE entities_fts MATCH 's t a i' AND rowid=?", id) == 1)
	decomposed := c.pageW("Decomposed witness", nil, "ộ")
	s.K("FTS strips decomposed diacritics", c.n("SELECT count(*) FROM entities_fts WHERE entities_fts MATCH 'o' AND rowid=?", decomposed) == 1)
	street := c.page("Straße")
	s.K("full-folded exact key resolves sharp s", c.n("SELECT entity_id FROM entity_names WHERE name_key='strasse'") == street)
	s.K("FTS token folding differs from name identity", c.n("SELECT count(*) FROM entities_fts WHERE entities_fts MATCH 'strasse' AND rowid=?", street) == 0 && c.n("SELECT count(*) FROM entities_fts WHERE entities_fts MATCH 'Straße' AND rowid=?", street) == 1)
}
