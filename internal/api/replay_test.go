package api_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/importer"
)

// The replay action: dry_run=1 rehearses and writes nothing, an agent may run neither, and the real run writes.
func TestReplayDryRunAction(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	src := filepath.Join(root, "Notes")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "a.md"), []byte("Lunch at the Pier Cafe.\n"), 0o644)
	ws, err := importer.Open(src + ".lifelog")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Setup(""); err != nil {
		t.Fatal(err)
	}
	if err := ws.DraftRules("source: import:notes\n"); err != nil {
		t.Fatal(err)
	}
	if err := ws.Approve("rules.md", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.MakeLedger(); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFacts("a.md", []byte(`{"file":"a.md","writes":[{"place":{"title":"Pier Cafe"},"quote":"at the Pier Cafe"}]}`)); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(ws.TrialDB())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s := &core.Store{DB: d}
	if _, err := ws.Apply(ctx, s, "a.md"); err != nil {
		t.Fatal(err)
	}
	h := api.New(s, ws)
	replay := func(c *client.Client, vals map[string]string) (*api.Entity, error) {
		actions, err := c.Catalog()
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range actions {
			if a.Name == "replay" {
				return c.Do(a, vals)
			}
		}
		t.Fatal("no replay action")
		return nil, nil
	}
	owner := client.InProcess(h, "cli")
	target := filepath.Join(t.TempDir(), "life.db")

	e, err := replay(owner, map[string]string{"to": target, "dry_run": "1"})
	if err != nil {
		t.Fatal(err)
	}
	p := e.Properties.(map[string]any)
	if p["dry_run"] != true || len(p["failures"].([]any)) != 0 || len(p["files"].([]any)) != 1 {
		t.Errorf("a dry run: %v", p)
	}
	if _, err := os.Stat(target); err == nil {
		t.Error("a dry run created the target")
	}
	var ce *client.Error
	if _, err := replay(owner, map[string]string{"to": target, "dry_run": "yes"}); !errors.As(err, &ce) || ce.Status != 422 {
		t.Errorf("dry_run=yes: %v", err)
	}
	if _, err := replay(client.InProcess(h, "agent:test"), map[string]string{"to": target, "dry_run": "1"}); !errors.As(err, &ce) || ce.Status != 403 {
		t.Errorf("an agent's dry run: %v", err)
	}
	if e, err = replay(owner, map[string]string{"to": target}); err != nil {
		t.Fatal(err)
	}
	if p := e.Properties.(map[string]any); p["dry_run"] != false || p["initialised"] != true {
		t.Errorf("the real run: %v", p)
	}
	if _, err := os.Stat(target); err != nil {
		t.Error("the real run did not write the target")
	}
}
