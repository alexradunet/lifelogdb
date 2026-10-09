package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"lifelog/internal/importer"
)

// TestExecutableImportRunWithALoopbackModel runs `lifelog import run` as a process against a model server on this
// machine (plan 088): the notes are applied or held, the progress holds no name, and a host that is not this
// machine is refused before any request. The command is no action of the catalog.
func TestExecutableImportRunWithALoopbackModel(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "lifelog.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if out, e := exec.CommandContext(ctx, "go", "build", "-mod=readonly", "-o", binary, ".").CombinedOutput(); e != nil {
		t.Fatalf("build %v %s", e, out)
	}
	source := filepath.Join(root, "Notes")
	if e := os.Mkdir(source, 0o700); e != nil {
		t.Fatal(e)
	}
	notes := map[string]string{"a.md": "A quiet day at home.\n", "b.md": "Coffee with Cara Example.\n"}
	for name, body := range notes {
		if e := os.WriteFile(filepath.Join(source, name), []byte(body), 0o600); e != nil {
			t.Fatal(e)
		}
	}
	ws, e := importer.Open(source + ".lifelog")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ws.Setup(""); e != nil {
		t.Fatal(e)
	}
	if e = ws.DraftRules("source: import:synthetic\n"); e != nil {
		t.Fatal(e)
	}
	r, e := ws.Review("rules.md")
	if e != nil {
		t.Fatal(e)
	}
	if e = ws.Approve("rules.md", time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC), r.Hash); e != nil {
		t.Fatal(e)
	}
	if _, e = ws.MakeLedger(); e != nil {
		t.Fatal(e)
	}

	answers := map[string]string{
		"a.md": `{"writes": []}`,
		"b.md": `{"writes": [{"person": {"title": "Cara Example"}, "quote": "Coffee with Cara Example"}]}`,
	}
	var asked atomic.Int64
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		asked.Add(1)
		var body struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		if json.NewDecoder(req.Body).Decode(&body) != nil || len(body.Messages) != 2 {
			http.Error(w, "bad request", 400)
			return
		}
		file, _, _ := strings.Cut(strings.TrimPrefix(body.Messages[1].Content, "Source file: "), "\n")
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": answers[file]}}}})
	}))
	defer model.Close()

	call := func(wantSuccess bool, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary, append([]string{"--workspace", ws.Dir}, args...)...)
		cmd.Dir = root
		out, e := cmd.CombinedOutput()
		if (e == nil) != wantSuccess {
			t.Fatalf("%v: %v\n%s", args, e, out)
		}
		return string(out)
	}

	if out := call(false, "import", "run", "--model-url", "http://10.0.0.5:8080/v1", "--model", "m"); !strings.Contains(out, "loopback") || asked.Load() != 0 {
		t.Fatalf("a model host that is not this machine:\n%s", out)
	}
	out := call(true, "import", "run", "--model-url", model.URL+"/v1", "--model", "tiny")
	if asked.Load() != 2 || !strings.Contains(out, "DONE a.md") || !strings.Contains(out, "HELD b.md") || !strings.Contains(out, "1 names proposed in entities.md") {
		t.Fatalf("the run (%d model calls):\n%s", asked.Load(), out)
	}
	if strings.Contains(out, "Cara") {
		t.Errorf("the progress holds a name:\n%s", out)
	}
	lines, _, e := ws.Ledger()
	if e != nil {
		t.Fatal(e)
	}
	states := map[string]string{}
	for _, l := range lines {
		states[l.File] = l.State
	}
	if states["a.md"] != "x" || states["b.md"] != " " {
		t.Errorf("ledger %v", states)
	}
	acts := call(true, "actions")
	if !strings.Contains(acts, `"import-status"`) {
		t.Fatalf("the catalog lists no import action:\n%.300s", acts)
	}
	if strings.Contains(acts, `"import-run"`) || strings.Contains(acts, `"run"`) {
		t.Error("import run is an action of the catalog")
	}
}
