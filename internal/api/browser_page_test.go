package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// page GETs a path as a browser does and returns the page whatever its status: a refusal is a page too.
func page(t *testing.T, h http.Handler, path string) string {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Errorf("GET %s: %d, and not a page: %.300s", path, rec.Code, body)
	}
	return body
}

// TestEveryPageIsSemanticHTML says what every page carries: both of this app's sheets under their markers, one
// main with a way to skip to it, the sidebar's sections with this page marked, a footer, the search dialog, no
// script at all (docs/plans/108-the-browser-made-current.md), and no id generated for a template to find.
func TestEveryPageIsSemanticHTML(t *testing.T) {
	c, h := fresh(t)
	root := must(c.Get(t.Context(), "/"))
	day := must(c.Do(t.Context(), find(root, "capture"), map[string]string{"text": "Ran with [[Sam]] #running", "mood": "4"}))
	sam := must(c.Get(t.Context(), "/pages?title=Sam"))
	gone := must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Scratch"}))
	must(c.Do(t.Context(), find(gone, "tombstone"), nil))

	for _, path := range []string{"/", "/days", href(day, "self"), href(sam, "self"), href(gone, "self"),
		"/metrics", "/search?q=ran", "/actions", "/integrity", "/tasks", "/deadlines", "/pages/999999"} {
		body := page(t, h, path)
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
	if body := page(t, h, "/"); !strings.Contains(body, `<a class="brand" href="/" aria-current="page">Lifelog</a>`) {
		t.Error("the home page does not mark the brand, its link, current")
	}
	if body := page(t, h, "/days/2026-01-01"); !strings.Contains(body, `<a href="/months/current" aria-current="page">`) {
		t.Error("a day that is not today does not mark Calendar current")
	}
	if body := page(t, h, "/people"); !strings.Contains(body, `<a href="/people" aria-current="page">`) {
		t.Error("the sidebar does not mark People current on its page")
	}
}

// TestADestructiveActionDrawsADangerButton says the warning comes from the catalog, not from a template that
// knows an action's name: a tombstone is drawn as a danger, and the revive that undoes it is not.
func TestADestructiveActionDrawsADangerButton(t *testing.T) {
	c, h := fresh(t)
	p := must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/")), "create-page"), map[string]string{"title": "Draft"}))
	if body := page(t, h, href(p, "self")); !strings.Contains(body, `<button class="danger">Delete</button>`) {
		t.Errorf("a page that offers a tombstone draws no danger button: %.400s", body)
	}
	gone := must(c.Do(t.Context(), find(p, "tombstone"), nil))
	body := page(t, h, href(gone, "self"))
	if !strings.Contains(body, "<button>Restore</button>") {
		t.Errorf("a deleted page offers no plain Restore: %.400s", body)
	}
	if strings.Contains(body, `<button class="danger">`) {
		t.Error("a deleted page draws a danger button for the revive that restores it")
	}
}
