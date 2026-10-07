package db

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
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
	dir := t.TempDir()
	cases := []struct {
		name    string
		path    string
		prepare func(*testing.T, string)
	}{
		{
			name: "empty file",
			path: filepath.Join(dir, "empty.db"),
			prepare: func(t *testing.T, p string) {
				t.Helper()
				writeFile(t, p, nil)
			},
		},
		{
			name: "nonempty file",
			path: filepath.Join(dir, "nonempty.db"),
			prepare: func(t *testing.T, p string) {
				t.Helper()
				writeFile(t, p, []byte("not a lifelog database\n"))
			},
		},
		{
			name:    "foreign sqlite file",
			path:    filepath.Join(dir, "foreign.db"),
			prepare: createForeignSQLiteFile,
		},
		{
			name: "directory",
			path: filepath.Join(dir, "directory.db"),
			prepare: func(t *testing.T, p string) {
				t.Helper()
				if err := os.Mkdir(p, 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.prepare(t, tc.path)
			before := snapshotPath(t, tc.path)
			if err := Init(tc.path); err == nil {
				t.Fatal("Init accepted an existing path")
			}
			assertSnapshot(t, tc.path, before)
		})
	}
}

type pathSnapshot struct {
	mode  os.FileMode
	bytes []byte
}

func snapshotPath(t *testing.T, p string) pathSnapshot {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	s := pathSnapshot{mode: info.Mode()}
	if !info.IsDir() {
		s.bytes, err = os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func assertSnapshot(t *testing.T, p string, want pathSnapshot) {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("existing path was removed: %v", err)
	}
	if info.Mode() != want.mode {
		t.Fatalf("existing path mode changed from %v to %v", want.mode, info.Mode())
	}
	if info.IsDir() {
		return
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("existing file cannot be read: %v", err)
	}
	if !bytes.Equal(got, want.bytes) {
		t.Fatalf("existing file changed from %q to %q", want.bytes, got)
	}
}

func writeFile(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func createForeignSQLiteFile(t *testing.T, p string) {
	t.Helper()
	d, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec("CREATE TABLE foreign_table(value TEXT)"); err != nil {
		d.Close()
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestInitRefusesPathWhenParentIsNotDirectory(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	want := []byte("parent sentinel")
	writeFile(t, parent, want)
	p := filepath.Join(parent, "life.db")
	if err := Init(p); err == nil {
		t.Fatal("Init treated a non-directory parent as a free database path")
	}
	got, err := os.ReadFile(parent)
	if err != nil {
		t.Fatalf("parent sentinel was removed: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("parent sentinel changed to %q", got)
	}
}

func TestInitConcurrentReservation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "life.db")
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = Init(p)
		}(i)
	}
	close(start)
	wg.Wait()

	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent Init successes = %d, errors = %v; want exactly one", successes, errs)
	}
	d, err := Open(p)
	if err != nil {
		t.Fatalf("winner database was not preserved as a valid life.db: %v", err)
	}
	d.Close()
}

func TestInitReservationBlocksCompetingLazyOpen(t *testing.T) {
	p := filepath.Join(t.TempDir(), "life.db")
	applyErr := errors.New("controlled apply failure")
	var competitorErr error
	err := initWith(p, func(*sql.DB) error {
		competitorErr = Init(p)
		return applyErr
	})
	if err == nil || !strings.Contains(err.Error(), applyErr.Error()) {
		t.Fatalf("initWith error = %v, want controlled apply failure", err)
	}
	if competitorErr == nil || !strings.Contains(competitorErr.Error(), "already exists") {
		_, statErr := os.Stat(p)
		t.Fatalf("competing Init error = %v, file stat after cleanup = %v; want reservation refusal", competitorErr, statErr)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("failed reserved file remains after cleanup: %v", err)
	}
}

func TestInitApplyFailureRemovesOwnedReservation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "life.db")
	applyErr := errors.New("controlled apply failure")
	err := initWith(p, func(*sql.DB) error { return applyErr })
	if err == nil || !strings.Contains(err.Error(), applyErr.Error()) {
		t.Fatalf("initWith error = %v, want controlled apply failure", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("failed owned reservation remains: %v", err)
	}
}

func TestInitApplyFailurePreservesReplacement(t *testing.T) {
	p := filepath.Join(t.TempDir(), "life.db")
	replacement := []byte("replacement created after reservation\n")
	applyErr := errors.New("controlled apply failure")
	err := initWith(p, func(*sql.DB) error {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
		writeFile(t, p, replacement)
		return applyErr
	})
	if err == nil || !strings.Contains(err.Error(), applyErr.Error()) {
		t.Fatalf("initWith error = %v, want controlled apply failure", err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("replacement was removed: %v", err)
	}
	if !bytes.Equal(got, replacement) {
		t.Fatalf("replacement changed to %q", got)
	}
}

func TestReservedCleanupWithoutIdentityPreservesPath(t *testing.T) {
	p := filepath.Join(t.TempDir(), "life.db")
	want := []byte("unknown identity sentinel\n")
	writeFile(t, p, want)
	if err := removeReservedDatabasePath(p, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("path with unknown identity was removed: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("path with unknown identity changed to %q", got)
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
	checkShadowWrite := func(conn *sql.DB, defensive bool) {
		t.Helper()
		var tableCount, id int64
		var before []byte
		if err := conn.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='entities_fts_data'").Scan(&tableCount); err != nil || tableCount != 1 {
			t.Fatalf("shadow table fixture: count=%d err=%v", tableCount, err)
		}
		if err := conn.QueryRow("SELECT id, block FROM entities_fts_data ORDER BY id LIMIT 1").Scan(&id, &before); err != nil {
			t.Fatal(err)
		}
		result, err := conn.Exec("UPDATE entities_fts_data SET block=block WHERE id=?", id)
		if defensive {
			if err == nil {
				t.Error("defensive connection allowed a write to an existing FTS shadow row")
			}
		} else {
			if err != nil {
				t.Fatalf("non-defensive control refused the same valid shadow write: %v", err)
			}
			if n, err := result.RowsAffected(); err != nil || n != 1 {
				t.Fatalf("non-defensive control affected=%d err=%v", n, err)
			}
		}
		var after []byte
		if err := conn.QueryRow("SELECT block FROM entities_fts_data WHERE id=?", id).Scan(&after); err != nil || !bytes.Equal(before, after) {
			t.Fatalf("shadow row changed: err=%v", err)
		}
	}
	checkShadowWrite(d.W, true)
	controlPath := filepath.Join(t.TempDir(), "non-defensive.db")
	if err := Init(controlPath); err != nil {
		t.Fatal(err)
	}
	control, err := sql.Open("sqlite", sqliteFileURI(controlPath, nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := control.Close(); err != nil {
			t.Error(err)
		}
	})
	checkShadowWrite(control, false)
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

func TestSnapshotDestinationErrors(t *testing.T) {
	root := t.TempDir()
	live := filepath.Join(root, "life.db")
	if err := Init(live); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	for _, dest := range []string{filepath.Join(root, "missing"), live} {
		if _, err := Snapshot(live, dest, time.Now()); err == nil {
			t.Errorf("accepted non-directory %s", dest)
		}
	}
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	// A dangling .git entry is still a privacy marker, not evidence of safety.
	if err := os.Symlink(filepath.Join(root, "absent"), filepath.Join(target, ".git")); err != nil {
		t.Logf("dangling-marker check unavailable: %v", err)
	} else if _, err := Snapshot(live, target, time.Now()); err == nil || !strings.Contains(err.Error(), "git work tree") {
		t.Errorf("dangling marker refusal: %v", err)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != ".git" {
			t.Errorf("refusal left artifact %s", entry.Name())
		}
	}
	after, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("destination error changed source")
	}
}
