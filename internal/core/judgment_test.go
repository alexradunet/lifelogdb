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
