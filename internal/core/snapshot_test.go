package core

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestASnapshotTellsARefusalFromAFailure: a name in use, a folder inside a git work tree and a folder that is not there
// are refusals, a 422 with the code snapshot_refused. A database that is not there is a failure: it is not a refusal.
func TestASnapshotTellsARefusalFromAFailure(t *testing.T) {
	s := fresh(t)
	now := time.Date(2031, 5, 14, 9, 30, 0, 0, time.UTC)
	refused := func(t *testing.T, err error) {
		t.Helper()
		var e *Error
		if !errors.As(err, &e) || e.Status != 422 || e.Code != "snapshot_refused" {
			t.Errorf("err = %#v, want a 422 snapshot_refused", err)
		}
	}
	t.Run("a name in use", func(t *testing.T) {
		s.SnapshotDir = t.TempDir()
		for _, name := range []string{"life-2031-05-14.db", "life-2031-05-14T093000.db"} {
			if err := os.WriteFile(filepath.Join(s.SnapshotDir, name), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		_, _, err := s.TakeSnapshot(ctx, now)
		refused(t, err)
	})
	t.Run("a git work tree", func(t *testing.T) {
		repo := t.TempDir()
		if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		s.SnapshotDir = repo
		_, _, err := s.TakeSnapshot(ctx, now)
		refused(t, err)
	})
	t.Run("no folder", func(t *testing.T) {
		s.SnapshotDir = filepath.Join(t.TempDir(), "absent")
		_, _, err := s.TakeSnapshot(ctx, now)
		refused(t, err)
	})
	t.Run("no database", func(t *testing.T) {
		// Deliberate damage: the file of the store is closed and removed.
		gone := fresh(t)
		gone.SnapshotDir = t.TempDir()
		path := gone.DB.Path()
		if err := gone.DB.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		_, _, err := gone.TakeSnapshot(ctx, now)
		var e *Error
		if !errors.Is(err, os.ErrNotExist) || errors.As(err, &e) {
			t.Errorf("err = %#v, want a failure that is not a refusal", err)
		}
	})
}

// TestARestoreCheckReportsAFailedClose: the restore check closes the copy that it checked. A Close that fails is an
// error of the check, because the copy is then not known to be left as written. The fault driver of
// integrity_test.go fails the Close of its connection.
func TestARestoreCheckReportsAFailedClose(t *testing.T) {
	for _, fault := range []string{"", "conn-close"} {
		s := faultIntegrityStore(t, &integrityConnector{fault: fault})
		res, err := restoreCheck(ctx, s.DB)
		switch {
		case fault == "" && (err != nil || res == nil || !res.OK):
			t.Errorf("a clean check = %+v, %v; want a passed check", res, err)
		case fault != "" && (res != nil || !errors.Is(err, errIntegrityFault)):
			t.Errorf("a check whose Close fails = %+v, %v; want no result and the fault", res, err)
		}
	}
}
