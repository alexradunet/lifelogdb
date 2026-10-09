package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/db"
	"lifelog/internal/photo"
	"lifelog/internal/photo/phototest"
)

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
func TestCommandInterruptPolicy(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{nil, false}, {[]string{"init"}, false},
		{[]string{"help"}, false}, {[]string{"unknown"}, false},
		{[]string{"import"}, false}, {[]string{"import", "status"}, false}, // removed with the import process (plan 089)
	} {
		if got := commandConsumesContext(opts{args: tc.args}); got != tc.want {
			t.Errorf("%v: intercept=%v want=%v", tc.args, got, tc.want)
		}
	}
	// snapshot, tasks and due run through the client like the rest since plans 081 and 082: an interrupt reaches them.
	for _, cmd := range []string{"serve", "mcp", "get", "actions", "do", "capture", "day", "page", "search", "query", "habits", "done", "skip", "rename", "file", "snapshot", "tasks", "due", "readings"} {
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
