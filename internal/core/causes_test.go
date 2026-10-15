package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A query that runs out of time says so: the cause stays in the error, so the api answers 504, not 422
// (docs/plans/098-the-codebase-made-cohesive.md).
func TestAnExpiredQueryKeepsItsCause(t *testing.T) {
	s := fresh(t)
	expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer cancel()
	_, err := s.Query(expired, "SELECT 1", 10)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the cause is lost: %v", err)
	}
}

// Readings from a table of a tombstoned metric are refused as "no metric" (404), before any row is read: a tombstoned
// metric is no metric to record against, on this path as on record.
func TestReadingsFromTableRefuseATombstonedMetric(t *testing.T) {
	s := fresh(t)
	if _, err := s.RegisterMetric(ctx, "cli", "Ferritin", "ng/mL", ""); err != nil {
		t.Fatal(err)
	}
	id, err := s.PageID(ctx, "Ferritin")
	if err != nil || id == 0 {
		t.Fatalf("the metric's page: %d %v", id, err)
	}
	if err := s.Tombstone(ctx, "cli", id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Iron results", "| day | ng/mL |\n|---|---|\n| 2026-01-01 | 70 |\n", ""); err != nil {
		t.Fatal(err)
	}
	_, err = s.ReadingsFromTable(ctx, "Iron results", "Ferritin", "")
	if status(err) != 404 {
		t.Fatalf("a tombstoned metric: %v, want 404", err)
	}
}
