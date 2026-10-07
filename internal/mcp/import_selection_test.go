package mcp

import (
	"context"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/importer"
	"lifelog/internal/photo"
	"lifelog/internal/photo/phototest"
)

func TestMCPPreparedSourceOwnerStampAndMeaning(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "Source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "daily.csv"), []byte("Date,Distance (m)\n2020-01-02,10\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ws, err := importer.Open(source + ".lifelog")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ws.Setup(""); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(source, "one.jpg"), phototest.JPEG(3, 2, photo.Meta{Orientation: 1}, true), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.MakeLedger(); err != nil {
		t.Fatal(err)
	}
	if err = ws.DraftRules("source: import:synthetic\n"); err != nil {
		t.Fatal(err)
	}
	approve := func(name string) {
		t.Helper()
		r, e := ws.Review(name)
		if e != nil {
			t.Fatal(e)
		}
		if e = ws.Approve(name, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), r.Hash); e != nil {
			t.Fatal(e)
		}
	}
	approve("rules.md")
	d, err := db.Open(ws.TrialDB())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := &core.Store{DB: d}
	if _, err = s.RegisterMetric(context.Background(), "cli", "Distance", "m", ""); err != nil {
		t.Fatal(err)
	}
	session, ctx := numericSession(t, client.InProcess(api.New(s, ws), "agent:synthetic"))
	callTool(t, ctx, session, "draft_prepared", map[string]any{"file": "daily.csv", "profile": "fit-date-csv-v1", "metrics": `{"distance":"Distance"}`})
	result, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "apply_prepared"})
	if err == nil && (result == nil || !result.IsError) {
		t.Fatal("MCP draft bypassed owner gate")
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "approve_prepared" || tool.Name == "approve" {
			t.Fatal("MCP exposed stamp")
		}
	}
	approve("prepared.md")
	callTool(t, ctx, session, "check_prepared", nil)
	callTool(t, ctx, session, "apply_prepared", nil)
	var v float64
	var day, src string
	if err = d.R.QueryRow(`SELECT value,day,source FROM measurements`).Scan(&v, &day, &src); err != nil || v != 10 || day != "2020-01-02" || src != "import:synthetic" {
		t.Fatalf("MCP meaning %g %s %s %v", v, day, src, err)
	}
	callTool(t, ctx, session, "draft_selected_photo", map[string]any{"file": "one.jpg", "title": "One.jpg", "choices": `{"capture":"none","gps":"none"}`})
	unapproved, e := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "apply_selected_photo"})
	if e == nil && !unapproved.IsError {
		t.Fatal("MCP selected draft bypassed stamp")
	}
	approve("selected-photo.md")
	callTool(t, ctx, session, "check_selected_photo", nil)
	callTool(t, ctx, session, "apply_selected_photo", nil)
	var unknown bool
	if e = d.R.QueryRow(`SELECT e.day IS NULL FROM files f JOIN entities e ON e.id=f.id`).Scan(&unknown); e != nil || !unknown {
		t.Fatalf("MCP unknown photo %v %v", unknown, e)
	}

}
