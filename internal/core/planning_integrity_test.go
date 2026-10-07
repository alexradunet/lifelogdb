package core

import "testing"

func TestPlanningIntegrityDetectsSemanticDamage(t *testing.T) {
	for _, damage := range []string{"project", "cadence", "ended-open"} {
		t.Run(damage, func(t *testing.T) {
			s := fresh(t)
			project, _, err := s.CreatePage(ctx, "cli", "House maintenance", "")
			if err != nil {
				t.Fatal(err)
			}
			task, _, err := s.CreateTask(ctx, "cli", TaskSpec{Label: "Check alarm", ProjectPageID: &project, RepeatUnit: "month", RepeatEvery: 1, AnchorDay: "2026-01-31"}, "")
			if err != nil {
				t.Fatal(err)
			}
			var occurrence *TaskOccurrence
			if err := s.Do(ctx, "cli", func(tx *Tx) error {
				var err error
				occurrence, _, err = tx.MaterializeTaskOccurrence(task, "1", "2026-02-28")
				return err
			}); err != nil {
				t.Fatal(err)
			}
			clean, err := s.Integrity(ctx)
			if err != nil || !clean.OK {
				t.Fatalf("valid planning fixture: %+v, %v", clean, err)
			}
			// Deliberate corruption bypasses only the guard under test. The writer built the fixture.
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := s.DB.W.ExecContext(ctx, q, args...); err != nil {
					t.Fatal(err)
				}
			}
			switch damage {
			case "project":
				journal, _, err := s.CreatePage(ctx, "cli", "2026-02-28", "")
				if err != nil {
					t.Fatal(err)
				}
				exec("DROP TRIGGER tasks_project_update")
				exec("UPDATE tasks SET project_page_id=? WHERE id=?", journal, task)
			case "cadence":
				exec("DROP TRIGGER task_occurrences_fixed")
				exec("UPDATE task_occurrences SET occurrence_key='2026-02-27' WHERE id=?", occurrence.ID)
			case "ended-open":
				exec("DROP TRIGGER tasks_end_skip")
				exec("UPDATE tasks SET repeat_until_day='2026-01-31' WHERE id=?", task)
			}
			got, err := s.Integrity(ctx)
			if err != nil || got.OK || got.ForeignKeys != 0 || !got.FullTextIndexOK || len(got.IntegrityCheck) != 1 || got.IntegrityCheck[0] != "ok" {
				t.Fatalf("planning semantic damage must be reported independently of structural checks: %+v, %v", got, err)
			}
			if damage == "project" {
				if len(got.InvalidTaskProjects) != 1 || got.InvalidTaskProjects[0] != task || len(got.InvalidTaskOccurrences) != 0 {
					t.Fatalf("project diagnostic identity: %+v", got)
				}
			} else if len(got.InvalidTaskOccurrences) != 1 || got.InvalidTaskOccurrences[0] != occurrence.ID || len(got.InvalidTaskProjects) != 0 {
				t.Fatalf("occurrence diagnostic identity: %+v", got)
			}
		})
	}
}

func TestPlanningIntegrityRetainsLifecycleAndHistoricalOutcomes(t *testing.T) {
	s := fresh(t)
	project, _, err := s.CreatePage(ctx, "cli", "Garden", "")
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := s.CreateTask(ctx, "cli", TaskSpec{Label: "Order seeds", ProjectPageID: &project, RepeatUnit: "month", RepeatEvery: 1, AnchorDay: "2026-01-31", RepeatUntilDay: "2026-01-31"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		for _, in := range []OccurrenceInput{
			{OccurrenceKey: "2026-01-31", OccurrenceChanges: OccurrenceChanges{DueDay: "2026-03-31", State: "open", ReminderMode: "inherit"}},
			{OccurrenceKey: "2026-02-28", OccurrenceChanges: OccurrenceChanges{DueDay: "2026-02-28", State: "done", ReminderMode: "inherit"}},
			{OccurrenceKey: "2026-03-31", OccurrenceChanges: OccurrenceChanges{DueDay: "2026-03-31", State: "skipped", ReminderMode: "off"}},
		} {
			if _, _, err := tx.CaptureTaskOccurrence(task, "1", in); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	once, _, err := s.CreateTask(ctx, "cli", TaskSpec{Label: "Replace rake"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.TaskOccurrenceLifecycle(once, "1", "once", "1", true) }); err != nil {
		t.Fatal(err)
	}
	for _, tombstone := range []bool{false, true} {
		if tombstone {
			if err := s.Tombstone(ctx, "cli", project); err != nil {
				t.Fatal(err)
			}
			if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.TaskLifecycle(task, "1", true) }); err != nil {
				t.Fatal(err)
			}
		}
		got, err := s.Integrity(ctx)
		if err != nil || !got.OK {
			t.Fatalf("valid history (tombstone=%v): %+v, %v", tombstone, got, err)
		}
	}
}

func TestPlanningIntegrityIdentifiesMissingOneOffOccurrence(t *testing.T) {
	s := fresh(t)
	task, _, err := s.CreateTask(ctx, "cli", TaskSpec{Label: "Book appointment"}, "")
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately bypass deletion and deferred ownership to model an externally damaged file.
	// Use one connection and restore foreign-key enforcement before returning it to the pool.
	conn, err := s.DB.W.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "DROP TRIGGER task_occurrences_no_delete"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatal(err)
	}
	_, damageErr := conn.ExecContext(ctx, "DELETE FROM task_occurrences WHERE task_id=?", task)
	_, restoreErr := conn.ExecContext(ctx, "PRAGMA foreign_keys=ON")
	closeErr := conn.Close()
	if damageErr != nil || restoreErr != nil || closeErr != nil {
		t.Fatalf("damage=%v restore=%v close=%v", damageErr, restoreErr, closeErr)
	}
	got, err := s.Integrity(ctx)
	if err != nil || got.OK || got.ForeignKeys != 1 || len(got.InvalidOneOffTasks) != 1 || got.InvalidOneOffTasks[0] != task || len(got.InvalidTaskOccurrences) != 0 {
		t.Fatalf("one-off ownership diagnostic: %+v, %v", got, err)
	}
}
