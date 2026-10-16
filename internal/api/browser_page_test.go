package api_test

import (
	"net/http"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// TestEveryPageIsSemanticHTML says what every page carries: both of this app's sheets under their markers, one
// main with a way to skip to it, the sidebar's sections with this page marked, a footer, the search dialog, no
// script at all (docs/plans/108-the-browser-made-current.md), and no id generated for a template to find.
func TestEveryPageIsSemanticHTML(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	day := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "capture"), map[string]string{"text": "Ran with [[Sam]] #running", "mood": "4"}))
	sam := apitest.Must(t)(c.Get(t.Context(), "/pages?title=Sam"))
	gone := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Scratch"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, gone, "tombstone"), nil))

	for _, path := range []string{"/", "/days", apitest.Href(day, "self"), apitest.Href(sam, "self"), apitest.Href(gone, "self"),
		"/metrics", "/search?q=ran", "/actions", "/integrity", "/pages/999999"} {
		status := http.StatusOK
		if path == "/pages/999999" {
			status = http.StatusNotFound
		}
		body := browseStatus(t, h, path, status)
		for _, want := range []string{
			`<style data-sheet="base">`, `<style data-sheet="app">`, // the two sheets, the base one first
			`<a class="skip" href="#main">`, `<main id="main">`, `<nav aria-label="Sections">`, "<footer>",
			`<dialog class="palette" id="palette" closedby="any" aria-label="Search">`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s does not carry %s", path, want)
			}
		}
		if strings.Contains(body, "<script") {
			t.Errorf("%s carries a script: a page is HTML the browser runs by itself", path)
		}
		if strings.Contains(body, `id="f-`) {
			t.Errorf("%s carries a generated id: a page is semantic HTML", path)
		}
		if n := strings.Count(body, `aria-current="page"`); n > 1 {
			t.Errorf("%s marks %d navigation links current, want one at most", path, n)
		}
	}
	if body := browse(t, h, "/"); !strings.Contains(body, `<a class="brand" href="/" aria-current="page">Lifelog</a>`) {
		t.Error("the home page does not mark the brand, its link, current")
	}
	if body := browse(t, h, "/days/2026-01-01"); !strings.Contains(body, `<a href="/months/current" aria-current="page">`) {
		t.Error("a day that is not today does not mark Calendar current")
	}
	if body := browse(t, h, "/browse?kind=person"); !strings.Contains(body, `<a href="/browse" aria-current="page">`) {
		t.Error("the sidebar does not mark Browse current on its page")
	}
}

// TestADestructiveActionDrawsADangerButton says the warning comes from the catalog, not from a template that
// knows an action's name: a tombstone is drawn as a danger, and the revive that undoes it is not.
func TestADestructiveActionDrawsADangerButton(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	p := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "create-page"), map[string]string{"title": "Draft"}))
	if body := browse(t, h, apitest.Href(p, "self")); !strings.Contains(body, `<button class="danger">Delete</button>`) {
		t.Errorf("a page that offers a tombstone draws no danger button: %.400s", body)
	}
	gone := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, p, "tombstone"), nil))
	body := browse(t, h, apitest.Href(gone, "self"))
	if !strings.Contains(body, "<button>Restore</button>") {
		t.Errorf("a deleted page offers no plain Restore: %.400s", body)
	}
	if strings.Contains(body, `<button class="danger">`) {
		t.Error("a deleted page draws a danger button for the revive that restores it")
	}
}
