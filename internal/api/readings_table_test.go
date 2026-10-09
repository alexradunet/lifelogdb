package api_test

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
)

// readings-from-table is an action of the catalog: an agent may run it, in-process and over HTTP, the page view
// offers it with the page filled when the body holds a table, and a second run writes nothing.
func TestReadingsFromTableOnEverySurface(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(map[bool]string{false: "in-process", true: "HTTP"}[remote], func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "life.db")
			if err := db.Init(p); err != nil {
				t.Fatal(err)
			}
			d, err := db.Open(p)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			handler := api.New(&core.Store{DB: d})
			owner := client.InProcess(handler, "cli")
			agent := client.InProcess(handler, "agent:test")
			if remote {
				srv := httptest.NewServer(handler)
				defer srv.Close()
				agent = client.Remote(srv.URL, "agent:test")
			}
			byName := map[string]api.Action{}
			actions, err := agent.Catalog()
			if err != nil {
				t.Fatal(err)
			}
			for _, a := range actions {
				byName[a.Name] = a
			}
			if _, err := owner.Do(byName["register-metric"], map[string]string{"name": "Weight", "unit": "kg"}); err != nil {
				t.Fatal(err)
			}
			page, err := agent.Do(byName["create-page"], map[string]string{"title": "Weighings", "body": "| day | weight (kg) |\n|---|---|\n| 2031-01-01 | 70.5 |\n| 2031-01-08 | 70 |\n"})
			if err != nil {
				t.Fatal(err)
			}
			var offered *api.Action
			for i, a := range page.Actions {
				if a.Name == "readings-from-table" {
					offered = &page.Actions[i]
				}
			}
			if offered == nil {
				t.Fatal("the page view does not offer readings-from-table")
			}
			var filled string
			for _, f := range offered.Fields {
				if f.Name == "page" {
					filled, _ = f.Value.(string)
				}
			}
			if filled != "Weighings" {
				t.Errorf("the offered action's page: %q", filled)
			}
			var got core.TableReadings
			for run, want := range []core.TableReadings{{Written: 2}, {Existing: 2}} {
				e, err := agent.Do(byName["readings-from-table"], map[string]string{"page": "Weighings", "metric": "weight"})
				if err != nil {
					t.Fatal(err)
				}
				b, _ := json.Marshal(e.Result)
				if err := json.Unmarshal(b, &got); err != nil {
					t.Fatal(err)
				}
				if got.Written != want.Written || got.Existing != want.Existing || len(got.Reported) != 0 || got.Unit != "kg" {
					t.Errorf("run %d: %+v", run+1, got)
				}
			}
		})
	}
}
