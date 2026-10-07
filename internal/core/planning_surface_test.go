package core

import "testing"

func TestPageRetainsTaskProjectContextAfterTombstones(t *testing.T) {
	s := fresh(t)
	projectID, _, err := s.CreatePage(ctx, "cli", "Garden project", "Context remains a page.")
	if err != nil {
		t.Fatal(err)
	}
	ordinaryID, _, err := s.CreatePage(ctx, "cli", "Ordinary page", "")
	if err != nil {
		t.Fatal(err)
	}
	page := func(id int64) *Page {
		t.Helper()
		p, err := s.PageByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	if page(projectID).TaskProject || page(ordinaryID).TaskProject {
		t.Fatal("unreferenced page reported task project context")
	}
	taskID, _, err := s.CreateTask(ctx, "cli", TaskSpec{Label: "Buy seeds", ProjectPageID: &projectID}, "")
	if err != nil {
		t.Fatal(err)
	}
	if p := page(projectID); !p.TaskProject || p.SessionKind || p.Type != "page" {
		t.Fatalf("project context %+v", p)
	}
	task, err := s.Task(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.TaskLifecycle(taskID, task.Version, true) }); err != nil {
		t.Fatal(err)
	}
	if !page(projectID).TaskProject {
		t.Fatal("task tombstone erased retained project context")
	}
	if err := s.Tombstone(ctx, "cli", projectID); err != nil {
		t.Fatal(err)
	}
	if p := page(projectID); !p.TaskProject || p.DeletedAt == "" {
		t.Fatalf("project tombstone lost reference context %+v", p)
	}
	if page(ordinaryID).TaskProject {
		t.Fatal("ordinary page acquired unrelated project context")
	}
}
