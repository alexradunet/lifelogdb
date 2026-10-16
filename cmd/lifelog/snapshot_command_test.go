package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifelog/internal/db"
)

// lifelog snapshot is the catalog's action in-process, --to choosing the folder. The store's clock is fixed, so the
// name of the snapshot does not depend on the day the test runs.
func TestSnapshotCommandRunsTheAction(t *testing.T) {
	live := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(t.Context(), live); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	e, stdout, _ := testEnv(t)
	e.clock = func() time.Time { return time.Date(2031, 5, 14, 12, 0, 0, 0, time.Local) }
	if err := runArgs(t.Context(), e, []string{"snapshot", "--db", live, "--to", dir}); err != nil {
		t.Fatalf("snapshot: %v\n%s", err, stdout)
	}
	want := filepath.Join(dir, "life-2031-05-14.db")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("no snapshot at %s: %v\n%s", want, err, stdout)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) {
		t.Fatalf("the report lacks the restore check:\n%s", stdout)
	}
	// Over --url the server's folder is the one.
	err := runArgs(t.Context(), e, []string{"snapshot", "--url", "http://127.0.0.1:1", "--to", dir})
	if err == nil || !strings.Contains(err.Error(), "drop --to") {
		t.Fatalf("--url with --to: %v", err)
	}
	// serve refuses a snapshot folder that is not one, before it opens anything.
	err = runArgs(t.Context(), e, []string{"serve", "--addr", "127.0.0.1:0", "--db", filepath.Join(t.TempDir(), "absent.db"), "--snapshots", filepath.Join(t.TempDir(), "missing")})
	if err == nil || !strings.Contains(err.Error(), "no folder") {
		t.Fatalf("serve --snapshots missing: %v", err)
	}
	o, err := parse([]string{"serve", "--snapshots", dir}, noVars)
	if err != nil || o.snapshots != dir {
		t.Fatalf("parse: %+v %v", o, err)
	}
}
