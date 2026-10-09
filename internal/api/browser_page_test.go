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
// main with a way to skip to it, the navigation with this page marked, a footer, and no script and no id
// generated for a template to find.
func TestEveryPageIsSemanticHTML(t *testing.T) {
	c, h := fresh(t)
	root := must(c.Get("/"))
	day := must(c.Do(find(root, "capture"), map[string]string{"text": "Ran with [[Sam]] #running", "mood": "4"}))
	sam := must(c.Get("/pages?title=Sam"))
	gone := must(c.Do(find(root, "create-page"), map[string]string{"title": "Scratch"}))
	must(c.Do(find(gone, "tombstone"), nil))

	for _, path := range []string{"/", "/days", href(day, "self"), href(sam, "self"), href(gone, "self"),
		"/metrics", "/habits", "/search?q=ran", "/actions", "/integrity", "/tasks", "/deadlines", "/pages/999999"} {
		body := page(t, h, path)
		for _, want := range []string{
			`<style data-sheet="base">`, `<style data-sheet="app">`, // the two sheets, the base one first
			`<a class="skip" href="#main">`, `<main id="main">`, `<nav aria-label="Sections">`, "<footer>",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s does not carry %s", path, want)
			}
		}
		for _, never := range []string{"<script", `id="f-`} {
			if strings.Contains(body, never) {
				t.Errorf("%s carries %s: a page is semantic HTML with no script and no generated id", path, never)
			}
		}
		if n := strings.Count(body, `aria-current="page"`); n > 1 {
			t.Errorf("%s marks %d navigation links current, want one at most", path, n)
		}
	}
	if body := page(t, h, "/"); !strings.Contains(body, `<a href="/" aria-current="page">Home</a>`) {
		t.Error("the home page does not mark Home current")
	}
	if body := page(t, h, href(day, "self")); !strings.Contains(body, `<a href="/days" aria-current="page">Days</a>`) {
		t.Error("a day does not mark Days current")
	}
}

// TestADestructiveActionDrawsADangerButton says the warning comes from the catalog, not from a template that
// knows an action's name: a tombstone is drawn as a danger, and the revive that undoes it is not.
func TestADestructiveActionDrawsADangerButton(t *testing.T) {
	c, h := fresh(t)
	p := must(c.Do(find(must(c.Get("/")), "create-page"), map[string]string{"title": "Draft"}))
	if body := page(t, h, href(p, "self")); !strings.Contains(body, `<button class="danger">Delete</button>`) {
		t.Errorf("a page that offers a tombstone draws no danger button: %.400s", body)
	}
	gone := must(c.Do(find(p, "tombstone"), nil))
	body := page(t, h, href(gone, "self"))
	if !strings.Contains(body, "<button>Revive</button>") {
		t.Errorf("a deleted page offers no plain revive: %.400s", body)
	}
	if strings.Contains(body, `<button class="danger">`) {
		t.Error("a deleted page draws a danger button for the revive that restores it")
	}
}
