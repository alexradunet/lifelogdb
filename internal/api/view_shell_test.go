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

// TestTheSidebarShowsTheMonth: the month is this month, with the days that have a day page marked and today framed.
func TestTheSidebarShowsTheMonth(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday) // Wednesday 14 May 2031
	if body := browse(t, h, "/days/"+fixedToday); !strings.Contains(body, "May 2031</a><span class=\"meta\">0 written</span>") {
		t.Errorf("an empty life log does not show its empty month:\n%s", sidebar(body))
	}

	for _, day := range []string{"2031-04-30", dayOf(-2), fixedToday} {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/"+day)), "capture"), map[string]string{"text": "Synthetic entry"}))
	}
	// A mood alone makes the day page with an empty body: a day page with an empty body is still written.
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/2031-05-03")), "capture"), map[string]string{"mood": "3"}))
	if body := apitest.Must(t)(c.Get(t.Context(), "/pages?title=2031-05-03")).Properties.(map[string]any)["body"]; body != "" {
		t.Fatalf("the day page of 2031-05-03 has the body %q, want an empty body", body)
	}

	body := browse(t, h, "/days/"+fixedToday)
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
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
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

// TestThePhoneHasABottomBarAndASheet: the sections of every day (Today, Calendar, Metrics, Browse) and More are one
// list (a phone's bottom bar); the month and the owner's links are the popover sheet that More opens and Close hides
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
	if got := links(bar[1]); got != "/days/today /months/current /metrics /browse popovertarget=\"sections\"" {
		t.Errorf("the bottom bar: %s", got)
	}
	if !strings.Contains(bar[1], `<a href="/browse" aria-current="page">`) || strings.Contains(bar[1], `class="here"`) {
		t.Errorf("the bottom bar does not mark Browse, or marks More:\n%s", bar[1])
	}
	if got := links(sheet[1]); !strings.HasPrefix(got, "/months/2031-05") || strings.Contains(got, "/browse") || strings.Contains(got, "/metrics") ||
		!strings.HasSuffix(got, "/actions /integrity /actions?new=snapshot") || !strings.Contains(sheet[1], `<a href="/actions?new=snapshot">Snapshot</a>`) {
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
		`<input type="search" name="q" placeholder="Search" aria-label="Search" accesskey="k">`,
		`<button type="button" class="button primary rail-new" popovertarget="new-menu" accesskey="n">`,
		`<a href="/days/today" aria-current="page" accesskey="t">`,
		`placeholder="What happened? Who was there?" accesskey="c" autofocus>`,
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
	entry := regexp.MustCompile(`<a href="([^"]+)"( aria-current="page")?[^>]*><svg class="icon"[^>]*>.*?</svg>([^<]+)</a>`)
	for path, current := range map[string]string{
		"/browse": "Browse", "/browse?kind=person": "Browse", "/browse?ghosts=1": "Browse", "/metrics": "Metrics",
		"/metrics/Weight": "Metrics", "/days/" + fixedToday: "Today", "/months/2031-05": "Calendar", "/search": "",
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
		if want := "Today=/days/today Calendar=/months/current Metrics=/metrics Browse=/browse"; strings.Join(got, " ") != want {
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
