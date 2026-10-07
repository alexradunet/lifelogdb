package db

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCopyTreatsLeadingFilePrefixAsLiteralPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a colon is not valid in a Windows filename")
	}
	t.Chdir(t.TempDir())
	source := filepath.Join(t.TempDir(), "source.db")
	if err := Init(source); err != nil {
		t.Fatal(err)
	}
	// SQLite accepts an existing empty VACUUM target. A URI interpretation must
	// not let the literal destination's prefix redirect the copy into this file.
	writeFile(t, "copy.db", nil)
	const destination = "file:copy.db"
	if err := Copy(source, destination); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile("copy.db"); err != nil || len(got) != 0 {
		t.Errorf("copy changed the alternate path: bytes=%d error=%v; want the existing empty file", len(got), err)
	}
	copied, err := Open(destination)
	if err != nil {
		t.Fatalf("literal destination does not hold the copied database: %v", err)
	}
	if err := copied.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCopyRefusesExistingDestinationEntries(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.db")
	if err := Init(source); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"empty", "nonempty", "dangling symlink", "existing symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			dest := filepath.Join(dir, "copy.db")
			target := filepath.Join(dir, "target.db")
			sentinel := []byte("keep this file")
			switch kind {
			case "empty":
				writeFile(t, dest, nil)
			case "nonempty":
				writeFile(t, dest, sentinel)
			case "existing symlink":
				writeFile(t, target, sentinel)
				fallthrough
			case "dangling symlink":
				if err := os.Symlink(target, dest); err != nil {
					t.Skipf("cannot create test-owned file symlink: %v", err)
				}
			}
			var before pathSnapshot
			if kind != "dangling symlink" {
				before = snapshotPath(t, dest)
			}
			if err := Copy(source, dest); err == nil {
				t.Error("Copy accepted an existing destination directory entry")
			}
			switch kind {
			case "dangling symlink":
				if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("copy created the dangling symlink's target: %v", err)
				}
			default:
				assertSnapshot(t, dest, before)
			}
			if kind == "dangling symlink" || kind == "existing symlink" {
				if got, err := os.Readlink(dest); err != nil || got != target {
					t.Errorf("destination symlink changed: target %q, error %v", got, err)
				}
			}
		})
	}
}

func TestCopyReservationFailureAndCleanup(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.db")
	if err := Init(source); err != nil {
		t.Fatal(err)
	}
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "owned reservation", true: "replacement preserved"}[replacement], func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "copy.db")
			want := []byte("replacement file")
			copyErr := errors.New("controlled copy failure")
			err := copyWith(source, dest, func(*sql.DB) error {
				// A competing copy must be refused before its SQLite connection opens.
				if err := Copy(source, dest); err == nil {
					t.Error("competing copy accepted the reserved destination")
				}
				if replacement {
					// Preserve the old inode so the filesystem cannot reuse it for the sentinel.
					if err := os.Rename(dest, dest+".reserved"); err != nil {
						t.Fatal(err)
					}
					writeFile(t, dest, want)
				}
				return copyErr
			})
			if !errors.Is(err, copyErr) {
				t.Fatalf("copy error = %v; want controlled copy failure", err)
			}
			if !replacement {
				if _, err := os.Lstat(dest); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("failed copy left its reservation: %v", err)
				}
				// A failure must leave the same name available for a real retry.
				if err := Copy(source, dest); err != nil {
					t.Fatalf("retry after failed copy: %v", err)
				}
				copied, err := Open(dest)
				if err != nil {
					t.Fatal(err)
				}
				if err := copied.Close(); err != nil {
					t.Fatal(err)
				}
			} else if got, err := os.ReadFile(dest); err != nil || !bytes.Equal(got, want) {
				t.Fatalf("failed copy changed its replacement: %q, %v", got, err)
			}
		})
	}
}

func TestCopySQLiteFailureRemovesReservation(t *testing.T) {
	dir := t.TempDir()
	source, dest := filepath.Join(dir, "invalid.db"), filepath.Join(dir, "copy.db")
	writeFile(t, source, []byte("not SQLite"))
	if err := Copy(source, dest); err == nil {
		t.Fatal("copy accepted an invalid source")
	}
	if _, err := os.Lstat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed SQLite copy left its destination: %v", err)
	}
}
