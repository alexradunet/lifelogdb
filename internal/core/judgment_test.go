package core

import "testing"

// The lifecycle of an entity answers like a task's or a session's (docs/plans/098-the-codebase-made-cohesive.md): a
// missing id is 404, the same state again is a no-op, and a promotion of what is not a plain page is a 409.
func TestEntityLifecycleStatusesAreTheCommonOnes(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Lifecycle witness", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", 999999); status(err) != 404 {
		t.Errorf("tombstone of a missing id: %v, want 404", err)
	}
	if err := s.Revive(ctx, "cli", 999999); status(err) != 404 {
		t.Errorf("revive of a missing id: %v, want 404", err)
	}
	if err := s.Promote(ctx, "cli", 999999, "place", ""); status(err) != 404 {
		t.Errorf("promote of a missing id: %v, want 404", err)
	}
	if err := s.Revive(ctx, "cli", id); err != nil {
		t.Errorf("revive of a live page is a no-op: %v", err)
	}
	if err := s.Tombstone(ctx, "cli", id); err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", id); err != nil {
		t.Errorf("tombstone of a tombstoned page is a no-op: %v", err)
	}
	p, err := s.PageByID(ctx, id)
	if err != nil || p.DeletedAt == "" {
		t.Fatalf("the page is tombstoned once: %+v %v", p, err)
	}
	if err := s.Promote(ctx, "cli", id, "place", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Promote(ctx, "cli", id, "person", ""); status(err) != 409 {
		t.Errorf("promote of a place: %v, want 409", err)
	}
}

// The page a task names as its project, made when no page has the title, is a plain page with no day, like the target
// of a wikilink: it does not appear in the day view of the day it was made.
func TestAnImplicitProjectPageHasNoDay(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreateTask(ctx, "cli", TaskForm{TaskSpec: TaskSpec{Label: "Plant the bulbs"}, Project: "Garden"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Task(ctx, id)
	if err != nil || task.ProjectPageID == nil {
		t.Fatalf("the task's project: %+v %v", task, err)
	}
	p, err := s.PageByID(ctx, *task.ProjectPageID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Day != "" {
		t.Errorf("the implicit project page %q has the day %q; a wikilink's target has none", p.Title, p.Day)
	}
}

// Relocating a reading's scope is the owner's: an agent and an import are refused with 403, as the api refuses every
// owner-only action.
func TestRelocationIsTheOwnersWith403(t *testing.T) {
	s := fresh(t)
	if _, err := s.RegisterMetric(ctx, "cli", "Steps", "steps", ""); err != nil {
		t.Fatal(err)
	}
	id, err := s.Record(ctx, "cli", Reading{Metric: "Steps", Day: "2026-01-02", Value: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"agent:lmstudio", "import:notebook"} {
		_, err := s.RelocateReading(ctx, source, id, Reading{Metric: "Steps", Day: "2026-01-02", Value: 100})
		if status(err) != 403 {
			t.Errorf("%s relocating a reading: %v, want 403", source, err)
		}
	}
}

// HabitStates over a window says, for each day and each habit active on it, what Habits says for that day alone: a
// hand-checked week with a habit that starts mid-week, a check-in, a miss and an invalid value.
func TestHabitStatesOverAWindowMatchTheDaysAlone(t *testing.T) {
	s := fresh(t)
	if _, err := s.RegisterMetric(ctx, "cli", "Walk", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.StartHabit(ctx, "cli", "Walk", "2026-03-03", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CheckIn(ctx, "cli", "Walk", "2026-03-03", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CheckIn(ctx, "cli", "Walk", "2026-03-04", false); err != nil {
		t.Fatal(err)
	}
	// Deliberate damage past the writer and the DDL: a value a habit never records, written with its trigger dropped,
	// so the state is invalid.
	withoutTrigger(t, s, "measurements_habit_binary", `INSERT INTO measurements(metric_id, day, value, source, created_at)
		SELECT h.metric_id, '2026-03-06', 7, 'cli', strftime('%Y-%m-%dT%H:%M:%fZ','now') FROM habit_periods h LIMIT 1`)
	got, err := s.HabitStates(ctx, "2026-03-01", "2026-03-07")
	if err != nil {
		t.Fatal(err)
	}
	want := []HabitDay{
		{"2026-03-03", HabitState{"Walk", "done"}}, {"2026-03-04", HabitState{"Walk", "not done"}},
		{"2026-03-05", HabitState{"Walk", "not recorded"}}, {"2026-03-06", HabitState{"Walk", "invalid"}},
		{"2026-03-07", HabitState{"Walk", "not recorded"}},
	}
	if len(got) != len(want) {
		t.Fatalf("%d states, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("state %d: %+v, want %+v", i, got[i], want[i])
		}
		alone, err := s.Habits(ctx, want[i].Day)
		if err != nil || len(alone) != 1 || alone[0] != want[i].HabitState {
			t.Errorf("the day %s alone: %+v %v, want %+v", want[i].Day, alone, err, want[i].HabitState)
		}
	}
	if before, err := s.Habits(ctx, "2026-03-02"); err != nil || len(before) != 0 {
		t.Errorf("the day before the habit started has no habit: %+v %v", before, err)
	}
}
