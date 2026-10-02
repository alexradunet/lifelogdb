package importer

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

var cafeFacts = map[string]any{
	"file": "Journal/2031-04-12.md",
	"writes": []any{
		map[string]any{"person": map[string]any{"title": "Cara"}, "quote": "Coffee with Cara"},
		map[string]any{"page": map[string]any{"title": "2031-04-12"}, "quote": "Coffee with Cara"},
		map[string]any{"link": map[string]any{"from": "2031-04-12", "to": "Cara", "kind": "about"}, "quote": "Coffee with Cara"},
	},
}

// fileSum is a database file's bytes, to tell that nothing wrote to it.
func fileSum(t *testing.T, path string) [32]byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}

// noRehearsalLeft fails when a rehearsal copy is still in the workspace.
func noRehearsalLeft(t *testing.T, w *Workspace) {
	t.Helper()
	es, err := os.ReadDir(w.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		if strings.HasPrefix(e.Name(), ".rehearsal") || strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("left behind in the workspace: %s", e.Name())
		}
	}
}

func createPage(t *testing.T, path, title string) {
	t.Helper()
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, _, err := (&core.Store{DB: d}).CreatePage(ctx, "cli", title, ""); err != nil {
		t.Fatal(err)
	}
}

// A replay rehearses on a throwaway copy of its target before it writes — a file that fails leaves the
// target as it was, a dry run lists every failure and writes nothing, and no copy is left behind.
func TestReplayRehearsesBeforeItWrites(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	f.w.MakeLedger()
	f.metrics(t)
	for file, facts := range map[string]map[string]any{"Journal/2031-04-11.md": dayFacts, "Journal/2031-04-12.md": cafeFacts} {
		if err := f.facts(t, file, facts); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.Apply(ctx, f.s, file); err != nil {
			t.Fatal(err)
		}
	}

	// into a database that does not exist yet: the second ledger file fails (its source changed since the
	// trial), after the first would have been written; the target is not even created
	fresh := filepath.Join(t.TempDir(), "life.db")
	cafe, _ := f.w.SourcePath("Journal/2031-04-12.md")
	os.WriteFile(cafe, []byte("Tea alone.\n"), 0o644)
	if _, err := f.w.Replay(ctx, f.s, fresh); code(err) != 422 || !strings.Contains(err.Error(), "Journal/2031-04-12.md") {
		t.Fatalf("a replay whose second file fails: %v", err)
	}
	if exists(fresh) {
		t.Error("a replay that failed its rehearsal created the target")
	}
	res, err := f.w.Rehearse(ctx, f.s, fresh)
	if err != nil || !res.DryRun || !res.Initialised || len(res.Failures) != 1 || res.Failures[0].File != "Journal/2031-04-12.md" {
		t.Fatalf("a dry run into a new database: %+v, %v", res, err)
	}
	if exists(fresh) {
		t.Error("a dry run created the target")
	}
	os.WriteFile(cafe, []byte(vault["Journal/2031-04-12.md"]), 0o644)

	// into a database that holds pages the trial never had: a name the import writes now looks like one of them
	target := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(target); err != nil {
		t.Fatal(err)
	}
	createPage(t, target, "Cara Example")
	before := fileSum(t, target)
	if _, err := f.w.Replay(ctx, f.s, target); code(err) != 422 || !strings.Contains(err.Error(), "Cara Example") ||
		strings.Contains(err.Error(), "2031-04-11") {
		t.Fatalf("a replay whose second file fails on the target: %v", err)
	}
	if fileSum(t, target) != before {
		t.Error("a replay that failed its rehearsal wrote to the target")
	}
	// the dry run reports every failure, not only the first
	createPage(t, target, "Riverside")
	before = fileSum(t, target)
	res, err = f.w.Rehearse(ctx, f.s, target)
	if err != nil {
		t.Fatal(err)
	}
	var failed []string
	for _, x := range res.Failures {
		failed = append(failed, x.File)
	}
	if res.Initialised || strings.Join(failed, ",") != "Journal/2031-04-11.md,Journal/2031-04-12.md" {
		t.Errorf("a dry run's failures: %+v", res.Failures)
	}
	if fileSum(t, target) != before {
		t.Error("a dry run wrote to the target")
	}

	// the owner decides the look-alikes; the dry run is clean and the replay writes, once
	f.approveRules(t, rulesBody+"- \"Cara\" ≠ \"Cara Example\"\n- \"Riverside Pool\" ≠ \"Riverside\"\n")
	if res, err = f.w.Rehearse(ctx, f.s, target); err != nil || len(res.Failures) != 0 || len(res.Files) != 2 || !res.Integrity.OK {
		t.Fatalf("a clean dry run: %+v, %v", res, err)
	}
	if fileSum(t, target) != before {
		t.Error("a clean dry run wrote to the target")
	}
	res, err = f.w.Replay(ctx, f.s, target)
	if err != nil || res.DryRun || len(res.Failures) != 0 || len(res.Files) != 2 || !res.Integrity.OK {
		t.Fatalf("replay: %+v, %v", res, err)
	}
	if res.Counts.Pages != res.Trial.Pages+2 { // the target's own two pages
		t.Errorf("replay: trial %d pages, target %d", res.Trial.Pages, res.Counts.Pages)
	}
	res, err = f.w.Replay(ctx, f.s, target)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res.Files {
		if strings.Contains(r.Summary, "new") || strings.Contains(r.Summary, "promoted") {
			t.Errorf("a second replay wrote %s: %s", r.File, r.Summary)
		}
	}
	noRehearsalLeft(t, f.w)
}
