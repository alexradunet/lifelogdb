package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
	"lifelog/siren"
)

// drawnByLink names the actions that a page opens by a link and not by a form of its own: the picker of the cover
// (rel choose-cover) is the one place of set-cover (docs/plans/159-the-class-of-an-action.md).
var drawnByLink = map[string]string{"set-cover": "choose-cover"}

// TestEveryActionOfAnEntityIsDrawnOnce: for each kind of entity that a browser draws, the HTML of an address draws each
// action that the JSON of the same address offers exactly once: one form that carries data-action="NAME" (its domain
// place, or the ⋯ menu, or the creates of the head), or one link that opens it. A template that places an action the
// table of the views does not list, or that lists one it does not draw, fails here.
func TestEveryActionOfAnEntityIsDrawnOnce(t *testing.T) {
	ctx := t.Context()
	c, h, _ := apitest.FreshOn(t, "cli", "2026-09-30")
	root := apitest.Must(t)(c.Get(ctx, "/"))
	do := func(name string, e *siren.Entity, v map[string]string) *siren.Entity {
		t.Helper()
		return apitest.Must(t)(c.Do(ctx, apitest.Action(t, e, name), v))
	}
	do("capture", root, map[string]string{"text": "Ran with [[Sam]] and [[Ana]]", "mood": "4"})
	ana := apitest.Must(t)(c.Get(ctx, "/pages?title=Ana"))
	do("promote", ana, map[string]string{"name": "Ana Popescu"})
	lisbon := do("create-page", root, map[string]string{"title": "Lisbon", "body": "A city."})
	do("file-pages", lisbon, map[string]string{"category": "Cities"})
	cities := apitest.Must(t)(c.Get(ctx, "/pages?title=Cities"))
	do("register-metric", root, map[string]string{"name": "weight", "unit": "kg"})
	reading := do("record", root, map[string]string{"metric": "weight", "day": "2026-09-15", "value": "71.2"})
	do("add-file", root, map[string]string{"title": "Pic.png", "sha256": strings.Repeat("cd", 32), "mime": "image/png"})
	do("add-file", root, map[string]string{"title": "Scan.pdf", "sha256": strings.Repeat("ab", 32), "mime": "application/pdf"})
	scan := apitest.Must(t)(c.Get(ctx, "/pages?title=Scan.pdf"))
	gone := do("create-page", root, map[string]string{"title": "Scratch"})
	do("tombstone-pages", gone, nil)

	for _, row := range []struct{ name, path string }{
		{"a page", apitest.Href(lisbon, "self")},
		{"a category page", apitest.Href(cities, "self")},
		{"a person", apitest.Href(ana, "self")},
		{"a file", apitest.Href(scan, "self")},
		{"a metric and its series", "/metrics/weight"},
		{"a reading", apitest.Href(reading, "self")},
		{"a deleted page", apitest.Href(gone, "self")},
		{"the stream", "/days"},
		{"browse", "/browse"},
		{"browse, the people", "/browse?kind=person"},
		{"browse, a category", "/browse?in=Cities"},
		{"browse, the media", "/browse?kind=media"},
		{"the metrics", "/metrics"},
		{"a week", "/weeks/2026-W40"},
		{"a month", "/months/2026-09"},
		{"a year", "/years/2026"},
		{"the integrity page", "/integrity"},
		{"the settings", "/settings"},
		{"a search", "/search?q=ran"},
		{"the conversation of a day", "/days/today/conversation"},
	} {
		t.Run(row.name, func(t *testing.T) {
			e, err := c.Get(ctx, row.path)
			if err != nil {
				t.Fatal(err)
			}
			drawnOnce(t, row.path, e, mainOf(browse(t, h, row.path)))
		})
	}
}

// drawnOnce holds the rule of the test above for one address: body is the HTML and e the JSON of the same address.
// The expected answer is the JSON of the entity, never the table of the views.
func drawnOnce(t *testing.T, path string, e *siren.Entity, body string) {
	t.Helper()
	offered := map[string]bool{}
	for _, a := range e.Actions {
		offered[a.Name] = true
		if rel := drawnByLink[a.Name]; rel != "" {
			if l := e.LinkByRel(rel); l == nil || strings.Count(body, `href="`+l.Href+`"`) != 1 {
				t.Errorf("%s: the link %s of %s is not drawn once", path, rel, a.Name)
			}
			continue
		}
		forms := strings.Count(body, `data-action="`+a.Name+`"`)
		links := strings.Count(body, `?new=`+a.Name)
		// A form is the one place. Only an action with no form is opened by a link, once (the quarters of a year
		// link the same create-page beside its form: they open it for a title of their own).
		if forms != 1 && !(forms == 0 && links == 1) {
			t.Errorf("%s: the action %s is drawn %d times as a form and %d as a link, want once", path, a.Name, forms, links)
		}
	}
	// Nothing is drawn that the entity does not offer.
	for _, m := range regexp.MustCompile(` data-action="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		if !offered[m[1]] {
			t.Errorf("%s draws a form of %s, which the entity does not offer", path, m[1])
		}
	}
}

// fakeTaskService answers the reads of the task protocol: two open tasks, and the first of them by its address.
func fakeTaskService(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tasks := []map[string]any{}
		if r.URL.Path == "/open" {
			tasks = []map[string]any{{"id": "t1", "title": "Water the plants", "list": "home", "due": "2000-01-01"}, {"id": "t2", "title": "Call Ana", "list": "work"}}
		}
		props := map[string]any{"tasks": tasks}
		if r.URL.Path == "/tasks/t1" {
			props = map[string]any{"id": "t1", "title": "Water the plants", "list": "home", "due": "2000-01-01"}
		}
		w.Header().Set("Content-Type", "application/vnd.siren+json")
		if err := json.NewEncoder(w).Encode(&siren.Entity{Class: []string{"tasks"}, Properties: props}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestEveryActionOfAnAppAndOfTheTaskServiceIsDrawnOnce is the test above for the pages that a connected app and the
// task service send: the actions of their entities are drawn by class, and each of them once.
func TestEveryActionOfAnAppAndOfTheTaskServiceIsDrawnOnce(t *testing.T) {
	h, apps, _ := withServer(t)
	apps.Expect("svc")
	var writes []siren.Action
	for _, href := range []string{"/tasks", "/tasks/{id}", "/tasks/{id}/complete", "/tasks/{id}/reopen", "/tasks/{id}/delete", "/sync"} {
		writes = append(writes, siren.Action{Name: "w" + href, Method: "POST", Href: href})
	}
	if err := apps.SetTasks("svc", fakeTaskService(t).URL, writes, []api.TaskList{{ID: "home", Name: "Home"}, {ID: "work", Name: "Work"}}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/with/testapp/", "/tasks", "/tasks/week", "/tasks/items/t1"} {
		t.Run(path, func(t *testing.T) {
			var e siren.Entity
			if rec := call(h, "GET", path, "", nil); rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &e) != nil {
				t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body)
			}
			if len(e.Actions) == 0 {
				t.Fatalf("%s offers no action: the test proves nothing", path)
			}
			drawnOnce(t, path, &e, mainOf(browse(t, h, path)))
		})
	}
}

// TestADayDrawsNoMenu: every action of a day has a place of its own (the capture box, the tiles, the editor, the
// cover link), so its menu is empty and draws no button.
func TestADayDrawsNoMenu(t *testing.T) {
	ctx := t.Context()
	c, h, _ := apitest.FreshOn(t, "cli", "2026-09-30")
	root := apitest.Must(t)(c.Get(ctx, "/"))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "capture"), map[string]string{"text": "A line", "mood": "3"}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	for _, path := range []string{"/days/2026-09-30", "/days/2026-09-01"} {
		e := apitest.Must(t)(c.Get(ctx, path))
		body := mainOf(browse(t, h, path))
		if strings.Contains(body, `aria-label="More actions"`) {
			t.Errorf("%s draws a menu button: a day has no action without a place", path)
		}
		for _, a := range e.Actions {
			if _, byLink := drawnByLink[a.Name]; !byLink && !strings.Contains(body, fmt.Sprintf(`data-action=%q`, a.Name)) {
				t.Errorf("%s does not draw the action %s", path, a.Name)
			}
		}
	}
}

// TestAnAskedFormIsNotInTheBackOfAForm: a form that the link of the page asked for (?new=NAME) is open on that page
// only. No form on it carries that query in its back, so another write does not open the dialog again.
func TestAnAskedFormIsNotInTheBackOfAForm(t *testing.T) {
	ctx := t.Context()
	c, h, _ := apitest.FreshOn(t, "cli", "2026-09-30")
	root := apitest.Must(t)(c.Get(ctx, "/"))
	lisbon := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-page"), map[string]string{"title": "Lisbon", "body": "A city."}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, lisbon, "file-pages"), map[string]string{"category": "Cities"}))
	self := apitest.Href(lisbon, "self")
	backs := regexp.MustCompile(`name="back" value="([^"]*)"`)
	for _, path := range []string{self + "?new=rename", self + "?new=tombstone-pages", self + "?new=file-pages", "/browse?in=Cities&new=file-pages"} {
		found := backs.FindAllStringSubmatch(browse(t, h, path), -1)
		if len(found) == 0 {
			t.Errorf("%s draws no form with a back: the test proves nothing", path)
		}
		for _, m := range found {
			if strings.Contains(m[1], "new=") {
				t.Errorf("%s: a form has the back %q, which asks for a form again", path, m[1])
			}
		}
	}
}
