package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
)

func TestMCPRevisionTokenRoundTripAndStaleRefusal(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprintf("remote=%v", remote), func(t *testing.T) {
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
			store := &core.Store{DB: d}
			h := api.New(store)
			c := client.InProcess(h, "agent:test")
			if remote {
				server := httptest.NewServer(h)
				t.Cleanup(server.Close)
				c = client.Remote(server.URL, "agent:test")
			}
			id, _, err := store.CreatePage(context.Background(), "cli", "MCP revision", "first")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.W.Exec(`UPDATE entities SET revision=9007199254740993 WHERE id=?`, id); err != nil {
				t.Fatal(err)
			}
			var clock string
			if err := d.R.QueryRow(`SELECT updated_at FROM entities WHERE id=?`, id).Scan(&clock); err != nil {
				t.Fatal(err)
			}
			session, ctx := numericSession(t, c)
			before := callTool(t, ctx, session, "get", map[string]any{"href": fmt.Sprintf("/pages/%d", id)})
			if got := before.Properties.(map[string]any)["version"]; got != "9007199254740993" {
				t.Fatalf("read token %#v", got)
			}
			saved := callTool(t, ctx, session, "save_body", map[string]any{"id": id, "version": "9007199254740993", "body": "committed"})
			if got := saved.Properties.(map[string]any)["version"]; got != "9007199254740994" {
				t.Fatalf("saved token %#v", got)
			}
			if _, err := d.W.Exec(`UPDATE entities SET updated_at=? WHERE id=?`, clock, id); err != nil {
				t.Fatal(err)
			}
			for _, token := range []string{"9007199254740993", clock} {
				result, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "save_body", Arguments: map[string]any{"id": id, "version": token, "body": "stale [[Forbidden]]"}})
				if err != nil {
					t.Fatal(err)
				}
				if !result.IsError || len(result.Content) != 1 {
					t.Fatalf("stale result %+v", result)
				}
				text, ok := result.Content[0].(*mcpsdk.TextContent)
				if !ok {
					t.Fatalf("error content %T", result.Content[0])
				}
				var entity api.Entity
				if err := json.Unmarshal([]byte(text.Text), &entity); err != nil {
					t.Fatal(err)
				}
				if got := entity.Properties.(map[string]any)["status"]; got != float64(409) {
					t.Fatalf("stale status %#v", got)
				}
			}
			state, err := store.PageByID(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			if state.Body != "committed" || state.Version != "9007199254740994" {
				t.Fatalf("stale tool changed %+v", state)
			}
			if target, err := store.PageID(context.Background(), "Forbidden"); err != nil || target != 0 {
				t.Fatalf("stale tool target %d %v", target, err)
			}
		})
	}
}
