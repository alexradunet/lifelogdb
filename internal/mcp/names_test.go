package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
)

func TestMCPStableNames(t *testing.T) {
	for _, remote := range []bool{false, true} {
		for _, kind := range []string{"page", "person", "place", "metric", "file"} {
			t.Run(fmt.Sprintf("remote=%v/type=%s", remote, kind), func(t *testing.T) {
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
				h := api.New(s)
				c := client.InProcess(h, "agent:names")
				if remote {
					server := httptest.NewServer(h)
					t.Cleanup(server.Close)
					c = client.Remote(server.URL, "agent:names")
				}
				old := "Old " + kind
				var id int64
				switch kind {
				case "page":
					id, _, err = s.CreatePage(context.Background(), "cli", old, "original prose")
				case "person":
					id, err = s.CreatePerson(context.Background(), "cli", old, "Synthetic Person", "", "")
				case "place":
					id, err = s.CreatePlace(context.Background(), "cli", old)
				case "metric":
					_, err = s.RegisterMetric(context.Background(), "cli", old, "kg", "original metric")
					if err == nil {
						id, err = s.PageID(context.Background(), old)
					}
				case "file":
					var kept core.Kept
					kept, err = s.AddFile(context.Background(), "cli", core.FileIn{Title: old, SHA256: strings.Repeat("a", 64), MIME: "text/plain", Body: "original file", Day: "2031-01-01"})
					id = kept.ID
				}
				if err != nil {
					t.Fatal(err)
				}
				before, err := s.PageByID(context.Background(), id)
				if err != nil {
					t.Fatal(err)
				}
				session, ctx := numericSession(t, c)
				for _, title := range []string{"New " + kind, "NEW " + strings.ToUpper(kind), old, "Final " + kind} {
					result := callTool(t, ctx, session, "rename", map[string]any{"id": id, "title": title})
					if result.Title != title {
						t.Fatalf("rename title=%q", result.Title)
					}
					resolved := callTool(t, ctx, session, "get", map[string]any{"href": "/pages?title=" + url.QueryEscape(old)})
					if resolved.Title != title || resolved.Properties.(map[string]any)["id"] != float64(id) {
						t.Fatalf("alias resolution: %+v", resolved)
					}
				}
				after, err := s.PageByID(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				if after.ID != id || after.Type != before.Type || after.Body != before.Body || after.Day != before.Day || !reflect.DeepEqual(after.Person, before.Person) || !reflect.DeepEqual(after.File, before.File) {
					t.Fatalf("typed rename changed data: %+v", after)
				}
				for _, bad := range []struct {
					title  string
					status int
				}{{"2031-02-01", 409}, {"Lab [unsafe]", 422}} {
					result, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "rename", Arguments: map[string]any{"id": id, "title": bad.title}})
					if err != nil {
						t.Fatal(err)
					}
					if !result.IsError || len(result.Content) != 1 {
						t.Fatalf("refusal: %+v", result)
					}
					text, ok := result.Content[0].(*mcpsdk.TextContent)
					if !ok {
						t.Fatalf("error content %T", result.Content[0])
					}
					var entity api.Entity
					if err := json.Unmarshal([]byte(text.Text), &entity); err != nil {
						t.Fatal(err)
					}
					if entity.Properties.(map[string]any)["status"] != float64(bad.status) {
						t.Fatalf("refusal status: %+v", entity)
					}
					current, err := s.PageByID(ctx, id)
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(current, after) {
						t.Fatal("refused tool changed entity")
					}
				}
			})
		}
	}
}
