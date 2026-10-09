package api_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
	lifelogmcp "lifelog/internal/mcp"
)

// The owner-only actions are refused to an agent and are never MCP tools; readings-from-table, which an agent uses
// during an import (docs/plans/089-no-import-process.md), is a tool like any other action.
func TestOwnerOnlyActionsPolicy(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "synthetic.db")
	if err := db.Init(p); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	agent := client.InProcess(api.New(&core.Store{DB: d, SnapshotDir: root}), "agent:policy-test")
	catalog := must(agent.Get("/actions"))
	register, snapshot, table := find(catalog, "register-metric"), find(catalog, "snapshot"), find(catalog, "readings-from-table")
	if !register.Owner || !snapshot.Owner || table.Name == "" || table.Owner {
		t.Fatalf("wrong catalog policy: %+v %+v %+v", register, snapshot, table)
	}
	for _, a := range catalog.Actions {
		if strings.Contains(a.Name, "approve") || strings.HasPrefix(a.Href, "/import") {
			t.Fatalf("an import or approval action in the catalog: %s", a.Name)
		}
	}
	for _, op := range []struct {
		a      api.Action
		fields map[string]string
	}{{register, map[string]string{"name": "Forbidden direct"}}, {snapshot, nil}} {
		_, err := agent.Do(op.a, op.fields)
		var ce *client.Error
		if !errors.As(err, &ce) || ce.Status != 403 {
			t.Fatalf("owner-only %s not refused: %v", op.a.Name, err)
		}
	}
	var count int
	if err := d.R.QueryRow("SELECT count(*) FROM entity_names WHERE name_key = 'forbidden direct'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("a refused registration wrote a metric: %d %v", count, err)
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
		found = found || tool.Name == "readings_from_table"
		if tool.Name == "register_metric" || tool.Name == "snapshot" || strings.Contains(tool.Name, "approve") {
			t.Fatalf("owner operation exposed as tool: %s", tool.Name)
		}
	}
	if !found {
		t.Fatal("readings_from_table missing from the MCP tools")
	}
}
