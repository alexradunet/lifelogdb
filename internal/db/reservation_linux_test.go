package db

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A file system can give the inode number of a removed file to the next new file: ext4 does, tmpfs does not. The
// cleanup of a failed Init or Copy finds its own file by the inode, so the reservation must hold its file open
// until the cleanup: the number of an inode in use cannot go to another file. This test finds the held file through
// /proc, so its answer does not depend on the file system of the temporary folder.
func TestAReservationHoldsItsFileUntilTheCleanup(t *testing.T) {
	failure := errors.New("controlled failure")
	for _, operation := range []string{"init", "copy"} {
		t.Run(operation, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "life.db")
			removeReservation := func(*sql.DB) error {
				reserved, err := os.Lstat(p)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				if !openInThisProcess(t, reserved) {
					t.Error("the reservation does not hold its file: the file system can give its inode to the next new file")
				}
				return failure
			}
			var err error
			if operation == "init" {
				err = initWith(p, removeReservation)
			} else {
				source := filepath.Join(t.TempDir(), "source.db")
				if err := Init(t.Context(), source); err != nil {
					t.Fatal(err)
				}
				err = copyWith(source, p, removeReservation)
			}
			if !errors.Is(err, failure) {
				t.Fatalf("%s error = %v; want the controlled failure", operation, err)
			}
			if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("the cleanup made a file at the removed path: %v", err)
			}
		})
	}
}

// openInThisProcess reports whether this process has a file open with the identity of info.
func openInThisProcess(t *testing.T, info os.FileInfo) bool {
	t.Helper()
	fds, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range fds {
		// Stat follows the link of /proc to the open file, also when the file has no name.
		open, err := os.Stat(filepath.Join("/proc/self/fd", fd.Name()))
		if err == nil && os.SameFile(open, info) {
			return true
		}
	}
	return false
}

// TestASuccessfulInitOrCopyClosesItsReservation: after an Init and a Copy that succeed, this process has no file open
// with the identity of the new file. A success path that did not close its reservation would hold the file open until
// the process ends.
func TestASuccessfulInitOrCopyClosesItsReservation(t *testing.T) {
	dir := t.TempDir()
	source, copied := filepath.Join(dir, "life.db"), filepath.Join(dir, "copy.db")
	if err := Init(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	if err := Copy(t.Context(), source, copied); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{source, copied} {
		info, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		if openInThisProcess(t, info) {
			t.Errorf("%s is open in this process after its success: the reservation was not closed", p)
		}
	}
}
