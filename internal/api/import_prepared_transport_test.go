package api_test

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/importer"
)

func TestPreparedCatalogTransportOwnerGateAndPersistedMeaning(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(map[bool]string{false: "in-process", true: "HTTP"}[remote], func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "Source")
			if err := os.Mkdir(source, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, "daily.csv"), []byte("Date,Distance (m)\n2020-01-02,10.5\n"), 0600); err != nil {
				t.Fatal(err)
			}
			ws, err := importer.Open(source + ".lifelog")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = ws.Setup(""); err != nil {
				t.Fatal(err)
			}
			if _, err = ws.MakeLedger(); err != nil {
				t.Fatal(err)
			}
			if err = ws.DraftRules("source: import:synthetic\n"); err != nil {
				t.Fatal(err)
			}
			review, err := ws.Review("rules.md")
			if err != nil {
				t.Fatal(err)
			}
			if err = ws.Approve("rules.md", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), review.Hash); err != nil {
				t.Fatal(err)
			}
			d, err := db.Open(ws.TrialDB())
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			store := &core.Store{DB: d}
			if _, err = store.RegisterMetric(context.Background(), "cli", "Distance", "m", ""); err != nil {
				t.Fatal(err)
			}
			handler := api.New(store, ws)
			c := client.InProcess(handler, "agent:synthetic")
			if remote {
				srv := httptest.NewServer(handler)
				defer srv.Close()
				c = client.Remote(srv.URL, "agent:synthetic")
			}
			actions, err := c.Catalog()
			if err != nil {
				t.Fatal(err)
			}
			byName := map[string]api.Action{}
			for _, a := range actions {
				byName[a.Name] = a
				if a.Name == "approve" || a.Name == "approve-prepared" {
					t.Fatal("owner stamp exposed to agent")
				}
			}
			if _, err = c.Do(byName["draft-prepared"], map[string]string{"file": "daily.csv", "profile": "fit-date-csv-v1", "metrics": `{"distance":"Distance"}`}); err != nil {
				t.Fatal(err)
			}
			if _, err = c.Do(byName["apply-prepared"], nil); err == nil {
				t.Fatal("agent draft applied without owner stamp")
			}
			review, err = ws.Review("prepared.md")
			if err != nil {
				t.Fatal(err)
			}
			if err = ws.Approve("prepared.md", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), review.Hash); err != nil {
				t.Fatal(err)
			}
			if _, err = c.Do(byName["check-prepared"], nil); err != nil {
				t.Fatal(err)
			}
			if _, err = c.Do(byName["apply-prepared"], nil); err != nil {
				t.Fatal(err)
			}
			var value float64
			var day, sourceName string
			var unscoped bool
			if err = d.R.QueryRow(`SELECT value,day,source,session_id IS NULL FROM measurements`).Scan(&value, &day, &sourceName, &unscoped); err != nil || value != 10.5 || day != "2020-01-02" || sourceName != "import:synthetic" || !unscoped {
				t.Fatalf("meaning %g %s %s %v %v", value, day, sourceName, unscoped, err)
			}
		})
	}
}
