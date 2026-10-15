package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
)

// browse is an MCP tool like any other of the catalog (docs/plans/117-one-browse-view.md): an agent reads the things
// filed in a category, and it is a read, so the tool says so.
func TestAnAgentBrowsesThroughMCP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	s := &core.Store{DB: d}
	ctx := context.Background()
	var ids []int64
	for _, title := range []string{"Ana", "Bo", "Cy"} {
		id, err := s.CreatePerson(ctx, "cli", title, "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if _, err := s.FilePages(ctx, "cli", []int64{ids[2], ids[0]}, "Family", false); err != nil {
		t.Fatal(err)
	}
	session, sctx := numericSession(t, client.InProcess(api.New(s), "agent:browse"))
	tools, err := session.ListTools(sctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range tools.Tools {
		if tool.Name == "browse" {
			found = true
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Errorf("browse is not marked a read: %+v", tool.Annotations)
			}
		}
	}
	if !found {
		t.Fatal("an agent has no browse tool")
	}
	got := callTool(t, sctx, session, "browse", map[string]any{"in": "Family", "kind": "person"})
	var rows []string
	for _, r := range got.Properties.(map[string]any)["rows"].([]any) {
		r := r.(map[string]any)
		rows = append(rows, fmt.Sprintf("%v:%v", r["title"], r["mentions"]))
	}
	if got.Class[0] != "browse" || strings.Join(rows, ",") != "Ana:0,Cy:0" {
		t.Errorf("browse returned %v %v", got.Class, rows)
	}
	unfiled := callTool(t, sctx, session, "browse", map[string]any{"kind": "person", "unfiled": 1})
	if rows := unfiled.Properties.(map[string]any)["rows"].([]any); len(rows) != 1 || rows[0].(map[string]any)["title"] != "Bo" {
		t.Errorf("browse unfiled returned %v", rows)
	}
}
