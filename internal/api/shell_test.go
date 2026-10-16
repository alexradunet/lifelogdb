package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/db/dbtest"
)

// TestTheSidebarShowsTheMonth: the month is this month, with the days that have a day page marked and today framed.
func TestTheSidebarShowsTheMonth(t *testing.T) {
	c, h := freshOn(t, fixedToday) // Wednesday 14 May 2031
	if body := browse(t, h, "/"); !strings.Contains(body, "May 2031</a><span class=\"meta\">0 written</span>") {
		t.Errorf("an empty life log does not show its empty month:\n%s", sidebar(body))
	}

	for _, day := range []string{"2031-04-30", dayOf(-2), fixedToday} {
		must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/days/"+day)), "capture"), map[string]string{"text": "Synthetic entry"}))
	}
	// A mood alone makes the day page with an empty body: a day page with an empty body is still written.
	must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/days/2031-05-03")), "capture"), map[string]string{"mood": "3"}))
	if body := must(c.Get(t.Context(), "/pages?title=2031-05-03")).Properties.(map[string]any)["body"]; body != "" {
		t.Fatalf("the day page of 2031-05-03 has the body %q, want an empty body", body)
	}

	body := browse(t, h, "/")
	for _, want := range []string{
		`<a href="/months/2031-05">May 2031</a><span class="meta">3 written</span>`,
		`<tr><td></td><td></td><td></td><td><a href="/days/2031-05-01" aria-label="Thu 1 May">1</a></td>`,
		`<a href="/days/2031-05-03" class="written" aria-label="Sat 3 May, written">3</a>`,
		`<a href="/days/2031-05-12" class="written" aria-label="Mon 12 May, written">12</a>`,
		`<a href="/days/2031-05-13" aria-label="Tue 13 May">13</a>`,
		`<a href="/days/2031-05-14" class="written" aria-current="date" aria-label="Wed 14 May, written">14</a>`,
		`<td><a href="/days/2031-05-31" aria-label="Sat 31 May">31</a></td><td></td></tr>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the sidebar lacks %s:\n%s", want, sidebar(body))
		}
	}
	if strings.Contains(sidebar(body), "2031-04-30") {
		t.Error("the sidebar's month shows a day of another month")
	}
}

// sidebar is the sidebar of a page, for a failure message.
func sidebar(body string) string {
	return regexp.MustCompile(`(?s)<aside class="rail">.*?</aside>`).FindString(body)
}

// TestTheSidebarMarksTodayOnTodaysDay: today's day is Today; any other day is in the Calendar.
func TestTheSidebarMarksTodayOnTodaysDay(t *testing.T) {
	_, h := freshOn(t, fixedToday)
	if body := browse(t, h, "/days/"+fixedToday); !strings.Contains(body, `<a href="/days/today" aria-current="page" accesskey="t">`) {
		t.Errorf("today's day does not mark Today:\n%s", sidebar(body))
	}
	if body := browse(t, h, "/days/"+dayOf(-1)); !strings.Contains(body, `<a href="/months/current" aria-current="page">`) {
		t.Errorf("another day does not mark Calendar:\n%s", sidebar(body))
	}
}

// TestThePageOfNoShellHasNoSidebar: the login page shows nothing of the database, so it has no sidebar.
func TestThePageOfNoShellHasNoSidebar(t *testing.T) {
	h, _ := publicHandler(t, api.PublicOptions{Token: testToken})
	body := request(h, "GET", "/login", "", map[string]string{"Accept": "text/html"}).Body.String()
	if !strings.Contains(body, `<div class="shell bare">`) || sidebar(body) != "" {
		t.Errorf("the login page has a sidebar:\n%s", sidebar(body))
	}
}

// TestTheFontsAreServedByExactName: every face base.css names is served as a font that a browser keeps; the
// licences are text; any other name is 404, and the route is a GET (HEAD too) and nothing else.
func TestTheFontsAreServedByExactName(t *testing.T) {
	_, h := fresh(t)
	get := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec
	}
	urls := regexp.MustCompile(`url\((/assets/fonts/[^)]+)\)`).FindAllStringSubmatch(page(t, h, "/"), -1)
	if len(urls) != 12 {
		t.Fatalf("base.css names %d font files, want 12 (6 faces, latin and latin-ext)", len(urls))
	}
	for _, u := range urls {
		rec := get("GET", u[1])
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "font/woff2" || !strings.HasPrefix(rec.Body.String(), "wOF2") ||
			rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("GET %s: %d %q %q", u[1], rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("Cache-Control"))
		}
	}
	for _, licence := range []string{"OFL-atkinson-hyperlegible.txt", "OFL-ibm-plex-mono.txt", "OFL-jersey-15.txt"} {
		if rec := get("GET", "/assets/fonts/"+licence); rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") ||
			!strings.Contains(rec.Body.String(), "SIL Open Font License, Version 1.1") {
			t.Errorf("the licence %s: %d %q", licence, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	for _, path := range []string{"/assets/fonts/unknown.woff2", "/assets/fonts/README.md", "/assets/fonts/..%2fbase.css",
		"/assets/fonts/%2e%2e%2fbase.css", "/assets/fonts/", "/assets/fonts/a%2Fb.woff2", "/assets/fonts/woff2", "/assets/base.css"} {
		if rec := get("GET", path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d, want 404", path, rec.Code)
		}
	}
	if rec := get("HEAD", urls[0][1]); rec.Code != http.StatusOK || rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "font/woff2" {
		t.Errorf("HEAD: %d, %d bytes", rec.Code, rec.Body.Len())
	}
	if rec := get("POST", urls[0][1]); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d, want 405", rec.Code)
	}

	pub, _ := publicHandler(t, api.PublicOptions{Token: testToken})
	if rec := request(pub, "GET", urls[0][1], "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("a public listener serves a font with no token: %d", rec.Code)
	}
	if rec := request(pub, "GET", urls[0][1], "", map[string]string{"Authorization": "Bearer " + testToken}); rec.Code != http.StatusOK {
		t.Errorf("a public listener refuses a font with the token: %d", rec.Code)
	}
}

// TestThePhoneHasABottomBarAndASheet: the sections of every day (Today, Calendar, Metrics, Browse) and More are one
// list (a phone's bottom bar); the month and the owner's links are the popover sheet that More opens and Close hides
// (#sections), with no script; each link is drawn once, and More is marked when the page is in the sheet.
func TestThePhoneHasABottomBarAndASheet(t *testing.T) {
	_, h := freshOn(t, fixedToday)
	body := browse(t, h, "/browse?kind=person")
	bar := regexp.MustCompile(`(?s)<nav aria-label="Sections">(.*?)</nav>`).FindStringSubmatch(body)
	sheet := regexp.MustCompile(`(?s)<div class="rail-more" id="sections" popover>(.*?)</aside>`).FindStringSubmatch(body)
	if bar == nil || sheet == nil {
		t.Fatalf("no bottom bar or no sheet:\n%s", sidebar(body))
	}
	links := func(s string) string {
		var out []string
		for _, m := range regexp.MustCompile(`<a href="([^"#]+)"|<button type="button" (popovertarget="sections")`).FindAllStringSubmatch(s, -1) {
			out = append(out, m[1]+m[2])
		}
		return strings.Join(out, " ")
	}
	if got := links(bar[1]); got != "/days/today /months/current /metrics /browse popovertarget=\"sections\"" {
		t.Errorf("the bottom bar: %s", got)
	}
	if !strings.Contains(bar[1], `<a href="/browse" aria-current="page">`) || strings.Contains(bar[1], `class="here"`) {
		t.Errorf("the bottom bar does not mark Browse, or marks More:\n%s", bar[1])
	}
	if got := links(sheet[1]); !strings.HasPrefix(got, "/months/2031-05") || strings.Contains(got, "/browse") || strings.Contains(got, "/metrics") ||
		!strings.HasSuffix(got, "/actions /integrity") || !strings.Contains(sheet[1], `<a href="/#owner">Snapshot</a>`) {
		t.Errorf("the sheet: %s", got)
	}
	if !strings.Contains(sheet[1], `<button type="button" class="button small" popovertarget="sections" popovertargetaction="hide">Close</button>`) {
		t.Error("the sheet has no Close")
	}
	if !strings.Contains(browse(t, h, "/integrity"), `<button type="button" popovertarget="sections" class="here">`) {
		t.Error("More is not marked on a page of the sheet")
	}
	if strings.Contains(browse(t, h, "/days/"+fixedToday), `popovertarget="sections" class="here">`) {
		t.Error("More is marked on Today")
	}
	if !strings.Contains(body, `content="width=device-width, initial-scale=1, viewport-fit=cover"`) {
		t.Error("the page does not reach the phone's edges (viewport-fit)")
	}
}

// TestTheStylePageReadsNothing: the style page is a whole page, it is not in the catalog, and it reads nothing from
// the database: with the store closed the shell's read fails and the page is still drawn. A public listener refuses
// it without the token. What it shows is TestTheStylePageRunsTheRealTemplates.
func TestTheStylePageReadsNothing(t *testing.T) {
	d, err := db.Open(t.Context(), dbtest.File(t))
	if err != nil {
		t.Fatal(err)
	}
	h := api.New(&core.Store{DB: d})
	if err := d.Close(); err != nil { // every query of the store now fails
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/styleguide", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Fatalf("GET /styleguide with the store closed: %d %q %.300s", rec.Code, rec.Header().Get("Content-Type"), body)
	}
	for _, id := range []string{`id="refused"`, `role="alert"`} {
		if strings.Contains(body, id) {
			t.Errorf("the style page carries %s: a second live region for the refused alert", id)
		}
	}

	c, live := fresh(t)
	catalog, err := c.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range catalog {
		if strings.Contains(a.Href, "styleguide") {
			t.Errorf("the catalog names the style page: %s", a.Name)
		}
	}
	if rec := request(live, "GET", "/styleguide", "", map[string]string{"Accept": "text/html"}); rec.Code != http.StatusOK {
		t.Errorf("GET /styleguide on an open store: %d", rec.Code)
	}
	if rec := request(live, "POST", "/styleguide", "", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /styleguide: %d, want 405", rec.Code)
	}

	pub, _ := publicHandler(t, api.PublicOptions{Token: testToken})
	if rec := request(pub, "GET", "/styleguide", "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("a public listener serves the style page with no token: %d", rec.Code)
	}
	if rec := request(pub, "GET", "/styleguide", "", map[string]string{"Authorization": "Bearer " + testToken, "Accept": "text/html"}); rec.Code != http.StatusOK {
		t.Errorf("a public listener refuses the style page with the token: %d", rec.Code)
	}
}

// TestTheManifestAndTheIconAreServed: a browser can install the app. The manifest and the icon have their types
// and a cache; a public listener refuses both without the token; the manifest's share target posts to a route that
// writes on the local day.
func TestTheManifestAndTheIconAreServed(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	get := func(h http.Handler, method, path string, headers map[string]string) *httptest.ResponseRecorder {
		return request(h, method, path, "", headers)
	}
	rec := get(h, "GET", "/manifest.webmanifest", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/manifest+json" ||
		rec.Header().Get("Cache-Control") != "public, max-age=3600" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("the manifest: %d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("Cache-Control"))
	}
	var m struct {
		Name        string `json:"name"`
		StartURL    string `json:"start_url"`
		Display     string `json:"display"`
		ThemeColor  string `json:"theme_color"`
		Icons       []struct{ Src, Purpose string }
		Shortcuts   []struct{ Name, URL string }
		ShareTarget struct {
			Action, Method, Enctype string
			Params                  struct {
				Text  string
				Files []struct {
					Name   string
					Accept []string
				}
			}
		} `json:"share_target"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("the manifest is not JSON: %v", err)
	}
	if m.Name != "Lifelog" || m.StartURL != "/" || m.Display != "standalone" || m.ThemeColor != "#3a2210" || len(m.Shortcuts) != 2 || len(m.Icons) != 2 ||
		m.ShareTarget.Action != "/days/today/capture" || m.ShareTarget.Method != "POST" || m.ShareTarget.Enctype != "multipart/form-data" ||
		m.ShareTarget.Params.Text != "text" || len(m.ShareTarget.Params.Files) != 0 { // a phone's share sheet sends text to capture
		t.Errorf("the manifest says %+v", m)
	}
	for _, i := range m.Icons {
		if i.Src != "/assets/icon.svg" {
			t.Errorf("the manifest names the icon %q", i.Src)
		}
	}

	icon := get(h, "GET", "/assets/icon.svg", nil)
	if icon.Code != http.StatusOK || icon.Header().Get("Content-Type") != "image/svg+xml" ||
		icon.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || icon.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("the icon: %d %q %q", icon.Code, icon.Header().Get("Content-Type"), icon.Header().Get("Cache-Control"))
	}
	if b := icon.Body.String(); !strings.HasPrefix(b, "<svg ") || strings.Contains(b, "<script") || strings.Contains(b, "<text") || len(b) > 1024 {
		t.Errorf("the icon is not a small SVG with no text and no script: %d bytes", len(b))
	}
	for _, path := range []string{"/manifest.webmanifest", "/assets/icon.svg"} {
		if r := get(h, "HEAD", path, nil); r.Code != http.StatusOK || r.Body.Len() != 0 {
			t.Errorf("HEAD %s: %d, %d bytes", path, r.Code, r.Body.Len())
		}
		if r := get(h, "POST", path, nil); r.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s: %d, want 405", path, r.Code)
		}
	}

	pub, _ := publicHandler(t, api.PublicOptions{Token: testToken})
	for _, path := range []string{"/manifest.webmanifest", "/assets/icon.svg"} {
		if r := get(pub, "GET", path, nil); r.Code != http.StatusUnauthorized {
			t.Errorf("a public listener serves %s with no token: %d", path, r.Code)
		}
		if r := get(pub, "GET", path, map[string]string{"Authorization": "Bearer " + testToken}); r.Code != http.StatusOK {
			t.Errorf("a public listener refuses %s with the token: %d", path, r.Code)
		}
	}

	// The share target: a POST of text to /days/today/capture writes on the local day.
	r := submit(h, "/days/today/capture", url.Values{"text": {"Shared from a phone"}}, "")
	if r.Code != http.StatusSeeOther {
		t.Fatalf("POST /days/today/capture: %d %.200s", r.Code, r.Body.String())
	}
	day := must(c.Get(t.Context(), "/days/"+fixedToday))
	if href(day, "page") == "" || !strings.Contains(browse(t, h, "/days/"+fixedToday), "Shared from a phone") {
		t.Errorf("the shared text is not on %s", fixedToday)
	}
	if page := must(c.Get(t.Context(), href(day, "page"))); !strings.Contains(props(page)["body"].(string), "Shared from a phone") {
		t.Errorf("the day page does not hold the shared text: %v", props(page)["body"])
	}
}
