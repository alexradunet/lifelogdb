package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifelog/internal/db"
)

// lifelog snapshot is the catalog's action in-process, --to choosing the folder.
func TestSnapshotCommandRunsTheAction(t *testing.T) {
	live := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(live); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = write
	runErr := runContext(context.Background(), opts{args: []string{"snapshot"}, db: live, to: dir, source: "cli"})
	os.Stdout = old
	write.Close()
	out := make([]byte, 1<<16)
	n, _ := read.Read(out)
	read.Close()
	if runErr != nil {
		t.Fatalf("snapshot: %v\n%s", runErr, out[:n])
	}
	want := filepath.Join(dir, "life-"+time.Now().Format("2006-01-02")+".db")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("no snapshot at %s: %v\n%s", want, err, out[:n])
	}
	if !strings.Contains(string(out[:n]), `"ok": true`) {
		t.Fatalf("the report lacks the restore check:\n%s", out[:n])
	}
	// Over --url the server's folder is the one.
	err = runContext(context.Background(), opts{args: []string{"snapshot"}, url: "http://127.0.0.1:1", to: dir, source: "cli"})
	if err == nil || !strings.Contains(err.Error(), "drop --to") {
		t.Fatalf("--url with --to: %v", err)
	}
	// serve refuses a snapshot folder that is not one, before it opens anything.
	err = runContext(context.Background(), opts{args: []string{"serve"}, addr: "127.0.0.1:0", db: filepath.Join(t.TempDir(), "absent.db"), snapshots: filepath.Join(t.TempDir(), "missing")})
	if err == nil || !strings.Contains(err.Error(), "no folder") {
		t.Fatalf("serve --snapshots missing: %v", err)
	}
	o, err := parse([]string{"serve", "--snapshots", dir})
	if err != nil || o.snapshots != dir {
		t.Fatalf("parse: %+v %v", o, err)
	}
}
