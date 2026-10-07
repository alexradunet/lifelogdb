package mcp

import (
	"context"
	"fmt"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"path/filepath"
	"testing"
)

func TestMCPSessionScopeAndOwnerRelocationBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(path); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	s := &core.Store{DB: d}
	if _, err := s.RegisterMetric(context.Background(), "cli", "Steps", "steps", ""); err != nil {
		t.Fatal(err)
	}
	session, ctx := numericSession(t, client.InProcess(api.New(s, nil), "agent:session"))
	callTool(t, ctx, session, "create_page", map[string]any{"title": "Workout"})
	captured := callTool(t, ctx, session, "capture_session", map[string]any{"kind": "Workout", "day": "2020-01-02", "start_at": "2020-01-02T08:00:00.000Z", "start_offset": "+23:59", "start_zone_unverified": "Claimed/Zone", "import_key": "18446744073709551615"})
	props := captured.Properties.(map[string]any)
	id := props["id"]
	if props["import_key"] != "18446744073709551615" || props["start_zone_unverified"] != "Claimed/Zone" {
		t.Fatal("lossy evidence")
	}
	if _, err := d.W.Exec("UPDATE sessions SET revision=9007199254740993 WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	edit := callTool(t, ctx, session, "edit_session", map[string]any{"id": id, "version": "9007199254740993", "kind": "Workout", "day": "2020-01-02", "start_at": "2020-01-02T08:00:00.000Z", "start_offset": "+23:59", "start_zone_unverified": "Claimed/Zone"})
	if edit.Properties.(map[string]any)["version"] != "9007199254740993" {
		t.Fatal("no-op lost exact token")
	}
	reading := callTool(t, ctx, session, "record", map[string]any{"metric": "Steps", "day": "2020-01-02", "value": 4000, "session_id": id})
	if reading.Properties.(map[string]any)["scope"] != "session" {
		t.Fatal("unlabeled session fact")
	}
	corrected := callTool(t, ctx, session, "correct", map[string]any{"id": reading.Properties.(map[string]any)["id"], "value": 3500})
	if fmt.Sprint(corrected.Properties.(map[string]any)["session_id"]) != fmt.Sprint(id) {
		t.Fatal("scope moved")
	}
	tools, err := session.ListTools(ctx, &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "relocate_reading" {
			t.Fatal("owner relocation exposed as agent tool")
		}
	}
}
