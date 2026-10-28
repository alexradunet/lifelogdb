package api_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
)

// TestTheSidebarDrawsNoMonth: this month is on Calendar, so the sidebar and the phone's sheet do not draw it a
// second time.
func TestTheSidebarDrawsNoMonth(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday) // Wednesday 14 May 2031
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday)), "capture"), map[string]string{"text": "Synthetic entry"}))
	for _, path := range []string{"/days/" + fixedToday, "/browse", "/integrity"} {
		rail := sidebar(browse(t, h, path))
		if rail == "" {
			t.Fatalf("%s: no sidebar", path)
		}
		for _, bad := range []string{`class="month-box"`, `href="/months/`, `href="/days/2031-05-`} {
			if strings.Contains(rail, bad) {
				t.Errorf("%s: the sidebar draws the month (%s):\n%s", path, bad, rail)
			}
		}
	}
}

// sidebar is the sidebar of a page, for a failure message.
func sidebar(body string) string {
	return regexp.MustCompile(`(?s)<aside class="rail">.*?</aside>`).FindString(body)
}

// TestTheSidebarMarksTodayOnTodaysDay: today's day is Today; any other day is in the Calendar.
func TestTheSidebarMarksTodayOnTodaysDay(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	if body := browse(t, h, "/days/"+fixedToday); !strings.Contains(body, `<a href="/days/today" aria-current="page" accesskey="t">`) {
		t.Errorf("today's day does not mark Today:\n%s", sidebar(body))
	}
	if body := browse(t, h, "/days/"+dayOf(-1)); !strings.Contains(body, `<a href="/days" aria-current="page">`) {
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

// TestThePhoneHasABottomBarAndASheet: the sections of every day (Today, Calendar, Metrics, Browse) and More are one
// list (a phone's bottom bar); the owner's links are the popover sheet that More opens and Close hides
// (#sections), with no script; each link is drawn once, and More is marked when the page is in the sheet.
func TestThePhoneHasABottomBarAndASheet(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
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
	if got := links(bar[1]); got != "/days/today /weeks/today /plans /days /metrics /browse /days/today/conversation popovertarget=\"sections\"" {
		t.Errorf("the bottom bar: %s", got)
	}
	if !strings.Contains(bar[1], `<a href="/browse" aria-current="page">`) || strings.Contains(bar[1], `class="here"`) {
		t.Errorf("the bottom bar does not mark Browse, or marks More:\n%s", bar[1])
	}
	if got := links(sheet[1]); got != "/actions /integrity /settings /actions?new=snapshot" || !strings.Contains(sheet[1], `<a href="/actions?new=snapshot">Snapshot</a>`) {
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

// The browser without a script (docs/plans/108-the-browser-made-current.md): what app.js once did, a page does by
// the browser's own means. The search dialog and the keys are two of these; the next two tests hold the HTML that
// asks for them.

// TestTheSearchDialogIsInThePage: every page of the shell carries the search dialog once, a modal with the search
// form inside, which the top bar's Search opens by its command and Escape or a tap outside closes (closedby). The
// login page, which has no shell, carries none (the public listener's test says so).
func TestTheSearchDialogIsInThePage(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	body := browse(t, h, "/days/"+fixedToday) // Home is the page of today
	for _, want := range []string{
		`<dialog class="palette" id="palette" closedby="any" aria-label="Search">` + "\n" + `<form method="GET" action="/search"><input type="search" name="q" autocomplete="off" aria-label="Search" placeholder="A page's title, or words of a body"></form>`,
		`<button type="button" class="rail-search-link button" commandfor="palette" command="show-modal" aria-label="Search">`,
	} {
		if strings.Count(body, want) != 1 {
			t.Errorf("Home carries %s %d times, want once", want, strings.Count(body, want))
		}
	}
}

// TestTheKeysAreInThePage: the keys are the browser's access keys, on the search, New, Today, the capture and the
// previous and next page, and the footer says so; there is no script to take any other key.
func TestTheKeysAreInThePage(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	day := browse(t, h, "/days/"+fixedToday)
	for _, want := range []string{
		`<button type="button" class="rail-search button" commandfor="palette" command="show-modal" accesskey="k">`,
		`<button type="button" class="button primary rail-new" popovertarget="new-menu" accesskey="n">`,
		`<a href="/days/today" aria-current="page" accesskey="t">`,
		`placeholder="What happened? Who was there?" accesskey="c">`,
		`<a class="button step" accesskey="[" href="/days/2031-05-13" rel="prev"`, `<a class="button step" accesskey="]" href="/days/2031-05-15" rel="next"`,
		`<p class="keys"><small>Access keys, with the browser's modifier: <kbd>k</kbd> search · <kbd>n</kbd> new · <kbd>c</kbd> capture · <kbd>t</kbd> today · <kbd>[</kbd> <kbd>]</kbd> previous, next</small></p>`,
	} {
		if !strings.Contains(day, want) {
			t.Errorf("the day lacks %s", want)
		}
	}
}

// TestTheSidebarHasMetricsAndBrowse: the sections of every day are Today, Calendar, Metrics and Browse, in the rail
// of a desktop and in the bottom bar of a phone (one list, which the sheet does not repeat); there is no group of
// the record. Browse is marked on every list of Browse, Metrics on the board and under it.
func TestTheSidebarHasMetricsAndBrowse(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "register-metric"), map[string]string{"name": "Weight", "unit": "kg"}))
	sections := regexp.MustCompile(`(?s)<nav aria-label="Sections">(.*?)</nav>`)
	entry := regexp.MustCompile(`<a href="([^"]+)"( aria-current="page")?[^>]*><span class="icon icon-[a-z]+" aria-hidden="true"></span>([^<]+)</a>`)
	for path, current := range map[string]string{
		"/browse": "Browse", "/browse?kind=person": "Browse", "/browse?ghosts=1": "Browse", "/metrics": "Metrics",
		"/metrics/Weight": "Metrics", "/days/" + fixedToday: "Today", "/months/2031-05": "Calendar", "/search": "", "/plans": "Plans", "/plans?order=page": "Plans",
	} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Accept", "text/html")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		body := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", path, rec.Code)
		}
		bar := sections.FindStringSubmatch(body)
		if bar == nil {
			t.Errorf("%s: no sections:\n%s", path, sidebar(body))
			continue
		}
		var got, marked []string
		for _, m := range entry.FindAllStringSubmatch(bar[1], -1) {
			got = append(got, m[3]+"="+m[1])
			if m[2] != "" {
				marked = append(marked, m[3])
			}
		}
		if want := "Today=/days/today This week=/weeks/today Plans=/plans Calendar=/days Metrics=/metrics Browse=/browse Chat=/days/today/conversation"; strings.Join(got, " ") != want {
			t.Errorf("%s: the sections are %v, want %s", path, got, want)
		}
		if strings.Join(marked, " ") != current {
			t.Errorf("%s marks %v, want %q", path, marked, current)
		}
		if strings.Contains(body, `aria-label="The record"`) || strings.Count(sidebar(body), `href="/browse"`) != 1 {
			t.Errorf("%s: the rail has a group of the record, or a second link to Browse", path)
		}
	}
}
