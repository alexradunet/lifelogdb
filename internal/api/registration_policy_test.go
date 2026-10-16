package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
	lifelogmcp "lifelog/internal/mcp"
)

// The owner-only actions are refused to an agent and are never MCP tools; every other action is a tool.
func TestOwnerOnlyActionsPolicy(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "synthetic.db")
	if err := db.Init(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	agent := client.InProcess(api.New(&core.Store{DB: d, SnapshotDir: root}), "agent:policy-test")
	catalog := must(agent.Get(t.Context(), "/actions"))
	register, snapshot, integrity, record := find(catalog, "register-metric"), find(catalog, "snapshot"), find(catalog, "integrity-check"), find(catalog, "record")
	if !register.Owner || !snapshot.Owner || !integrity.Owner || record.Name == "" || record.Owner {
		t.Fatalf("wrong catalog policy: %+v %+v %+v %+v", register, snapshot, integrity, record)
	}
	sent := map[string]map[string]string{"register-metric": {"name": "Forbidden direct"}}
	for _, a := range catalog.Actions {
		if !a.Owner {
			continue
		}
		_, err := agent.Do(t.Context(), a, sent[a.Name])
		var ce *client.Error
		if !errors.As(err, &ce) || ce.Status != 403 {
			t.Fatalf("owner-only %s not refused: %v", a.Name, err)
		}
	}
	var count int
	if err := d.R.QueryRow("SELECT count(*) FROM entity_names WHERE name_key = 'forbidden direct'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("a refused registration wrote a metric: %d %v", count, err)
	}

	// Inspect the actual SDK listing, without invoking any MCP tool.
	srv, err := lifelogmcp.New(t.Context(), agent, "test")
	if err != nil {
		t.Fatal(err)
	}
	st, ct := mcpsdk.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, st) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("SDK listing server did not stop")
		}
	})
	session, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "policy-test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, tool := range tools.Tools {
		listed[tool.Name] = true
	}
	for _, a := range catalog.Actions {
		if tool := strings.ReplaceAll(a.Name, "-", "_"); listed[tool] == a.Owner {
			t.Errorf("action %s (owner %v): MCP tool %s listed %v", a.Name, a.Owner, tool, listed[tool])
		}
	}
}

// The full-text check of the integrity checks holds the write lock while it runs, so it is the owner's alone: a GET
// route refuses an agent or an import as a POST route does.
func TestIntegrityCheckIsRefusedToAnAgent(t *testing.T) {
	_, h := fresh(t)
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
