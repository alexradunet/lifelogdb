package core

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"lifelog/internal/db"
)

// TakeSnapshot is the owner's snapshot action (docs/plans/082-snapshot-in-the-catalog.md): a dated copy of the
// database in SnapshotDir, or beside the database when none is set, and its restore check (docs/cookbook/take-a-
// snapshot.md, D25): the four integrity checks on a connection whose Close skips PRAGMA optimize, so the file is left
// as written. A snapshot that fails the check, or cannot be checked, is renamed to life-DATE.failed.db (D25). The path
// (the new one) is returned with the check. A refusal of the folder or the name (db.ErrRefused: no folder,
// a folder inside a git work tree, a name in use) is a 422 with the code snapshot_refused; any other error is a
// failure.
func (s *Store) TakeSnapshot(ctx context.Context, now time.Time) (string, *IntegrityResult, error) {
	dir := s.SnapshotDir
	if dir == "" {
		dir = filepath.Dir(s.DB.Path())
	}
	path, err := db.Snapshot(ctx, s.DB.Path(), dir, now)
	if errors.Is(err, db.ErrRefused) {
		return "", nil, &Error{Status: 422, Msg: err.Error(), Code: "snapshot_refused", Err: err}
	}
	if err != nil {
		return "", nil, err
	}
	d, err := db.OpenSnapshot(ctx, path)
	if err != nil {
		return failedSnapshot(path, nil, fmt.Errorf("the snapshot %s cannot be checked: %w", path, err))
	}
	res, err := restoreCheck(ctx, d)
	if err != nil {
		return failedSnapshot(path, nil, fmt.Errorf("the restore check of %s: %w", path, err))
	}
	if !res.OK {
		return failedSnapshot(path, res, nil)
	}
	return path, res, nil
}

// failedSnapshot keeps a snapshot that did not pass its restore check, for the owner to read, under the failed name
// (db.MarkFailed), and returns the new path with res and err as they are. A rename that fails is joined to err, and the
// path stays the old one.
func failedSnapshot(path string, res *IntegrityResult, err error) (string, *IntegrityResult, error) {
	failed, renameErr := db.MarkFailed(path)
	return failed, res, errors.Join(err, renameErr)
}

// restoreCheck runs the integrity checks on the open snapshot d, then closes it. A Close that fails is an error of the
// check: the copy is then not known to be left as written.
func restoreCheck(ctx context.Context, d *db.DB) (*IntegrityResult, error) {
	res, err := (&Store{DB: d}).Integrity(ctx)
	if err := errors.Join(err, d.Close()); err != nil {
		return nil, err
	}
	return res, nil
}
