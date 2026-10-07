package tests

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"lifelog/internal/core"
)

// dates: time (lifelog_meta.instants and .days, D10) — instants, local days and their round-trip CHECKs, why `IS`
// and not `=`, the zone, and created_at written by the writer.
func dates(s *S) {
	exactTimeBoundaries(s)
	fractional := s.fresh()
	s.K("measurements.created_at accepts a UTC instant with milliseconds", fractional.tryx("INSERT INTO measurements(metric_id,day,value,source,created_at) VALUES (1,'2026-01-01',3,'ui','2026-01-01T12:34:56.123Z')") == "OK")

	c := s.fresh()
	instantProbe := func(value string) error {
		_, err := c.tryIdentity("page", fmt.Sprintf("Instant witness %d", s.next()), M{"created_at": value})
		return err
	}

	// ---- instants
	for _, bad := range []string{"2026-06-09 10:00:00.000", "2026-06-09T10:00:00Z", "2026-06-09T10:00:00.000", "2026-06-09T10:00:00.000+02:00", "2026-06-09T25:00:00.000Z", ""} {
		s.K(fmt.Sprintf("instant %q rejected on entities.created_at", bad), instantProbe(bad) != nil)
	}
	s.K("a well-formed instant is accepted", instantProbe("2026-06-09T10:00:00.000Z") == nil)
	s.K("created_at has no default and is NOT NULL (writer-written)", c.tab("SELECT \"notnull\", dflt_value FROM pragma_table_info('entities') WHERE name='created_at'") == "1|None")
	s.K("strftime('%f') renders SS.SSS (three digits, rounded)", c.str("select strftime('%f','2026-06-09 21:14:03.482999')") == "03.483")
	s.K("whole-second input is widened to .000Z: one fixed width", c.str("select strftime('%Y-%m-%dT%H:%M:%fZ','2026-06-09T21:14:03Z')") == "2026-06-09T21:14:03.000Z")
	s.K("CURRENT_TIMESTAMP is second-precision and non-ISO", regexp.MustCompile(`^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d$`).MatchString(c.str("select CURRENT_TIMESTAMP")))
	s.K("same-second instants order by their fraction as plain text", c.n("select '2026-06-09T21:14:03.482Z' < '2026-06-09T21:14:03.483Z'") == 1)

	// ---- days, on every day column
	for _, bad := range []string{"2026-9-3", "2026-02-31", "banana", "2026-13-01", "20260903", "2026-09-03T00:00"} {
		i := c.anyIdentity("page")
		r := c.tryx("UPDATE entities SET day=? WHERE id=?", bad, i)
		s.K(fmt.Sprintf("entities.day rejects %q", bad), strings.HasPrefix(r, "ERR"), r)
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
	_, updateErr := c.tryIdentity("page", "Updated instant witness", M{"updated_at": "2026-01-01T00:00:00Z"})
	s.K("entities.updated_at without milliseconds refused", updateErr != nil)
	s.K("a tombstone that is not an instant refused", strings.Contains(c.tryx("UPDATE entities SET deleted_at='2026-01-01' WHERE id=?", c.anyIdentity("page")), "entities_deleted_at"))
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

// The contract is the only copy of boundary values; both writer and DDL are
// compared with its independently specified answers on every exact-time column.
func exactTimeBoundaries(s *S) {
	vectors := regexp.MustCompile(`(?m)^\| (day|instant) \| `+"`([^`]*)`"+` \| (yes|no) \|$`).FindAllStringSubmatch(s.d.Page("contract/exact-time.md"), -1)
	s.K("exact-time contract contains boundary vectors", len(vectors) >= 20)
	// One pristine fixture is enough: each column probe rolls back its own savepoint,
	// including successful writes under a mutant, before the next independent case.
	c := s.fresh()
	// Created at the earliest instant, so that every valid vector is also a tombstone that follows creation
	// (entities_deleted_after_created): these probes test the shape of deleted_at, not its order.
	entity, err := c.tryIdentity("page", "Time witness", M{"created_at": "0000-01-01T00:00:00.000Z"})
	if err != nil {
		stop("time witness: %v", err)
	}
	person := c.named("person", "")
	habit := c.metric("Habit boundary", "")
	otherHabit := c.metric("Other habit", "")
	c.must("INSERT INTO habit_periods(metric_id,start_day,source) VALUES(?,'0000-01-01','ui')", habit)
	sessionKind := c.page("Session calendar kind")
	c.must("BEGIN IMMEDIATE")
	for _, v := range vectors {
		typ, value, want := v[1], v[2], v[3] == "yes"
		accepted := core.IsDay(value)
		if typ == "instant" {
			accepted = core.IsInstant(value)
		}
		s.K(fmt.Sprintf("writer exact %s %q agrees with vector", typ, value), accepted == want)
		queries := map[string]string{}
		if typ == "day" {
			queries["entities.day"] = fmt.Sprintf("UPDATE entities SET day=? WHERE id=%d", entity)
			queries["people.birth_day"] = fmt.Sprintf("UPDATE people SET birth_day=? WHERE id=%d", person)
			queries["people.death_day"] = fmt.Sprintf("UPDATE people SET death_day=? WHERE id=%d", person)
			queries["measurements.day"] = "INSERT INTO measurements(metric_id,day,value,source,created_at) VALUES(1,?,3,'ui','2026-01-01T00:00:00.000Z')"
			queries["habit_periods.start_day"] = fmt.Sprintf("INSERT INTO habit_periods(metric_id,start_day,source) VALUES(%d,?,'ui')", otherHabit)
			queries["habit_periods.end_day"] = fmt.Sprintf("UPDATE habit_periods SET end_day=? WHERE metric_id=%d", habit)
		} else {
			queries["entities.created_at"] = "" // creation evidence is tested at insert, never by rewriting it
			for _, col := range []string{"updated_at", "deleted_at"} {
				queries["entities."+col] = fmt.Sprintf("UPDATE entities SET %s=? WHERE id=%d", col, entity)
			}
			queries["measurements.created_at"] = "INSERT INTO measurements(metric_id,day,value,source,created_at) VALUES(1,'2026-01-01',3,'ui',?)"
			queries["measurements.taken_at"] = "INSERT INTO measurements(metric_id,day,taken_at,value,source,created_at) VALUES(1,'2026-01-01',?,3,'ui','2026-01-01T00:00:00.000Z')"
			queries["links.created_at"] = fmt.Sprintf("INSERT INTO links(from_id,to_id,kind,source,created_at) VALUES(%d,%d,'about','ui',?)", entity, person)
		}

		if typ == "day" {
			queries["sessions.day"] = fmt.Sprintf("INSERT INTO sessions(kind_id,day,start_at,source,created_at,updated_at) VALUES(%d,?,'2026-01-01T00:00:00.000Z','ui','2026-01-01T00:00:00.000Z','2026-01-01T00:00:00.000Z')", sessionKind)
		} else {
			queries["sessions.start_at"] = fmt.Sprintf("INSERT INTO sessions(kind_id,day,start_at,source,created_at,updated_at) VALUES(%d,'2026-01-01',?,'ui','2026-01-01T00:00:00.000Z','2026-01-01T00:00:00.000Z')", sessionKind)
			queries["sessions.end_at"] = fmt.Sprintf("INSERT INTO sessions(kind_id,day,start_local,end_at,source,created_at,updated_at) VALUES(%d,'2026-01-01','2026-01-01T00:00:00.000',?,'ui','2026-01-01T00:00:00.000Z','2026-01-01T00:00:00.000Z')", sessionKind)
			for _, col := range []string{"created_at", "updated_at", "deleted_at"} {
				columns := []string{"kind_id", "day", "start_at", "source", "created_at", "updated_at"}
				values := []string{fmt.Sprint(sessionKind), "'2026-01-01'", "'2026-01-01T00:00:00.000Z'", "'ui'", "'2026-01-01T00:00:00.000Z'", "'2026-01-01T00:00:00.000Z'"}
				if col == "deleted_at" { // created at the earliest instant: the probe tests shape, not order
					columns = append(columns, col)
					values = append(values, "?")
					values[4] = "'0000-01-01T00:00:00.000Z'"
				} else {
					for i, name := range columns {
						if name == col {
							values[i] = "?"
						}
					}
				}
				queries["sessions."+col] = "INSERT INTO sessions(" + strings.Join(columns, ",") + ") VALUES(" + strings.Join(values, ",") + ")"
			}
		}
		keys := make([]string, 0, len(queries))
		for col := range queries {
			keys = append(keys, col)
		}
		sort.Strings(keys)
		for _, col := range keys {
			c.must("SAVEPOINT boundary_probe")
			got := "OK"
			if col == "entities.created_at" {
				if _, err := c.tryIdentity("page", "Creation boundary", M{"created_at": value}); err != nil {
					got = "ERR " + err.Error()
				}
			} else {
				got = c.tryx(queries[col], value)
			}
			c.must("ROLLBACK TO boundary_probe")
			c.must("RELEASE boundary_probe")
			s.K(fmt.Sprintf("exact %s %q accepted=%v", col, value, want), (got == "OK") == want, got)
		}
	}
	c.must("ROLLBACK")
}
