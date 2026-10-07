package tests

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

func sessionFixture(c *C, kind int64, extra M) int64 {
	m := M{"kind_id": kind, "day": "2020-01-02", "start_at": "2020-01-01T23:00:00.000Z", "source": "ui", "created_at": "2026-01-01T00:00:00.000Z", "updated_at": "2026-01-01T00:00:00.000Z"}
	for k, v := range extra {
		m[k] = v
	}
	cols, marks, vals := m.split()
	return c.rows("INSERT INTO sessions("+strings.Join(cols, ",")+") VALUES("+marks+") RETURNING id", vals...)[0][0].(int64)
}

func recordedSessions(s *S) {

	isolated := s.fresh()
	person := isolated.named("person", "Kind ownership witness")
	isolated.must("DROP TRIGGER sessions_kind_insert")
	s.K("session kind discriminator requires page", isolated.tryx("INSERT INTO sessions(kind_id,kind_entity_type,day,start_at,source,created_at,updated_at) VALUES(?,'person','2020-01-02','2020-01-01T00:00:00.000Z','ui',"+NOW+","+NOW+")", person) != "OK")
	s.K("session kind FK checks structural type", isolated.tryx("INSERT INTO sessions(kind_id,day,start_at,source,created_at,updated_at) VALUES(?,'2020-01-02','2020-01-01T00:00:00.000Z','ui',"+NOW+","+NOW+")", person) != "OK")
	c := s.freshWith(F{Path: filepath.Join(s.dir, "recorded-sessions.db"), Hardened: true})
	kind := c.page("Workout")
	page := strings.ReplaceAll(s.d.Page("contract/session-time.md"), "\r\n", "\n")
	_, rest, ok := strings.Cut(page, "<!-- session-endpoint-vectors -->")
	if !ok {
		stop("session vectors missing")
	}
	_, rest, ok = strings.Cut(rest, "```json\n")
	if !ok {
		stop("session vector JSON missing")
	}
	encoded, _, ok := strings.Cut(rest, "```")
	if !ok {
		stop("session vector fence missing")
	}
	var vectors []map[string]any
	if err := json.Unmarshal([]byte(encoded), &vectors); err != nil {
		stop("session vectors: %v", err)
	}
	for i, v := range vectors {
		m := M{"kind_id": kind, "day": "2020-01-02", "source": "ui", "created_at": "2026-01-01T00:00:00.000Z", "updated_at": "2026-01-01T00:00:00.000Z"}
		for k, x := range v {
			if k != "accepted" {
				m[k] = x
			}
		}
		cols, marks, vals := m.split()
		outcome := c.tryx("INSERT INTO sessions("+strings.Join(cols, ",")+") VALUES("+marks+")", vals...)
		s.K(fmt.Sprintf("session endpoint vector %d", i), (outcome == "OK") == v["accepted"].(bool), outcome)
	}

	s.K("session revision must be positive", c.tryx("INSERT INTO sessions(kind_id,day,start_at,source,revision,created_at,updated_at) VALUES(?,'2020-01-02','2020-01-01T00:00:00.000Z','ui',0,"+NOW+","+NOW+")", kind) != "OK")
	s.K("session source syntax enforced", c.tryx("INSERT INTO sessions(kind_id,day,start_at,source,created_at,updated_at) VALUES(?,'2020-01-02','2020-01-01T00:00:00.000Z','invalid source',"+NOW+","+NOW+")", kind) != "OK")
	id := sessionFixture(c, kind, M{"import_key": "18446744073709551615"})
	for _, col := range []string{"start_offset", "end_offset", "start_zone_unverified", "end_zone_unverified"} {
		probe := sessionFixture(c, kind, M{"end_at": "2020-01-02T01:00:00.000Z"})
		bad := "UTC\x00hidden"
		if strings.Contains(col, "offset") {
			bad = "+00:00\x00hidden"
		}
		before := c.tab("SELECT * FROM sessions ORDER BY id")
		result := c.tryx("UPDATE sessions SET "+col+"=? WHERE id=?", bad, probe)
		s.K("session NUL update refuses "+col, result != "OK" && before == c.tab("SELECT * FROM sessions ORDER BY id"), result)
	}
	s.K("session source key is lossless text", c.str("SELECT import_key FROM sessions WHERE id=?", id) == "18446744073709551615")
	for _, alias := range []string{"id", "rowid", "_rowid_", "oid"} {
		state := c.tab("SELECT * FROM sessions ORDER BY id")
		c.must("SAVEPOINT session_identity_probe")
		outcome := c.tryx("UPDATE sessions SET "+alias+"=?,day='2020-01-03' WHERE id=?", id+10000, id)
		unchanged := state == c.tab("SELECT * FROM sessions ORDER BY id")
		c.must("ROLLBACK TO session_identity_probe")
		c.must("RELEASE session_identity_probe")
		s.K("session "+alias+" immutable", outcome != "OK" && unchanged, outcome)
	}
	for _, col := range []string{"source", "import_key", "created_at"} {
		value := "changed"
		if col == "created_at" {
			value = "2027-01-01T00:00:00.000Z"
		}
		s.K("session "+col+" immutable", c.tryx("UPDATE sessions SET "+col+"=? WHERE id=?", value, id) != "OK")
	}
	s.K("session cannot be deleted", c.tryx("DELETE FROM sessions WHERE id=?", id) != "OK")
	before := c.n("SELECT revision FROM sessions WHERE id=?", id)
	c.must("UPDATE sessions SET day='2020-01-03' WHERE id=?", id)
	after := c.n("SELECT revision FROM sessions WHERE id=?", id)
	s.K("session metadata edit advances revision", after > before)
	c.must("UPDATE sessions SET day='2020-01-03' WHERE id=?", id)
	s.K("session metadata no-op preserves revision", c.n("SELECT revision FROM sessions WHERE id=?", id) == after)
	s.K("session revision cannot decrease", c.tryx("UPDATE sessions SET revision=1 WHERE id=?", id) != "OK")
	s.K("session duplicate source key refuses", c.tryx("INSERT INTO sessions(kind_id,day,start_at,source,import_key,created_at,updated_at) VALUES(?,'2020-01-02','2020-01-02T00:00:00.000Z','ui','18446744073709551615',"+NOW+","+NOW+")", kind) != "OK")
	journal := c.dayPage("2020-01-02", "")
	s.K("session kind refuses journal page", c.tryx("INSERT INTO sessions(kind_id,day,start_at,source,created_at,updated_at) VALUES(?,'2020-01-02','2020-01-02T00:00:00.000Z','ui',"+NOW+","+NOW+")", journal) != "OK")
	s.K("session edit refuses journal kind", c.tryx("UPDATE sessions SET kind_id=? WHERE id=?", journal, id) != "OK")
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", kind)
	s.K("session kind refuses tombstoned new selection", c.tryx("INSERT INTO sessions(kind_id,day,start_at,source,created_at,updated_at) VALUES(?,'2020-01-02','2020-01-02T00:00:00.000Z','ui',"+NOW+","+NOW+")", kind) != "OK")
	s.K("kind tombstone retains existing session", c.n("SELECT kind_id FROM sessions WHERE id=?", id) == kind)
	// Deliberate guard damage: the literal contract query, not integrity_check/FKs, detects a journal kind.
	c.must("DROP TRIGGER sessions_kind_update")
	c.must("UPDATE sessions SET kind_id=? WHERE id=?", journal, id)
	checks := statements(sqlBlocks(s.d.Page("contract/integrity-checks.md"))[0])
	s.K("session kind semantic query detects journal damage", contains(c.col(checks[4]), ids(id)))
}

func measurementScopes(s *S) {
	c := s.freshWith(F{Path: filepath.Join(s.dir, "measurement-scopes.db"), Hardened: true})
	kind := c.page("Workout")
	a, b := sessionFixture(c, kind, nil), sessionFixture(c, kind, nil)
	metric := c.metric("Steps", "steps")
	c.must("INSERT INTO measurements(metric_id,day,value,session_id,source,created_at) VALUES(?,'2020-01-02',4000,?,'ui',"+NOW+")", metric, a)
	root := c.n("SELECT id FROM measurements WHERE metric_id=?", metric)
	c.must("SAVEPOINT scope_probe")
	s.K("measurement correction refuses changed session scope", c.tryx("INSERT INTO measurements(metric_id,day,value,session_id,supersedes_id,source,created_at) VALUES(?,'2020-01-02',3500,?,?,'ui',"+NOW+")", metric, b, root) != "OK")
	c.must("ROLLBACK TO scope_probe")
	c.must("RELEASE scope_probe")
	c.must("SAVEPOINT scope_probe")
	s.K("measurement correction refuses NULL scope change", c.tryx("INSERT INTO measurements(metric_id,day,value,supersedes_id,source,created_at) VALUES(?,'2020-01-02',3500,?,'ui',"+NOW+")", metric, root) != "OK")

	c.must("ROLLBACK TO scope_probe")
	c.must("RELEASE scope_probe")
	c.must("UPDATE sessions SET deleted_at="+NOW+" WHERE id=?", b)
	s.K("new scoped value requires live session", c.measure(metric, "2020-01-02", 2000, M{"session_id": b}) != "OK")
	c.must("UPDATE sessions SET deleted_at="+NOW+" WHERE id=?", a)
	s.K("same-scope retraction allowed after tombstone", c.measure(metric, "2020-01-02", nil, M{"session_id": a, "supersedes_id": root}) == "OK")
	// Deliberate scope guard damage, preserving otherwise valid FKs and chain identity.
	c.must("DROP TRIGGER measurements_supersede_scope")
	leaf := c.n("SELECT id FROM measurements WHERE supersedes_id=?", root)
	damaged := c.rows("INSERT INTO measurements(metric_id,session_id,day,value,supersedes_id,source,created_at) VALUES(?,?,'2020-01-02',NULL,?,'ui',"+NOW+") RETURNING id", metric, b, leaf)[0][0].(int64)
	checks := statements(sqlBlocks(s.d.Page("contract/integrity-checks.md"))[0])
	s.K("scope semantic query detects correction damage", contains(c.col(checks[5]), ids(damaged)))
}
