package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

// TestSnapshot is `lifelog snapshot` on a live file the writer still holds open (docs/cookbook/take-a-snapshot.md):
// the dated name, the time added on a second snapshot that day, never an existing file, the restore check passing
// and leaving the snapshot as it was.
func TestSnapshot(t *testing.T) {
	ctx := context.Background()
	live := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(live); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(live)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, _, err := (&core.Store{DB: d}).Capture(ctx, "cli", "2026-10-02", "Walked to [[Lakeside]] with [[Sam]].", nil); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 300 { // enough rows that PRAGMA optimize would have statistics to write into the snapshot
		if _, _, err := (&core.Store{DB: d}).Capture(ctx, "cli", day.AddDate(0, 0, i).Format("2006-01-02"), fmt.Sprintf("Ran with [[Sam]] past [[Note %d]].", i), nil); err != nil {
			t.Fatal(err)
		}
	}

	dir := t.TempDir()
	now := time.Date(2026, 10, 2, 14, 30, 5, 0, time.Local)
	first, res, err := takeSnapshot(ctx, live, dir, now)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(first) != "life-2026-10-02.db" {
		t.Errorf("the first snapshot of the day is %s, want life-2026-10-02.db", filepath.Base(first))
	}
	if !res.OK {
		t.Errorf("the restore check failed on a clean file: %+v", res)
	}
	// the restore check, as takeSnapshot runs it, on a snapshot no check has touched yet
	untouched, err := db.Snapshot(live, t.TempDir(), now)
	if err != nil {
		t.Fatal(err)
	}
	b0 := mustRead(t, untouched)
	r, err := db.OpenSnapshot(untouched)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	r.R.QueryRow("SELECT count(*) FROM pages WHERE title IN ('2026-10-02', 'Lakeside', 'Sam')").Scan(&n)
	if res, err := (&core.Store{DB: r}).Integrity(ctx); err != nil || !res.OK {
		t.Errorf("the restore check on the snapshot: %+v %v", res, err)
	}
	r.Close()
	if n != 3 {
		t.Errorf("the snapshot holds %d of the 3 pages", n)
	}
	if !bytes.Equal(b0, mustRead(t, untouched)) {
		t.Error("the restore check changed the snapshot (PRAGMA optimize at close?)")
	}

	before := mustRead(t, first)
	second, res, err := takeSnapshot(ctx, live, dir, now)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(second) != "life-2026-10-02T143005.db" || !res.OK {
		t.Errorf("the second snapshot of the day is %s (restore check ok: %v), want life-2026-10-02T143005.db", filepath.Base(second), res.OK)
	}
	after, _ := os.ReadFile(first)
	if !bytes.Equal(before, after) {
		t.Error("the restore check or the second snapshot changed the first snapshot")
	}
	if _, _, err := takeSnapshot(ctx, live, dir, now); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("a third snapshot in the same second: %v, want a refusal", err)
	}
	if !bytes.Equal(before, mustRead(t, first)) {
		t.Error("a refused snapshot changed an existing file")
	}
	// SQLite itself writes into an existing empty file: the refusal is the writer's own
	next := now.AddDate(0, 0, 1)
	for _, name := range []string{"life-2026-10-03.db", "life-2026-10-03T143005.db"} {
		os.WriteFile(filepath.Join(dir, name), nil, 0o644)
	}
	if _, _, err := takeSnapshot(ctx, live, dir, next); err == nil {
		t.Error("a snapshot onto an existing empty file was not refused")
	}
	if b := mustRead(t, filepath.Join(dir, "life-2026-10-03T143005.db")); len(b) != 0 {
		t.Errorf("a refused snapshot wrote %d bytes into an existing empty file", len(b))
	}
}

func TestSnapshotRefusesAGitWorkTree(t *testing.T) {
	live := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(live); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	sub := filepath.Join(repo, "backups")
	os.Mkdir(sub, 0o755)
	if _, _, err := takeSnapshot(context.Background(), live, sub, time.Now()); err == nil || !strings.Contains(err.Error(), "git work tree") {
		t.Errorf("a snapshot inside a git work tree: %v, want a refusal", err)
	}
	if ents, _ := os.ReadDir(sub); len(ents) != 0 {
		t.Errorf("a refused snapshot left %d files", len(ents))
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
