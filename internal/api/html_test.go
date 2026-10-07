package api_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// browse GETs a path as a browser does and fails unless it is a whole page.
func browse(t *testing.T, h http.Handler, path string) string {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Errorf("GET %s: %d %.300s", path, rec.Code, body)
	}
	return body
}

// submit posts a form as a browser does.
func submit(h http.Handler, path string, form url.Values, referer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	if referer != "" {
		req.Header.Set("Referer", "http://"+req.Host+referer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestEveryViewRenders(t *testing.T) {
	c, h := fresh(t)
	root := must(c.Get("/"))
	day := must(c.Do(find(root, "capture"), map[string]string{"text": "Ran with [[Sam]] #running, `[[not a link]]`", "mood": "4"}))
	must(c.Do(find(must(c.Get(href(day, "page"))), "save-body"), map[string]string{"body": "Ran with [[Sam]] and [[Ana]] #running"}))
	ana := must(c.Get("/pages?title=Ana"))
	must(c.Do(find(ana, "promote"), map[string]string{"to": "person", "name": "Ana Popescu"}))
	must(c.Do(find(root, "create-place"), map[string]string{"title": "Lisbon"}))
	must(c.Do(find(root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	must(c.Do(find(root, "register-metric"), map[string]string{"name": "walk"}))
	var reading *string
	for _, d := range []string{"2026-09-01", "2026-09-15", "2026-09-28"} {
		m := must(c.Do(find(root, "record"), map[string]string{"metric": "weight", "day": d, "value": "71.2"}))
		reading = &m.Links[0].Href
	}
	walk := must(c.Get("/metrics/walk"))
	must(c.Do(find(walk, "start-habit"), map[string]string{"start_day": "2026-09-20"}))
	draft := must(c.Do(find(root, "create-page"), map[string]string{"title": "Draft", "body": "old words"}))
	must(c.Do(find(draft, "rename"), map[string]string{"title": "Essay"}))
	gone := must(c.Do(find(root, "create-page"), map[string]string{"title": "Scratch"}))
	must(c.Do(find(gone, "tombstone"), nil))

	for _, p := range []string{"/", "/actions", "/days", href(day, "self"), "/people", "/places", "/ghosts", "/metrics",
		"/metrics/walk", "/measurements/1", "/search", "/search?q=ran", "/integrity", href(gone, "self"),
		href(draft, "self"), essay(t, h)} {
		browse(t, h, p)
	}
	for path, want := range map[string][]string{
		href(day, "page"): {`name="version" value="`, `<a href="/pages?title=Sam" class="wikilink">Sam</a>`,
			`<a href="/pages?title=running" class="wikilink">#running</a>`},
		"/pages?title=Ana":       {"Ana Popescu", "Backlinks", "mentioned in"},
		"/metrics/weight":        {`<svg class="chart"`, "71.2"},
		"/habits?day=2026-09-28": {`<button>Done</button>`, `<button>Not done</button>`, `name="done" value="1"`, `name="done" value="0"`},
		*reading:                 {`action="/measurements/`, "Correct"},
		href(gone, "self"):       {"Deleted", `action="` + href(gone, "self") + `/revive"`},
	} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Accept", "text/html")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusSeeOther { // /pages?title= opens the page
			path = rec.Header().Get("Location")
		}
		body := browse(t, h, path)
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("%s does not show %s", path, w)
			}
		}
	}
	if body := browse(t, h, href(day, "page")); strings.Contains(body, `/promote"`) {
		t.Error("a day page shows a promote form the API does not offer")
	}
}

// essay is where the renamed page's title leads.
func essay(t *testing.T, h http.Handler) string {
	req := httptest.NewRequest("GET", "/pages?title=Essay", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("/pages?title=Essay: %d", rec.Code)
	}
	return rec.Header().Get("Location")
}

func TestBrowserKeepsWhatAFailedSaveSent(t *testing.T) {
	c, h := fresh(t)
	p := must(c.Do(find(must(c.Get("/")), "create-page"), map[string]string{"title": "Diet", "body": "v1"}))
	save := find(p, "save-body")
	must(c.Do(save, map[string]string{"body": "v2"}))
	var version string
	for _, f := range save.Fields {
		if f.Name == "version" {
			version = f.Value.(string)
		}
	}
	rec := submit(h, save.Href, url.Values{"body": {"my <long> draft"}, "version": {version}}, href(p, "self"))
	body := rec.Body.String()
	if rec.Code != http.StatusConflict || !strings.Contains(body, "my &lt;long&gt; draft</textarea>") ||
		!strings.Contains(body, `href="`+href(p, "self")+`">Back`) {
		t.Errorf("a stale browser save: %d %.600s", rec.Code, body)
	}
}

func TestFeedbackURLPrivacyUsesStructureNotRandomSubstrings(t *testing.T) {
	opaque := "bad" + strings.Repeat("0", 61)
	if !feedbackLocation("/pages/1?feedback="+opaque, "/pages/1") {
		t.Fatal("valid random hex may contain the substring bad")
	}
	for _, location := range []string{
		"/pages/1?feedback=" + opaque + "&message=bad",
		"/pages/1?feedback=" + opaque + "&body=private%GG",
		"/pages/1?feedback=" + opaque + "&private%GG=value",
		"/pages/1?feedback=" + opaque + "&note=private%",
		"/pages/1?feedback=bad",
		"/pages/Revived?feedback=" + opaque,
	} {
		if feedbackLocation(location, "/pages/1") {
			t.Fatalf("accepted private or malformed URL %s", location)
		}
	}
}

func TestBrowserMutationFeedback(t *testing.T) {
	c, h := fresh(t)
	root := must(c.Get("/"))
	target := must(c.Do(find(root, "create-page"), map[string]string{"title": "Revived target"}))
	must(c.Do(find(target, "tombstone"), nil))
	rec := submit(h, "/pages", url.Values{"title": {"Feedback page"}, "body": {"[[Revived target]] [[bad/name]]"}}, "")
	if rec.Code != 303 {
		t.Fatalf("POST: %d %s", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	body := browse(t, h, location)
	for _, want := range []string{"Result", "revived", "skipped", "bad/name"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in redirected feedback", want)
		}
	}
	if strings.Contains(browse(t, h, location), "<h2>Result</h2>") {
		t.Error("receipt replayed")
	}
	page := must(c.Get("/pages?title=Feedback%20page"))
	if !feedbackLocation(location, href(page, "self")) {
		t.Errorf("feedback URL must contain only the resource path and opaque receipt: %s", location)
	}
}
