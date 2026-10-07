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
	kind, _, err := store.CreatePage(ctx, "cli", "Sleep", "")
	if err != nil {
		stop("ghost session kind: %v", err)
	}
	err = store.Do(ctx, "cli", func(tx *core.Tx) error {
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
	c.must("UPDATE entities SET created_at='2000-01-01T00:00:00.000Z' WHERE id IN (?,?,?)", kind, day, unused)
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
