package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db/dbtest"
)

// The owner-only actions are refused to an agent. A refused action writes nothing: no metric, and no file in the
// snapshot folder. That they are never MCP tools is a test of mcp (TestTheToolsAreTheActionsThatAreNotTheOwners).
func TestOwnerOnlyActionsPolicy(t *testing.T) {
	d, snapshots := dbtest.New(t), t.TempDir()
	agent := client.InProcess(api.New(&core.Store{DB: d, SnapshotDir: snapshots}), "agent:policy-test")
	catalog := apitest.Must(t)(agent.Get(t.Context(), "/actions"))
	register, snapshot, integrity, record := apitest.Action(t, catalog, "register-metric"), apitest.Action(t, catalog, "snapshot"), apitest.Action(t, catalog, "integrity-check"), apitest.Action(t, catalog, "record")
	if !register.Owner || !snapshot.Owner || !integrity.Owner || record.Owner {
		t.Fatalf("wrong catalog policy: %+v %+v %+v %+v", register, snapshot, integrity, record)
	}
	sent := map[string]map[string]string{"register-metric": {"name": "Forbidden direct"}}
	for _, a := range catalog.Actions {
		if !a.Owner {
			continue
		}
		_, err := agent.Do(t.Context(), a, sent[a.Name])
		if apitest.Status(err) != 403 {
			t.Fatalf("owner-only %s not refused: %v", a.Name, err)
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

// The full-text check of the integrity checks holds the write lock while it runs, so it is the owner's alone: a GET
// route refuses an agent or an import as a POST route does.
func TestIntegrityCheckIsRefusedToAnAgent(t *testing.T) {
	_, h, _ := apitest.Fresh(t, "cli")
	for _, tc := range []struct {
		source string
		status int
		code   string
	}{
		{"", http.StatusOK, ""},
		{"agent:x", http.StatusForbidden, "forbidden"},
		{"import:x", http.StatusForbidden, "forbidden"},
	} {
		req := httptest.NewRequest("GET", "/integrity", nil)
		if tc.source != "" {
			req.Header.Set(api.SourceHeader, tc.source)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Errorf("source %q: status %d, want %d: %.200s", tc.source, rec.Code, tc.status, rec.Body.String())
		}
		var e struct {
			Properties struct {
				Code string `json:"code"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
			t.Fatalf("source %q: %v", tc.source, err)
		}
		if tc.code != "" && e.Properties.Code != tc.code {
			t.Errorf("source %q: code %q, want %q", tc.source, e.Properties.Code, tc.code)
		}
	}
}
