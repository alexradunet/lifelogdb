package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// The browser's plan lines (docs/plans/136-plan-lines-and-period-pages.md, step 5). Today is Wednesday 2031-05-14,
// in ISO week 2031-W20.

func TestThePlannedBlockOfADayHoldsTheLinesOfOtherPages(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	do := func(fields map[string]string) (string, string) {
		res, _ := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "plan"), fields)).Result.(map[string]any)
		return fmt.Sprint(res["page_id"]), fmt.Sprint(res["line"])
	}
	bank, line := do(map[string]string{"page": "Bank", "text": "Call the bank", "due": "2031-05-14"})
	do(map[string]string{"page": "Bank", "text": "Pay rent", "on": "2031-05-10"})

	page := browse(t, h, "/days/"+fixedToday)
	for _, want := range []string{
		`<section class="group plan" aria-label="Planned">`, `<h3>Today</h3>`, `<h3>Earlier</h3>`,
		`<span class="text">Call the bank</span>`, `<a href="/pages/` + bank + `">Bank</a>`, `<span class="badge">due Wed 14 May</span>`,
		`<form class="tick-line" method="post" action="/pages/` + bank + `/tick"><input type="hidden" name="back" value="/days/` + fixedToday + `">` +
			`<input type="hidden" name="line" value="` + line + `"><input type="hidden" name="text" value="Call the bank">` +
			`<button name="outcome" value="done" class="small">Done</button><button name="outcome" value="dropped" class="small quiet">Dropped</button></form>`,
		`action="/pages/` + bank + `/carry"`, `<input type="hidden" name="text" value="Call the bank">`, `name="to" value="2031-05-15"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the day does not carry %s", want)
		}
	}
	if strings.Contains(page, "Nothing planned") {
		t.Error("a day with lines says that nothing is planned")
	}
	if other := browse(t, h, "/days/2031-05-13"); !strings.Contains(other, "<h2>Planned that day</h2>") || !strings.Contains(other, `<p class="empty">Nothing planned for this day.</p>`) {
		t.Errorf("a past day with no line:\n%s", mainOf(other))
	}

	// Done from the Planned block: the body then reads as done, with no form for the line.
	rec := submit(h, "/pages/"+bank+"/tick", url.Values{"line": {line}, "text": {"Call the bank"}, "outcome": {"done"}, "back": {"/"}}, "/")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("tick: %d %s", rec.Code, rec.Body.String())
	}
	body := browse(t, h, "/pages/"+bank)
	if !strings.Contains(body, `<span class="box done" role="img" aria-label="done">`) || !strings.Contains(body, `<small class="token">done:2031-05-14</small>`) {
		t.Errorf("the ticked line is not drawn as done:\n%s", mainOf(body))
	}
	if strings.Contains(body, `form="tick-`+line+`"`) {
		t.Error("a done line still draws a tick button")
	}
	// A line that changed since the page was read is a refusal, and it names the cause.
	if rec := submit(h, "/pages/"+bank+"/tick", url.Values{"line": {line}, "text": {"Call the bank"}, "outcome": {"done"}}, "/"); rec.Code != http.StatusConflict {
		t.Errorf("tick of a closed line: %d", rec.Code)
	}
}

func TestAnOpenLineInABodyTicksAndThePreviewDoesNot(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	made := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Errands",
		"body": "Before.\n\n- [ ] Buy <b>stamps</b> on:2031-05-15 every:week\n- [x] Post it done:2031-05-13\n- [-] Skip\n- [>] Moved\n- [ ]\n"}))
	id := strings.TrimPrefix(apitest.Href(made, "self"), "/pages/")
	body := browse(t, h, "/pages/"+id)
	for _, want := range []string{
		`<button class="box open" form="tick-3" name="outcome" value="done" aria-label="Done: Buy &lt;b&gt;stamps&lt;/b&gt;">`,
		`<form id="tick-3" method="post" action="/pages/` + id + `/tick"><input type="hidden" name="back" value="/pages/` + id + `">` +
			`<input type="hidden" name="line" value="3"><input type="hidden" name="text" value="Buy &lt;b&gt;stamps&lt;/b&gt;"></form>`,
		`<small class="token">on:2031-05-15</small>`, `<small class="token">every:week</small>`,
		`class="box done"`, `class="box dropped"`, `class="box carried"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not carry %s:\n%s", want, body)
		}
	}
	if n := strings.Count(body, `<form id="tick-`); n != 1 {
		t.Errorf("%d tick forms in the body, want 1 (the closed lines and the empty one have none)", n)
	}
	// Ticking from the body is the same write as from the Planned block.
	if rec := submit(h, "/pages/"+id+"/tick", url.Values{"line": {"3"}, "text": {"Buy <b>stamps</b>"}, "outcome": {"done"}, "back": {"/pages/" + id}}, "/pages/"+id); rec.Code != http.StatusSeeOther {
		t.Fatalf("tick from the body: %d %s", rec.Code, rec.Body.String())
	}
	after := browse(t, h, "/pages/"+id)
	if next := regexp.MustCompile(`<form id="tick-(\d+)"`).FindStringSubmatch(after); next == nil || next[1] == "3" || !strings.Contains(after, `value="Buy &lt;b&gt;stamps&lt;/b&gt;"`) ||
		!strings.Contains(after, `<small class="token">on:2031-05-22</small>`) {
		t.Errorf("the next line of an every: line has no tick form with a new line number:\n%s", mainOf(after))
	}

	req := httptest.NewRequest("POST", "/render", strings.NewReader(url.Values{"body": {"- [ ] Call\n- [x] Done"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Body.String(); !strings.Contains(got, `<span class="box open"`) || !strings.Contains(got, `class="box done"`) || strings.Contains(got, `<form id="tick-`) || strings.Contains(got, `<button class="box`) {
		t.Errorf("the preview draws a tick form:\n%s", mainOf(got))
	}
}

func TestAWikilinkInAPlannedLineIsALink(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "plan"), map[string]string{"page": fixedToday, "text": "Buy a present for [[Ana]] and *wrap* it #gift", "on": fixedToday}))
	page := browse(t, h, "/days/"+fixedToday)
	planned := page[strings.Index(page, `aria-label="Planned"`):]
	for _, want := range []string{`class="wikilink">Ana</a>`, `<em>wrap</em>`, `#gift</a>`} {
		if !strings.Contains(planned, want) {
			t.Errorf("the planned line does not carry %s:\n%s", want, planned)
		}
	}
	words := planned[strings.Index(planned, `<span class="text">`):]
	words = words[:strings.Index(words, "</span>")]
	if strings.Contains(words, "[[Ana]]") || strings.Contains(words, "<p>") {
		t.Errorf("the planned line is raw or in a paragraph:\n%s", planned)
	}
}

func TestAWeekListsItsSevenDaysAndTheirLines(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "plan"), map[string]string{"page": "Dentist", "text": "Book a visit", "on": "2031-05-14"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "plan"), map[string]string{"page": "2031-W20", "text": "Finish the report"}))

	page := browse(t, h, "/weeks/2031-W20")
	if strings.Contains(page, "no value") {
		t.Errorf("the week draws a missing value:\n%s", mainOf(page))
	}
	if !strings.Contains(page, "<h1>Week 20 of 2031</h1>") {
		t.Errorf("the title of the week:\n%s", mainOf(page))
	}
	for d := 12; d <= 18; d++ {
		if want := fmt.Sprintf(`href="/days/2031-05-%02d"`, d); !strings.Contains(page, want) {
			t.Errorf("the week does not list %s", want)
		}
	}
	if strings.Contains(page, `href="/days/2031-05-11"`) || strings.Contains(page, `href="/days/2031-05-19"`) {
		t.Error("the week lists a day outside it")
	}
	wed := page[strings.Index(page, `href="/days/2031-05-14"`):strings.Index(page, `href="/days/2031-05-15"`)]
	if !strings.Contains(wed, "Book a visit") || !strings.Contains(wed, `name="outcome" value="done"`) {
		t.Errorf("Wednesday does not hold its line with the tick:\n%s", wed)
	}
	for _, want := range []string{`aria-label="The page of the period"`, `<h2>The week page</h2>`, "Finish the report", `href="/weeks/2031-W19" rel="prev"`, `aria-current="page">Week</a>`, `name="page" value="2031-W20"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the week does not carry %s", want)
		}
	}
	empty := browse(t, h, "/weeks/2031-W21")
	if strings.Contains(empty, "The week page") {
		t.Error("a week with no page draws a heading for it")
	}
	if !strings.Contains(empty, "This week has no page yet") {
		t.Error("a week with no page does not say so")
	}

	req := httptest.NewRequest("GET", "/weeks/today", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound || rec.Header().Get("Location") != "/weeks/2031-W20" {
		t.Errorf("/weeks/today: %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	for _, bad := range []string{"/weeks/2031-W53", "/weeks/2031-W00", "/weeks/2031-W5", "/weeks/x"} {
		if _, err := c.Get(t.Context(), bad); apitest.Status(err) != 422 {
			t.Errorf("GET %s: %v, want 422", bad, err)
		}
	}
}

func TestAMonthAndAYearShowTheirPagesAndLines(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	if page := browse(t, h, "/months/2031-05"); strings.Contains(page, "The page of the period") || !strings.Contains(page, `<p class="empty">Nothing is planned in this period.</p>`) {
		t.Errorf("a month with no page:\n%s", mainOf(page))
	}
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "plan"), map[string]string{"page": "2031-05", "text": "Plan the month"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "plan"), map[string]string{"page": "2031", "text": "Run a marathon", "due": "2031-10-10"}))
	month := browse(t, h, "/months/2031-05")
	for _, want := range []string{`aria-label="The page of the period"`, "Plan the month", `form="tick-1"`, `aria-label="Planned"`, `name="page" value="2031-05"`} {
		if !strings.Contains(month, want) {
			t.Errorf("the month does not carry %s", want)
		}
	}
	if strings.Index(month, "Plan the month") > strings.Index(month, `<table class="calendar">`) {
		t.Error("the page of the month is below its calendar")
	}
	year := browse(t, h, "/years/2031")
	for _, want := range []string{"Run a marathon", `aria-label="Quarters"`, `href="?new=plan&amp;page=2031-Q4#plan"`, `name="page" value="2031"`} {
		if !strings.Contains(year, want) {
			t.Errorf("the year does not carry %s", want)
		}
	}
	// The link of a quarter with no page opens the plan form for that quarter.
	opened := browse(t, h, "/years/2031?new=plan&page=2031-Q4")
	if !strings.Contains(opened, `<details class="action" open>`) || !strings.Contains(opened, `name="page" value="2031-Q4"`) {
		t.Errorf("?new=plan&page=2031-Q4 does not open the form for the quarter:\n%s", mainOf(opened))
	}
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "plan"), map[string]string{"page": "2031-Q4", "text": "Ship it"}))
	if after := browse(t, h, "/years/2031"); !strings.Contains(after, `<a href="/pages/`) || strings.Contains(after, `href="?new=plan&amp;page=2031-Q4#plan"`) {
		t.Errorf("a quarter with a page still links its opener:\n%s", after)
	}
}

func TestAPeriodPageNamesItsKindAndItsView(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	for key, want := range map[string][]string{
		"2031-W20": {`<span class="kind">week page</span>`, `<a href="/weeks/2031-W20">The week view</a>`},
		"2031-05":  {`<span class="kind">month page</span>`, `<a href="/months/2031-05">The month view</a>`},
		"2031":     {`<span class="kind">year page</span>`, `<a href="/years/2031">The year view</a>`},
		"2031-Q4":  {`<span class="kind">quarter page</span>`, `<a href="/months/2031-10">October</a>`, `<a href="/months/2031-12">December</a>`},
	} {
		res, _ := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "plan"), map[string]string{"page": key, "text": "Aim " + key, "on": "2031-10-02"})).Result.(map[string]any)
		page := browse(t, h, fmt.Sprint("/pages/", res["page_id"]))
		want = append(want, `aria-label="Planned"`, `Aim `+key)
		for _, w := range want {
			if !strings.Contains(page, w) {
				t.Errorf("the page %s does not carry %s", key, w)
			}
		}
	}
}

// mainOf is the part of a page between its main tags: what a failure of a test needs to show.
func mainOf(page string) string {
	_, rest, _ := strings.Cut(page, `<main id="main">`)
	rest, _, _ = strings.Cut(rest, "</main>")
	return rest
}
