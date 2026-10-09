package mcp

import (
	"context"
	"fmt"
	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"path/filepath"
	"testing"
)

func TestMCPRecordedPeriodActions(t *testing.T) {
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
	session, ctx := numericSession(t, client.InProcess(api.New(s), "agent:period"))
	created := callTool(t, ctx, session, "create_period", map[string]any{"title": "Synthetic study", "start_boundary": "2018-09", "end_boundary": ".."})
	properties := created.Properties.(map[string]any)
	id := properties["id"]
	version := properties["version"]
	listed := callTool(t, ctx, session, "get", map[string]any{"href": "/periods?day=2018-09-15&as_of=2018-09-15"})
	ps := listed.Properties.(map[string]any)["periods"].([]any)
	if len(ps) != 1 || ps[0].(map[string]any)["membership"] != "possible" {
		t.Fatal("ongoing horizon invented certainty")
	}
	edited := callTool(t, ctx, session, "edit_period", map[string]any{"id": id, "version": version, "start_boundary": "2018-09", "end_boundary": "2018-09-15"})
	if fmt.Sprint(edited.Properties.(map[string]any)["version"]) == fmt.Sprint(version) {
		t.Fatal("unchanged token")
	}
	persisted, err := s.LifePeriods(context.Background(), "2018-09-15", "", false)
	if err != nil || len(persisted) != 1 || persisted[0].Membership != "definite" {
		t.Fatalf("%+v %v", persisted, err)
	}
}
