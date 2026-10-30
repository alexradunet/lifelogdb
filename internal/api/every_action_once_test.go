package api_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

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
			body := mainOf(browse(t, h, row.path))
			for _, a := range e.Actions {
				if rel := drawnByLink[a.Name]; rel != "" {
					if l := e.LinkByRel(rel); l == nil || strings.Count(body, `href="`+l.Href+`"`) != 1 {
						t.Errorf("%s: the link %s of %s is not drawn once", row.path, rel, a.Name)
					}
					continue
				}
				forms := strings.Count(body, `data-action="`+a.Name+`"`)
				links := strings.Count(body, `?new=`+a.Name)
				// A form is the one place. Only an action with no form is opened by a link, once (the quarters of a year
				// link the same create-page beside its form: they open it for a title of their own).
				if forms != 1 && !(forms == 0 && links == 1) {
					t.Errorf("%s: the action %s is drawn %d times as a form and %d as a link, want once", row.path, a.Name, forms, links)
				}
			}
			// Nothing is drawn that the entity does not offer.
			offered := map[string]bool{}
			for _, a := range e.Actions {
				offered[a.Name] = true
			}
			for _, m := range regexp.MustCompile(` data-action="([^"]*)"`).FindAllStringSubmatch(body, -1) {
				if !offered[m[1]] && !railOwns(m[1]) {
					t.Errorf("%s draws a form of %s, which the entity does not offer", row.path, m[1])
				}
			}
		})
	}
}

// railOwns names the actions that the shell draws on every page, whatever the entity offers: the search of the rail.
func railOwns(name string) bool { return name == "search" && false }

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
	_ = http.StatusOK
}
