package api_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/importer"
	lifelogmcp "lifelog/internal/mcp"
)

// Workspace registration consumes owner-approved rows; it is not direct metric creation.
func TestWorkspaceRegistrationPolicy(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "Synthetic")
	if err := os.Mkdir(src, 0700); err != nil {
		t.Fatal(err)
	}
	ws, err := importer.Open(src + ".lifelog")
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.DraftRules("source: import:synthetic-policy\n"); err != nil {
		t.Fatal(err)
	}
	approveWorkspaceFile(t, ws, "rules.md")
	p := filepath.Join(root, "synthetic.db")
	if err := db.Init(p); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	agent := client.InProcess(api.New(&core.Store{DB: d}, ws), "agent:policy-test")
	catalog := must(agent.Get("/actions"))
	plural, singular, replay := find(catalog, "register-metrics"), find(catalog, "register-metric"), find(catalog, "replay")
	if plural.Name == "" || plural.Owner || singular.Name == "" || !singular.Owner || replay.Name == "" || !replay.Owner {
		t.Fatalf("wrong catalog policy: %+v %+v %+v", plural, singular, replay)
	}
	for _, a := range catalog.Actions {
		if strings.Contains(a.Name, "approve") {
			t.Fatalf("approval exposed in API catalog: %s", a.Name)
		}
	}
	must(agent.Do(find(catalog, "propose-metric"), map[string]string{"name": "Synthetic Dose", "unit": "mg"}))
	if _, err := agent.Do(plural, nil); err != nil {
		var ce *client.Error
		if !errors.As(err, &ce) || ce.Status != 422 || !strings.Contains(err.Error(), "owner approves") {
			t.Fatalf("wrong unapproved registration refusal: %v", err)
		}
	} else {
		t.Fatal("unapproved workspace registration accepted")
	}
	var count int
	if err := d.R.QueryRow("SELECT count(*) FROM entity_names WHERE name_key = 'synthetic dose'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("unapproved registration wrote metric: %d %v", count, err)
	}
	approveWorkspaceFile(t, ws, "metrics.md")
	must(agent.Do(plural, nil))
	var source, unit string
	if err := d.R.QueryRow("SELECT e.source,m.unit FROM entities e JOIN entity_names n ON n.entity_id=e.id JOIN metrics m ON m.id=e.id WHERE n.name_key='synthetic dose'").Scan(&source, &unit); err != nil || source != "import:synthetic-policy" || unit != "mg" {
		t.Fatalf("approved registration: source=%s unit=%s error=%v", source, unit, err)
	}
	for _, op := range []struct {
		a      api.Action
		fields map[string]string
	}{{singular, map[string]string{"name": "Forbidden direct"}}, {replay, map[string]string{"to": filepath.Join(root, "must-not-exist.db"), "dry_run": "1"}}} {
		_, err := agent.Do(op.a, op.fields)
		var ce *client.Error
		if !errors.As(err, &ce) || ce.Status != 403 {
			t.Fatalf("owner-only %s not refused: %v", op.a.Name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "must-not-exist.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused replay touched target: %v", err)
	}

	// Inspect the actual SDK listing, without invoking any MCP tool.
	srv, err := lifelogmcp.New(agent, "test")
	if err != nil {
		t.Fatal(err)
	}
	st, ct := mcpsdk.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, st) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("SDK listing server did not stop")
		}
	})
	session, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "policy-test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range tools.Tools {
		found = found || tool.Name == "register_metrics"
		if tool.Name == "register_metric" || tool.Name == "replay" || strings.Contains(tool.Name, "approve") {
			t.Fatalf("owner operation exposed as tool: %s", tool.Name)
		}
	}
	if !found {
		t.Fatal("approved workspace registration missing from model tools")
	}
}
