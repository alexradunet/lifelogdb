package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
	"lifelog/internal/client"
)

// The one page of a metric (docs/plans/100-one-page-a-metric.md): Record, every reading and the metric's own page.

// metricFixture is Weight (kg) with a note, a reading of 2024 and two of 2026, and today's day page, which names it.
// The server's day is 2026-10-13. It returns the three readings' addresses, oldest first.
func metricFixture(t *testing.T) (*client.Client, http.Handler, []string) {
	t.Helper()
	c, h, _ := apitest.FreshOn(t, "cli", "2026-10-13")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "Weight", "unit": "kg", "note": "Before breakfast."}))
	var readings []string
	for _, r := range [][2]string{{"2024-03-01", "81.5"}, {"2026-10-01", "77.4"}, {"2026-10-12", "77.1"}} {
		m := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "Weight", "day": r[0], "value": r[1]}))
		readings = append(readings, apitest.Href(m, "self"))
	}
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "capture"), map[string]string{"text": "Weighed in: [[Weight]] is down."}))
	return c, h, readings
}

func TestAMetricIsOnePage(t *testing.T) {
	c, h, readings := metricFixture(t)
	body := browse(t, h, "/metrics/Weight")

	var record string
	for _, m := range formRE.FindAllStringSubmatch(body, -1) {
		if m[1] == "/measurements" {
			record = m[0]
		}
	}
	if !strings.Contains(record, `<input type="hidden" name="metric" value="Weight">`) || strings.Contains(record, `<select name="metric"`) {
		t.Errorf("the record form does not fix its metric to the page's: %s", record)
	}
	table := strings.Index(body, `class="readings-year"`) // the sidebar draws a month table before the page
	if at := strings.Index(body, record); record == "" || table < 0 || at > table {
		t.Errorf("Record is not before the readings (record at %d, table at %d)", at, table)
	}
	if strings.Contains(body, ">All readings<") {
		t.Error("the page links All readings: every reading is on it")
	}
	at := func(s string) int { return strings.LastIndex(body, `href="`+s+`"`) }
	if !(at(readings[2]) < at(readings[1]) && at(readings[1]) < at(readings[0])) || at(readings[0]) < 0 {
		t.Errorf("the readings are not every one, newest first: %d %d %d", at(readings[2]), at(readings[1]), at(readings[0]))
	}
	for _, year := range []string{"2026", "2024"} {
		if !strings.Contains(body, `<details class="readings-year" open><summary><span class="year-name">`+year+`</span>`) {
			t.Errorf("no open fold of %s: a short series opens every year", year)
		}
	}
	page := apitest.Must(t)(c.Get(t.Context(), "/pages?title=2026-10-13"))
	for _, want := range []string{"Before breakfast.", `href="` + apitest.Href(page, "self") + `"`, "Mentioned in"} {
		if !strings.Contains(body, want) {
			t.Errorf("the metric page does not show %s", want)
		}
	}

	all := apitest.Must(t)(c.Get(t.Context(), "/metrics/Weight"))
	for _, name := range []string{"save-body", "rename", "link", "unlink", "tombstone"} {
		if a := apitest.Action(t, all, name); !strings.Contains(body, `<form method="POST" action="`+a.Href+`"`) {
			t.Errorf("the metric page has no form of %s: %+v", name, a)
		}
	}
	if n := len(all.Properties.(map[string]any)["readings"].([]any)); n != 3 {
		t.Errorf("the series reads %d readings with no from, want every one (3)", n)
	}
	if apitest.Href(all, "self") != "/metrics/Weight" {
		t.Errorf("the series asked with no query names %s as itself", apitest.Href(all, "self"))
	}
	window := apitest.Must(t)(c.Get(t.Context(), "/metrics/Weight?from=2026-01-01"))
	if n := len(window.Properties.(map[string]any)["readings"].([]any)); n != 2 {
		t.Errorf("from=2026-01-01 reads %d readings, want 2", n)
	}
	if b := browse(t, h, "/metrics/Weight?from=2026-01-01"); !strings.Contains(b, ">All readings<") {
		t.Error("a window does not lead back to every reading")
	}

	// Record as a browser sends the form: the fields it hides, and a day and a value. The page comes back with the
	// new reading as its first row.
	sent := url.Values{"day": {"2026-10-13"}, "value": {"76.9"}}
	for _, f := range hiddenRE.FindAllStringSubmatch(record, -1) {
		sent.Set(f[1], f[2])
	}
	rec := submit(h, "/measurements", sent, "/metrics/Weight")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/metrics/Weight" {
		t.Fatalf("record from the page: %d to %q, want 303 back to it", rec.Code, rec.Header().Get("Location"))
	}
	after := browse(t, h, "/metrics/Weight")
	fold := after[strings.Index(after, `class="readings-year"`):]
	if first := strings.Index(fold, "<tbody>"); first < 0 || !strings.HasPrefix(fold[first:], `<tbody><tr><td><a href="/days/2026-10-13">`) || !strings.Contains(fold[first:first+200], ">76.9</a>") {
		t.Error("the reading recorded from the page is not its first row")
	}
}

func TestTheBrowserOpensAMetricsPageAtItsMetric(t *testing.T) {
	c, h, _ := metricFixture(t)
	series := apitest.Must(t)(c.Get(t.Context(), "/metrics/Weight"))
	self := apitest.Href(series, "page")

	req := httptest.NewRequest("GET", self, nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/metrics/Weight" {
		t.Fatalf("a browser at the metric's page: %d to %q, want 303 to /metrics/Weight", rec.Code, rec.Header().Get("Location"))
	}
	if p := apitest.Must(t)(c.Get(t.Context(), self)); p.Class[0] != "metric" || p.Properties.(map[string]any)["body"] != "Before breakfast." {
		t.Errorf("JSON at the metric's page is not the page: %v %v", p.Class, p.Properties)
	}

	// A save with no page to return to ends on the metric, with its toast.
	save := apitest.Action(t, series, "save-body")
	version := ""
	for _, f := range save.Fields {
		if f.Name == "version" {
			version = fmt.Sprint(f.Value)
		}
	}
	rec = submit(h, save.Href, url.Values{"body": {"Before breakfast. See [[Scales]]."}, "version": {version}}, "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest("GET", rec.Header().Get("Location"), nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	to, err := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusSeeOther || err != nil || to.Path != "/metrics/Weight" {
		t.Fatalf("the saved page sends the browser to %d %q, want the metric", rec.Code, rec.Header().Get("Location"))
	}
	if body := browse(t, h, to.String()); !strings.Contains(body, "<h2>Result</h2>") || !strings.Contains(body, `Created <a href="/pages?title=Scales">Scales</a>.`) {
		t.Error("the toast of the save is lost on the way to the metric")
	}

	// A deleted metric stays a page: the page view, with Revive.
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/metrics/Weight")), "tombstone"), nil))
	body := browse(t, h, self)
	if !strings.Contains(body, "Deleted") || !strings.Contains(body, `action="`+self+`/revive"`) || strings.Contains(body, "<h2>Properties</h2>") {
		t.Error("a deleted metric's page is not the page view with Revive")
	}
}

func TestTheSeriesCarriesItsPage(t *testing.T) {
	c, _, _ := metricFixture(t)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Body"}))
	series := apitest.Must(t)(c.Get(t.Context(), "/metrics/weight"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, series, "link"), map[string]string{"to": "Body", "kind": "part-of"}))

	series = apitest.Must(t)(c.Get(t.Context(), "/metrics/weight"))
	p := series.Properties.(map[string]any)
	if series.Title != "Weight" || p["unit"] != "kg" || p["body"] != "Before breakfast." || fmt.Sprint("/pages/", p["id"]) != apitest.Href(series, "page") {
		t.Errorf("the series lacks its page: title %q, %v", series.Title, p)
	}
	edge := func(list any, title string) bool {
		edges, _ := list.([]any)
		for _, e := range edges {
			if e.(map[string]any)["title"] == title {
				return true
			}
		}
		return false
	}
	if !edge(p["links"], "Body") || !edge(p["backlinks"], "2026-10-13") {
		t.Errorf("the series lacks its links or backlinks: %v %v", p["links"], p["backlinks"])
	}
	link := apitest.Action(t, series, "link")
	if link.Title != "Add a link" || !hasOption(link, "kind", "part-of") {
		t.Errorf("link is not Add a link with every kind: %+v", link)
	}
	for _, name := range []string{"save-body", "rename", "unlink", "tombstone"} {
		if !hasAction(series, name) {
			t.Errorf("the series does not offer its page's %s: %s", name, names(series))
		}
	}

	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, series, "rename"), map[string]string{"title": "Body weight"}))
	for _, path := range []string{"/metrics/Body%20weight", "/metrics/Weight"} {
		s := apitest.Must(t)(c.Get(t.Context(), path))
		if s.Title != "Body weight" || len(s.Properties.(map[string]any)["readings"].([]any)) != 3 {
			t.Errorf("%s after the rename: %q with %v", path, s.Title, s.Properties.(map[string]any)["readings"])
		}
	}
}

// TestAYearShowsItsNewestReadingsFirst: a year's fold shows its newest ten readings, and the rest of the year are in
// a closed fold under them that says how many it holds, so the page's other groups stay near; every reading is
// still on the page, newest first.
func TestAYearShowsItsNewestReadingsFirst(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", "2026-10-13")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "Weight", "unit": "kg"}))
	for d := 1; d <= 15; d++ {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "Weight", "day": fmt.Sprintf("2026-09-%02d", d), "value": fmt.Sprint(70 + d)}))
	}
	body := browse(t, h, "/metrics/Weight")
	fold := body[strings.Index(body, `<details class="readings-year" open>`):]
	end := strings.Index(fold, `</details></details>`)
	if end < 0 {
		t.Fatal("the year's fold holds no fold of its older readings")
	}
	fold = fold[:end]
	open, more, _ := strings.Cut(fold, `<details class="readings-more"><summary>5 more readings of 2026</summary>`)
	if more == "" {
		t.Fatal("the year does not fold its older five readings under its newest ten")
	}
	if n := strings.Count(open, `<a href="/measurements/`); n != 10 {
		t.Errorf("the open part of the year has %d readings, want 10", n)
	}
	if !strings.Contains(open, `<a href="/days/2026-09-15">`) || !strings.Contains(open, `<a href="/days/2026-09-06">`) || strings.Contains(open, `<a href="/days/2026-09-05">`) {
		t.Error("the open part is not the newest ten, 15 to 6 September")
	}
	if n := strings.Count(more, `<a href="/measurements/`); n != 5 || !strings.Contains(more, `<a href="/days/2026-09-01">`) {
		t.Errorf("the closed part has %d readings, want the five of 1 to 5 September", n)
	}
	if !strings.Contains(body, `<span class="meta">15 readings · 71 to 85 kg</span>`) {
		t.Error("the year's summary does not count all its readings")
	}
}

// TestTheFoldOfOneOlderReadingSaysReading: a year of eleven readings folds one under its newest ten, and the fold
// says "1 more reading", not "1 more readings".
func TestTheFoldOfOneOlderReadingSaysReading(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", "2026-10-13")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "Weight", "unit": "kg"}))
	for d := 1; d <= 11; d++ {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "Weight", "day": fmt.Sprintf("2026-09-%02d", d), "value": fmt.Sprint(70 + d)}))
	}
	body := browse(t, h, "/metrics/Weight")
	if want := `<details class="readings-more"><summary>1 more reading of 2026</summary>`; !strings.Contains(body, want) {
		t.Errorf("the fold of one older reading is not %s", want)
	}
}

func TestBrowserAllReadingsCoordinates(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "synthetic"}))
	for _, day := range []string{"2026-01-01", "2026-09-01"} {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "synthetic", "day": day, "value": "1"}))
	}
	body := browse(t, h, "/metrics/synthetic?from=2026-06-03&to=2026-09-01")
	link := regexp.MustCompile(`href="([^"]+)"\>All readings`).FindStringSubmatch(body)
	if len(link) != 2 {
		t.Fatal("missing all readings link")
	}
	all := browse(t, h, strings.ReplaceAll(link[1], "&amp;", "&"))
	chartSVG := regexp.MustCompile(`(?s)<svg class="chart".*?</svg>`).FindString(all) // the icons of the sidebar have circles too
	xs := regexp.MustCompile(`<circle cx="([^"]+)"`).FindAllStringSubmatch(chartSVG, -1)
	if len(xs) != 2 || xs[0][1] == xs[1][1] {
		t.Fatalf("collapsed coordinates: %v", xs)
	}
	if !strings.Contains(body, `cx="626.0"`) {
		t.Error("90 day endpoint")
	}
	if strings.Contains(browse(t, h, "/metrics/synthetic?from=2026-09-01&to=2026-09-01"), `<svg class="chart"`) {
		t.Error("same day exclusive range should be empty")
	}
	if strings.Contains(browse(t, h, "/metrics/synthetic?from=2025-01-01&to=2025-02-01"), `<svg class="chart"`) {
		t.Error("empty chart")
	}
}

func TestAllReadingsIncludesEntireAdmittedCalendar(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	actions := apitest.Must(t)(c.Get(t.Context(), "/actions"))
	metric := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, actions, "register-metric"), map[string]string{"name": "Steps", "unit": "steps"}))
	days := []string{"0000-01-01", "0000-02-29", "0001-01-01", "0001-01-02"}
	var plain []string
	values := map[string]string{}
	for i, day := range days {
		fact := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, actions, "record"), map[string]string{"metric": "Steps", "day": day, "value": fmt.Sprint(10 + i)}))
		id := apitest.Href(fact, "self")
		values[id] = fmt.Sprint(10 + i)
		plain = append(plain, id)
	}
	metricPage := apitest.Must(t)(c.Get(t.Context(), apitest.Href(metric, "page")))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, metricPage, "rename"), map[string]string{"title": "Stride count"}))
	assertRows := func(t *testing.T, e *api.Entity, want []string) {
		t.Helper()
		rows := e.Properties.(map[string]any)["readings"].([]any)
		got := []string{}
		for _, row := range rows {
			r := row.(map[string]any)
			got = append(got, fmt.Sprint("/measurements/", r["id"]))
			id := got[len(got)-1]
			if fmt.Sprint(r["value"]) != values[id] {
				t.Fatalf("row %s value=%v want=%v", id, r["value"], values[id])
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ordered identities=%v want=%v", got, want)
		}
	}
	// The all-readings link drops the lower bound and keeps the upper one; following it again changes nothing.
	q := url.Values{"from": {"0000-12-31"}, "to": {"0001-01-01"}}
	bounded := apitest.Must(t)(c.Get(t.Context(), "/metrics/Steps?"+q.Encode())) // retained alias
	link := apitest.Href(bounded, "all-readings")
	expanded := apitest.Must(t)(c.Get(t.Context(), link))
	assertRows(t, expanded, plain[:3])
	u, err := url.Parse(apitest.Href(expanded, "self"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("all_history") != "1" || u.Query().Has("from") || u.Query().Get("to") != "0001-01-01" {
		t.Fatalf("history navigation=%s", u)
	}
	assertRows(t, apitest.Must(t)(c.Get(t.Context(), apitest.Href(expanded, "self"))), plain[:3])
	assertRows(t, apitest.Must(t)(c.Get(t.Context(), apitest.Href(expanded, "all-readings"))), plain[:3])
	if body := browse(t, h, link); !strings.Contains(body, "All history") {
		t.Fatal("missing explicit history label")
	}
	// Explicit ranges retain their exclusive lower and inclusive upper limits.
	bounded = apitest.Must(t)(c.Get(t.Context(), "/metrics/Stride%20count?from=0000-01-01&to=0000-02-29"))
	rows := bounded.Properties.(map[string]any)["readings"].([]any)
	if len(rows) != 1 || fmt.Sprint("/measurements/", rows[0].(map[string]any)["id"]) != plain[1] || fmt.Sprint(rows[0].(map[string]any)["value"]) != "11" {
		t.Fatal("bounded range semantics changed")
	}
	// A default window near the minimum must not manufacture negative-year input.
	assertRows(t, apitest.Must(t)(c.Get(t.Context(), "/metrics/Steps?to=0000-01-01")), plain[:1])
	for _, q := range []string{"all_history=2", "all_history=1&from=0000-01-01", "all_history=1&to=-001-01-01"} {
		if _, err := c.Get(t.Context(), "/metrics/Steps?"+q); err == nil {
			t.Fatalf("accepted invalid history query %s", q)
		}
	}
}

// TestAReadingsFactsAreOnACard: a reading's page puts its facts on a card.
func TestAReadingsFactsAreOnACard(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	r := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "weight", "day": fixedToday, "value": "71.4"}))
	if body := browse(t, h, apitest.Href(r, "self")); !strings.Contains(body, `<div class="sheet list"><dl class="facts">`) {
		t.Error("a reading's facts are not on a card")
	}
}

// New metric, new category and a new reading from /metrics (docs/plans/113-new-metric-on-metrics.md).

func TestRegisterMetricTakesACategoryAndAHabit(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	register := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/metrics")), "register-metric")
	if !register.Owner || register.Href != "/metrics" {
		t.Fatalf("/metrics offers register-metric as %+v", register)
	}
	var fields []string
	for _, f := range register.Fields {
		fields = append(fields, f.Name)
		if f.Name == "category" && f.Required {
			t.Errorf("%s is required, want optional", f.Name)
		}
	}
	if !slices.Equal(fields, []string{"name", "unit", "note", "category"}) {
		t.Errorf("register-metric fields: %v", fields)
	}

	walk := apitest.Must(t)(c.Do(ctx, register, map[string]string{"name": "Walk", "category": "Sport"}))
	if walk.Result != "New metric Walk, in Sport." {
		t.Errorf("the answer's sentence: %v", walk.Result)
	}
	if got, want := groupsOf(t, c), "Sport:Walk Not filed:Mood"; got != want {
		t.Errorf("groups %q, want %q", got, want)
	}
	if got := props(apitest.Must(t)(c.Get(ctx, "/metrics/Walk")))["categories"]; !slices.Equal(anyStrings(got), []string{"Sport"}) {
		t.Errorf("Walk's categories %v, want [Sport]", got)
	}

	// A refused metric writes nothing: neither the metric nor its category.
	if _, err := c.Do(ctx, register, map[string]string{"name": "Run", "unit": "km", "category": "2031-05-01"}); err == nil {
		t.Fatal("a metric filed in a day was written")
	}
	if got, want := groupsOf(t, c), "Sport:Walk Not filed:Mood"; got != want {
		t.Errorf("after a refusal, groups %q, want %q", got, want)
	}
	if _, err := c.Get(ctx, "/pages?title=Run"); err == nil {
		t.Error("a refused register-metric made the metric Run")
	}
}

func anyStrings(x any) []string {
	var out []string
	l, _ := x.([]any)
	for _, s := range l {
		out = append(out, s.(string))
	}
	return out
}

func TestTheMetricsPageMakesAMetricACategoryAndAReading(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)

	page := browse(t, h, "/metrics")
	for _, want := range []string{`<details class="action new-metric" id="new-metric">`, `<form class="kind-number"`, `<form class="kind-score"`,
		`<form class="kind-habit"`, `<a class="button" href="/metrics?new=file-pages#pick">New category</a>`,
		`<input type="radio" name="new-kind" value="number" checked>`} {
		if !strings.Contains(page, want) {
			t.Errorf("/metrics lacks %s", want)
		}
	}
	if home := browse(t, h, "/"); strings.Contains(home, `method="POST" action="/metrics"`) {
		t.Error("Home still draws the New metric form")
	}
	if p := browse(t, h, "/metrics?new=register-metric&category=Sport"); !strings.Contains(p, `id="new-metric" open>`) ||
		!strings.Contains(p, `name="category" list="metric-categories" value="Sport"`) {
		t.Error("?new=register-metric&category=Sport does not open New metric with Sport filled in")
	}
	if p := browse(t, h, "/metrics?new=file-pages"); !strings.Contains(p, `<input type="checkbox" class="pick-on" checked>`) {
		t.Error("?new=file-pages does not turn Select on")
	}

	// The habit form and the number form, as a browser sends them.
	for _, form := range []url.Values{
		{"kind": {"habit"}, "name": {"Walk"}, "unit": {""}, "category": {"Sport"}, "note": {""}, "back": {"/metrics"}},
		{"kind": {"number"}, "name": {"Weight"}, "unit": {"kg"}, "category": {"Body"}, "note": {"Before breakfast"}, "back": {"/metrics"}},
	} {
		rec := submit(h, "/metrics", form, "")
		if rec.Code != http.StatusSeeOther || !feedbackLocation(rec.Header().Get("Location"), "/metrics") {
			t.Fatalf("New metric %s: %d %q %.300s", form.Get("name"), rec.Code, rec.Header().Get("Location"), rec.Body.String())
		}
		if form.Get("name") == "Walk" && !strings.Contains(browse(t, h, rec.Header().Get("Location")), "New metric Walk, in Sport.") {
			t.Error("the toast does not say what New metric made")
		}
	}
	page = browse(t, h, "/metrics")
	for _, want := range []string{`href="/metrics?new=register-metric&amp;category=Body#new-metric">+ New metric here</a>`,
		`aria-label="Add a reading of Weight"`, `aria-label="Add a reading of Walk"`} {
		if !strings.Contains(page, want) {
			t.Errorf("/metrics lacks %s", want)
		}
	}

	// The card's +: one value, for today.
	rec := submit(h, "/measurements", url.Values{"metric": {"Weight"}, "day": {fixedToday}, "value": {"80.5"}, "back": {"/metrics"}}, "")
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || (loc != "/metrics" && !feedbackLocation(loc, "/metrics")) {
		t.Fatalf("the card's +: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	var weight []any
	for _, x := range props(apitest.Must(t)(c.Get(t.Context(), "/metrics")))["metrics"].([]any) {
		if m := x.(map[string]any); m["name"] == "Weight" {
			weight = m["recent"].([]any)
		}
	}
	if len(weight) != 1 || weight[0].(map[string]any)["day"] != fixedToday || weight[0].(map[string]any)["value"] != json.Number("80.5") {
		t.Errorf("Weight's readings %v, want 80.5 on %s", weight, fixedToday)
	}

	// A refused form opens again on its own kind, with what it sent.
	submit(h, "/metrics", url.Values{"kind": {"score"}, "name": {"Mood"}, "unit": {"1-5"}, "back": {"/metrics"}}, "")
	rec = submit(h, "/metrics", url.Values{"kind": {"score"}, "name": {"Mood"}, "unit": {"1-10"}, "category": {"Mind"}, "back": {"/metrics"}}, "")
	body := rec.Body.String()
	if rec.Code != http.StatusConflict || !strings.Contains(body, `id="new-metric" open>`) || !strings.Contains(body, `<input type="radio" name="new-kind" value="score" checked>`) ||
		!strings.Contains(body, `value="1-10"`) || !strings.Contains(body, `value="Mind"`) {
		t.Errorf("a refused New metric: %d %.600s", rec.Code, body)
	}
}

// The metrics as cards, and file-pages (docs/plans/112-metrics-as-cards.md): several metrics filed in a category in
// one write, and the cards /metrics draws of them. The parity table of cmd/lifelog runs file-pages on every surface.

// cardsFixture registers Ferritin (read on two days long ago), Weight (read on the two days before fixedToday), Walk
// (a habit) and Steps (never read), and returns the client, the handler and the metric ids by name.
func cardsFixture(t *testing.T, c *client.Client) map[string]string {
	t.Helper()
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	for _, m := range []map[string]string{{"name": "Ferritin", "unit": "ug/L", "note": "iron stores"}, {"name": "Weight", "unit": "kg"},
		{"name": "Walk"}, {"name": "Steps", "unit": "n"}} {
		apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "register-metric"), m))
	}
	record := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/metrics/Ferritin")), "record")
	for _, r := range []map[string]string{{"metric": "Ferritin", "day": "2030-03-01", "value": "40"}, {"metric": "Ferritin", "day": "2030-09-01", "value": "55"},
		{"metric": "Weight", "day": "2031-05-12", "value": "80"}, {"metric": "Weight", "day": "2031-05-13", "value": "79.5"}} {
		apitest.Must(t)(c.Do(ctx, record, r))
	}
	ids := map[string]string{}
	for _, x := range props(apitest.Must(t)(c.Get(ctx, "/metrics")))["metrics"].([]any) {
		m := x.(map[string]any)
		ids[m["name"].(string)] = fmt.Sprint(m["id"])
	}
	return ids
}

// groupsOf says the groups of /metrics: "title:names" for each, in order.
func groupsOf(t *testing.T, c *client.Client) string {
	t.Helper()
	var got []string
	for _, g := range props(apitest.Must(t)(c.Get(t.Context(), "/metrics")))["groups"].([]any) {
		g := g.(map[string]any)
		var ms []string
		for _, m := range g["metrics"].([]any) {
			ms = append(ms, m.(map[string]any)["name"].(string))
		}
		got = append(got, g["title"].(string)+":"+strings.Join(ms, ","))
	}
	return strings.Join(got, " ")
}

func TestTheMetricsPageDrawsCards(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	ids := cardsFixture(t, c)
	file := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/metrics")), "file-pages")
	apitest.Must(t)(c.Do(ctx, file, map[string]string{"ids": ids["Ferritin"], "category": "Iron"}))
	apitest.Must(t)(c.Do(ctx, file, map[string]string{"ids": ids["Weight"] + "," + ids["Walk"], "category": "Body"}))
	iron := apitest.Must(t)(c.Get(ctx, "/pages?title=Iron"))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, iron, "link"), map[string]string{"to": "Body", "kind": "part-of"}))

	// The JSON carries each metric's recent readings, oldest first.
	for _, x := range props(apitest.Must(t)(c.Get(ctx, "/metrics")))["metrics"].([]any) {
		m := x.(map[string]any)
		if m["name"] != "Weight" {
			continue
		}
		r := m["recent"].([]any)
		if len(r) != 2 || r[0].(map[string]any)["day"] != "2031-05-12" || r[1].(map[string]any)["value"] != json.Number("79.5") {
			t.Errorf("Weight's recent readings: %v", r)
		}
	}

	page := browse(t, h, "/metrics")
	for _, want := range []string{
		`<nav class="kinds jump" aria-label="Groups"><a href="#c-`, `>Body <small>3</small></a>`, `<a href="#not-filed">Not filed <small>2</small></a>`,
		`id="not-filed"`, `<details class="metric-sub" open><summary><h3>Iron <small>1</small></h3><span class="meta">newest `, `<a href="/pages/` + idOfSelf(t, apitest.Href(iron, "self")) + `">Open Iron</a>`,
		`<a class="name" href="/metrics/Weight">Weight</a>`, `79.5 <span class="unit">kg</span>`, `<time class="meta" datetime="2031-05-13" title="yesterday · Tue 13 May · 2 readings">yesterday</time>`, `<svg class="trend"`,
		`<div class="reading-tile metric-card stale">`, `title="iron stores">Ferritin</a>`, `<span class="reading none"><span aria-hidden="true">–</span> <span class="unit">n</span></span><span class="line-none" aria-hidden="true"></span>`, `<div class="card-foot"><span class="meta">No reading</span>`,
		`<form id="pick" class="pick-bar" method="POST" action="/pages/file">`, `<input type="hidden" name="back" value="/metrics">`,
		`<input type="checkbox" name="ids" value="` + ids["Weight"] + `" form="pick" aria-label="Select Weight">`, `<option value="Iron">Body/Iron</option>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("/metrics lacks %s", want)
		}
	}
	if main := page[strings.Index(page, "<main"):]; strings.Contains(main, "<table") { // the rail's month is a table
		t.Error("the category lens draws a table")
	}
	if strings.Contains(page, `class="pick-on" checked`) {
		t.Error("Select is on with nothing refused")
	}

	// The browser's form: a box per card, back to /metrics with the sentence.
	rec := submit(h, "/pages/file", url.Values{"ids": {ids["Mood"], ids["Steps"]}, "category": {"Daily"}, "move": {"0"}, "back": {"/metrics"}}, "")
	if rec.Code != http.StatusSeeOther || !feedbackLocation(rec.Header().Get("Location"), "/metrics") {
		t.Fatalf("the form: %d %q %.300s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if after := browse(t, h, rec.Header().Get("Location")); !strings.Contains(after, "Put 2 in Daily (a new category).") || !strings.Contains(after, `<a href="#c-`) || strings.Contains(after, `id="not-filed"`) {
		t.Errorf("/metrics after the filing: %.600s", after)
	}
	// A refused form comes back to the page with Select on and the category it sent.
	rec = submit(h, "/pages/file", url.Values{"category": {"Daily"}, "back": {"/metrics"}}, "")
	if body := rec.Body.String(); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(body, `<input type="checkbox" class="pick-on" checked>`) ||
		!strings.Contains(body, `value="Daily"`) || !strings.Contains(body, "missing field ids") {
		t.Errorf("no box checked: %d %.600s", rec.Code, body)
	}
}

// The Metrics board folds, and ?view=list draws the same metrics as rows (docs/plans/118-fold-and-find.md, rules 4
// and 5).
func TestTheMetricsBoardFoldsAndHasAList(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	ids := cardsFixture(t, c)
	file := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/metrics")), "file-pages")
	apitest.Must(t)(c.Do(ctx, file, map[string]string{"ids": ids["Ferritin"], "category": "Iron"}))
	apitest.Must(t)(c.Do(ctx, file, map[string]string{"ids": ids["Weight"] + "," + ids["Walk"], "category": "Body"}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/pages?title=Iron")), "link"), map[string]string{"to": "Body", "kind": "part-of"}))

	cards := browse(t, h, "/metrics")
	for _, want := range []string{
		`<nav class="kinds view-switch" aria-label="View"><a href="/metrics" aria-current="true">Cards</a><a href="/metrics?view=list">List</a></nav>`,
		// Body holds Weight and Walk, and Ferritin under Iron: its newest reading is Weight's, yesterday.
		`<details class="group metric-group" id="c-`, ` open><summary><h2>Body <small>3</small></h2><span class="meta">newest yesterday · Tue 13 May</span></summary>`,
		`<details class="metric-sub" open><summary><h3>Iron <small>1</small></h3><span class="meta">newest `,
		// Mood and Steps have no reading: the line of Not filed says no day.
		`<details class="group metric-group" id="not-filed" open><summary><h2>Not filed <small>2</small></h2></summary>`,
	} {
		if !strings.Contains(cards, want) {
			t.Errorf("/metrics lacks %s", want)
		}
	}

	list := browse(t, h, "/metrics?view=list")
	for _, want := range []string{
		`<a href="/metrics">Cards</a><a href="/metrics?view=list" aria-current="true">List</a>`,
		`<ul class="plain metric-rows"><li class="metric-row pick">`,
		`<input type="checkbox" name="ids" value="` + ids["Weight"] + `" form="pick" aria-label="Select Weight">`,
		`<a class="name" href="/metrics/Weight">Weight</a>`,
		`79.5 <span class="unit">kg</span></a><span class="meta">yesterday · Tue 13 May</span><span class="line"><svg class="trend"`,
		`<li class="metric-row pick stale">`, `<span class="meta none">No reading yet · n</span>`,
		`<input type="hidden" name="back" value="/metrics?view=list">`,
	} {
		if !strings.Contains(list, want) {
			t.Errorf("/metrics?view=list lacks %s", want)
		}
	}
	// Each + Add of a row and the pick bar return to the list.
	if strings.Contains(list, `class="reading-tile metric-card`) || strings.Count(list, `<input type="hidden" name="back" value="/metrics?view=list">`) != 6 {
		t.Error("the list draws a card, or a form of it does not return to the list")
	}
	// The tools of a group are its foot: after its cards, and in Iron after Ferritin (docs/plans/119-the-finish.md, rule 3).
	for _, page := range []string{cards, list} {
		iron := page[strings.Index(page, `<h3>Iron`):]
		if ferritin, tools := strings.Index(iron, `>Ferritin</a>`), strings.Index(iron, `<div class="group-tools">`); ferritin < 0 || tools < ferritin {
			t.Errorf("the tools of Iron come before its cards: Ferritin at %d, the tools at %d", ferritin, tools)
		}
	}
	names := regexp.MustCompile(`<a class="name" href="/metrics/(\w+)"`)
	if a, b := names.FindAllString(cards, -1), names.FindAllString(list, -1); len(a) != 5 || strings.Join(a, " ") != strings.Join(b, " ") {
		t.Errorf("the cards %v and the rows %v are not the same metrics", a, b)
	}

	// + Add on a row returns to the list.
	rec := submit(h, "/measurements", url.Values{"metric": {"Steps"}, "day": {fixedToday}, "value": {"9000"}, "back": {"/metrics?view=list"}}, "")
	if loc, err := url.Parse(rec.Header().Get("Location")); rec.Code != http.StatusSeeOther || err != nil || loc.Path != "/metrics" || loc.Query().Get("view") != "list" {
		t.Fatalf("+ Add on the list: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if after := browse(t, h, rec.Header().Get("Location")); !strings.Contains(after, `9000 <span class="unit">n</span></a><span class="meta">today</span>`) {
		t.Error("the list after + Add lacks the reading")
	}
}
