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
	root := must(c.Get(t.Context(), "/"))
	day := must(c.Do(t.Context(), find(root, "capture"), map[string]string{"text": "Ran with [[Sam]] #running, `[[not a link]]`", "mood": "4"}))
	must(c.Do(t.Context(), find(must(c.Get(t.Context(), href(day, "page"))), "save-body"), map[string]string{"body": "Ran with [[Sam]] and [[Ana]] #running"}))
	ana := must(c.Get(t.Context(), "/pages?title=Ana"))
	must(c.Do(t.Context(), find(ana, "promote"), map[string]string{"to": "person", "name": "Ana Popescu"}))
	must(c.Do(t.Context(), find(root, "create-place"), map[string]string{"title": "Lisbon"}))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "walk"}))
	var reading *string
	for _, d := range []string{"2026-09-01", "2026-09-15", "2026-09-28"} {
		m := must(c.Do(t.Context(), find(root, "record"), map[string]string{"metric": "weight", "day": d, "value": "71.2"}))
		reading = &m.Links[0].Href
	}
	walk := must(c.Get(t.Context(), "/metrics/walk"))
	must(c.Do(t.Context(), find(walk, "start-habit"), map[string]string{"start_day": "2026-09-20"}))
	draft := must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Draft", "body": "old words"}))
	must(c.Do(t.Context(), find(draft, "rename"), map[string]string{"title": "Essay"}))
	gone := must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Scratch"}))
	must(c.Do(t.Context(), find(gone, "tombstone"), nil))

	for _, p := range []string{"/", "/actions", "/days", href(day, "self"), "/people", "/places", "/ghosts", "/metrics",
		"/metrics/walk", "/measurements/1", "/search", "/search?q=ran", "/integrity", href(gone, "self"), "/weeks/2026-W40", "/years/2026",
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

// staleSave is a page with a save that is already stale: the version it names is not the current one.
func staleSave(t *testing.T) (h http.Handler, self, action, version string) {
	t.Helper()
	c, h := fresh(t)
	p := must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/")), "create-page"), map[string]string{"title": "Diet", "body": "v1"}))
	save := find(p, "save-body")
	must(c.Do(t.Context(), save, map[string]string{"body": "v2"}))
	for _, f := range save.Fields {
		if f.Name == "version" {
			version = f.Value.(string)
		}
	}
	return h, href(p, "self"), save.Href, version
}

// TestBrowserKeepsWhatAFailedSaveSent: with no page to return to, a refused save answers with the error view, which
// holds what the form sent.
func TestBrowserKeepsWhatAFailedSaveSent(t *testing.T) {
	h, self, action, version := staleSave(t)
	rec := submit(h, action, url.Values{"body": {"my <long> draft"}, "version": {version}}, self)
	body := rec.Body.String()
	if rec.Code != http.StatusConflict || !strings.Contains(body, "my &lt;long&gt; draft</textarea>") ||
		!strings.Contains(body, `href="`+self+`">Back`) || !strings.Contains(body, `aria-label="What you sent"`) {
		t.Errorf("a stale browser save: %d %.600s", rec.Code, body)
	}
}

// TestARefusedSaveComesBackOnItsPage: a form that names its page (back) is answered with that page, with the error's
// status, the alert first and the form filled, open and described by the alert.
func TestARefusedSaveComesBackOnItsPage(t *testing.T) {
	h, self, action, version := staleSave(t)
	rec := submit(h, action, url.Values{"body": {"my <long> draft"}, "version": {version}, "back": {self}}, self)
	body := rec.Body.String()
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409: %.400s", rec.Code, body)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control %q, want no-store", got)
	}
	for _, want := range []string{"<title>Not saved · Diet · Lifelog</title>",
		`role="alert" id="refused" tabindex="-1" autofocus>`, "my &lt;long&gt; draft</textarea>",
		`<details open><summary>Edit</summary>`, `data-action="save-body" aria-describedby="refused"`,
		`value="` + version + `"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the refused page lacks %s", want)
		}
	}
	if n := strings.Count(body, "autofocus"); n != 1 {
		t.Errorf("%d autofocus on the refused page, want 1 (the alert)", n)
	}
	if strings.Contains(body, `aria-label="What you sent"`) {
		t.Error("the refused page also shows the error view's copy of the text")
	}
}

// TestARefusedCaptureComesBackOnHome: a capture refused from Home comes back on Home with the text still in the box.
func TestARefusedCaptureComesBackOnHome(t *testing.T) {
	_, h := freshOn(t, "2026-10-12") // the form posts to that day, and Home's capture is today's
	rec := submit(h, "/days/2026-10-12/capture", url.Values{"text": {"A note that must not be lost."}, "mood": {"9"}, "back": {"/"}}, "")
	body := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422: %.400s", rec.Code, body)
	}
	for _, want := range []string{"<title>Not saved · Lifelog · Lifelog</title>", `aria-label="Capture"`, "Add to today", `role="alert" id="refused"`,
		">A note that must not be lost.</textarea>", `aria-describedby="refused"`} {
		if !strings.Contains(body, want) {
			t.Errorf("Home, refused, lacks %s", want)
		}
	}
	if n := strings.Count(body, "autofocus"); n != 1 {
		t.Errorf("%d autofocus on the refused page, want 1 (the alert)", n)
	}
}

// TestARefusalWithoutAPageToReturnToIsTheErrorView: a back path whose page is missing, or that redirects, falls back
// to the error view, and a JSON client gets the error entity whatever the form named.
func TestARefusalWithoutAPageToReturnToIsTheErrorView(t *testing.T) {
	h, self, action, version := staleSave(t)
	for _, back := range []string{"/pages/999999", "/days/today"} {
		rec := submit(h, action, url.Values{"body": {"my draft"}, "version": {version}, "back": {back}}, self)
		body := rec.Body.String()
		if rec.Code != http.StatusConflict || !strings.Contains(body, `aria-label="What you sent"`) ||
			strings.Contains(body, `id="refused"`) {
			t.Errorf("back %s: %d, want the error view: %.400s", back, rec.Code, body)
		}
	}

	req := httptest.NewRequest("POST", action, strings.NewReader(url.Values{"body": {"my draft"}, "version": {version}, "back": {self}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/vnd.siren+json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/vnd.siren+json") ||
		!strings.Contains(rec.Body.String(), `"stale_version"`) || strings.Contains(rec.Body.String(), "refused") {
		t.Errorf("a JSON client's refusal: %d %s %.300s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
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
	root := must(c.Get(t.Context(), "/"))
	target := must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Revived target"}))
	must(c.Do(t.Context(), find(target, "tombstone"), nil))
	rec := submit(h, "/pages", url.Values{"title": {"Feedback page"}, "body": {"[[Revived target]] [[bad/name]]"}}, "")
	if rec.Code != 303 {
		t.Fatalf("POST: %d %s", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	body := browse(t, h, location)
	for _, want := range []string{"Result", `Revived <a href="/pages?title=Revived+target">Revived target</a>.`, "Not linked, not a valid title: <code>[[bad/name]]</code>."} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in redirected feedback", want)
		}
	}
	if strings.Contains(browse(t, h, location), "<h2>Result</h2>") {
		t.Error("receipt replayed")
	}
	page := must(c.Get(t.Context(), "/pages?title=Feedback%20page"))
	if !feedbackLocation(location, href(page, "self")) {
		t.Errorf("feedback URL must contain only the resource path and opaque receipt: %s", location)
	}
}

// TestAnEmptyPageSaysSoWithItsEditorClosed: a page with no text says so in words, as every empty view does, and its
// editor is closed under the sentence, so the page does not open as a large empty box with the focus in it.
func TestAnEmptyPageSaysSoWithItsEditorClosed(t *testing.T) {
	c, h := fresh(t)
	sam := must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/")), "create-person"), map[string]string{"title": "Sam"}))
	body := browse(t, h, hrefOf(sam, "self"))
	if !strings.Contains(body, `<p class="empty">Nothing written here yet.</p>`) {
		t.Error("an empty page does not say that nothing is written")
	}
	if !strings.Contains(body, `<details><summary>Edit</summary>`) || strings.Contains(body, `<details open><summary>Edit</summary>`) {
		t.Error("an empty page opens its editor")
	}
	if strings.Count(body, "autofocus") != 0 {
		t.Error("an empty page puts the focus in its editor")
	}
}
