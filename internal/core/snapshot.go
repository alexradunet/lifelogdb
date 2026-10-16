package core

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"lifelog/internal/db"
)

// Snapshot takes a dated copy of the database at from into dir and runs its restore check (docs/cookbook/take-a-
// snapshot.md, D25): the four integrity checks on a connection whose Close skips PRAGMA optimize, so the file is left
// as written. The path is returned with the check. A refusal of the folder or the name (db.ErrRefused: no folder,
// a folder inside a git work tree, a name in use) is a 422 with the code snapshot_refused; any other error is a
// failure.
func Snapshot(ctx context.Context, from, dir string, now time.Time) (string, *IntegrityResult, error) {
	path, err := db.Snapshot(ctx, from, dir, now)
	if errors.Is(err, db.ErrRefused) {
		return "", nil, &Error{Status: 422, Msg: err.Error(), Code: "snapshot_refused", Err: err}
	}
	if err != nil {
		return "", nil, err
	}
	d, err := db.OpenSnapshot(ctx, path)
	if err != nil {
		return path, nil, fmt.Errorf("the snapshot %s cannot be checked: %w", path, err)
	}
	defer d.Close()
	res, err := (&Store{DB: d}).Integrity(ctx)
	if err != nil {
		return path, nil, fmt.Errorf("the restore check of %s: %w", path, err)
	}
	return path, res, nil
}

// TakeSnapshot is the owner's snapshot action (docs/plans/082-snapshot-in-the-catalog.md): the file lands in
// SnapshotDir, or beside the database when none is set.
func (s *Store) TakeSnapshot(ctx context.Context, now time.Time) (string, *IntegrityResult, error) {
	dir := s.SnapshotDir
	if dir == "" {
		dir = filepath.Dir(s.DB.Path())
	}
	return Snapshot(ctx, s.DB.Path(), dir, now)
}
