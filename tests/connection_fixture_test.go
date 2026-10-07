package tests

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSchemaFixtureUsesLiteralFilename(t *testing.T) {
	d := realDocs()
	s := &S{d: d, ddl: d.DDL(), dir: t.TempDir()}
	defer s.close()
	path := filepath.Join(s.dir, "literal # percent%20.db")
	c := s.freshWith(F{Path: path, Hardened: true})
	id := c.page("Literal path fixture")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fixture opened a different filename: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	ro := s.readOnly(path)
	ro.must("PRAGMA trusted_schema=OFF")
	if got := ro.str("SELECT title FROM entity_names WHERE entity_id=?", id); got != "Literal path fixture" {
		t.Fatalf("read-only reopen title=%q", got)
	}
	if err := ro.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(path) && entry.Name() != filepath.Base(path)+"-wal" && entry.Name() != filepath.Base(path)+"-shm" {
			t.Errorf("unexpected fixture file %q", entry.Name())
		}
	}
}
