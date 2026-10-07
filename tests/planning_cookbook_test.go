package tests

func planningCookbook(s *S) {
	c := planningDB(s, "planning-cookbook")
	blocks := sqlBlocks(s.d.Page("cookbook/tasks.md"))
	if len(blocks) != 6 {
		stop("task cookbook has %d blocks, want 6", len(blocks))
	}
	project := c.page("Garden supplies project")
	p := P{"task_label": "Buy supplies", "project_page_id": project, "source": "ui", "due_day": "2026-10-05", "planning_clock": "09:00",
		"planning_task_version": 1, "planning_occurrence_version": 1, "planning_state": "open", "planning_completed_at": nil,
		"planning_reminder_mode": "inherit", "planning_reminder_at": nil, "planning_until": "2026-10-31",
		"planning_from": "2026-10-01", "planning_through": "2026-11-30"}
	run := func(i int) {
		if _, err := c.runBlock(blocks[i], p, nil); err != nil {
			stop("task cookbook block %d: %v", i, err)
		}
	}
	run(0)
	s.K("cookbook creates one-off with its once deadline", c.tab("SELECT occurrence_key,due_day,state FROM task_occurrences WHERE task_id=?", p["planning_once_id"]) == "once|2026-10-05|open")
	run(1)
	task := p["planning_task_id"]
	s.K("cookbook creates recurring definition without future rows", c.n("SELECT count(*) FROM task_occurrences WHERE task_id=?", task) == 0)
	run(2)
	s.K("cookbook materializes initial slot", c.tab("SELECT occurrence_key,due_day,state,reminder_mode FROM task_occurrences WHERE task_id=?", task) == "2026-02-28|2026-02-28|open|inherit")
	run(3)
	s.K("cookbook reschedules without changing occurrence key", c.tab("SELECT occurrence_key,due_day FROM task_occurrences WHERE task_id=?", task) == "2026-02-28|2026-10-05")
	before := planningState(c)
	run(2)
	s.K("cookbook materialization retry preserves current occurrence", planningState(c) == before)
	rows := c.rows(blocks[5], p)
	keys := func(rows [][]any) string {
		var selected [][]any
		for _, row := range rows {
			selected = append(selected, []any{row[2], row[3]})
		}
		return tab(selected)
	}
	s.K("cookbook deadline query merges moved-in and virtual slots", keys(rows) == "2026-02-28|2026-10-05; 2026-10-31|2026-10-31; 2026-11-30|2026-11-30", keys(rows))
	s.K("cookbook deadline read does not materialize virtual slots", c.n("SELECT count(*) FROM task_occurrences WHERE task_id=?", task) == 1)
	p["due_day"] = "2026-10-10"
	run(3)
	s.K("cookbook stale occurrence version changes no data", planningState(c) == before)
	p["planning_occurrence_version"] = c.n("SELECT revision FROM task_occurrences WHERE task_id=?", task)
	c.must("UPDATE tasks SET reminder_local_time='10:00' WHERE id=?", task)
	before = planningState(c)
	run(3)
	s.K("cookbook stale task version changes no data", planningState(c) == before)
	p["planning_task_version"] = c.n("SELECT revision FROM tasks WHERE id=?", task)
	p["due_day"] = nil
	run(3)
	p["planning_from"], p["planning_through"] = "2026-02-01", "2026-02-28"
	s.K("cookbook undated persisted slot suppresses original virtual deadline", len(c.rows(blocks[5], p)) == 0)
	c.must("UPDATE task_occurrences SET due_day='2026-02-28',deleted_at="+NOW+" WHERE task_id=?", task)
	s.K("cookbook tombstoned occurrence suppresses its virtual slot", len(c.rows(blocks[5], p)) == 0)
	c.must("UPDATE task_occurrences SET deleted_at=NULL,state='skipped' WHERE task_id=?", task)
	s.K("cookbook skipped occurrence suppresses its virtual slot", len(c.rows(blocks[5], p)) == 0)
	p["planning_from"], p["planning_through"] = "2026-10-01", "2026-11-30"
	run(4)
	s.K("cookbook stop updates inclusive definition end", c.str("SELECT repeat_until_day FROM tasks WHERE id=?", task) == "2026-10-31")
	s.K("cookbook stopped virtual slots are absent", keys(c.rows(blocks[5], p)) == "2026-10-31|2026-10-31")
	c.must("UPDATE entities SET deleted_at='2026-10-01T00:00:00.000Z' WHERE id=?", project)
	rows = c.rows(blocks[5], p)
	s.K("cookbook reports tombstoned project without hiding work", len(rows) == 1 && rows[0][len(rows[0])-1] == "2026-10-01T00:00:00.000Z")
	c.must("UPDATE tasks SET deleted_at="+NOW+" WHERE id=?", task)
	s.K("cookbook task tombstone suppresses virtual and persisted work", len(c.rows(blocks[5], p)) == 0)
	s.K("cookbook planning results remain structurally clean", c.integrityOK())
}
