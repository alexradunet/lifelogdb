package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

	"lifelog/internal/api/apitest"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/db/dbtest"
)

// TestStableNamesAndExactRevisionThroughTheCLI runs the command lines in-process on a file that it changes between
// them: the version token of a revision above 2^53 is exact, a stale token is a 409, a rename keeps the old title as
// an alias of the same page, and a save at the largest revision is refused with nothing written.
func TestStableNamesAndExactRevisionThroughTheCLI(t *testing.T) {
	live := dbtest.File(t)
	d, err := db.Open(t.Context(), live)
	if err != nil {
		t.Fatal(err)
	}
	s := &core.Store{DB: d}
	id, _, err := s.CreatePage(context.Background(), "cli", "Process old", "first", "")
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
	e, stdout, _ := testEnv(t)
	call := func(wantSuccess bool, args ...string) (string, error) {
		t.Helper()
		stdout.Reset()
		err := runArgs(t.Context(), e, append([]string{"--db", live}, args...))
		if (err == nil) != wantSuccess {
			t.Fatalf("%v: %v\n%s", args, err, stdout)
		}
		return stdout.String(), err
	}
	type response struct {
		Title      string
		Properties map[string]any
	}
	read := func(name string) *response {
		t.Helper()
		out, _ := call(true, "page", name)
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
		// main writes this error as "lifelog: 409: ..." and exits 1.
		_, err := call(false, "do", "save-body", "id="+strconv.FormatInt(id, 10), "version="+stale, "body=stale [[Forbidden]]")
		if apitest.Status(err) != 409 {
			t.Fatalf("stale refusal not semantic 409: %v", err)
		}
	}
	call(true, "rename", strconv.FormatInt(id, 10), "Process final")
	old, final := read("Process old"), read("Process final")
	if fmt.Sprint(old.Properties["id"]) != fmt.Sprint(final.Properties["id"]) || old.Title != "Process final" || old.Properties["body"] != "committed [[Kept]]" {
		t.Fatalf("process alias identity: %+v %+v", old, final)
	}
	d, err = db.Open(t.Context(), live)
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
	d, err = db.Open(t.Context(), live)
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
