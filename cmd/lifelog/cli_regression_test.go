package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/photo"
	"lifelog/internal/photo/phototest"
)

func TestWorkspaceDatabasePrecedence(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "Synthetic")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	workspace := source + ".lifelog"
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	env := filepath.Join(root, "environment.db")
	explicit := filepath.Join(root, "explicit.db")
	trial := filepath.Join(workspace, "trial.db")
	for p, title := range map[string]string{env: "Environment", explicit: "Explicit", trial: "Trial"} {
		if err := db.Init(p); err != nil {
			t.Fatal(err)
		}
		d, err := db.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = (&core.Store{DB: d}).CreatePage(context.Background(), "cli", title, "Synthetic")
		d.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("LIFELOG_DB", env)
	for _, tc := range []struct {
		name  string
		flags []string
		want  string
	}{
		{"default-workspace", nil, "Trial"},
		{"explicit-equals-environment", []string{"--db", env}, "Environment"},
		{"explicit-different", []string{"--db=" + explicit}, "Explicit"},
	} {
		for _, command := range []string{"page", "snapshot"} {
			t.Run(tc.name+"/"+command, func(t *testing.T) {
				argv := []string{command, "--workspace", workspace}
				var dest string
				if command == "page" {
					argv = append(argv, tc.want)
				} else {
					dest = t.TempDir()
					argv = append(argv, "--to", dest)
				}
				argv = append(argv, tc.flags...)
				o, err := parse(argv)
				if err != nil {
					t.Fatal(err)
				}
				out := captureStdout(t, func() {
					if err := runContext(context.Background(), o); err != nil {
						t.Fatal(err)
					}
				})
				if command == "page" {
					if !strings.Contains(out, tc.want) {
						t.Fatalf("selected wrong database: %s", out)
					}
					return
				}
				entries, err := os.ReadDir(dest)
				if err != nil || len(entries) != 1 {
					t.Fatalf("snapshots: %v %v", entries, err)
				}
				d, err := db.OpenSnapshot(filepath.Join(dest, entries[0].Name()))
				if err != nil {
					t.Fatal(err)
				}
				defer d.Close()
				var title string
				if err := d.R.QueryRow("SELECT title FROM pages WHERE title IN ('Trial','Explicit','Environment')").Scan(&title); err != nil || title != tc.want {
					t.Fatalf("snapshot title=%q err=%v want=%q", title, err, tc.want)
				}
			})
		}
	}
}

func TestFileCommandNumberCoordinates(t *testing.T) {
	root := t.TempDir()
	live := filepath.Join(root, "synthetic.db")
	if err := db.Init(live); err != nil {
		t.Fatal(err)
	}
	picks := filepath.Join(root, "picks")
	if err := os.Mkdir(picks, 0700); err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		name     string
		lat, lon float64
	}{{"a.jpg", 41.1496, -8.611}, {"b.jpg", 41.151, -8.610}, {"c.jpg", 38.7, -9.1}} {
		data := phototest.JPEG(40, 30, photo.Meta{Taken: "2026-10-05 09:00:00", HasGPS: true, Lat: p.lat, Lon: p.lon}, true)
		if err := os.WriteFile(filepath.Join(picks, p.name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	o, err := parse([]string{"file", picks, "--db", live, "--dry-run"})
	if err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := runContext(context.Background(), o); err != nil {
			t.Fatal(err)
		}
	})
	var report batch
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Unmatched) != 2 || report.Unmatched[0].Count != 2 || report.Unmatched[1].Count != 1 {
		t.Fatalf("incorrect coordinate groups: %s", out)
	}
	if !strings.Contains(report.Unmatched[0].Map, "41.1496") || !strings.Contains(report.Unmatched[1].Map, "38.7") {
		t.Fatalf("coordinate maps: %s", out)
	}
}

func TestInventoryCommandCancellation(t *testing.T) {
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		ctx := inventoryCanceledContext{Context: context.Background(), err: sentinel}
		err := runContext(ctx, opts{args: []string{"import", "takeout", "inventory", filepath.Join(t.TempDir(), "absent")}})
		if !errors.Is(err, sentinel) {
			t.Fatalf("context did not reach inventory: got %v want %v", err, sentinel)
		}
	}
}

type inventoryCanceledContext struct {
	context.Context
	err error
}

func (c inventoryCanceledContext) Err() error { return c.err }

func TestCommandInterruptPolicy(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{nil, false}, {[]string{"init"}, false}, {[]string{"snapshot"}, false},
		{[]string{"import", "setup"}, false}, {[]string{"import", "approve", "rules"}, false},
		{[]string{"help"}, false}, {[]string{"unknown"}, false}, {[]string{"import", "unknown"}, false},
		{[]string{"import", "takeout"}, false}, {[]string{"import", "takeout", "inventory", "folder"}, true},
		{[]string{"import"}, true}, {[]string{"import", "status"}, true}, {[]string{"import", "check", "file"}, true},
		{[]string{"import", "apply", "file"}, true}, {[]string{"import", "replay"}, true},
	} {
		if got := commandConsumesContext(opts{args: tc.args}); got != tc.want {
			t.Errorf("%v: intercept=%v want=%v", tc.args, got, tc.want)
		}
	}
	for _, cmd := range []string{"serve", "mcp", "get", "actions", "do", "capture", "day", "page", "search", "query", "habits", "done", "skip", "rename", "file"} {
		if !commandConsumesContext(opts{args: []string{cmd}}) {
			t.Errorf("%s must consume signal context", cmd)
		}
	}
}

func TestCoordinateNumbersAndErrors(t *testing.T) {
	for _, v := range []any{json.Number("-8.611"), float64(-8.611)} {
		n, err := coordinate(v)
		if err != nil || n != -8.611 {
			t.Fatalf("coordinate(%v)=%v,%v", v, n, err)
		}
	}
	for _, v := range []any{json.Number("1e999"), json.Number("bad"), nil, "41.2"} {
		if _, err := coordinate(v); err == nil {
			t.Errorf("invalid coordinate %v accepted", v)
		}
	}
	id := json.Number("9007199254740993")
	b, err := summarise([]keptFile{{Path: "a.jpg", Result: map[string]any{"id": id, "unmatched": map[string]any{"lat": json.Number("41.1496"), "lon": json.Number("-8.611")}}}}, false)
	if err != nil || b.Files[0].Result["id"] != id {
		t.Fatalf("summary changed exact ID: %v %v", b, err)
	}
	for _, field := range []string{"lat", "lon"} {
		u := map[string]any{"lat": json.Number("41"), "lon": json.Number("-8")}
		u[field] = json.Number("1e999")
		if _, err := summarise([]keptFile{{Result: map[string]any{"unmatched": u}}}, false); err == nil {
			t.Errorf("summary swallowed %s conversion error", field)
		}
	}
}

func TestInventoryCommandCancellationDuringTraversal(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	counted := &inventoryTraversalContext{Context: ctx, cancel: cancel}
	if err := runContext(counted, opts{args: []string{"import", "takeout", "inventory", root}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("traversal cancellation: %v", err)
	}
	if counted.calls < 4 {
		t.Fatal("inventory did not reach traversal checkpoint")
	}
}

type inventoryTraversalContext struct {
	context.Context
	cancel context.CancelFunc
	calls  int
}

func (c *inventoryTraversalContext) Err() error {
	c.calls++
	if c.calls == 4 {
		c.cancel()
	}
	return c.Context.Err()
}
