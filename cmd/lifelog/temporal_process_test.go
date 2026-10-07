package main

import (
	"context"
	"encoding/json"
	"fmt"
	"lifelog/internal/db"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestExecutableTemporalRecordsAndScopes(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "lifelog.exe")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "go", "build", "-mod=readonly", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build %v %s", err, out)
	}
	path := filepath.Join(root, "synthetic.db")
	if err := db.Init(path); err != nil {
		t.Fatal(err)
	}
	call := func(args ...string) map[string]any {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, append([]string{"--db", path}, args...)...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
		var e map[string]any
		if err := json.Unmarshal(out, &e); err != nil {
			t.Fatalf("JSON %v %s", err, out)
		}
		return e
	}
	call("do", "create-period", "title=Study", "start_boundary=2018-09", "end_boundary=..")
	periods := call("get", "/periods?day=2018-09-15&as_of=2018-09-15")
	if periods["properties"].(map[string]any)["periods"].([]any)[0].(map[string]any)["membership"] != "possible" {
		t.Fatal("CLI horizon pruned starts")
	}
	call("do", "create-page", "title=Workout")
	call("do", "register-metric", "name=Steps", "unit=steps")
	captured := call("do", "capture-session", "kind=Workout", "day=2020-01-02", "start_local=2020-01-01T23:00:00.000", "end_local=2020-01-02T07:00:00.000", "start_zone_unverified=Claimed/Zone", "import_key=18446744073709551615")
	props := captured["properties"].(map[string]any)
	id := fmt.Sprint(props["id"])
	if props["import_key"] != "18446744073709551615" {
		t.Fatal("lossy CLI key")
	}
	call("do", "record", "metric=Steps", "day=2020-01-02", "value=10000")
	reading := call("do", "record", "metric=Steps", "day=2020-01-02", "value=4000", "session_id="+id)
	rid := fmt.Sprint(reading["properties"].(map[string]any)["id"])
	defaultSeries := call("get", "/metrics/Steps?from=2020-01-01&to=2020-01-02")
	if len(defaultSeries["properties"].(map[string]any)["readings"].([]any)) != 1 {
		t.Fatal("scope mixing")
	}
	moved := call("do", "relocate-reading", "id="+rid, "metric=Steps", "day=2020-01-02", "value=4000")
	if moved["result"] == nil || moved["properties"].(map[string]any)["scope"] != "unassociated" {
		t.Fatal("missing atomic relocation result")
	}
}
