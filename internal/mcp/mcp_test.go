package mcp

import (
	"context"
	"encoding/json"
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
)

func TestValidateToolNamesRejectsNormalizedDuplicates(t *testing.T) {
	for _, tc := range []struct {
		name    string
		actions []api.Action
		want    string
	}{
		{"catalog collision", []api.Action{{Name: "same-name"}, {Name: "same_name"}}, `"same_name"`},
		{"built-in get", []api.Action{{Name: "get"}}, `"get"`},
		{"built-in get day", []api.Action{{Name: "get-day"}}, `"get_day"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateToolNames(tc.actions)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateToolNames() = %v, want a collision containing %s", err, tc.want)
			}
		})
	}
	if err := validateToolNames([]api.Action{{Name: "get", Owner: true}, {Name: "find"}, {Name: "import-find"}}); err != nil {
		t.Fatalf("owner-only duplicate and distinct public tools were rejected: %v", err)
	}
}

func TestFindToolsWithWorkspace(t *testing.T) {
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
	seen := map[string]bool{}
	for _, tool := range tools.Tools {
		if seen[tool.Name] {
			t.Fatalf("duplicate MCP tool %q", tool.Name)
		}
		seen[tool.Name] = true
	}
	for _, name := range []string{"find", "import_find", "get", "get_day"} {
		if !seen[name] {
			t.Fatalf("MCP tools lack %q; have %v", name, seen)
		}
	}
	for _, ownerOnly := range []string{"register_metric", "replay"} {
		if seen[ownerOnly] {
			t.Fatalf("owner-only action %q was exposed as an MCP tool", ownerOnly)
		}
	}

	page := callTool(t, ctx, session, "find", map[string]any{"title": "Ana"})
	if page.Title != "Ana" || href(page, "self") == "" {
		t.Fatalf("find returned %+v", page)
	}
	matches := callTool(t, ctx, session, "import_find", map[string]any{"text": "Ana"})
	if matches.Title != "Find Ana" || href(matches, "self") != "/import/find?text=Ana" {
		t.Fatalf("import_find returned %+v", matches)
	}
}

func mcpTestClient(t *testing.T) *client.Client {
	t.Helper()
	p := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(p); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Notebook"), 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := importer.Open(filepath.Join(root, "Notebook.lifelog"))
	if err != nil {
		t.Fatal(err)
	}
	c := client.InProcess(api.New(&core.Store{DB: d}, ws), "agent:test")
	if _, err := c.Do(actionByName(t, must(c.Get("/")), "create-page"), map[string]string{"title": "Ana", "body": "Synthetic page"}); err != nil {
		t.Fatal(err)
	}
	return c
}

func actionByName(t *testing.T, e *api.Entity, name string) api.Action {
	t.Helper()
	for _, a := range e.Actions {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("%s not found in %v", name, e.Actions)
	return api.Action{}
}

func callTool(t *testing.T, ctx context.Context, session *mcpsdk.ClientSession, name string, args map[string]any) *api.Entity {
	t.Helper()
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("%s returned MCP error content: %v", name, res.Content)
	}
	if len(res.Content) != 1 {
		t.Fatalf("%s returned %d content items", name, len(res.Content))
	}
	text, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("%s returned %T content", name, res.Content[0])
	}
	var e api.Entity
	if err := json.Unmarshal([]byte(text.Text), &e); err != nil {
		t.Fatalf("%s returned non-entity JSON %q: %v", name, text.Text, err)
	}
	return &e
}

func must(e *api.Entity, err error) *api.Entity {
	if err != nil {
		panic(err)
	}
	return e
}

func href(e *api.Entity, rel string) string {
	for _, l := range e.Links {
		if len(l.Rel) > 0 && l.Rel[0] == rel {
			return l.Href
		}
	}
	return ""
}
