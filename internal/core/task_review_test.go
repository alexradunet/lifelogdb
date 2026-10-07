package core

import (
	"fmt"
	"testing"
)

func TestTaskReminderFutureLeapYearBoundary(t *testing.T) {
	// All these winter dates share Bucharest's UTC+02:00 offset. Traversing
	// a zone-data year boundary must not turn an ordinary clock unresolved.
	for day := 28; day <= 31; day++ {
		date := fmt.Sprintf("2040-12-%02d", day)
		t.Run(date, func(t *testing.T) {
			got, ok := resolveTaskClock(date, "09:00", "Europe/Bucharest")
			want := date + "T07:00:00.000Z"
			if !ok || got != want {
				t.Fatalf("got %q, resolved=%v; want %s", got, ok, want)
			}
		})
	}
}

func TestCaptureOneOffTaskPreservesHistoricalOutcomeAndImportIdentity(t *testing.T) {
	s := fresh(t)
	spec := TaskSpec{Label: "Send the annual letter"}
	initial := OccurrenceInput{
		OccurrenceKey: "once", ImportKey: "occurrence:letter:2025",
		OccurrenceChanges: OccurrenceChanges{
			DueDay: "2025-12-20", State: "done", CompletedAt: "2025-12-19T13:14:15.000Z",
			ReminderMode: "at", ReminderOverrideAt: "2025-12-19T12:00:00.000Z",
		},
	}
	var id int64
	err := s.Do(ctx, "import:synthetic", func(tx *Tx) error {
		var existing bool
		var err error
		id, existing, err = tx.CaptureOneOffTask(spec, "task:letter:2025", initial)
		if err == nil && existing {
			t.Error("new import reported existing")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Task(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	occurrence, err := s.TaskOccurrence(ctx, id, "once")
	if err != nil {
		t.Fatal(err)
	}
	if occurrence.OccurrenceChanges != initial.OccurrenceChanges || occurrence.ImportKey != initial.ImportKey || occurrence.Source != "import:synthetic" {
		t.Fatalf("historical import lost its evidence or identity: %+v", occurrence)
	}
	changed := OccurrenceChanges{DueDay: "2025-12-22", State: "open", ReminderMode: "off"}
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		return tx.EditTaskOccurrence(id, task.Version, "once", occurrence.Version, changed)
	}); err != nil {
		t.Fatal(err)
	}
	edited, err := s.TaskOccurrence(ctx, id, "once")
	if err != nil {
		t.Fatal(err)
	}
	err = s.Do(ctx, "import:synthetic", func(tx *Tx) error {
		replayed, existing, err := tx.CaptureOneOffTask(spec, "task:letter:2025", initial)
		if err == nil && (replayed != id || !existing) {
			t.Errorf("replay identity got (%d,%v), want (%d,true)", replayed, existing, id)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := s.TaskOccurrence(ctx, id, "once")
	if err != nil {
		t.Fatal(err)
	}
	if *replayed != *edited {
		t.Fatalf("import replay changed edited occurrence: got %+v, want %+v", replayed, edited)
	}

	// A new definition must not steal the occurrence source identity. Its
	// failed initial occurrence must also roll back the newly inserted task.
	err = s.Do(ctx, "import:synthetic", func(tx *Tx) error {
		_, _, err := tx.CaptureOneOffTask(spec, "task:another-letter", initial)
		return err
	})
	if err == nil {
		t.Fatal("duplicate occurrence source key was accepted")
	}
	var tasks, occurrences int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM tasks),(SELECT count(*) FROM task_occurrences)`).Scan(&tasks, &occurrences); err != nil {
		t.Fatal(err)
	}
	if tasks != 1 || occurrences != 1 {
		t.Fatalf("failed import left rows: tasks=%d occurrences=%d", tasks, occurrences)
	}
}

func TestTaskOccurrenceReplayPreservesTombstonesAndBothIdentityBindings(t *testing.T) {
	s := fresh(t)
	p := planningTask(t, s, TaskSpec{Label: "Synthetic recurring import", RepeatUnit: "day", RepeatEvery: 1, AnchorDay: "2026-10-07"})
	other := planningTask(t, s, p.TaskSpec)
	in := OccurrenceInput{OccurrenceKey: "2026-10-07", ImportKey: "occurrence:stable", OccurrenceChanges: OccurrenceChanges{DueDay: "2026-10-07", State: "open", ReminderMode: "inherit"}}
	capture := func(task *Task, input OccurrenceInput) (*TaskOccurrence, bool, error) {
		var got *TaskOccurrence
		var existing bool
		err := s.Do(ctx, "import:synthetic", func(tx *Tx) error {
			var err error
			got, existing, err = tx.CaptureTaskOccurrence(task.ID, task.Version, input)
			return err
		})
		return got, existing, err
	}
	o, existing, err := capture(p, in)
	if err != nil || existing {
		t.Fatalf("initial capture: %+v existing=%v err=%v", o, existing, err)
	}
	changed := o.OccurrenceChanges
	changed.DueDay, changed.State, changed.CompletedAt = "2026-11-15", "done", "2026-10-06T11:00:00.000Z"
	o = planningEditOccurrence(t, s, p, o, changed)
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		if err := tx.TaskOccurrenceLifecycle(p.ID, p.Version, o.OccurrenceKey, o.Version, true); err != nil {
			return err
		}
		return tx.TaskLifecycle(p.ID, p.Version, true)
	}); err != nil {
		t.Fatal(err)
	}
	p = planningReadTask(t, s, p.ID)
	o = planningReadOccurrence(t, s, p.ID, in.OccurrenceKey)
	got, existing, err := capture(p, in)
	if err != nil || !existing || *got != *o {
		t.Fatalf("replay changed retained outcome: got=%+v want=%+v existing=%v err=%v", got, o, existing, err)
	}
	if _, _, err := capture(other, in); status(err) != 409 {
		t.Fatalf("source identity selected a different task: %v", err)
	}
	conflicting := in
	conflicting.ImportKey = "occurrence:another"
	if _, _, err := capture(p, conflicting); status(err) != 409 {
		t.Fatalf("natural identity selected a different source key: %v", err)
	}
	if got := planningReadOccurrence(t, s, p.ID, in.OccurrenceKey); *got != *o {
		t.Fatalf("refusal changed retained outcome: got=%+v want=%+v", got, o)
	}
	var count int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM task_occurrences`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("refused retries created rows: count=%d err=%v", count, err)
	}
}
