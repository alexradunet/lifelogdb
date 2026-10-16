package mcp

import (
	"testing"

	"lifelog/internal/api/apitest"
)

// An agent files a new place where it makes it (docs/plans/117-one-browse-view.md, rule 6): the tool create_place
// takes category, as the HTTP action does, and one write makes the place and its part-of link.
func TestAnAgentFilesANewPlaceWhereItIsMade(t *testing.T) {
	agent, _, _ := apitest.Fresh(t, "agent:places")
	session, sctx := numericSession(t, agent)
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
