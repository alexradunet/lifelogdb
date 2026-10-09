package api_test

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
)

func TestTaskProjectPromotionActionsMatchRetainedReferences(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprintf("remote=%v", remote), func(t *testing.T) {
			ctx := context.Background()
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
			projectID, _, err := s.CreatePage(ctx, "cli", "Garden project", "Preserve this context.")
			if err != nil {
				t.Fatal(err)
			}
			ordinaryID, _, err := s.CreatePage(ctx, "cli", "Ordinary page", "")
			if err != nil {
				t.Fatal(err)
			}
			resource := fmt.Sprintf("/pages/%d", projectID)
			initial, err := c.Get(resource)
			if err != nil {
				t.Fatal(err)
			}
			// Retain actions advertised before the reference existed. Their direct
			// invocation must still refuse after the resource's legal actions change.
			promote, period := find(initial, "promote"), find(initial, "promote-period")
			if promote.Name == "" || period.Name == "" {
				t.Fatal("unreferenced page omitted promotion actions")
			}
			taskID, _, err := s.CreateTask(ctx, "cli", core.TaskSpec{Label: "Buy seeds", ProjectPageID: &projectID}, "")
			if err != nil {
				t.Fatal(err)
			}
			for _, deleted := range []bool{false, true} {
				if deleted {
					task, err := s.Task(ctx, taskID)
					if err != nil {
						t.Fatal(err)
					}
					if err := s.Do(ctx, "cli", func(tx *core.Tx) error { return tx.TaskLifecycle(taskID, task.Version, true) }); err != nil {
						t.Fatal(err)
					}
				}
				page, err := c.Get(resource)
				if err != nil {
					t.Fatal(err)
				}
				for _, action := range []string{"promote", "promote-period"} {
					if find(page, action).Name != "" {
						t.Errorf("task deleted=%v: project page advertises forbidden %s", deleted, action)
					}
				}
				if props(page)["task_project"] != true {
					t.Errorf("task deleted=%v: project context flag=%v", deleted, props(page)["task_project"])
				}
				body := browse(t, h, resource)
				if strings.Contains(body, resource+`/promote"`) || strings.Contains(body, resource+`/promote-period"`) {
					t.Errorf("task deleted=%v: browser renders forbidden project promotion", deleted)
				}
				before, err := s.PageByID(ctx, projectID)
				if err != nil {
					t.Fatal(err)
				}
				for _, attempt := range []struct {
					action api.Action
					values map[string]string
				}{
					{promote, map[string]string{"to": "person", "name": "Unexpected person"}},
					{period, map[string]string{"version": before.Version}},
				} {
					_, err := c.Do(attempt.action, attempt.values)
					var ce *client.Error
					if !errors.As(err, &ce) || ce.Status != 422 {
						t.Errorf("task deleted=%v: direct %s got %v, want refusal", deleted, attempt.action.Name, err)
					}
					after, err := s.PageByID(ctx, projectID)
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(before, after) {
						t.Fatalf("refused %s changed project: before %+v after %+v", attempt.action.Name, before, after)
					}
				}
				var typedRows int
				if err := d.R.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM people WHERE id=?)+(SELECT count(*) FROM periods WHERE id=?)`, projectID, projectID).Scan(&typedRows); err != nil {
					t.Fatal(err)
				}
				if typedRows != 0 {
					t.Fatal("refused promotion retained a typed extension")
				}
			}
			ordinary, err := c.Get(fmt.Sprintf("/pages/%d", ordinaryID))
			if err != nil {
				t.Fatal(err)
			}
			if find(ordinary, "promote").Name == "" || find(ordinary, "promote-period").Name == "" {
				t.Fatal("ordinary page lost promotion actions")
			}
			if props(ordinary)["task_project"] != false {
				t.Fatalf("ordinary page project context=%v, want false", props(ordinary)["task_project"])
			}
		})
	}
}
