package tests

import (
	"fmt"
	"regexp"
	"strings"
)

// dates: time (lifelog_meta.instants and .days, D10) — instants, local days and their round-trip CHECKs, why `IS`
// and not `=`, the zone, and created_at written by the writer.
func dates(s *S) {
	c := s.fresh()
	ENT := "INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES ('page',?,'2026-06-09T10:00:00.000Z','ui')"

	// ---- instants
	for _, bad := range []string{"2026-06-09 10:00:00.000", "2026-06-09T10:00:00Z", "2026-06-09T10:00:00.000", "2026-06-09T10:00:00.000+02:00", "2026-06-09T25:00:00.000Z", ""} {
		s.K(fmt.Sprintf("instant %q rejected on entities.created_at", bad), strings.HasPrefix(c.tryx(ENT, bad), "ERR"))
	}
	s.K("a well-formed instant is accepted", c.tryx("INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES ('page','2026-06-09T10:00:00.000Z','2026-06-09T10:00:00.000Z','ui')") == "OK")
	s.K("created_at has no default and is NOT NULL (writer-written)", c.tab("SELECT \"notnull\", dflt_value FROM pragma_table_info('entities') WHERE name='created_at'") == "1|None")
	s.K("strftime('%f') renders SS.SSS (three digits, rounded)", c.str("select strftime('%f','2026-06-09 21:14:03.482999')") == "03.483")
	s.K("whole-second input is widened to .000Z: one fixed width", c.str("select strftime('%Y-%m-%dT%H:%M:%fZ','2026-06-09T21:14:03Z')") == "2026-06-09T21:14:03.000Z")
	s.K("CURRENT_TIMESTAMP is second-precision and non-ISO", regexp.MustCompile(`^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d$`).MatchString(c.str("select CURRENT_TIMESTAMP")))
	s.K("same-second instants order by their fraction as plain text", c.n("select '2026-06-09T21:14:03.482Z' < '2026-06-09T21:14:03.483Z'") == 1)

	// ---- days, on every day column
	for _, bad := range []string{"2026-9-3", "2026-02-31", "banana", "2026-13-01", "20260903", "2026-09-03T00:00"} {
		n := s.next()
		i := c.ent("page")
		r := c.tryx("INSERT INTO pages(id,title,title_key,day) VALUES (?,?,?,?)", i, fmt.Sprintf("Day test %d", n), fmt.Sprintf("day test %d", n), bad)
		s.K(fmt.Sprintf("pages.day rejects %q", bad), strings.HasPrefix(r, "ERR"), r)
		r = c.tryx("INSERT INTO measurements(metric_id,day,value,source,created_at) VALUES (1,?,1,'ui','2026-01-01T00:00:00.000Z')", bad)
		s.K(fmt.Sprintf("measurements.day rejects %q", bad), strings.HasPrefix(r, "ERR"), r)
		r = c.tryx("UPDATE people SET birth_day=? WHERE id=?", bad, c.named("person", ""))
		s.K(fmt.Sprintf("people.birth_day rejects %q", bad), strings.HasPrefix(r, "ERR"), r)
	}
	// the IS in the round-trip: with = the same CHECK accepts garbage
	t := s.connect("")
	t.must("CREATE TABLE eq (d TEXT CHECK (date(d) = d)) STRICT")
	t.must("CREATE TABLE isx (d TEXT CHECK (date(d) IS d)) STRICT")
	s.K("date('2026-9-3') is NULL", t.n("select date('2026-9-3') is null") == 1)
	s.K("a CHECK with = accepts 2026-9-3 (NULL passes a CHECK)", t.tryx("INSERT INTO eq VALUES ('2026-9-3')") == "OK")
	s.K("the same CHECK with IS rejects it", strings.HasPrefix(t.tryx("INSERT INTO isx VALUES ('2026-9-3')"), "ERR"))
	eqCmp := regexp.MustCompile(`(?:date|strftime)\([^)]*\)\s*=\s*\w+`)
	s.K("every day and instant CHECK in schema uses IS, never =", !eqCmp.MatchString(code(s.ddl)), eqCmp.FindAllString(code(s.ddl), -1))

	// ---- the other instants and days
	ENT2 := "INSERT INTO entities(entity_type,created_at,updated_at,source) VALUES ('page','2026-01-01T00:00:00.000Z',?,'ui')"
	s.K("entities.updated_at without milliseconds refused", strings.HasPrefix(c.tryx(ENT2, "2026-01-01T00:00:00Z"), "ERR"))
	s.K("a tombstone that is not an instant refused", strings.HasPrefix(c.tryx("UPDATE entities SET deleted_at='2026-01-01' WHERE id=?", c.ent("page")), "ERR"))
	pa, pb := c.named("person", ""), c.named("person", "")
	s.K("measurements.created_at must be an instant", strings.HasPrefix(c.tryx("INSERT INTO measurements(metric_id,day,value,source,created_at) VALUES (1,'2026-01-01',1,'ui','2026-01-01 10:00:00')"), "ERR"))
	s.K("links.created_at must be an instant", strings.HasPrefix(c.tryx("INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES (?,?,'related','2026-01-01 10:00:00','ui')", pa, pb), "ERR"))
	s.K("people.death_day must round-trip", strings.HasPrefix(c.tryx("UPDATE people SET death_day='2026-9-3' WHERE id=?", pa), "ERR"))
	hab := c.metric("hab", "")
	s.K("habit_periods.end_day must round-trip", strings.HasPrefix(c.habit(hab, "2026-01-01", "2026-2-1"), "ERR"))

	// ---- zone
	for _, tz := range []any{"Europe/Berlin", "UTC", "America/Argentina/Buenos_Aires", "Etc/GMT+5", "America/Port-au-Prince", nil, strings.Repeat("x", 64)} {
		s.K(fmt.Sprintf("measurements.tz %.20v accepted", tz), c.measure(1, "2026-06-09", 3, M{"taken_at": "2026-06-09T22:30:00.000Z", "tz": tz}) == "OK")
	}
	for _, tz := range []string{"", "Europe Berlin", strings.Repeat("x", 65), "Europe/Berlin\n", "ünï/x", "a;b", "Europe/Berlin "} {
		s.K(fmt.Sprintf("measurements.tz %.14q rejected", tz), strings.HasPrefix(c.measure(1, "2026-06-09", 3, M{"tz": tz}), "ERR"))
	}
	s.K("only a timed reading has a zone: entities have no tz (D10)", c.n("select count(*) from pragma_table_info('entities') where name='tz'") == 0)
}
