package main

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"lifelog/internal/db"
)

func TestExecutablePeriodRecords(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "lifelog.exe")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "go", "build", "-mod=readonly", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build %v %s", err, out)
	}
	path := filepath.Join(root, "synthetic.db")
	if err := db.Init(t.Context(), path); err != nil {
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
}
