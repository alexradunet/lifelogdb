package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db/dbtest"
)

// file-pages is an MCP tool like any other of the catalog (docs/plans/112-metrics-as-cards.md): an agent files
// metrics in a category, as it may link one.
func TestAnAgentFilesMetricsThroughMCP(t *testing.T) {
	d := dbtest.New(t)
	s := &core.Store{DB: d}
	for _, m := range []string{"Ferritin", "LDL"} {
		if _, err := s.CreateMetric(context.Background(), "cli", core.NewMetric{Name: m, Unit: "mg/dL"}); err != nil {
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

// A day page is never filed (D26): file_pages with a day page's id is an MCP error carrying the 422 and SQLite's
// sentence, and nothing is written, not the other page's link, not the category.
func TestAnAgentCannotFileADayPageThroughMCP(t *testing.T) {
	d := dbtest.New(t)
	s := &core.Store{DB: d}
	plain, _, err := s.CreatePage(context.Background(), "cli", "Plans", "", "")
	if err != nil {
		t.Fatal(err)
	}
	day, _, err := s.Capture(context.Background(), "cli", "2031-05-02", "a synthetic entry", nil)
	if err != nil {
		t.Fatal(err)
	}
	session, ctx := numericSession(t, client.InProcess(api.New(s), "agent:file"))
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "file_pages", Arguments: map[string]any{"ids": fmt.Sprintf("%d,%d", plain, day), "category": "Garden"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || len(res.Content) != 1 {
		t.Fatalf("file_pages with a day page: %+v, want one error content", res)
	}
	text, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("error content %T", res.Content[0])
	}
	var e api.Entity
	if err := json.Unmarshal([]byte(text.Text), &e); err != nil {
		t.Fatal(err)
	}
	p, _ := e.Properties.(map[string]any)
	if p["status"] != float64(422) || !strings.Contains(fmt.Sprint(p["message"]), "a day page is never filed in a category (D26)") {
		t.Errorf("file_pages with a day page: %v, want a 422 with the trigger's sentence", p)
	}
	if id, err := s.PageID(context.Background(), "Garden"); err != nil || id != 0 {
		t.Errorf("a refused filing made the category, page %d (%v)", id, err)
	}
	for _, id := range []int64{plain, day} {
		pg, err := s.PageByID(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range pg.Out {
			if l.Kind == "part-of" {
				t.Errorf("page %d is part-of %s after a refused filing", id, l.Title)
			}
		}
	}
}
