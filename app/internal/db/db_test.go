package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSchemaCopyIsCurrent(t *testing.T) {
	b, err := os.ReadFile("../../../docs/schema/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != Schema {
		t.Fatal("internal/db/schema.sql differs from docs/schema/schema.sql: run `go generate ./...`")
	}
}

// Fresh returns an initialised throwaway database.
func Fresh(t *testing.T) *DB {
	t.Helper()
	p := filepath.Join(t.TempDir(), "life.db")
	if err := Init(p); err != nil {
		t.Fatal(err)
	}
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestInitRefusesExistingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "life.db")
	os.WriteFile(p, nil, 0o644)
	if err := Init(p); err == nil {
		t.Fatal("Init overwrote an existing file")
	}
}

func TestOpenRefusesForeignFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "other.db")
	o, _ := sql.Open("lifelog", dsn(p, false))
	o.Exec("CREATE TABLE x(a)")
	o.Close()
	if _, err := Open(p); err == nil || !strings.Contains(err.Error(), "not a Lifelog") {
		t.Fatalf("Open of a non-Lifelog file: %v", err)
	}
}

func TestPragmasAndReaders(t *testing.T) {
	d := Fresh(t)
	for _, p := range pragmas {
		var v int64
		d.W.QueryRow("PRAGMA " + p.name).Scan(&v)
		if v != p.want {
			t.Errorf("PRAGMA %s = %d, want %d", p.name, v, p.want)
		}
	}
	if _, err := d.R.Exec("INSERT INTO lifelog_meta(key, value) VALUES ('x', 'y')"); err == nil {
		t.Error("the read-only pool accepted a write")
	}
	if _, err := d.W.Exec("INSERT INTO pages_fts_data VALUES (99, 'x')"); err == nil {
		t.Error("defensive mode is off: an FTS shadow table was writable")
	}
}

func TestWriteTakesTheLockUpFront(t *testing.T) {
	d := Fresh(t)
	var path string
	d.W.QueryRow("SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&path)
	other, err := sql.Open("sqlite", // the plain driver: this second writer is the test's, not ours
		"file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(0)")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	err = d.Write(context.Background(), func(*sql.Tx) error {
		// nothing written yet, but BEGIN IMMEDIATE already holds the write lock
		_, err := other.Exec("BEGIN IMMEDIATE")
		if err == nil {
			other.Exec("ROLLBACK")
			t.Error("a second writer got the lock inside an open Write")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
