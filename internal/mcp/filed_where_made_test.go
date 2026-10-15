package mcp

import (
	"path/filepath"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
)

// An agent files a new place where it makes it (docs/plans/117-one-browse-view.md, rule 6): the tool create_place
// takes category, as the HTTP action does, and one write makes the place and its part-of link.
func TestAnAgentFilesANewPlaceWhereItIsMade(t *testing.T) {
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
	session, sctx := numericSession(t, client.InProcess(api.New(s), "agent:places"))
	got := callTool(t, sctx, session, "create_place", map[string]any{"title": "Lakeside", "category": "Trips"})
	var parts []string
	for _, l := range got.Entities {
		if len(l.Rel) == 1 && l.Rel[0] == "part-of" {
			parts = append(parts, l.Title)
		}
	}
	if len(parts) != 1 || parts[0] != "Trips" {
		t.Errorf("Lakeside made through MCP is part-of %v, want [Trips]", parts)
	}
	rows := callTool(t, sctx, session, "browse", map[string]any{"in": "Trips", "kind": "place"}).Properties.(map[string]any)["rows"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["title"] != "Lakeside" {
		t.Errorf("browse in Trips: %v, want Lakeside", rows)
	}
}
