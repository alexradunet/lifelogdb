package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSchemaCopyIsCurrent(t *testing.T) {
	b, err := os.ReadFile("../../docs/schema/schema.sql")
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

func TestLiteralDatabasePaths(t *testing.T) {
	type literalCase struct {
		name      string
		requested string
		sentinel  string
		relative  bool
	}

	root := t.TempDir()
	cases := []literalCase{
		{
			name:      "absolute percent escape text",
			requested: filepath.Join(root, "literal paths café", "life %23 percent.db"),
			sentinel:  filepath.Join(root, "literal paths café", "life # percent.db"),
		},
		{
			name:      "absolute fragment character",
			requested: filepath.Join(root, "literal paths café", "life # fragment.db"),
			sentinel:  filepath.Join(root, "literal paths café", "life "),
		},
		{
			name:      "relative percent escape text",
			requested: filepath.Join("relative café %23", "life %23.db"),
			sentinel:  filepath.Join("relative café #", "life #.db"),
			relative:  true,
		},
	}
	if runtime.GOOS != "windows" {
		cases = append(cases, literalCase{
			name:      "absolute query character",
			requested: filepath.Join(root, "literal paths café", "life ? query.db"),
			sentinel:  filepath.Join(root, "literal paths café", "life "),
		})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.relative {
				cwd := filepath.Join(root, "cwd # %23 café")
				if err := os.MkdirAll(cwd, 0o755); err != nil {
					t.Fatal(err)
				}
				t.Chdir(cwd)
			}
			prepareLiteralSentinel(t, tc.sentinel)
			if err := os.MkdirAll(filepath.Dir(tc.requested), 0o755); err != nil {
				t.Fatal(err)
			}

			if err := Init(tc.requested); err != nil {
				t.Fatalf("Init(%q) failed; requested literal file %q, alternate sentinel %q: %v", tc.requested, absForError(tc.requested), absForError(tc.sentinel), err)
			}
			assertSamePhysicalFile(t, tc.requested)
			assertLiteralSentinel(t, tc.sentinel)

			d, err := Open(tc.requested)
			if err != nil {
				t.Fatalf("Open(%q) failed for requested literal file %q: %v", tc.requested, absForError(tc.requested), err)
			}
			if err := d.Write(context.Background(), func(tx *sql.Tx) error {
				_, err := tx.Exec(`INSERT INTO lifelog_meta(key, value) VALUES ('literal-path-test', ?)`, tc.name)
				return err
			}); err != nil {
				d.Close()
				t.Fatal(err)
			}
			var got string
			if err := d.R.QueryRow(`SELECT value FROM lifelog_meta WHERE key = 'literal-path-test'`).Scan(&got); err != nil || got != tc.name {
				d.Close()
				t.Fatalf("read back from %q = %q, %v; want %q", absForError(tc.requested), got, err, tc.name)
			}
			d.Close()

			copyStem := "copy-" + strings.NewReplacer(" ", "-", "?", "query").Replace(tc.name)
			copyPath := filepath.Join(filepath.Dir(tc.requested), copyStem+" # %23.db")
			copySentinel := filepath.Join(filepath.Dir(tc.requested), copyStem+" ")
			prepareLiteralSentinel(t, copySentinel)
			if err := Copy(tc.requested, copyPath); err != nil {
				t.Fatalf("Copy(%q, %q) failed; source literal %q, destination literal %q: %v", tc.requested, copyPath, absForError(tc.requested), absForError(copyPath), err)
			}
			assertSamePhysicalFile(t, copyPath)
			assertLiteralSentinel(t, copySentinel)
			c, err := Open(copyPath)
			if err != nil {
				t.Fatalf("Open(copy %q) failed: %v", absForError(copyPath), err)
			}
			if err := c.R.QueryRow(`SELECT value FROM lifelog_meta WHERE key = 'literal-path-test'`).Scan(&got); err != nil || got != tc.name {
				c.Close()
				t.Fatalf("read back from copied literal file %q = %q, %v; want %q", absForError(copyPath), got, err, tc.name)
			}
			c.Close()
		})
	}
}

const literalSentinel = "literal sqlite path sentinel\n"

func prepareLiteralSentinel(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(literalSentinel), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertLiteralSentinel(t *testing.T, p string) {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("alternate sentinel %q was removed: %v", absForError(p), err)
	}
	if string(b) != literalSentinel {
		t.Fatalf("alternate sentinel %q changed to %q", absForError(p), string(b))
	}
}

func assertSamePhysicalFile(t *testing.T, requested string) {
	t.Helper()
	d, err := Open(requested)
	if err != nil {
		t.Fatalf("Open(%q) after Init/Copy failed: %v", requested, err)
	}
	defer d.Close()
	var reported string
	if err := d.W.QueryRow("SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&reported); err != nil {
		t.Fatal(err)
	}
	want, err := os.Stat(requested)
	if err != nil {
		t.Fatalf("requested literal file %q does not exist: SQLite reported %q: %v", absForError(requested), absForError(reported), err)
	}
	got, err := os.Stat(reported)
	if err != nil {
		t.Fatalf("SQLite reported database file %q for requested %q, but it cannot be statted: %v", absForError(reported), absForError(requested), err)
	}
	if !os.SameFile(want, got) {
		t.Fatalf("SQLite opened %q, not requested literal file %q", absForError(reported), absForError(requested))
	}
}

func absForError(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
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
	var ts int64 = -1
	if err := d.R.QueryRow("PRAGMA trusted_schema").Scan(&ts); err != nil || ts != 0 {
		t.Errorf("the read-only pool has trusted_schema = %d (%v), want 0", ts, err)
	}
	var path string
	d.W.QueryRow("SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&path)
	trusting, _ := sql.Open("lifelog", "file:"+filepath.ToSlash(path)+"?mode=ro&_pragma=trusted_schema(1)")
	defer trusting.Close()
	if err := trusting.Ping(); err == nil || !strings.Contains(err.Error(), "trusted_schema") {
		t.Errorf("a reader with trusted_schema=ON was not refused: %v", err)
	}
	cp := filepath.Join(t.TempDir(), "copy.db")
	if err := Copy(path, cp); err != nil {
		t.Errorf("Copy through a reader with trusted_schema=OFF: %v", err)
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
