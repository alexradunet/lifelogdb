package api_test

import (
	"fmt"
	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestRecordedPeriodTransportParity(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprint(remote), func(t *testing.T) {
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
			c := client.InProcess(h, "cli")
			if remote {
				server := httptest.NewServer(h)
				t.Cleanup(server.Close)
				c = client.Remote(server.URL, "cli")
			}
			catalog, err := c.Get("/actions")
			if err != nil {
				t.Fatal(err)
			}
			created, err := c.Do(find(catalog, "create-period"), map[string]string{"title": "Synthetic study", "body": "With [[Sam]].", "start_boundary": "2018-09", "end_boundary": ".."})
			if err != nil {
				t.Fatal(err)
			}

			version := fmt.Sprint(created.Properties.(map[string]any)["version"])
			list, err := c.Get("/periods?day=2018-09-15&as_of=2018-09-15")
			if err != nil {
				t.Fatal(err)
			}
			ps := list.Properties.(map[string]any)["periods"].([]any)
			if len(ps) != 1 || ps[0].(map[string]any)["membership"] != "possible" || list.Properties.(map[string]any)["as_of"] != "2018-09-15" {
				t.Fatalf("membership %+v", list.Properties)
			}
			edited, err := c.Do(find(created, "edit-period"), map[string]string{"version": version, "start_boundary": "2018-09", "end_boundary": "2018-09-15"})
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(edited.Properties.(map[string]any)["version"]) == version {
				t.Fatal("revision unchanged")
			}
			if _, err := c.Do(find(created, "edit-period"), map[string]string{"version": version, "start_boundary": "2018"}); err == nil {
				t.Fatal("stale edit accepted")
			}
			list, err = c.Get("/periods?day=2018-09-15")
			if err != nil {
				t.Fatal(err)
			}
			if list.Properties.(map[string]any)["periods"].([]any)[0].(map[string]any)["membership"] != "definite" {
				t.Fatal("observed-end contrast")
			}
		})
	}
}
