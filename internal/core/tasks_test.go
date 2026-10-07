package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"lifelog/internal/db"
)

func planningTask(t *testing.T, s *Store, spec TaskSpec) *Task {
	t.Helper()
	id, _, err := s.CreateTask(ctx, "cli", spec, "")
	if err != nil {
		t.Fatal(err)
	}
	return planningReadTask(t, s, id)
}

func planningReadTask(t *testing.T, s *Store, id int64) *Task {
	t.Helper()
	p, err := s.Task(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func planningReadOccurrence(t *testing.T, s *Store, taskID int64, key string) *TaskOccurrence {
	t.Helper()
	o, err := s.TaskOccurrence(ctx, taskID, key)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func planningMaterialize(t *testing.T, s *Store, p *Task, key string) *TaskOccurrence {
	t.Helper()
	var o *TaskOccurrence
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		var err error
		o, _, err = tx.MaterializeTaskOccurrence(p.ID, p.Version, key)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return o
}

func planningEditOccurrence(t *testing.T, s *Store, p *Task, o *TaskOccurrence, changes OccurrenceChanges) *TaskOccurrence {
	t.Helper()
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		return tx.EditTaskOccurrence(p.ID, p.Version, o.OccurrenceKey, o.Version, changes)
	}); err != nil {
		t.Fatal(err)
	}
	return planningReadOccurrence(t, s, p.ID, o.OccurrenceKey)
}

func TestTaskDeadlineMergeUsesCurrentDueDaysAndRetainedSlots(t *testing.T) {
	s := fresh(t)
	p := planningTask(t, s, TaskSpec{Label: "Monthly letter", RepeatUnit: "month", RepeatEvery: 1, AnchorDay: "2026-10-01", RepeatUntilDay: "2027-02-01"})
	oct := planningMaterialize(t, s, p, "2026-10-01")
	nov := planningMaterialize(t, s, p, "2026-11-01")
	dec := planningMaterialize(t, s, p, "2026-12-01")
	jan := planningMaterialize(t, s, p, "2027-01-01")
	moved := oct.OccurrenceChanges
	moved.DueDay = "2026-11-01"
	oct = planningEditOccurrence(t, s, p, oct, moved)
	moved = nov.OccurrenceChanges
	moved.DueDay = "2026-12-01"
	nov = planningEditOccurrence(t, s, p, nov, moved)
	undated := dec.OccurrenceChanges
	undated.DueDay = ""
	dec = planningEditOccurrence(t, s, p, dec, undated)
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		return tx.TaskOccurrenceLifecycle(p.ID, p.Version, jan.OccurrenceKey, jan.Version, true)
	}); err != nil {
		t.Fatal(err)
	}
	jan = planningReadOccurrence(t, s, p.ID, jan.OccurrenceKey)
	for _, tc := range []struct {
		from, through string
		historical    bool
		want          *TaskOccurrence
	}{
		{"2026-10-01", "2026-10-31", false, nil},
		{"2026-11-01", "2026-11-30", false, oct},
		{"2026-12-01", "2026-12-31", false, nov},
		{"2027-01-01", "2027-01-31", false, nil},
		{"2027-01-01", "2027-01-31", true, jan},
	} {
		t.Run(fmt.Sprintf("%s/historical=%v", tc.from, tc.historical), func(t *testing.T) {
			got, err := s.TaskOccurrences(ctx, p.ID, tc.from, tc.through, tc.historical)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == nil {
				if len(got) != 0 {
					t.Fatalf("suppressed/moved slot regenerated: %+v", got)
				}
			} else if len(got) != 1 || got[0] != *tc.want {
				t.Fatalf("deadline merge got %+v, want %+v", got, tc.want)
			}
		})
	}
	got, err := s.TaskOccurrences(ctx, p.ID, "2027-02-01", "2027-02-28", false)
	if err != nil || len(got) != 1 || !got[0].Virtual || got[0].ID != 0 || got[0].OccurrenceKey != "2027-02-01" || got[0].DueDay != "2027-02-01" || got[0].State != "open" || got[0].TaskVersion != p.Version {
		t.Fatalf("virtual slot got %+v, %v", got, err)
	}
	if dec.DueDay != "" || dec.OccurrenceKey != "2026-12-01" {
		t.Fatalf("undating changed identity: %+v", dec)
	}
	var n int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM task_occurrences`).Scan(&n); err != nil || n != 4 {
		t.Fatalf("reads materialized rows: count=%d err=%v", n, err)
	}
}

func TestTaskProjectsKeepIndependentLabelsAndCurrentPageContext(t *testing.T) {
	s := fresh(t)
	firstProject, _, err := s.CreatePage(ctx, "cli", "Home maintenance", "")
	if err != nil {
		t.Fatal(err)
	}
	secondProject, _, err := s.CreatePage(ctx, "cli", "Family visits", "")
	if err != nil {
		t.Fatal(err)
	}
	spec := TaskSpec{Label: "Review arrangements", ProjectPageID: &firstProject, ReminderLocalTime: "09:00", ReminderZone: "UTC"}
	first := planningTask(t, s, spec)
	spec.ProjectPageID = &secondProject
	second := planningTask(t, s, spec)
	if first.ID == second.ID || first.ProjectTitle != "Home maintenance" || second.ProjectTitle != "Family visits" {
		t.Fatalf("duplicate labels lost identity/context: first=%+v second=%+v", first, second)
	}
	o := planningReadOccurrence(t, s, first.ID, "once")
	changes := o.OccurrenceChanges
	changes.DueDay = "2026-10-07"
	planningEditOccurrence(t, s, first, o, changes)
	if _, err := s.Rename(ctx, "cli", firstProject, "House upkeep"); err != nil {
		t.Fatal(err)
	}
	renamed := planningReadTask(t, s, first.ID)
	if renamed.ProjectPageID == nil || *renamed.ProjectPageID != firstProject || renamed.ProjectTitle != "House upkeep" || renamed.Label != first.Label {
		t.Fatalf("project rename changed task identity: %+v", renamed)
	}
	if err := s.Promote(ctx, "cli", secondProject, "person", "Synthetic person"); status(err) != 422 {
		t.Fatalf("promoted a retained project reference: %v", err)
	}
	if err := s.Tombstone(ctx, "cli", firstProject); err != nil {
		t.Fatal(err)
	}
	renamed = planningReadTask(t, s, first.ID)
	got, err := s.TaskOccurrences(ctx, first.ID, "2026-10-07", "2026-10-07", false)
	if err != nil || len(got) != 1 || got[0].State != "open" || got[0].ProjectDeletedAt == "" || got[0].ProjectTitle != "House upkeep" || got[0].TaskDeletedAt != "" {
		t.Fatalf("project tombstone hid or completed task: %+v err=%v", got, err)
	}
	reminder, err := ResolveTaskReminder(*renamed, got[0])
	if err != nil || reminder.State != "resolved" || reminder.At != "2026-10-07T09:00:00.000Z" {
		t.Fatalf("project tombstone suppressed reminder intent: %+v err=%v", reminder, err)
	}
}

func TestTaskStopUsesOriginalKeysAndRetainsHistoricalOutcomes(t *testing.T) {
	s := fresh(t)
	p := planningTask(t, s, TaskSpec{Label: "Review accounts", RepeatUnit: "month", RepeatEvery: 1, AnchorDay: "2026-10-01"})
	oct := planningMaterialize(t, s, p, "2026-10-01")
	nov := planningMaterialize(t, s, p, "2026-11-01")
	dec := planningMaterialize(t, s, p, "2026-12-01")
	jan := planningMaterialize(t, s, p, "2027-01-01")
	moved := oct.OccurrenceChanges
	moved.DueDay = "2026-12-15"
	oct = planningEditOccurrence(t, s, p, oct, moved)
	done := nov.OccurrenceChanges
	done.State = "done" // Historical evidence knows the outcome, not its instant.
	nov = planningEditOccurrence(t, s, p, nov, done)
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		return tx.TaskOccurrenceLifecycle(p.ID, p.Version, jan.OccurrenceKey, jan.Version, true)
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.StopTask(p.ID, p.Version, "2026-10-31") }); err != nil {
		t.Fatal(err)
	}
	stopped := planningReadTask(t, s, p.ID)
	if stopped.RepeatUntilDay != "2026-10-31" || stopped.Version == p.Version {
		t.Fatalf("stop did not update definition: %+v", stopped)
	}
	for _, old := range []*TaskOccurrence{oct, nov} {
		got := planningReadOccurrence(t, s, p.ID, old.OccurrenceKey)
		if got.State != old.State || got.DueDay != old.DueDay || got.CompletedAt != old.CompletedAt || got.Version != old.Version {
			t.Fatalf("stop changed retained original key/outcome: got %+v, want %+v", got, old)
		}
	}
	for _, key := range []string{"2026-12-01", "2027-01-01"} {
		got := planningReadOccurrence(t, s, p.ID, key)
		if got.State != "skipped" || key == "2027-01-01" && got.DeletedAt == "" {
			t.Fatalf("future original key not suppressed: %+v", got)
		}
	}
	dec = planningReadOccurrence(t, s, p.ID, dec.OccurrenceKey)
	open := dec.OccurrenceChanges
	open.State = "open"
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		return tx.EditTaskOccurrence(p.ID, stopped.Version, dec.OccurrenceKey, dec.Version, open)
	}); status(err) != 422 {
		t.Fatalf("reopened after end: %v", err)
	}
	if got := planningReadOccurrence(t, s, p.ID, dec.OccurrenceKey); *got != *dec {
		t.Fatalf("refused reopen changed row: %+v", got)
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.StopTask(p.ID, stopped.Version, "2026-11-30") }); status(err) != 422 {
		t.Fatalf("extended an ended recurrence: %v", err)
	}
	if err := s.Do(ctx, "import:synthetic", func(tx *Tx) error {
		_, _, err := tx.CaptureTaskOccurrence(p.ID, stopped.Version, OccurrenceInput{OccurrenceKey: "2027-02-01", ImportKey: "review:2027:02", OccurrenceChanges: OccurrenceChanges{DueDay: "2027-02-01", State: "done", ReminderMode: "inherit"}})
		return err
	}); err != nil {
		t.Fatalf("refused evidenced historical outcome: %v", err)
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		_, _, err := tx.MaterializeTaskOccurrence(p.ID, stopped.Version, "2027-03-01")
		return err
	}); status(err) != 422 {
		t.Fatalf("new open slot beyond end: %v", err)
	}
	got, err := s.TaskOccurrences(ctx, p.ID, "2026-12-01", "2026-12-31", false)
	if err != nil || len(got) != 2 || got[0].OccurrenceKey != "2026-12-01" || got[0].State != "skipped" || got[1].OccurrenceKey != "2026-10-01" || got[1].State != "open" {
		t.Fatalf("stopped deadline merge %+v, %v", got, err)
	}
}

func TestTaskEditsValidateBothRevisionsAndKeepNoOps(t *testing.T) {
	s := fresh(t)
	p := planningTask(t, s, TaskSpec{Label: "Call home", ReminderLocalTime: "09:00", ReminderZone: "UTC"})
	o := planningReadOccurrence(t, s, p.ID, "once")
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.EditTask(p.ID, p.Version, p.TaskSpec) }); err != nil {
		t.Fatal(err)
	}
	if got := planningReadTask(t, s, p.ID); !reflect.DeepEqual(got, p) {
		t.Fatalf("definition no-op changed row: %+v", got)
	}
	changed := p.TaskSpec
	changed.ReminderLocalTime = "10:00"
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.EditTask(p.ID, p.Version, changed) }); err != nil {
		t.Fatal(err)
	}
	current := planningReadTask(t, s, p.ID)
	currentOccurrence := planningReadOccurrence(t, s, p.ID, "once")
	if current.Version == p.Version || currentOccurrence.Version != o.Version || currentOccurrence.TaskVersion != current.Version {
		t.Fatalf("independent edit versions: task=%+v occurrence=%+v", current, currentOccurrence)
	}
	changes := o.OccurrenceChanges
	changes.DueDay = "2026-10-07"
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.EditTaskOccurrence(p.ID, p.Version, "once", o.Version, changes) }); status(err) != 409 {
		t.Fatalf("stale task version accepted: %v", err)
	}
	edited := planningEditOccurrence(t, s, current, currentOccurrence, changes)
	changes.State = "done"
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.EditTaskOccurrence(p.ID, current.Version, "once", o.Version, changes) }); status(err) != 409 {
		t.Fatalf("stale occurrence version accepted: %v", err)
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		return tx.EditTaskOccurrence(p.ID, current.Version, "once", edited.Version, edited.OccurrenceChanges)
	}); err != nil {
		t.Fatal(err)
	}
	if got := planningReadOccurrence(t, s, p.ID, "once"); *got != *edited {
		t.Fatalf("stale edits/no-op changed occurrence: got %+v, want %+v", got, edited)
	}
}

func TestTaskStopRollbackAndCancellationRestoreWholeState(t *testing.T) {
	s := fresh(t)
	p := planningTask(t, s, TaskSpec{Label: "Backup review", RepeatUnit: "month", RepeatEvery: 1, AnchorDay: "2026-10-01"})
	nov := planningMaterialize(t, s, p, "2026-11-01")
	dec := planningMaterialize(t, s, p, "2026-12-01")
	rollback := errors.New("deliberate caller rollback")
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		if err := tx.StopTask(p.ID, p.Version, "2026-10-31"); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		if got := planningReadTask(t, s, p.ID); !reflect.DeepEqual(got, p) {
			t.Fatalf("aborted stop changed definition: %+v", got)
		}
		for _, o := range []*TaskOccurrence{nov, dec} {
			if got := planningReadOccurrence(t, s, p.ID, o.OccurrenceKey); *got != *o {
				t.Fatalf("aborted stop changed occurrence: got %+v, want %+v", got, o)
			}
		}
	}
	check()
	request, cancel := context.WithCancel(ctx)
	defer cancel()
	err := s.Do(request, "cli", func(tx *Tx) error {
		if err := tx.StopTask(p.ID, p.Version, "2026-10-31"); err != nil {
			return err
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) && !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("cancelled transaction committed: %v", err)
	}
	check()
	// Deliberate exhaustion fault: the later occurrence's touch must roll back
	// the entire stop, including any previously touched occurrence and parent.
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE task_occurrences SET revision=9223372036854775807 WHERE id=?`, dec.ID); err != nil {
		t.Fatal(err)
	}
	dec = planningReadOccurrence(t, s, p.ID, dec.OccurrenceKey)
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.StopTask(p.ID, p.Version, "2026-10-31") }); err == nil {
		t.Fatal("stop succeeded despite exhausted occurrence revision")
	}
	check()
}

func TestTaskLifecycleContextSurvivesReopenAndSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(path); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	s := &Store{DB: d}
	project, _, err := s.CreatePage(ctx, "cli", "Annual maintenance", "Synthetic project context")
	if err != nil {
		t.Fatal(err)
	}
	p := planningTask(t, s, TaskSpec{Label: "Check the alarm", ProjectPageID: &project, RepeatUnit: "month", RepeatEvery: 1, AnchorDay: "2026-10-01", ReminderLocalTime: "09:00", ReminderZone: "UTC"})
	o := planningMaterialize(t, s, p, "2026-10-01")
	changes := o.OccurrenceChanges
	changes.ReminderMode, changes.ReminderOverrideAt = "at", "2026-10-01T11:00:00.000Z"
	o = planningEditOccurrence(t, s, p, o, changes)
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		return tx.TaskOccurrenceLifecycle(p.ID, p.Version, o.OccurrenceKey, o.Version, true)
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", project); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.TaskLifecycle(p.ID, p.Version, true) }); err != nil {
		t.Fatal(err)
	}
	p = planningReadTask(t, s, p.ID)
	o = planningReadOccurrence(t, s, p.ID, o.OccurrenceKey)
	if o.TaskDeletedAt == "" || o.ProjectDeletedAt == "" || o.ProjectTitle != "Annual maintenance" || o.DeletedAt == "" {
		t.Fatalf("historical identity lacks lifecycle context: %+v", o)
	}
	check := func(s *Store) {
		t.Helper()
		if got := planningReadTask(t, s, p.ID); !reflect.DeepEqual(got, p) {
			t.Fatalf("definition changed across persistence: got %+v want %+v", got, p)
		}
		if got := planningReadOccurrence(t, s, p.ID, o.OccurrenceKey); *got != *o {
			t.Fatalf("occurrence changed across persistence: got %+v want %+v", got, o)
		}
		active, err := s.TaskOccurrences(ctx, p.ID, "2026-10-01", "2026-12-31", false)
		if err != nil || len(active) != 0 {
			t.Fatalf("tombstone active results %+v err=%v", active, err)
		}
		history, err := s.TaskOccurrences(ctx, p.ID, "2026-10-01", "2026-12-31", true)
		if err != nil || len(history) != 1 || history[0] != *o {
			t.Fatalf("historical read invented slots or lost context: %+v err=%v", history, err)
		}
	}
	check(s)
	// Copy is the production VACUUM INTO engine used by Snapshot; naming and
	// destination-policy tests live with db.Snapshot itself.
	snapshot := filepath.Join(t.TempDir(), "snapshot.db")
	if err := db.Copy(path, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d, err = db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.DB = d
	check(s)
	restored, err := db.OpenSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := restored.Close(); err != nil {
			t.Error(err)
		}
	}()
	check(&Store{DB: restored})
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.TaskLifecycle(p.ID, p.Version, false) }); err != nil {
		t.Fatal(err)
	}
	revived := planningReadTask(t, s, p.ID)
	tomb := planningReadOccurrence(t, s, p.ID, o.OccurrenceKey)
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		return tx.TaskOccurrenceLifecycle(p.ID, revived.Version, tomb.OccurrenceKey, tomb.Version, false)
	}); err != nil {
		t.Fatal(err)
	}
	active, err := s.TaskOccurrences(ctx, p.ID, "2026-10-01", "2026-12-31", false)
	if err != nil || len(active) != 3 || active[0].ID != o.ID || active[0].DeletedAt != "" || active[0].TaskDeletedAt != "" || active[0].ProjectDeletedAt == "" || active[0].ReminderOverrideAt != o.ReminderOverrideAt {
		t.Fatalf("explicit restoration lost state/context: %+v err=%v", active, err)
	}
	check(&Store{DB: restored}) // Restoring live state cannot mutate the earlier snapshot.
}

func TestTaskDeadlineReadBoundsAndCancellationLeaveNoRows(t *testing.T) {
	s := fresh(t)
	p := planningTask(t, s, TaskSpec{Label: "Daily check", RepeatUnit: "day", RepeatEvery: 1, AnchorDay: "0000-01-01"})
	if _, err := s.TaskOccurrences(ctx, p.ID, "0000-01-01", "9999-12-31", false); status(err) != 422 {
		t.Fatalf("unbounded result accepted: %v", err)
	}
	request, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.TaskOccurrences(request, p.ID, "2026-10-01", "2026-10-31", false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read: %v", err)
	}
	got, err := s.TaskOccurrences(ctx, p.ID, "2026-10-07", "2026-10-07", false)
	if err != nil || len(got) != 1 || got[0].OccurrenceKey != "2026-10-07" || !got[0].Virtual {
		t.Fatalf("failed read leaked resources: %+v err=%v", got, err)
	}
	var n int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM task_occurrences`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("read persisted rows: n=%d err=%v", n, err)
	}
}

func TestTaskConcurrentMaterializationRetainsOneOccurrence(t *testing.T) {
	s := fresh(t)
	p := planningTask(t, s, TaskSpec{Label: "Retry check", RepeatUnit: "day", RepeatEvery: 1, AnchorDay: "2026-10-07"})
	type result struct {
		id       int64
		existing bool
		err      error
	}
	const workers = 8
	results := make(chan result, workers)
	start := make(chan struct{})
	for range workers {
		go func() {
			<-start
			var r result
			r.err = s.Do(ctx, "import:synthetic", func(tx *Tx) error {
				o, existing, err := tx.CaptureTaskOccurrence(p.ID, p.Version, OccurrenceInput{OccurrenceKey: "2026-10-07", ImportKey: "day:2026-10-07", OccurrenceChanges: OccurrenceChanges{DueDay: "2026-10-07", State: "open", ReminderMode: "inherit"}})
				if err == nil {
					r.id, r.existing = o.ID, existing
				}
				return err
			})
			results <- r
		}()
	}
	close(start)
	var id int64
	created := 0
	for range workers {
		r := <-results
		if r.err != nil {
			t.Errorf("concurrent capture: %v", r.err)
			continue
		}
		if id == 0 {
			id = r.id
		}
		if r.id != id {
			t.Errorf("duplicate identity %d vs %d", r.id, id)
		}
		if !r.existing {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("created %d occurrences, want one", created)
	}
	o := planningReadOccurrence(t, s, p.ID, "2026-10-07")
	if o.ID != id || o.ImportKey != "day:2026-10-07" || o.Version != "1" || o.State != "open" || o.DueDay != "2026-10-07" {
		t.Fatalf("retried occurrence changed %+v", o)
	}
}
