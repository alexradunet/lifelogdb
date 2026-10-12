package api_test

import (
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
)

// The owner's snapshot action, from the client and from a browser form; refused to an agent.
func TestSnapshotIsTheOwnersFromAnySurface(t *testing.T) {
	p := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	dir := t.TempDir()
	h := api.New(&core.Store{DB: d, SnapshotDir: dir})
	owner := client.InProcess(h, "cli")
	agent := client.InProcess(h, "agent:lmstudio")
	root := must(owner.Get(t.Context(), "/"))
	if _, _, err := (&core.Store{DB: d}).Capture(t.Context(), "cli", "2031-08-01", "a row to copy", nil); err != nil {
		t.Fatal(err)
	}
	action := find(root, "snapshot")
	if action.Name == "" || !action.Owner {
		t.Fatalf("the root offers the owner the snapshot action: %+v", action)
	}

	first := must(owner.Do(t.Context(), action, nil))
	props := first.Properties.(map[string]any)
	path, _ := props["snapshot"].(string)
	today := time.Now().Format("2006-01-02")
	if filepath.Dir(path) != dir || filepath.Base(path) != "life-"+today+".db" {
		t.Fatalf("snapshot path %q: want life-%s.db in %s", path, today, dir)
	}
	if st, err := os.Stat(path); err != nil || st.Size() == 0 {
		t.Fatalf("the file: %v", err)
	}
	if check, _ := props["restore_check"].(map[string]any); check["ok"] != true {
		t.Fatalf("restore check: %v", props["restore_check"])
	}
	if first.Result == nil || hrefOf(first, "self") != "/" {
		t.Fatalf("the answer carries the result and points home: result=%v self=%q", first.Result, hrefOf(first, "self"))
	}
	second := must(owner.Do(t.Context(), action, nil))
	again, _ := second.Properties.(map[string]any)["snapshot"].(string)
	if again == path || !strings.HasPrefix(filepath.Base(again), "life-"+today+"T") {
		t.Fatalf("a second snapshot the same day: %q", again)
	}

	// An agent is refused and writes nothing.
	before, _ := os.ReadDir(dir)
	if _, err := agent.Do(t.Context(), action, nil); !clientStatus(err, 403) {
		t.Fatalf("agent: %v", err)
	}
	after, _ := os.ReadDir(dir)
	if len(after) != len(before) {
		t.Fatalf("an agent's refusal wrote a file: %d -> %d", len(before), len(after))
	}

	// A browser form is answered home with the path as feedback. The second file is removed first: a third
	// snapshot within the same second would find its time-named file in use, a refusal of its own.
	if err := os.Remove(again); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/snapshots", nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 303 || !strings.HasPrefix(rec.Header().Get("Location"), "/?feedback=") {
		t.Fatalf("browser form: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	req = httptest.NewRequest("GET", rec.Header().Get("Location"), nil)
	req.Header.Set("Accept", "text/html")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "life-"+today) || !strings.Contains(rec.Body.String(), "Took a snapshot: <code>") || !strings.Contains(rec.Body.String(), "<dt>restore check</dt>") {
		t.Fatalf("feedback lacks the snapshot: %.500s", rec.Body.String())
	}
	if body := browse(t, h, "/"); !strings.Contains(body, `action="/snapshots"`) {
		t.Fatalf("the home page lacks the snapshot form")
	}
}

// A snapshot that fails its restore check is an error on every surface, not a 200 with ok false inside: the file is
// kept (the check does not delete), and the answer names it with the code restore_check_failed
// (docs/plans/098-the-codebase-made-cohesive.md).
func TestASnapshotThatFailsItsRestoreCheckIsAnError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	dir := t.TempDir()
	h := api.New(&core.Store{DB: d, SnapshotDir: dir})
	owner := client.InProcess(h, "cli")
	root := must(owner.Get(t.Context(), "/"))
	must(owner.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "Walk"}))
	must(owner.Do(t.Context(), find(must(owner.Get(t.Context(), "/metrics/Walk")), "start-habit"), map[string]string{"start_day": "2026-01-01"}))
	// Deliberate damage, past the writer: a habit's reading that is not 0 or 1. The DDL cannot refuse it; the
	// semantic integrity check reports it, in the live file and in every copy of it.
	if _, err := d.W.ExecContext(t.Context(), `INSERT INTO measurements(metric_id, day, value, source, created_at)
		SELECT h.metric_id, '2026-10-01', 5, 'cli', strftime('%Y-%m-%dT%H:%M:%fZ','now') FROM habit_periods h LIMIT 1`); err != nil {
		t.Fatal(err)
	}
	e, err := owner.Do(t.Context(), find(root, "snapshot"), nil)
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != 500 {
		t.Fatalf("a failed restore check answers 500, got %v", err)
	}
	props, _ := e.Properties.(map[string]any)
	if props["code"] != "restore_check_failed" {
		t.Errorf("code = %v, want restore_check_failed", props["code"])
	}
	today := time.Now().Format("2006-01-02")
	if _, err := os.Stat(filepath.Join(dir, "life-"+today+".db")); err != nil {
		t.Errorf("the snapshot is kept although its check failed: %v", err)
	}
}
