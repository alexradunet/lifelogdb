package core

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"lifelog/internal/db"
)

// Snapshot takes a dated copy of the database at from into dir and runs its restore check (docs/cookbook/take-a-
// snapshot.md, D25): the four integrity checks on a connection whose Close skips PRAGMA optimize, so the file is left
// as written. The path is returned with the check; an error names what refused (a folder inside a git work tree,
// a name in use) or what failed.
func Snapshot(ctx context.Context, from, dir string, now time.Time) (string, *IntegrityResult, error) {
	path, err := db.Snapshot(from, dir, now)
	if err != nil {
		return "", nil, err
	}
	d, err := db.OpenSnapshot(path)
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
