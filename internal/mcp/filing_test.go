package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
)

// file-pages is an MCP tool like any other of the catalog (docs/plans/112-metrics-as-cards.md): an agent files
// metrics in a category, as it may link one.
func TestAnAgentFilesMetricsThroughMCP(t *testing.T) {
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
	for _, m := range []string{"Ferritin", "LDL"} {
		if _, err := s.RegisterMetric(context.Background(), "cli", m, "mg/dL", ""); err != nil {
			t.Fatal(err)
		}
	}
	var ids []int64
	for _, m := range []string{"Ferritin", "LDL"} {
		id, err := s.PageID(context.Background(), m)
		if err != nil || id == 0 {
			t.Fatalf("%s: %d %v", m, id, err)
		}
		ids = append(ids, id)
	}
	session, ctx := numericSession(t, client.InProcess(api.New(s), "agent:file"))
	filed := callTool(t, ctx, session, "file_pages", map[string]any{"ids": fmt.Sprintf("%d,%d", ids[0], ids[1]), "category": "Bloodwork"})
	p := filed.Properties.(map[string]any)
	if filed.Class[0] != "filed" || fmt.Sprint(p["added"]) != "2" || p["created"] != true {
		t.Fatalf("file_pages returned %v %v", filed.Class, p)
	}
	ms, err := s.Metrics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		if (m.Name == "Ferritin" || m.Name == "LDL") && (len(m.Categories) != 1 || m.Categories[0] != "Bloodwork") {
			t.Errorf("%s is filed in %v, want [Bloodwork]", m.Name, m.Categories)
		}
	}
}
