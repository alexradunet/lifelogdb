package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The planning actions are MCP tools like any other of the catalog, and get follows a task link.
func TestPlanningToolsFromTheCatalog(t *testing.T) {
	c := mcpTestClient(t)
	srv, err := New(c, "test")
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	errc := make(chan error, 1)
	go func() { errc <- srv.Run(ctx, serverTransport) }()
	var session *mcpsdk.ClientSession
	defer func() {
		if session != nil {
			if err := session.Close(); err != nil {
				t.Errorf("close MCP session: %v", err)
			}
		}
		cancel()
		if err := <-errc; err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("MCP server stopped with %v", err)
		}
	}()
	session, err = mcpsdk.NewClient(&mcpsdk.Implementation{Name: "lifelog-test", Version: "0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := session.ListTools(ctx, &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]*mcpsdk.Tool{}
	for _, tool := range tools.Tools {
		seen[tool.Name] = tool
	}
	for _, name := range []string{"create_task", "edit_task", "stop_task", "tombstone_task", "revive_task", "capture_occurrence", "edit_occurrence", "tombstone_occurrence", "revive_occurrence", "deadlines"} {
		if seen[name] == nil {
			t.Errorf("MCP tools lack %q", name)
		}
	}
	if seen["deadlines"] != nil && (seen["deadlines"].Annotations == nil || !seen["deadlines"].Annotations.ReadOnlyHint) {
		t.Errorf("deadlines is a read: %+v", seen["deadlines"].Annotations)
	}
	task := callTool(t, ctx, session, "create_task", map[string]any{"label": "Call Ana", "project": "Ana", "due_day": "2031-07-01"})
	if task.Title != "Call Ana" || href(task, "project") == "" {
		t.Fatalf("create_task returned %+v", task)
	}
	self := strings.SplitN(href(task, "self"), "?", 2)[0]
	followed := callTool(t, ctx, session, "get", map[string]any{"href": self})
	if followed.Title != "Call Ana" {
		t.Fatalf("get %s returned %+v", self, followed)
	}
	due := callTool(t, ctx, session, "deadlines", map[string]any{"from": "2031-07-01", "through": "2031-07-31"})
	if len(due.Entities) != 1 || !strings.HasPrefix(due.Entities[0].Title, "Call Ana") {
		t.Fatalf("deadlines returned %+v", due.Entities)
	}
}
