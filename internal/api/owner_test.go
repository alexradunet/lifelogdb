package api_test

import (
	"os"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db/dbtest"
)

// The owner-only actions are refused to an agent and to an import, with 403 and the code forbidden. The full-text
// check of the integrity checks (integrity-check) holds the write lock while it runs, so it is the owner's alone too:
// its GET route refuses as a POST route does. A refused action writes nothing: no metric, and no file in the snapshot
// folder. That they are never MCP tools is a test of mcp (TestTheToolsAreTheActionsThatAreNotTheOwners).
func TestOwnerOnlyActionsPolicy(t *testing.T) {
	d, snapshots := dbtest.New(t), t.TempDir()
	h := api.New(&core.Store{DB: d, SnapshotDir: snapshots})
	catalog := apitest.Must(t)(client.InProcess(h, "agent:policy-test").Get(t.Context(), "/actions"))
	register, snapshot, integrity, record := apitest.Action(t, catalog, "register-metric"), apitest.Action(t, catalog, "snapshot"), apitest.Action(t, catalog, "integrity-check"), apitest.Action(t, catalog, "record")
	if !register.Owner || !snapshot.Owner || !integrity.Owner || integrity.Method != "GET" || record.Owner {
		t.Fatalf("wrong catalog policy: %+v %+v %+v %+v", register, snapshot, integrity, record)
	}
	sent := map[string]map[string]string{"register-metric": {"name": "Forbidden direct"}}
	for _, source := range []string{"agent:policy-test", "import:policy-test"} {
		c := client.InProcess(h, source)
		for _, a := range catalog.Actions {
			if !a.Owner {
				continue
			}
			e, err := c.Do(t.Context(), a, sent[a.Name])
			var code any
			if e != nil {
				p, _ := e.Properties.(map[string]any)
				code = p["code"]
			}
			if apitest.Status(err) != 403 || code != "forbidden" {
				t.Errorf("%s: owner-only %s: %v, code %v; want 403 forbidden", source, a.Name, err, code)
			}
		}
	}
	var count int
	if err := d.R.QueryRow("SELECT count(*) FROM entity_names WHERE name_key = 'forbidden direct'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("a refused registration wrote a metric: %d %v", count, err)
	}
	if files, err := os.ReadDir(snapshots); err != nil || len(files) != 0 {
		t.Fatalf("a refused snapshot wrote in the snapshot folder: %v %v", files, err)
	}
}

// A writer name that is not well formed is refused where it is used. A write that inserts a row meets
// entities_source, so the 422 is SQLite's and names the constraint. An owner-only action may only read, and meets no
// CHECK, so the boundary refuses the name: "Agent:x" must not pass as the owner by missing the prefix "agent:".
func TestMalformedWriterIsRefusedByTheSchemaOrTheOwnerCheck(t *testing.T) {
	d := dbtest.New(t)
	h := api.New(&core.Store{DB: d, SnapshotDir: t.TempDir()})
	catalog := apitest.Must(t)(client.InProcess(h, "cli").Get(t.Context(), "/actions"))
	for _, source := range []string{"Bad Source!", "Agent:x", strings.Repeat("a", 65)} {
		c := client.InProcess(h, source)
		_, err := c.Do(t.Context(), apitest.Action(t, catalog, "create-page"), map[string]string{"title": "Malformed " + source[:3], "body": "words"})
		if apitest.Status(err) != 422 || !strings.Contains(err.Error(), "entities_source") {
			t.Errorf("create-page as %q: %v; want 422 that names entities_source", source, err)
		}
		_, err = c.Do(t.Context(), apitest.Action(t, catalog, "integrity-check"), nil)
		if apitest.Status(err) != 422 {
			t.Errorf("integrity-check as %q: %v; want 422", source, err)
		}
	}
	var count int
	if err := d.R.QueryRow("SELECT count(*) FROM entity_names WHERE title LIKE 'Malformed%'").Scan(&count); err != nil || count != 0 {
		t.Errorf("a refused write left pages: %d %v", count, err)
	}
}
