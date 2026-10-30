package api_test

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// browse GETs a path as a browser does and fails unless the answer is a whole page with the status 200.
func browse(t *testing.T, h http.Handler, path string) string {
	t.Helper()
	return browseStatus(t, h, path, http.StatusOK)
}

// browseStatus is browse for an answer with another status: a refusal is a whole page too.
func browseStatus(t *testing.T, h http.Handler, path string, status int) string {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != status || !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Errorf("GET %s: %d, want %d and a whole page: %.300s", path, rec.Code, status, body)
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

// TestEveryViewRenders draws every view of a small life as a browser gets it: one table of paths, each a whole page
// with its status. Every page carries what semantic HTML needs: both of this app's sheets under their markers, one
// main with a way to skip to it, the sidebar's sections, one link at most marked in the whole sidebar (the brand, the
// sections and the owner's links), a footer, the search dialog, no script at all
// (docs/plans/108-the-browser-made-current.md), and no id generated for a template to find. The one exception is the
// id of a datalist (the form's id, or else the action's name, then the field's name), which the list of its input
// names: each list has its datalist on the page, and no id is repeated on a page. Lisbon is in a category, so the
// pages draw datalists. A row also names the words that its page shows. Which link of the sidebar a page marks is
// TestNavMarksThePageTheBrowserIsOn's.
func TestEveryViewRenders(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	day := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "capture"), map[string]string{"text": "Ran with [[Sam]] #running, `[[not a link]]`", "mood": "4"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), apitest.Href(day, "page"))), "save-body"), map[string]string{"body": "Ran with [[Sam]] and [[Ana]] #running"}))
	ana := apitest.Must(t)(c.Get(t.Context(), "/pages?title=Ana"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, ana, "promote"), map[string]string{"name": "Ana Popescu"}))
	sam := apitest.Must(t)(c.Get(t.Context(), "/pages?title=Sam"))
	lisbon := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Lisbon"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, lisbon, "file-pages"), map[string]string{"category": "Cities"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "walk"}))
	var reading string
	for _, d := range []string{"2026-09-01", "2026-09-15", "2026-09-28"} {
		m := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "weight", "day": d, "value": "71.2"}))
		reading = m.Links[0].Href
	}
	draft := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Draft", "body": "old words"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, draft, "rename"), map[string]string{"title": "Essay"}))
	gone := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Scratch"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, gone, "tombstone-pages"), nil))

	dayPage := apitest.Href(day, "page")
	bodies := map[string]string{}
	ids, lists := regexp.MustCompile(` id="([^"]*)"`), regexp.MustCompile(` list="([^"]*)"`)
	datalists := 0
	for _, row := range []struct {
		path   string
		status int
		want   []string
	}{
		{"/actions", http.StatusOK, nil},
		{"/actions?new=snapshot", http.StatusOK, []string{`<details class="action" open><summary class="button">Take a snapshot`}}, // the rail's Snapshot link
		{"/days", http.StatusOK, []string{`<article class="day-card" aria-label="`, `<article class="day-card slim" aria-label="2026-09-28">`,
			`<a href="/days" aria-current="page">Days</a>`, `</span>Ana</a>`,
			`<form class="tiny" method="GET" action="/pages/random" data-action="random-page"><input type="hidden" name="kind" value="day"><button class="quiet">A random day</button></form>`}},
		{apitest.Href(day, "self"), http.StatusOK, nil},
		{"/weeks/2026-W40", http.StatusOK, nil}, // a week draws its days and the form that makes its page: no id repeats
		{dayPage, http.StatusOK, []string{`name="version" value="`, `<a href="/pages?title=Sam" class="wikilink">Sam</a>`,
			`<a href="/pages?title=running" class="wikilink">#running</a>`}},
		{apitest.Href(sam, "self"), http.StatusOK, nil},
		{opens(t, h, "Ana"), http.StatusOK, []string{"Ana Popescu", "Backlinks", "mentioned in", `<div class="snippet">Ran with Sam and Ana #running</div>`}},
		{"/browse", http.StatusOK, []string{`placeholder="A page's title, or words of a body">`, `<span class="first">Essay</span>`}},
		{"/browse?kind=person", http.StatusOK, []string{`<form class="tiny" method="GET" action="/pages/random" data-action="random-page"><input type="hidden" name="kind" value="person"><button class="quiet">A random one</button></form>`}},
		{"/browse?deleted=1", http.StatusOK, []string{`<span class="first">Scratch</span>`}},
		{"/browse?ghosts=1", http.StatusOK, nil},
		{"/metrics", http.StatusOK, nil},
		{"/metrics/walk", http.StatusOK, nil},
		{"/metrics/weight", http.StatusOK, []string{`<svg class="chart"`, "71.2"}},
		{"/measurements/1", http.StatusOK, nil},
		{reading, http.StatusOK, []string{`action="/measurements/`, "Correct"}},
		{"/search", http.StatusOK, nil},
		{"/search?q=ran", http.StatusOK, []string{`<nav class="kinds chips" aria-label="Kind">`, `<a href="/search?q=ran" aria-current="page">All</a>`,
			`<section class="result-group" aria-label="Days"><div class="panel-head"><h2>Days</h2><a class="meta" href="/search?kind=day&amp;q=ran">Show only these</a>`,
			`<span class="hit-icon" aria-hidden="true">`}},
		{"/search?q=ran&kind=day", http.StatusOK, []string{`aria-current="page">Days</a>`, `aria-label="Pages by their words"`, `<input type="hidden" name="kind" value="day">`}},
		{"/integrity", http.StatusOK, []string{`action="/search/rebuild"`, `action="/links/resync"`}},
		{"/settings", http.StatusOK, []string{"<h1>Settings</h1>", `type="password"`}},
		{"/days/today/conversation", http.StatusOK, []string{"<h1>Chat</h1>", `/say"`, "Nothing is said on this day yet"}},
		{apitest.Href(gone, "self"), http.StatusOK, []string{"Deleted", `action="` + apitest.Href(gone, "self") + `/revive"`}},
		{"/years/2026", http.StatusOK, []string{`<a href="/years/2026" aria-current="page">Year</a>`}},
		{"/months/2026-09", http.StatusOK, []string{`<a href="/months/2026-09" aria-current="page">Month</a>`, `<a href="/days">Days</a>`}},
		{apitest.Href(draft, "self"), http.StatusOK, nil},
		{opens(t, h, "Essay"), http.StatusOK, nil},
		{"/pages/999999", http.StatusNotFound, nil},
	} {
		body := browseStatus(t, h, row.path, row.status)
		bodies[row.path] = body
		for _, want := range append([]string{
			`<style data-sheet="base">`, `<style data-sheet="app">`, // the two sheets, the base one first
			`<a class="skip" href="#main">`, `<main id="main">`, `<nav aria-label="Sections">`, "<footer>",
			`<dialog class="palette" id="palette" closedby="any" aria-label="Search">`,
		}, row.want...) {
			if !strings.Contains(body, want) {
				t.Errorf("%s does not carry %s", row.path, want)
			}
		}
		if strings.Contains(strings.ReplaceAll(body, chatScript, ""), "<script") {
			t.Errorf("%s carries a script besides chat.js", row.path)
		}
		if strings.Contains(body, `id="f-`) {
			t.Errorf("%s carries a generated id: a page is semantic HTML", row.path)
		}
		seen := map[string]bool{}
		for _, m := range ids.FindAllStringSubmatch(body, -1) {
			if seen[m[1]] {
				t.Errorf("%s carries the id %q twice", row.path, m[1])
			}
			seen[m[1]] = true
		}
		for _, m := range lists.FindAllStringSubmatch(body, -1) {
			if !strings.Contains(body, `<datalist id="`+m[1]+`">`) {
				t.Errorf("%s has an input with list=%q and no datalist of that id", row.path, m[1])
			}
			datalists++
		}
		rail, _, _ := strings.Cut(body[strings.Index(body, `<aside class="rail">`)+1:], "</aside>")
		if n := strings.Count(rail, `aria-current="page"`); n > 1 {
			t.Errorf("%s marks %d links of the sidebar current, want one at most", row.path, n)
		}
	}
	// Plain labels (docs/plans/129-the-ux-pass.md, part 4): the screen has no technical word where a plain one serves.
	for _, c := range []struct{ path, want, not string }{
		{apitest.Href(day, "self"), "<span>Entry</span>", "(CommonMark)"},
		{dayPage, "", "<dt>version</dt>"},
		{"/search", "<span>Words</span>", "required</small>"},
		{"/search", "How to search", "FTS5"},
		{"/search?q=ran", "<span>Words</span>", "required</small>"},
		{"/search?q=ran", "How to search", "FTS5"},
		{"/months/2026-09", "", "A day with a page is tinted"},
	} {
		if c.want != "" && !strings.Contains(bodies[c.path], c.want) {
			t.Errorf("%s lacks %q", c.path, c.want)
		}
		if strings.Contains(bodies[c.path], c.not) {
			t.Errorf("%s still carries %q", c.path, c.not)
		}
	}
	if strings.Contains(bodies[dayPage], `/promote"`) {
		t.Error("a day page shows a promote form the API does not offer")
	}
	if datalists == 0 {
		t.Error("no page draws a datalist: the check of list and datalist checks nothing")
	}
}

// opens is the path of the page that GET /pages?title= opens for the title: a 303 to the page.
func opens(t *testing.T, h http.Handler, title string) string {
	t.Helper()
	req := httptest.NewRequest("GET", "/pages?title="+url.QueryEscape(title), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("/pages?title=%s: %d", title, rec.Code)
	}
	return rec.Header().Get("Location")
}

// TestADestructiveActionDrawsADangerButton says the warning comes from the catalog, not from a template that
// knows an action's name: a tombstone is drawn as a danger, and the revive that undoes it is not.
func TestADestructiveActionDrawsADangerButton(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	p := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "create-page"), map[string]string{"title": "Draft"}))
	if body := browse(t, h, apitest.Href(p, "self")); !strings.Contains(body, `<button class="danger">Delete</button>`) {
		t.Errorf("a page that offers a tombstone draws no danger button: %.400s", body)
	}
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, p, "tombstone-pages"), nil))
	body := browse(t, h, apitest.Href(p, "self"))
	if !strings.Contains(body, "<button>Restore</button>") {
		t.Errorf("a deleted page offers no plain Restore: %.400s", body)
	}
	if strings.Contains(body, `<button class="danger">`) {
		t.Error("a deleted page draws a danger button for the revive that restores it")
	}
}

// TestAnEmptyPageSaysSoWithItsEditorClosed: a page with no text says so in words, as every empty view does, and its
// editor is closed under the sentence, so the page does not open as a large empty box with the focus in it.
func TestAnEmptyPageSaysSoWithItsEditorClosed(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	sam := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "create-person"), map[string]string{"title": "Sam"}))
	body := browse(t, h, apitest.Href(sam, "self"))
	if !strings.Contains(body, `<p class="empty">Nothing written here yet.</p>`) {
		t.Error("an empty page does not say that nothing is written")
	}
	if !strings.Contains(body, `<details class="body-editor"><summary>Edit</summary>`) || strings.Contains(body, `<details class="body-editor" open><summary>Edit</summary>`) {
		t.Error("an empty page opens its editor")
	}
	if strings.Count(body, "autofocus") != 0 {
		t.Error("an empty page puts the focus in its editor")
	}
}

// staleSave is a page with a save that is already stale: the version it names is not the current one.
func staleSave(t *testing.T) (h http.Handler, self, action, version string) {
	t.Helper()
	c, h, _ := apitest.Fresh(t, "cli")
	p := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "create-page"), map[string]string{"title": "Diet", "body": "v1"}))
	save := apitest.Action(t, p, "save-body")
	apitest.Must(t)(c.Do(t.Context(), save, map[string]string{"body": "v2"}))
	for _, f := range save.Fields {
		if f.Name == "version" {
			version = f.Value.(string)
		}
	}
	return h, apitest.Href(p, "self"), save.Href, version
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

// TestARefusedSaveAnswersWithTheErrorPage: a refused form is answered with the error page in the shell, with the
// error's status (docs/plans/131-the-pareto-pass.md, part 3): the alert, the text the form sent, which nothing has
// saved, and a link Back to the page it came from.
func TestARefusedSaveAnswersWithTheErrorPage(t *testing.T) {
	h, self, action, version := staleSave(t)
	rec := submit(h, action, url.Values{"body": {"my <long> draft"}, "version": {version}, "back": {self}}, self)
	body := rec.Body.String()
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409: %.400s", rec.Code, body)
	}
	for _, want := range []string{"<h1>Not saved</h1>", `class="rail"`, `role="alert"`, `aria-label="What you sent"`,
		"my &lt;long&gt; draft</textarea>", `<a href="` + self + `">Back</a>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the refused page lacks %s", want)
		}
	}
}

// goneClient is a client that went away: each write of a body fails.
type goneClient struct {
	header http.Header
	status int
}

func (g *goneClient) Header() http.Header       { return g.header }
func (g *goneClient) WriteHeader(status int)    { g.status = status }
func (g *goneClient) Write([]byte) (int, error) { return 0, errors.New("the client went away") }

// TestARefusedPageThatCannotBeSentIsLogged: when the client of a refused form went away, the write of the page it came
// from fails. The server's log has it one time, under the route of the POST that the client sent, not the route of the
// GET of its page; nothing else can be done.
func TestARefusedPageThatCannotBeSentIsLogged(t *testing.T) {
	h, self, action, version := staleSave(t)
	req := httptest.NewRequest("POST", action, strings.NewReader(url.Values{"body": {"my draft"}, "version": {version}, "back": {self}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged) // no test of this package runs in parallel
	t.Cleanup(func() { log.SetOutput(prev) })
	w := &goneClient{header: http.Header{}}
	h.ServeHTTP(w, req)
	if w.status != http.StatusConflict || !strings.HasPrefix(w.header.Get("Content-Type"), "text/html") {
		t.Errorf("status %d %q, want the refused page with 409", w.status, w.header.Get("Content-Type"))
	}
	if n := strings.Count(logged.String(), "the client went away"); n != 1 {
		t.Errorf("the log has the failed write %d times, want 1: %q", n, logged.String())
	}
	if !strings.Contains(logged.String(), "lifelog: POST /pages/{id}/body: ") {
		t.Errorf("the log does not name the route of the POST: %q", logged.String())
	}
}

// TestAnEmptySaveIsRefused: the browser marks the body required, but a client that does not check the form (or an old
// browser) can still send an empty body. The save is refused 422 with the alert, and the page keeps its body and its
// version.
func TestAnEmptySaveIsRefused(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	p := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "create-page"), map[string]string{"title": "Diet", "body": "v1"}))
	save := apitest.Action(t, p, "save-body")
	var version string
	for _, f := range save.Fields {
		if f.Name == "version" {
			version, _ = f.Value.(string)
		}
	}
	rec := submit(h, save.Href, url.Values{"body": {""}, "version": {version}, "back": {apitest.Href(p, "self")}}, apitest.Href(p, "self"))
	body := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(body, `role="alert"`) || !strings.Contains(body, "missing field body") {
		t.Errorf("an empty browser save: %d %.400s", rec.Code, body)
	}
	after := apitest.Must(t)(c.Get(t.Context(), apitest.Href(p, "self"))).Properties.(map[string]any)
	if after["body"] != "v1" || after["version"] != version {
		t.Errorf("after an empty save the page has body %q and version %q, want %q and %q", after["body"], after["version"], "v1", version)
	}
}

// TestARefusedCaptureKeepsItsText: a capture refused from the page of today (Home) is answered with the error page,
// which keeps the text, so nothing is lost, and links Back to the page.
func TestARefusedCaptureKeepsItsText(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", "2026-10-12")
	rec := submit(h, "/days/2026-10-12/capture", url.Values{"text": {"A note that must not be lost."}, "mood": {"9"}, "back": {"/"}}, "/")
	body := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(body, `aria-label="What you sent"`) ||
		!strings.Contains(body, "A note that must not be lost.") || !strings.Contains(body, `<a href="/">Back</a>`) {
		t.Errorf("a refused capture: %d, want the error view that keeps the text: %.400s", rec.Code, body)
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
