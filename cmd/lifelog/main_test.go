package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
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

// TestFileCommand is `lifelog file PATH` on a throwaway database: the original is hashed and never stored, its text
// read from --text, its picture made from it; the same file again keeps one page.
func TestFileCommand(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "life.db")
	if err := db.Init(live); err != nil {
		t.Fatal(err)
	}
	m := image.NewGray(image.Rect(0, 0, 64, 48))
	var b bytes.Buffer
	if err := jpeg.Encode(&b, m, nil); err != nil {
		t.Fatal(err)
	}
	pic, text := filepath.Join(dir, "IMG_0001.jpg"), filepath.Join(dir, "caption.txt")
	os.WriteFile(pic, b.Bytes(), 0o644)
	os.WriteFile(text, []byte("Dawn at the lake with [[Sam]]."), 0o644)
	for range 2 {
		o, err := parse([]string{"file", pic, "--db", live, "--text", text, "--day", "2026-10-04"})
		if err != nil {
			t.Fatal(err)
		}
		if err := run(o); err != nil {
			t.Fatal(err)
		}
	}
	d, err := db.Open(live)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := &core.Store{DB: d}
	id, _ := s.PageID(context.Background(), "IMG_0001.jpg")
	p, err := s.PageByID(context.Background(), id)
	if err != nil || p.Type != "file" || p.Body != "Dawn at the lake with [[Sam]]." || p.Day != "2026-10-04" || p.File == nil || !p.File.Preview {
		t.Fatalf("the file page: %v %+v", err, p)
	}
	var n int
	d.R.QueryRow(`SELECT count(*) FROM files`).Scan(&n)
	if n != 1 {
		t.Errorf("%d files rows after keeping one file twice", n)
	}
}

// TestPhotosInventory runs `lifelog import photos inventory` on a tiny synthetic export: it reads and writes nothing.
func TestPhotosInventory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Google Photos", "Photos from 2019")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "IMG_1.jpg"), []byte("\xff\xd8\xff\xd9"), 0o644)
	os.WriteFile(filepath.Join(dir, "IMG_1.jpg.json"), []byte(`{"title":"IMG_1.jpg","photoTakenTime":{"timestamp":"1"}}`), 0o644)
	for _, human := range []string{"", "--human"} {
		argv := []string{"import", "photos", "inventory", filepath.Dir(dir)}
		if human != "" {
			argv = append(argv, human)
		}
		o, err := parse(argv)
		if err != nil {
			t.Fatal(err)
		}
		if err := run(o); err != nil {
			t.Fatal(err)
		}
	}
}
