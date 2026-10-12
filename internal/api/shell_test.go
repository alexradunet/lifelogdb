package api_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api"
)

// TestTheSidebarCountsWhatIsDueAndShowsTheMonth: the Due count is the open occurrences overdue (in the 30 days
// before today) or due today, as the root's due lists them; a done, a skipped, a later, an older and a tombstoned
// one are not counted. The month is this month, with the days that have a day page marked and today framed.
func TestTheSidebarCountsWhatIsDueAndShowsTheMonth(t *testing.T) {
	c, h := freshOn(t, fixedToday) // Wednesday 14 May 2031
	if body := browse(t, h, "/"); strings.Contains(body, `class="count"`) || !strings.Contains(body, "May 2031</a><span class=\"meta\">0 written</span>") {
		t.Errorf("an empty life log shows a count, or not its empty month:\n%s", sidebar(body))
	}

	root := must(c.Get("/"))
	create := func(v map[string]string) string {
		e := must(c.Do(find(root, "create-task"), v))
		return strings.SplitN(hrefOf(e, "self"), "?", 2)[0]
	}
	outcome := func(task string, values map[string]string) {
		occ := must(c.Get(task + "/occurrences/once"))
		must(c.Do(find(occ, "edit-occurrence"), values))
	}
	create(map[string]string{"label": "Book the dentist", "due_day": dayOf(-3)})                                                // counted
	create(map[string]string{"label": "Water the plants", "repeat_unit": "day", "repeat_every": "7", "anchor_day": dayOf(-14)}) // -14, -7 and 0: three
	create(map[string]string{"label": "Renew the passport", "due_day": dayOf(2)})                                               // later
	create(map[string]string{"label": "Pay the old bill", "due_day": dayOf(-40)})                                               // before the window
	outcome(create(map[string]string{"label": "Call Dana Example", "due_day": dayOf(-1)}), map[string]string{"state": "done", "completed_at": "now"})
	outcome(create(map[string]string{"label": "Clean the gutter", "due_day": dayOf(-2)}), map[string]string{"state": "skipped"})
	gone := create(map[string]string{"label": "Old plan", "due_day": dayOf(-4)})
	must(c.Do(find(must(c.Get(gone)), "tombstone-task"), nil))

	for _, day := range []string{"2031-04-30", "2031-05-03", dayOf(-2), fixedToday} {
		must(c.Do(find(must(c.Get("/days/"+day)), "capture"), map[string]string{"text": "Synthetic entry"}))
	}
	page := must(c.Get("/pages?title=2031-05-03"))
	must(c.Do(find(page, "save-body"), map[string]string{"body": ""})) // a day page with an empty body is still written

	body := browse(t, h, "/")
	if n := len(props(must(c.Get("/")))["due"].([]any)); n != 4 {
		t.Fatalf("the root lists %d due, want 4", n)
	}
	for _, want := range []string{
		`<span class="count" title="Open, overdue or due today">4</span>`,
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
	if body := browse(t, h, "/days/"+fixedToday); !strings.Contains(body, `<a href="/days/today" aria-current="page">`) {
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

// TestThePhoneHasABottomBarAndASheet: the sections of every day and More are one list (a phone's bottom bar); the
// sections of the record, the month and the owner's links are the sheet that More opens (#sections), with no script;
// each link is drawn once, and More is marked when the page is in the sheet.
func TestThePhoneHasABottomBarAndASheet(t *testing.T) {
	_, h := freshOn(t, fixedToday)
	body := browse(t, h, "/people")
	bar := regexp.MustCompile(`(?s)<nav aria-label="Sections">(.*?)</nav>`).FindStringSubmatch(body)
	sheet := regexp.MustCompile(`(?s)<div class="rail-more" id="sections">(.*?)</aside>`).FindStringSubmatch(body)
	if bar == nil || sheet == nil {
		t.Fatalf("no bottom bar or no sheet:\n%s", sidebar(body))
	}
	links := func(s string) string {
		var out []string
		for _, m := range regexp.MustCompile(`<a href="([^"#]+|#sections)"`).FindAllStringSubmatch(s, -1) {
			out = append(out, m[1])
		}
		return strings.Join(out, " ")
	}
	if got := links(bar[1]); got != "/days/today /months/current /deadlines /tasks #sections" {
		t.Errorf("the bottom bar: %s", got)
	}
	if got := links(sheet[1]); !strings.HasPrefix(got, "/people /places /files /metrics /habits /sessions /periods /ghosts /months/2031-05") ||
		!strings.HasSuffix(got, "/actions /integrity") || !strings.Contains(sheet[1], `<a href="/#owner">Snapshot</a>`) {
		t.Errorf("the sheet: %s", got)
	}
	if !strings.Contains(sheet[1], `<a class="button small" href="#closed">Close</a>`) {
		t.Error("the sheet has no Close")
	}
	if !strings.Contains(bar[1], `<a href="#sections" class="here">`) {
		t.Error("More is not marked on a page of the sheet")
	}
	if strings.Contains(browse(t, h, "/days/"+fixedToday), `<a href="#sections" class="here">`) {
		t.Error("More is marked on Today")
	}
	if !strings.Contains(body, `content="width=device-width, initial-scale=1, viewport-fit=cover"`) {
		t.Error("the page does not reach the phone's edges (viewport-fit)")
	}
}
