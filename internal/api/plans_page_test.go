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
		`<li class="plan-row" id="row-` + bank + `-` + line + `">`,
		`<form method="POST" action="/pages/` + bank + `/tick" class="tick-line">` + "\n" +
			`<input type="hidden" name="line" value="` + line + `"><input type="hidden" name="text" value="Call the bank">` +
			`<input type="hidden" name="back" value="/days/` + fixedToday + `#row-` + bank + `-` + line + `">` + "\n" +
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

	// Done from the Planned block: the body then reads as done, and its box reopens the line.
	rec := submit(h, "/pages/"+bank+"/tick", url.Values{"line": {line}, "text": {"Call the bank"}, "outcome": {"done"}, "back": {"/"}}, "/")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("tick: %d %s", rec.Code, rec.Body.String())
	}
	body := browse(t, h, "/pages/"+bank)
	if !strings.Contains(body, `<button class="box done" id="box-`+line+`" form="tick-`+line+`" name="outcome" value="reopen" aria-label="Reopen: Call the bank">`) || !strings.Contains(body, `<span class="badge">done Wed 14 May</span>`) {
		t.Errorf("the ticked line is not drawn as done:\n%s", mainOf(body))
	}
	if strings.Contains(body, `form="tick-`+line+`" name="outcome" value="done"`) {
		t.Error("a done line still draws a tick button")
	}
	// A line that changed since the page was read is a refusal, and it names the cause.
	if rec := submit(h, "/pages/"+bank+"/tick", url.Values{"line": {line}, "text": {"Call the bank"}, "outcome": {"done"}}, "/"); rec.Code != http.StatusConflict {
		t.Errorf("tick of a closed line: %d", rec.Code)
	}
	// The toast of the tick offers the Undo, which reopens the line and returns to the page.
	rec = submit(h, "/pages/"+bank+"/tick", url.Values{"line": {line}, "text": {"Call the bank"}, "outcome": {"reopen"}, "back": {"/pages/" + bank}}, "/")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("reopen: %d %s", rec.Code, rec.Body.String())
	}
	if toast := browse(t, h, rec.Header().Get("Location")); !strings.Contains(toast, "<p>Reopened.</p>") || strings.Contains(toast, `value="Undo"`) || strings.Contains(toast, ">Undo<") {
		t.Errorf("the toast of a reopen:\n%s", mainOf(toast))
	}
	if body := browse(t, h, "/pages/"+bank); !strings.Contains(body, `<button class="box open" id="box-`+line+`" form="tick-`+line+`" name="outcome" value="done"`) || strings.Contains(body, "done:2031-05-14") {
		t.Errorf("the reopened line is not open:\n%s", mainOf(body))
	}
	rec = submit(h, "/pages/"+bank+"/tick", url.Values{"line": {line}, "text": {"Call the bank"}, "outcome": {"done"}, "back": {"/pages/" + bank}}, "/")
	toast := browse(t, h, rec.Header().Get("Location"))
	for _, want := range []string{"<p>Done.</p>", `<form class="undo" method="post" action="/pages/` + bank + `/tick"><input type="hidden" name="back" value="/pages/` + bank + `">` +
		`<input type="hidden" name="line" value="` + line + `"><input type="hidden" name="text" value="Call the bank"><input type="hidden" name="outcome" value="reopen"><button class="small quiet">Undo</button></form>`} {
		if !strings.Contains(toast, want) {
			t.Errorf("the toast of a tick does not carry %s:\n%s", want, mainOf(toast))
		}
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
		`<button class="box open" id="box-3" form="tick-3" name="outcome" value="done" aria-label="Done: Buy &lt;b&gt;stamps&lt;/b&gt;">`,
		`<form id="tick-3" method="post" action="/pages/` + id + `/tick"><input type="hidden" name="back" value="/pages/` + id + `#box-3">` +
			`<input type="hidden" name="line" value="3"><input type="hidden" name="text" value="Buy &lt;b&gt;stamps&lt;/b&gt;"></form>`,
		`<span class="badge">on Thu 15 May</span>`, `<span class="badge">every week</span>`,
		`class="box done"`, `class="box dropped"`, `class="box carried"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not carry %s:\n%s", want, body)
		}
	}
	// The open line, the done line and the dropped line have a form; the carried line and the empty one have none.
	if n := strings.Count(body, `<form id="tick-`); n != 3 {
		t.Errorf("%d tick forms in the body, want 3", n)
	}
	if !strings.Contains(body, `<button class="box done" id="box-4" form="tick-4" name="outcome" value="reopen" aria-label="Reopen: Post it">`) ||
		!strings.Contains(body, `<button class="box dropped" id="box-5" form="tick-5" name="outcome" value="reopen" aria-label="Reopen: Skip">`) {
		t.Errorf("a closed line has no reopen box:\n%s", body)
	}
	// Ticking from the body is the same write as from the Planned block.
	if rec := submit(h, "/pages/"+id+"/tick", url.Values{"line": {"3"}, "text": {"Buy <b>stamps</b>"}, "outcome": {"done"}, "back": {"/pages/" + id}}, "/pages/"+id); rec.Code != http.StatusSeeOther {
		t.Fatalf("tick from the body: %d %s", rec.Code, rec.Body.String())
	}
	after := browse(t, h, "/pages/"+id)
	// The ticked line keeps its form (it reopens). The next line has a form of a new number.
	if !regexp.MustCompile(`<button class="box open" id="box-\d+" form="tick-(\d+)"`).MatchString(after) || strings.Contains(after, `<button class="box open" id="box-3" form="tick-3"`) || !strings.Contains(after, `value="Buy &lt;b&gt;stamps&lt;/b&gt;"`) ||
		!strings.Contains(after, `<span class="badge">on Thu 22 May</span>`) {
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

// TestThePlansPageGroupsTheOpenLinesByDayAndByPage: today is Wednesday 2031-05-14. By day there are four groups in a
// fixed order; by page there is one group a page, in the order of the titles. Every form of the page posts to an action
// of the entity, and a row never repeats the page of its own group.
func TestThePlansPageGroupsTheOpenLinesByDayAndByPage(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	if body := browse(t, h, "/plans"); !strings.Contains(body, `<p class="empty">Nothing is planned.</p>`) || !strings.Contains(body, "0 open plans") {
		t.Errorf("the empty page:\n%s", mainOf(body))
	}
	ids := map[string]string{}
	plan := func(page, words string, extra map[string]string) {
		fields := map[string]string{"page": page, "text": words}
		for k, v := range extra {
			fields[k] = v
		}
		res, _ := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "plan"), fields)).Result.(map[string]any)
		ids[page] = fmt.Sprint(res["page_id"])
	}
	plan("Shop", "Return the drill", map[string]string{"on": "2031-06-01", "due": "2031-05-12"})
	plan("Project", "Call the architect", nil)
	plan("Garden", "Plant roses", map[string]string{"on": "2031-05-20"})
	plan("Bank", "Call the bank", map[string]string{"due": "2031-05-14"})
	plan("Bank", "Pay rent", map[string]string{"on": "2031-05-10"})

	page := browse(t, h, "/plans")
	last := -1
	for _, head := range []string{"Past its day", "Today", "Later", "No day"} {
		at := strings.Index(page, "<h3>"+head+"</h3>")
		if at <= last {
			t.Errorf("the group %q is at %d, after the previous one at %d", head, at, last)
		}
		last = at
	}
	// Within a group the rows go by day: the drill (1 June) comes after the roses (20 May) in Later.
	rows := regexp.MustCompile(`<span class="text">([^<]+)</span>`).FindAllStringSubmatch(page, -1)
	var got []string
	for _, m := range rows {
		got = append(got, m[1])
	}
	if want := "Pay rent|Call the bank|Plant roses|Return the drill|Call the architect"; strings.Join(got, "|") != want {
		t.Errorf("the rows are %v, want %s", got, want)
	}
	for _, want := range []string{
		"5 open plans", `<a href="/plans" aria-current="page">By day</a>`, `<a href="/plans?order=page">By page</a>`,
		`<span class="badge danger">due Mon 12 May</span>`, // a due before today, on a line planned for a later day
		`<span class="badge">due Wed 14 May</span>`,        // a due of today is not past
		`name="page" value="` + fixedToday + `"`,           // the plan form offers today
		`action="/pages/` + ids["Bank"] + `/schedule"`, `name="on" value="2031-06-01"`, `name="due" value="2031-05-12"`,
		`<input type="hidden" name="back" value="/plans">`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("/plans does not carry %s", want)
		}
	}

	// Every form posts to an action of the entity.
	entity := apitest.Must(t)(c.Get(t.Context(), "/plans"))
	var allowed []*regexp.Regexp
	for _, a := range entity.Actions {
		allowed = append(allowed, regexp.MustCompile("^"+strings.ReplaceAll(regexp.QuoteMeta(a.Href), `\{id\}`, "[0-9]+")+"$"))
	}
	forms := regexp.MustCompile(`<form\b[^>]*\saction="([^"]+)"`).FindAllStringSubmatch(withoutChat(page), -1)
	seen := map[string]bool{}
	for _, f := range forms {
		ok := false
		for _, re := range allowed {
			ok = ok || re.MatchString(f[1])
		}
		if !ok && !strings.HasPrefix(f[1], "/search") {
			t.Errorf("the form %s posts to no action of /plans", f[1])
		}
		seen[f[1][strings.LastIndex(f[1], "/")+1:]] = true
	}
	for _, name := range []string{"tick", "carry", "schedule", "plan"} {
		if !seen[name] {
			t.Errorf("/plans draws no form of %s", name)
		}
	}

	byPage := browse(t, h, "/plans?order=page")
	last = -1
	for _, title := range []string{"Bank", "Garden", "Project", "Shop"} {
		head := `<h3><a href="/pages/` + ids[title] + `">` + title + `</a></h3>`
		at := strings.Index(byPage, head)
		if at <= last {
			t.Errorf("the head of %s is at %d, after the previous one at %d", title, at, last)
		}
		last = at
		if n := strings.Count(byPage, `>`+title+`</a>`); n != 1 {
			t.Errorf("the page %s is named %d times, want once (its head)", title, n)
		}
	}
	for _, want := range []string{`<a href="/plans">By day</a>`, `<a href="/plans?order=page" aria-current="page">By page</a>`, `<input type="hidden" name="back" value="/plans?order=page">`} {
		if !strings.Contains(byPage, want) {
			t.Errorf("/plans?order=page does not carry %s", want)
		}
	}

	// A schedule from the page returns to it, and the message names the new dates.
	rec := submit(h, "/pages/"+ids["Garden"]+"/schedule", url.Values{"line": {"1"}, "text": {"Plant roses"}, "on": {"2031-05-16"}, "due": {""}, "back": {"/plans"}}, "/plans")
	if rec.Code != http.StatusSeeOther || withoutReceipt(t, rec.Header().Get("Location")) != "/plans" {
		t.Fatalf("schedule: %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	toast := browse(t, h, rec.Header().Get("Location"))
	if want := `Scheduled line 1 of <a href="/pages/` + ids["Garden"] + `">its page</a>: on Fri 16 May.`; !strings.Contains(toast, want) {
		t.Errorf("the message of a schedule lacks %q:\n%s", want, mainOf(toast))
	}
	if strings.Contains(toast, ">Undo<") {
		t.Error("a schedule offers an Undo")
	}
}
