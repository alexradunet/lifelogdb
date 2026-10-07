package tests

import (
	"path/filepath"
	"strings"
)

// These are isolated DDL probes, not an alternative application writer. The
// production writer scenarios live beside internal/core's planning operations.
func planningDB(s *S, name string) *C {
	c := s.freshWith(F{Path: filepath.Join(s.dir, name+".db"), Hardened: true})
	c.must("PRAGMA synchronous=FULL")
	return c
}

func planningInsert(c *C, table string, fields M) int64 {
	cols, marks, args := fields.split()
	return c.rows("INSERT INTO "+table+"("+strings.Join(cols, ",")+") VALUES("+marks+") RETURNING id", args...)[0][0].(int64)
}

func planningTask(c *C, extra M) int64 {
	m := M{"label": "Buy supplies", "repeat_unit": "day", "repeat_every": 1, "anchor_day": "2026-01-01", "source": "ui", "created_at": "2026-01-01T00:00:00.000Z", "updated_at": "2026-01-01T00:00:00.000Z"}
	for k, v := range extra {
		m[k] = v
	}
	return planningInsert(c, "tasks", m)
}

func planningOccurrence(c *C, task int64, key string, extra M) int64 {
	var due any = key
	if key == "once" {
		due = nil
	}
	m := M{"task_id": task, "occurrence_key": key, "due_day": due, "source": "ui", "created_at": "2026-01-01T00:00:00.000Z", "updated_at": "2026-01-01T00:00:00.000Z"}
	for k, v := range extra {
		m[k] = v
	}
	return planningInsert(c, "task_occurrences", m)
}

func planningState(c *C) string {
	return c.tab("SELECT * FROM tasks ORDER BY id") + "\n" + c.tab("SELECT * FROM task_occurrences ORDER BY id")
}

// Every refusal runs in a rollback-only savepoint, including a successful write
// under a mutant, so later witnesses do not depend on the mutant's side effects.
func planningRefusal(s *S, c *C, label, sql string, args ...any) {
	c.must("SAVEPOINT planning_probe")
	before := planningState(c)
	got := c.tryx(sql, args...)
	unchanged := planningState(c) == before
	c.must("ROLLBACK TO planning_probe")
	c.must("RELEASE planning_probe")
	s.K(label, got != "OK" && unchanged, got)
}

func planning(s *S) {
	c := planningDB(s, "planning")
	project := c.page("Garden")
	other := c.page("Workshop")
	a := planningTask(c, M{"project_page_id": project, "import_key": "18446744073709551615"})
	b := planningTask(c, M{"project_page_id": other})
	s.K("task labels repeat without named-identity collisions", a != b && c.n("SELECT count(*) FROM tasks WHERE label='Buy supplies'") == 2 && c.n("SELECT count(*) FROM entity_names WHERE title='Buy supplies'") == 0)
	s.K("task import key remains lossless text", c.str("SELECT import_key FROM tasks WHERE id=?", a) == "18446744073709551615")
	s.K("ghost pages exclude retained task projects", c.n("SELECT count(*) FROM ghost_pages WHERE id=?", project) == 0)
	planningRefusal(s, c, "retained task project prevents typed promotion", "UPDATE entities SET entity_type='person' WHERE id=?", project)
	journal := c.dayPage("2026-10-01", "")
	person := c.named("person", "Project kind witness")
	for _, tc := range []struct {
		label string
		id    int64
	}{{"journal", journal}, {"typed", person}, {"missing", 999999}} {
		planningRefusal(s, c, "task project update refuses "+tc.label, "UPDATE tasks SET project_page_id=? WHERE id=?", tc.id, a)
	}
	dead := c.page("Archived project")
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", dead)
	planningRefusal(s, c, "task project update refuses tombstone", "UPDATE tasks SET project_page_id=? WHERE id=?", dead, a)
	for _, tc := range []struct {
		label string
		id    int64
	}{{"journal", journal}, {"typed", person}, {"tombstone", dead}, {"missing", 999999}} {
		planningRefusal(s, c, "task project insert refuses "+tc.label, "INSERT INTO tasks(label,project_page_id,repeat_unit,repeat_every,anchor_day,source,created_at,updated_at) VALUES('Invalid project',?,'day',1,'2026-01-01','ui',"+NOW+","+NOW+")", tc.id)
	}
	c.must("UPDATE entity_names SET title='GARDEN' WHERE entity_id=?", project)
	s.K("project spelling edit preserves task reference", c.n("SELECT project_page_id FROM tasks WHERE id=?", a) == project)
	occ := planningOccurrence(c, a, "2026-10-01", M{"import_key": "same-source-instance"})
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", project)
	s.K("project tombstone retains task and open outcome", c.n("SELECT project_page_id FROM tasks WHERE id=?", a) == project && c.str("SELECT state FROM task_occurrences WHERE id=?", occ) == "open")
	c.must("UPDATE tasks SET label='Supplies for garden' WHERE id=?", a)
	s.K("retained tombstoned project does not prevent unrelated task edit", c.str("SELECT label FROM tasks WHERE id=?", a) == "Supplies for garden")

	// Independent table identity/provenance guards, including SQLite's aliases.
	for _, table := range []string{"tasks", "task_occurrences"} {
		id, editable := b, "label='Changed'"
		if table == "task_occurrences" {
			id, editable = occ, "due_day='2026-11-01'"
		}
		for _, alias := range []string{"id", "rowid", "_rowid_", "oid"} {
			planningRefusal(s, c, table+" "+alias+" identity immutable", "UPDATE "+table+" SET "+alias+"=?,"+editable+" WHERE id=?", id+10000, id)
		}
		for _, field := range []string{"source", "import_key", "created_at"} {
			value := "changed"
			if field == "created_at" {
				value = "2027-01-01T00:00:00.000Z"
			}
			planningRefusal(s, c, table+" "+field+" immutable", "UPDATE "+table+" SET "+field+"=? WHERE id=?", value, id)
		}
		planningRefusal(s, c, table+" refuses hard delete", "DELETE FROM "+table+" WHERE id=?", id)
		before := c.n("SELECT revision FROM "+table+" WHERE id=?", id)
		c.must("UPDATE "+table+" SET "+editable+" WHERE id=?", id)
		after := c.n("SELECT revision FROM "+table+" WHERE id=?", id)
		s.K(table+" content edit advances revision", after > before)
		c.must("UPDATE "+table+" SET "+editable+" WHERE id=?", id)
		s.K(table+" no-op preserves revision", c.n("SELECT revision FROM "+table+" WHERE id=?", id) == after)
		planningRefusal(s, c, table+" revision cannot decrease", "UPDATE "+table+" SET revision=1 WHERE id=?", id)
		c.must("SAVEPOINT planning_revision")
		c.must("UPDATE "+table+" SET deleted_at="+NOW+" WHERE id=?", id)
		s.K(table+" tombstone advances revision", c.n("SELECT revision FROM "+table+" WHERE id=?", id) > after)
		c.must("ROLLBACK TO planning_revision")
		c.must("RELEASE planning_revision")
		s.K(table+" rollback restores revision", c.n("SELECT revision FROM "+table+" WHERE id=?", id) == after)
		c.must("SAVEPOINT planning_exhaustion")
		c.must("UPDATE "+table+" SET revision=9223372036854775807 WHERE id=?", id)
		change := "label='Exhausted'"
		if table == "task_occurrences" {
			change = "due_day='2027-01-01'"
		}
		planningRefusal(s, c, table+" revision exhaustion refuses edit atomically", "UPDATE "+table+" SET "+change+" WHERE id=?", id)
		c.must("ROLLBACK TO planning_exhaustion")
		c.must("RELEASE planning_exhaustion")
	}
	for _, change := range []string{"repeat_unit='week'", "repeat_every=2", "anchor_day='2026-01-02'", "repeat_unit=NULL,repeat_every=NULL,anchor_day=NULL"} {
		planningRefusal(s, c, "task cadence immutable "+change, "UPDATE tasks SET "+change+" WHERE id=?", a)
	}
	planningRefusal(s, c, "occurrence task binding immutable", "UPDATE task_occurrences SET task_id=? WHERE id=?", b, occ)
	planningRefusal(s, c, "occurrence key immutable", "UPDATE task_occurrences SET occurrence_key='2026-10-02' WHERE id=?", occ)
	planningRefusal(s, c, "task source-key uniqueness", "INSERT INTO tasks(label,repeat_unit,repeat_every,anchor_day,source,import_key,created_at,updated_at) VALUES('Duplicate','day',1,'2026-01-01','ui','18446744073709551615',"+NOW+","+NOW+")")
	planningRefusal(s, c, "occurrence source-key uniqueness", "INSERT INTO task_occurrences(task_id,occurrence_key,due_day,source,import_key,created_at,updated_at) VALUES(?,'2026-10-02','2026-10-02','ui','same-source-instance',"+NOW+","+NOW+")", a)
	planningRefusal(s, c, "occurrence task-key uniqueness", "INSERT INTO task_occurrences(task_id,occurrence_key,due_day,source,created_at,updated_at) VALUES(?,'2026-10-01','2026-10-01','ui',"+NOW+","+NOW+")", a)
	c.must("UPDATE task_occurrences SET state='done',completed_at='2026-10-01T12:00:00.000Z',reminder_mode='at',reminder_override_at='2026-10-02T12:00:00.000Z' WHERE id=?", occ)
	prior := planningState(c)
	c.must("INSERT INTO task_occurrences(task_id,occurrence_key,due_day,source,created_at,updated_at) VALUES(?,'2026-10-01','2026-10-01','ui',"+NOW+","+NOW+") ON CONFLICT(task_id,occurrence_key) DO NOTHING", a)
	s.K("slot conflict preserves edited deadline outcome override and revision", planningState(c) == prior)
	c.must("UPDATE task_occurrences SET deleted_at="+NOW+" WHERE id=?", occ)
	prior = planningState(c)
	c.must("INSERT INTO task_occurrences(task_id,occurrence_key,due_day,source,created_at,updated_at) VALUES(?,'2026-10-01','2026-10-01','ui',"+NOW+","+NOW+") ON CONFLICT(task_id,occurrence_key) DO NOTHING", a)
	s.K("slot conflict does not restore tombstoned occurrence", planningState(c) == prior)
	c.must("UPDATE tasks SET deleted_at="+NOW+" WHERE id=?", a)
	planningRefusal(s, c, "new occurrence requires live task", "INSERT INTO task_occurrences(task_id,occurrence_key,due_day,source,created_at,updated_at) VALUES(?,'2026-10-02','2026-10-02','ui',"+NOW+","+NOW+")", a)
	s.K("task tombstone retains historical occurrence identity", c.n("SELECT task_id FROM task_occurrences WHERE id=?", occ) == a)
	s.K("planning file retains structural integrity", c.integrityOK())

	planningOneOff(s)
	planningEnd(s)
	planningIntegrity(s)
}

func planningIntegrity(s *S) {
	checks := statements(sqlBlocks(s.d.Page("contract/integrity-checks.md"))[0])
	if len(checks) != 10 {
		stop("planning integrity needs ten literal statements, got %d", len(checks))
	}
	c := planningDB(s, "planning-integrity")
	project := c.page("Integrity project")
	journal := c.dayPage("2026-10-01", "")
	task := planningTask(c, M{"project_page_id": project, "repeat_unit": "month", "anchor_day": "2026-01-31"})
	history := planningOccurrence(c, task, "2026-03-31", M{"state": "done"})
	c.must("UPDATE tasks SET repeat_until_day='2026-02-28',deleted_at="+NOW+" WHERE id=?", task)
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", project)
	s.K("planning semantic checks retain tombstoned context and historical done beyond end", c.tab(checks[6]) == "" && c.tab(checks[7]) == "" && c.tab(checks[8]) == "")
	c.must("DROP TRIGGER tasks_project_update")
	c.must("UPDATE tasks SET project_page_id=? WHERE id=?", journal, task)
	s.K("planning project semantic query detects journal reference damage", c.integrityOK() && contains(c.col(checks[6]), ids(task)))
	c.must("DROP TRIGGER task_occurrences_open")
	c.must("UPDATE task_occurrences SET state='open' WHERE id=?", history)
	s.K("planning occurrence semantic query detects open slot beyond end", c.integrityOK() && contains(c.col(checks[8]), ids(history)))
	c.must("PRAGMA foreign_keys=OFF")
	orphan := planningTask(c, M{"repeat_unit": nil, "repeat_every": nil, "anchor_day": nil})
	s.K("planning one-off semantic query detects missing once occurrence", contains(c.col(checks[7]), ids(orphan)))
	c.must("DROP TRIGGER task_occurrences_admit")
	invalid := planningOccurrence(c, task, "2026-03-28", M{"state": "skipped"})
	s.K("planning occurrence semantic query detects calendar nonmember", contains(c.col(checks[8]), ids(invalid)))
}

func planningOneOff(s *S) {
	c := planningDB(s, "planning-once")
	c.must("BEGIN IMMEDIATE")
	planningTask(c, M{"repeat_unit": nil, "repeat_every": nil, "anchor_day": nil})
	got := c.tryx("COMMIT")
	s.K("one-off definition cannot commit without its once occurrence", got != "OK", got)
	if got != "OK" {
		c.must("ROLLBACK")
	}
	c.must("BEGIN IMMEDIATE")
	id := planningTask(c, M{"repeat_unit": nil, "repeat_every": nil, "anchor_day": nil})
	occ := planningOccurrence(c, id, "once", nil)
	c.must("COMMIT")
	s.K("one-off definition and once occurrence commit together", c.n("SELECT task_id FROM task_occurrences WHERE id=?", occ) == id)
	planningRefusal(s, c, "one-off duplicate once refused", "INSERT INTO task_occurrences(task_id,occurrence_key,source,created_at,updated_at) VALUES(?,'once','ui',"+NOW+","+NOW+")", id)
	planningRefusal(s, c, "one-off date key refused", "INSERT INTO task_occurrences(task_id,occurrence_key,due_day,source,created_at,updated_at) VALUES(?,'2026-10-01','2026-10-01','ui',"+NOW+","+NOW+")", id)
	planningRefusal(s, c, "one-off cannot acquire recurrence", "UPDATE tasks SET repeat_unit='day',repeat_every=1,anchor_day='2026-01-01' WHERE id=?", id)
	c.must("BEGIN IMMEDIATE")
	rolled := planningTask(c, M{"repeat_unit": nil, "repeat_every": nil, "anchor_day": nil})
	planningOccurrence(c, rolled, "once", nil)
	c.must("ROLLBACK")
	s.K("rollback of one-off creation retains neither definition nor occurrence", c.n("SELECT count(*) FROM tasks WHERE id=?", rolled) == 0 && c.n("SELECT count(*) FROM task_occurrences WHERE task_id=?", rolled) == 0)
	s.K("one-off circular FK remains structurally clean", c.integrityOK())
}

func planningEnd(s *S) {
	c := planningDB(s, "planning-end")
	task := planningTask(c, M{"anchor_day": "2026-10-01"})
	early := planningOccurrence(c, task, "2026-10-01", M{"due_day": "2026-11-12"})
	later := planningOccurrence(c, task, "2026-11-01", nil)
	done := planningOccurrence(c, task, "2026-11-02", M{"state": "done", "completed_at": "2026-11-02T10:00:00.000Z"})
	before := planningState(c)
	c.must("BEGIN IMMEDIATE")
	c.must("UPDATE tasks SET repeat_until_day='2026-10-31' WHERE id=?", task)
	c.must("ROLLBACK")
	s.K("ending rollback restores definition outcomes and revisions", planningState(c) == before)
	oldTask, oldOccurrence := c.n("SELECT revision FROM tasks WHERE id=?", task), c.n("SELECT revision FROM task_occurrences WHERE id=?", later)
	c.must("UPDATE tasks SET repeat_until_day='2026-10-31' WHERE id=?", task)
	s.K("ending skips persisted open slots after original-key cutoff", c.str("SELECT state FROM task_occurrences WHERE id=?", later) == "skipped")
	s.K("ending advances definition and changed occurrence revisions", c.n("SELECT revision FROM tasks WHERE id=?", task) > oldTask && c.n("SELECT revision FROM task_occurrences WHERE id=?", later) > oldOccurrence)
	s.K("ending preserves done evidence beyond cutoff", c.tab("SELECT state,completed_at FROM task_occurrences WHERE id=?", done) == "done|2026-11-02T10:00:00.000Z")
	s.K("ending preserves earlier slot moved beyond cutoff", c.tab("SELECT state,due_day FROM task_occurrences WHERE id=?", early) == "open|2026-11-12")
	planningRefusal(s, c, "ended recurrence cannot extend end", "UPDATE tasks SET repeat_until_day='2026-11-01' WHERE id=?", task)
	planningRefusal(s, c, "ended recurrence cannot clear end", "UPDATE tasks SET repeat_until_day=NULL WHERE id=?", task)
	planningRefusal(s, c, "ended slot cannot reopen", "UPDATE task_occurrences SET state='open' WHERE id=?", later)
	planningRefusal(s, c, "new open slot beyond end refused", "INSERT INTO task_occurrences(task_id,occurrence_key,due_day,source,created_at,updated_at) VALUES(?,'2026-11-03','2026-11-03','ui',"+NOW+","+NOW+")", task)
	historical := planningOccurrence(c, task, "2026-11-03", M{"state": "done"})
	s.K("historical done beyond end retains unknown completion time", c.tab("SELECT state,completed_at FROM task_occurrences WHERE id=?", historical) == "done|None")
	planningRefusal(s, c, "historical done beyond end cannot reopen", "UPDATE task_occurrences SET state='open' WHERE id=?", historical)
	c.must("UPDATE tasks SET repeat_until_day='2026-09-30' WHERE id=?", task)
	s.K("end before anchor admits empty sequence and skips remaining opens", c.n("SELECT count(*) FROM task_occurrences WHERE task_id=? AND state='open'", task) == 0)
	s.K("ended planning file retains structural integrity", c.integrityOK())
}
