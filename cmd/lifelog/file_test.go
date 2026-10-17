package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/db/dbtest"
	"lifelog/internal/photo/phototest"
)

// TestFileCommand is `lifelog file PATH` on a throwaway database: the original is hashed and never stored, its text
// read from --text, its picture made from it; the same file again keeps one page.
func TestFileCommand(t *testing.T) {
	dir, live := t.TempDir(), dbtest.File(t)
	pic, text := filepath.Join(dir, "IMG_0001.jpg"), filepath.Join(dir, "caption.txt")
	os.WriteFile(pic, phototest.JPEG(64, 48), 0o644)
	os.WriteFile(text, []byte("Dawn at the lake with [[Sam]]."), 0o644)
	e, _, _ := testEnv(t)
	for range 2 {
		if err := runArgs(t.Context(), e, []string{"file", pic, "--db", live, "--text", text, "--day", "2026-10-04"}); err != nil {
			t.Fatal(err)
		}
	}
	d, err := db.Open(t.Context(), live)
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

// TestFileCommandFilesInACategory is `lifelog file PATH --category TITLE`: the original kept and its page filed in the
// category in one write, the category made when it is new (docs/plans/117-one-browse-view.md, rule 6).
func TestFileCommandFilesInACategory(t *testing.T) {
	dir, live := t.TempDir(), dbtest.File(t)
	memo := filepath.Join(dir, "memo.txt")
	if err := os.WriteFile(memo, []byte("a synthetic memo"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, _, _ := testEnv(t)
	if err := runArgs(t.Context(), e, []string{"file", memo, "--db", live, "--day", "2026-10-04", "--category", "Papers"}); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(t.Context(), live)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := &core.Store{DB: d}
	file, err := s.PageID(t.Context(), "memo.txt")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := s.PageID(t.Context(), "Papers")
	if err != nil {
		t.Fatal(err)
	}
	if p, err := s.PageByID(t.Context(), cat); err != nil || p.Type != "page" {
		t.Fatalf("the category Papers: %v %+v, want a plain page", err, p)
	}
	var all, filed int
	if err := d.R.QueryRowContext(t.Context(), `SELECT count(*), count(*) FILTER (WHERE from_id = ? AND to_id = ?)
		FROM links WHERE kind = 'part-of'`, file, cat).Scan(&all, &filed); err != nil {
		t.Fatal(err)
	}
	if all != 1 || filed != 1 || file == 0 {
		t.Errorf("%d part-of links, %d from memo.txt (%d) to Papers (%d): want the one link", all, filed, file, cat)
	}
}
