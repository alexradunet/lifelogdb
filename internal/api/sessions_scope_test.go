package api_test

import (
	"fmt"
	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionAndScopeTransportParity(t *testing.T) {
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
			agent := client.InProcess(h, "agent:test")
			if remote {
				server := httptest.NewServer(h)
				t.Cleanup(server.Close)
				c = client.Remote(server.URL, "cli")
				agent = client.Remote(server.URL, "agent:test")
			}
			actions, err := c.Get("/actions")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.Do(find(actions, "create-page"), map[string]string{"title": "Workout"}); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Do(find(actions, "register-metric"), map[string]string{"name": "Steps", "unit": "steps"}); err != nil {
				t.Fatal(err)
			}
			session, err := c.Do(find(actions, "capture-session"), map[string]string{"kind": "Workout", "day": "2020-01-02", "start_local": "2020-01-01T23:00:00.000", "end_local": "2020-01-02T07:00:00.000", "start_zone_unverified": "Claimed/Zone", "import_key": "18446744073709551615"})
			if err != nil {
				t.Fatal(err)
			}
			props := session.Properties.(map[string]any)
			sessionID := fmt.Sprint(props["id"])
			if props["import_key"] != "18446744073709551615" || props["start_zone_unverified"] != "Claimed/Zone" || props["time_basis"] != "unresolved" {
				t.Fatalf("evidence %+v", props)
			}
			unassociated, err := c.Do(find(actions, "record"), map[string]string{"metric": "Steps", "day": "2020-01-02", "value": "10000"})
			if err != nil {
				t.Fatal(err)
			}
			scoped, err := c.Do(find(actions, "record"), map[string]string{"metric": "Steps", "day": "2020-01-02", "value": "4000", "session_id": sessionID})
			if err != nil {
				t.Fatal(err)
			}
			if scoped.Properties.(map[string]any)["scope"] != "session" || unassociated.Properties.(map[string]any)["scope"] != "unassociated" {
				t.Fatal("unlabeled scopes")
			}
			series, err := c.Get("/metrics/Steps?from=2020-01-01&to=2020-01-02")
			if err != nil {
				t.Fatal(err)
			}
			if len(series.Properties.(map[string]any)["readings"].([]any)) != 1 {
				t.Fatal("session totals leaked into default series")
			}
			corrected, err := c.Do(find(scoped, "correct"), map[string]string{"value": "3500"})
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(corrected.Properties.(map[string]any)["session_id"]) != sessionID {
				t.Fatal("correction moved scope")
			}
			if _, err := agent.Do(find(corrected, "relocate-reading"), map[string]string{"metric": "Steps", "day": "2020-01-02", "value": "3500"}); err == nil || !strings.Contains(err.Error(), "403") {
				t.Fatalf("agent relocation %v", err)
			}
			moved, err := c.Do(find(corrected, "relocate-reading"), map[string]string{"metric": "Steps", "day": "2020-01-03", "value": "3500"})
			if err != nil {
				t.Fatal(err)
			}
			if moved.Properties.(map[string]any)["scope"] != "unassociated" || moved.Result == nil {
				t.Fatal("atomic relocation result missing")
			}
			if _, err := c.Do(find(session, "tombstone-session"), nil); err != nil {
				t.Fatal(err)
			}
			active, err := c.Get("/sessions")
			if err != nil {
				t.Fatal(err)
			}
			if len(active.Properties.(map[string]any)["sessions"].([]any)) != 0 {
				t.Fatal("tombstone still active")
			}
			historical, err := c.Get("/sessions?include_deleted=1")
			if err != nil {
				t.Fatal(err)
			}
			if len(historical.Properties.(map[string]any)["sessions"].([]any)) != 1 {
				t.Fatal("historical session lost")
			}
		})
	}
}
