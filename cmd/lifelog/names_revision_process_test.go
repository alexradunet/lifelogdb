package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

func TestExecutableStableNamesAndExactRevision(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "lifelog.exe")
	buildCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if out, err := exec.CommandContext(buildCtx, "go", "build", "-mod=readonly", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	live := filepath.Join(root, "synthetic.db")
	if err := db.Init(live); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(live)
	if err != nil {
		t.Fatal(err)
	}
	s := &core.Store{DB: d}
	id, _, err := s.CreatePage(context.Background(), "cli", "Process old", "first")
	if err != nil {
		d.Close()
		t.Fatal(err)
	}
	if _, err := d.W.Exec(`UPDATE entities SET revision=9007199254740993 WHERE id=?`, id); err != nil {
		d.Close()
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	call := func(wantSuccess bool, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, append([]string{"--db", live}, args...)...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if (err == nil) != wantSuccess {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	type response struct {
		Title      string
		Properties map[string]any
	}
	read := func(name string) *response {
		t.Helper()
		out := call(true, "page", name)
		var e response
		if err := json.Unmarshal([]byte(out), &e); err != nil {
			t.Fatalf("read JSON: %v %s", err, out)
		}
		return &e
	}
	p := read("Process old")
	if p.Properties["version"] != "9007199254740993" {
		t.Fatalf("lossy token: %#v", p.Properties["version"])
	}
	token := p.Properties["version"].(string)
	call(true, "do", "save-body", "id="+strconv.FormatInt(id, 10), "version="+token, "body=committed [[Kept]]")
	for _, stale := range []string{token, "2026-01-01T00:00:00.000Z"} {
		if out := call(false, "do", "save-body", "id="+strconv.FormatInt(id, 10), "version="+stale, "body=stale [[Forbidden]]"); !strings.Contains(out, "409") {
			t.Fatalf("stale refusal not semantic 409: %s", out)
		}
	}
	call(true, "rename", strconv.FormatInt(id, 10), "Process final")
	old, final := read("Process old"), read("Process final")
	if fmt.Sprint(old.Properties["id"]) != fmt.Sprint(final.Properties["id"]) || old.Title != "Process final" || old.Properties["body"] != "committed [[Kept]]" {
		t.Fatalf("process alias identity: %+v %+v", old, final)
	}
	d, err = db.Open(live)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.Exec(`UPDATE entities SET revision=9223372036854775807 WHERE id=?`, id); err != nil {
		d.Close()
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	max := read("Process old")
	if max.Properties["version"] != "9223372036854775807" {
		t.Fatalf("lossy max token: %#v", max.Properties["version"])
	}
	call(true, "do", "save-body", "id="+strconv.FormatInt(id, 10), "version=9223372036854775807", "body=committed [[Kept]]")
	call(false, "do", "save-body", "id="+strconv.FormatInt(id, 10), "version=9223372036854775807", "body=exhausted [[Forbidden]]")
	d, err = db.Open(live)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	after, err := (&core.Store{DB: d}).PageByID(context.Background(), id)
	if err != nil || after.Body != "committed [[Kept]]" || after.Version != "9223372036854775807" {
		t.Fatalf("refusal state: %+v %v", after, err)
	}
	if bad, err := (&core.Store{DB: d}).PageID(context.Background(), "Forbidden"); err != nil || bad != 0 {
		t.Fatalf("forbidden target: %d %v", bad, err)
	}
}
