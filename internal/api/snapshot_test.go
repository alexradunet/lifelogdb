package api_test

import (
	"context"
	"encoding/json"
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
	"lifelog/internal/db/dbtest"
)

// The owner's snapshot action, from the client and from a browser form; refused to an agent.
func TestSnapshotIsTheOwnersFromAnySurface(t *testing.T) {
	d := dbtest.New(t)
	dir := t.TempDir()
	clock := func() time.Time { return time.Date(2026, 10, 2, 14, 30, 5, 0, time.UTC) }
	h := api.New(&core.Store{DB: d, SnapshotDir: dir, Clock: clock, Zone: time.UTC})
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
	today := "2026-10-02" // the day of the store's clock
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
	if filepath.Base(again) != "life-"+today+"T143005.db" {
		t.Fatalf("a second snapshot the same day: %q, want life-%sT143005.db", again, today)
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
	// snapshot at the same instant of the clock would find its time-named file in use, a refusal of its own.
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
	d := dbtest.New(t)
	dir := t.TempDir()
	clock := func() time.Time { return time.Date(2026, 10, 2, 14, 30, 5, 0, time.UTC) }
	h := api.New(&core.Store{DB: d, SnapshotDir: dir, Clock: clock, Zone: time.UTC})
	owner := client.InProcess(h, "cli")
	root := must(owner.Get(t.Context(), "/"))
	must(owner.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "Walk"}))
	// Deliberate damage, past the writer and the DDL: a correction of a reading of another metric, written with its
	// trigger dropped. The semantic integrity check reports it, in the live file and in every copy of it.
	must(owner.Do(t.Context(), find(root, "record"), map[string]string{"metric": "Walk", "day": "2026-10-01", "value": "1"}))
	withoutTrigger(t, d.W, "measurements_supersede_metric", `INSERT INTO measurements(metric_id, day, value, supersedes_id, source, created_at)
		SELECT 1, '2026-10-01', 3, (SELECT id FROM measurements LIMIT 1), 'cli', strftime('%Y-%m-%dT%H:%M:%fZ','now')`)
	e, err := owner.Do(t.Context(), find(root, "snapshot"), nil)
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != 500 {
		t.Fatalf("a failed restore check answers 500, got %v", err)
	}
	props, _ := e.Properties.(map[string]any)
	if props["code"] != "restore_check_failed" {
		t.Errorf("code = %v, want restore_check_failed", props["code"])
	}
	if _, err := os.Stat(filepath.Join(dir, "life-2026-10-02.db")); err != nil {
		t.Errorf("the snapshot is kept although its check failed: %v", err)
	}
}

// TestOnlyARefusedSnapshotIsSnapshotRefused: 422 snapshot_refused is for a refusal that core reports, here a folder
// inside a git work tree. A snapshot whose time ran out is a 504 timeout, and one that fails for another cause (its
// request cancelled) is a 500 internal, as every other route answers them. A 504 keeps the text of its cause; a 500
// says only "internal error". None of them leaves a file.
func TestOnlyARefusedSnapshotIsSnapshotRefused(t *testing.T) {
	d := dbtest.New(t)
	snapshot := func(t *testing.T, ctx context.Context, dir string, status int, code string) (message string) {
		t.Helper()
		req := httptest.NewRequestWithContext(ctx, "POST", "/snapshots", nil)
		rec := httptest.NewRecorder()
		api.New(&core.Store{DB: d, SnapshotDir: dir}).ServeHTTP(rec, req)
		var e api.Entity
		if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
			t.Fatalf("the answer is not an entity: %v: %s", err, rec.Body.String())
		}
		if got := props(&e)["code"]; rec.Code != status || got != code {
			t.Errorf("answer %d %v, want %d %s: %s", rec.Code, got, status, code, rec.Body.String())
		}
		if files, err := os.ReadDir(dir); err != nil || len(files) != 0 {
			t.Errorf("the snapshot folder after the error: %d files, %v", len(files), err)
		}
		message, _ = props(&e)["message"].(string)
		return message
	}
	t.Run("a refusal", func(t *testing.T) {
		repo := t.TempDir()
		if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(repo, "backups")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		snapshot(t, t.Context(), dir, 422, "snapshot_refused")
	})
	t.Run("a timeout", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
		defer cancel()
		if msg := snapshot(t, ctx, t.TempDir(), 504, "timeout"); msg == "internal error" || !strings.Contains(msg, context.DeadlineExceeded.Error()) {
			t.Errorf("the message of a 504 is %q, want the text of its cause (%s)", msg, context.DeadlineExceeded)
		}
	})
	t.Run("a failure", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if msg := snapshot(t, ctx, t.TempDir(), 500, "internal"); msg != "internal error" {
			t.Errorf("the message of a 500 is %q, want internal error", msg)
		}
	})
}
