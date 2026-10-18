package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
	"lifelog/internal/api/siren"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db/dbtest"
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

// TestTheLatestTileWritesAScaleInWords: the Latest tile of a metric's page writes a scale's unit as every tile does,
// "of 5" for 1-5, and any other unit as it is.
func TestTheLatestTileWritesAScaleInWords(t *testing.T) {
	c, h, _ := metricFixture(t)
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "record"), map[string]string{"metric": "Mood", "day": "2026-10-12", "value": "4"}))
	for path, value := range map[string]string{"/metrics/Mood": `4 <span class="unit">of 5</span>`, "/metrics/Weight": `77.1 <span class="unit">kg</span>`} {
		latest := regexp.MustCompile(`<span class="name">Latest, Mon 12 Oct</span>\n<a class="reading" href="/measurements/\d+">` + regexp.QuoteMeta(value) + `</a>`)
		if body := browse(t, h, path); !latest.MatchString(body) {
			t.Errorf("the Latest tile of %s does not write %s", path, value)
		}
	}
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
	table := strings.Index(body, `class="readings-year"`)
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
	all := apitest.Must(t)(c.Get(t.Context(), "/metrics/Weight"))
	if !strings.Contains(body, `href="`+apitest.Href(all, "page")+`"`) || names(all) != "record" {
		t.Errorf("the readings do not link the metric's page, or offer more than record: %s", names(all))
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

// TestAMetricsPageIsAPage: the page of a metric (D27) is a page in a browser too: its text, the pages that mention
// it, its actions and a link to its readings (docs/plans/131-the-pareto-pass.md, part 3). A save returns to it with
// its toast. A deleted metric's page offers Restore.
func TestAMetricsPageIsAPage(t *testing.T) {
	c, h, _ := metricFixture(t)
	series := apitest.Must(t)(c.Get(t.Context(), "/metrics/Weight"))
	self := apitest.Href(series, "page")
	page := apitest.Must(t)(c.Get(t.Context(), self))
	if page.Class[0] != "metric" || page.Properties.(map[string]any)["body"] != "Before breakfast." || apitest.Href(page, "series") != "/metrics/Weight" {
		t.Errorf("the metric's page: %v %v, readings %q", page.Class, page.Properties, apitest.Href(page, "series"))
	}
	day := apitest.Must(t)(c.Get(t.Context(), "/pages?title=2026-10-13"))
	body := browse(t, h, self)
	for _, want := range []string{"Before breakfast.", `href="` + apitest.Href(day, "self") + `"`, `<a href="/metrics/Weight">Its readings</a>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the metric's page does not show %s", want)
		}
	}
	for _, name := range []string{"save-body", "rename", "file-pages", "tombstone-pages"} {
		if a := apitest.Action(t, page, name); !strings.Contains(body, `action="`+a.Href+`"`) {
			t.Errorf("the metric's page has no form of %s: %+v", name, a)
		}
	}

	save := apitest.Action(t, page, "save-body")
	rec := submit(h, save.Href, url.Values{"body": {"Before breakfast. See [[Scales]]."}, "version": {fieldOf(save, "version")}}, "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	to, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || to.Path != self {
		t.Fatalf("the save sends the browser to %q, want the page", rec.Header().Get("Location"))
	}
	if body := browse(t, h, to.String()); !strings.Contains(body, "<h2>Result</h2>") || !strings.Contains(body, `Created <a href="/pages?title=Scales">Scales</a>.`) {
		t.Error("the toast of the save is lost")
	}

	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, page, "tombstone-pages"), nil))
	body = browse(t, h, self)
	if !strings.Contains(body, "Deleted") || !strings.Contains(body, `action="`+self+`/revive"`) || strings.Contains(body, "<h2>Properties</h2>") {
		t.Error("a deleted metric's page is not the page view with Revive")
	}
}

func TestTheSeriesLinksItsPage(t *testing.T) {
	c, _, _ := metricFixture(t)
	series := apitest.Must(t)(c.Get(t.Context(), "/metrics/weight"))
	p := series.Properties.(map[string]any)
	if series.Title != "Weight" || p["unit"] != "kg" || apitest.Href(series, "page") == "" || p["body"] != nil {
		t.Errorf("the series: title %q, %v, page %q", series.Title, p, apitest.Href(series, "page"))
	}
	page := apitest.Must(t)(c.Get(t.Context(), apitest.Href(series, "page")))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, page, "rename"), map[string]string{"title": "Body weight"}))
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
	assertRows := func(t *testing.T, e *siren.Entity, want []string) {
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
	if u.Query().Has("from") || u.Query().Get("to") != "0001-01-01" {
		t.Fatalf("history navigation=%s", u)
	}
	assertRows(t, apitest.Must(t)(c.Get(t.Context(), apitest.Href(expanded, "self"))), plain[:3])
	if apitest.Href(expanded, "all-readings") != "" {
		t.Errorf("all history links all-readings again: %s", apitest.Href(expanded, "all-readings"))
	}
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
	if _, err := c.Get(t.Context(), "/metrics/Steps?to=-001-01-01"); err == nil {
		t.Fatal("accepted a to that is not a day")
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

// New metric on /metrics (docs/plans/113-new-metric-on-metrics.md): one form of register-metric, a name, a unit and a
// note (docs/plans/131-the-pareto-pass.md, part 3). file-pages files a metric in a category.

func TestRegisterMetricTakesANameAUnitAndANote(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	register := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/metrics")), "register-metric")
	if !register.Owner || register.Href != "/metrics" {
		t.Fatalf("/metrics offers register-metric as %+v", register)
	}
	var fields []string
	for _, f := range register.Fields {
		fields = append(fields, f.Name)
	}
	if !slices.Equal(fields, []string{"name", "unit", "note"}) {
		t.Errorf("register-metric fields: %v", fields)
	}
	walk := apitest.Must(t)(c.Do(ctx, register, map[string]string{"name": "Walk"}))
	if walk.Result != "New metric Walk." {
		t.Errorf("the answer's sentence: %v", walk.Result)
	}
	if again := apitest.Must(t)(c.Do(ctx, register, map[string]string{"name": "walk"})); again.Result != "Walk exists." {
		t.Errorf("the answer's sentence of a metric that exists: %v", again.Result)
	}
	if got, want := groupsOf(t, c), "Not filed:Mood,Walk"; got != want {
		t.Errorf("groups %q, want %q", got, want)
	}
}

// TestTheMetricsPageMakesAMetric: New metric is one boxed form of register-metric; ?new=register-metric opens it with
// the focus in its name. A card has no + Add and the page no New category: the day's form and the metric's page
// record, and Select files (docs/plans/131-the-pareto-pass.md, part 2).
func TestTheMetricsPageMakesAMetric(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	page := browse(t, h, "/metrics")
	if !strings.Contains(page, `<form method="POST" action="/metrics"`) || strings.Contains(page, "new-kind") || strings.Contains(page, "New category") {
		t.Errorf("/metrics does not draw one New metric form, or draws the kinds or New category")
	}
	if home := browse(t, h, "/days/"+fixedToday); strings.Contains(home, `method="POST" action="/metrics"`) {
		t.Error("Home, the page of today, draws the New metric form")
	}
	if p := browse(t, h, "/metrics?new=register-metric"); !strings.Contains(p, `<details class="action" open>`) ||
		!regexp.MustCompile(`<input type="text" name="name" value="" required placeholder="[^"]*" autofocus>`).MatchString(p) {
		t.Error("?new=register-metric does not open New metric with the focus in its name")
	}
	rec := submit(h, "/metrics", formSends(t, page, `action="/metrics"`, url.Values{"name": {"Weight"}, "unit": {"kg"}, "note": {"Before breakfast"}}), "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("New metric: %d %q %.300s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	made := map[string]string{}
	for _, x := range props(apitest.Must(t)(c.Get(t.Context(), "/metrics")))["metrics"].([]any) {
		m := x.(map[string]any)
		made[m["name"].(string)] = fmt.Sprintf("unit %q, note %v", m["unit"], m["note"])
	}
	if want := `unit "kg", note Before breakfast`; made["Weight"] != want {
		t.Errorf("the form made Weight with %s, want %s", made["Weight"], want)
	}
	if page = browse(t, h, "/metrics"); strings.Contains(page, `aria-label="Add a reading of`) || strings.Contains(page, "+ New metric here") {
		t.Error("/metrics draws + Add on a card or + New metric here")
	}
}

// TestAScoreRangeOutOfOrderIsRefused: a score's range is lo-hi with lo less than hi (metrics_range_order, D6). A
// browser can send 5-1: the server answers the schema's 422 with the sentence of the trigger, and nothing is written.
func TestAScoreRangeOutOfOrderIsRefused(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	rec := submit(h, "/metrics", url.Values{"name": {"Pain"}, "unit": {"5-1"}}, "/metrics")
	const trigger = "a scale names its range as its unit, lo-hi with lo less than hi, as in 1-5 (D6)"
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), trigger) || !strings.Contains(rec.Body.String(), `<a href="/metrics">Back</a>`) {
		t.Errorf("a score with the range 5-1: %d %.600s", rec.Code, rec.Body.String())
	}
	if e, err := c.Get(t.Context(), "/pages?title=Pain"); apitest.Status(err) != http.StatusNotFound {
		t.Errorf("the page Pain after the refusal: %v %v, want none", e, err)
	}
}

// The metrics as cards (docs/plans/112-metrics-as-cards.md): the cards that /metrics draws. Several metrics filed in a
// category in one write are TestFileMetricsAtOnce's; the parity table of cmd/lifelog runs file-pages on every surface.

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
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, iron, "file-pages"), map[string]string{"category": "Body"}))

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
		`<form id="pick" method="POST" action="/pages/file" class="inline">` + "\n" + `<input type="hidden" name="back" value="/metrics">`,
		`<input type="checkbox" name="ids" value="` + ids["Weight"] + `" form="pick" aria-label="Select Weight">`,
		`<input type="text" name="category" value="" required list="pick-category" placeholder="Bloodwork, Sport, Financial"><datalist id="pick-category"><option value="Body"><option value="Iron"></datalist>`,
		`<label><input type="checkbox" name="move" value="1"><span>Move (remove the other categories)</span></label>`,
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

	// The browser's form: a box per card, back to /metrics with the sentence. Move is not checked, so the form sends
	// no move, which the server reads as 0.
	rec := submit(h, "/pages/file", formSends(t, page, `id="pick"`, url.Values{"ids": {ids["Mood"], ids["Steps"]}, "category": {"Daily"}}), "")
	if rec.Code != http.StatusSeeOther || !feedbackLocation(rec.Header().Get("Location"), "/metrics") {
		t.Fatalf("the form: %d %q %.300s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if after := browse(t, h, rec.Header().Get("Location")); !strings.Contains(after, "Put 2 in Daily (a new category).") || !strings.Contains(after, `<a href="#c-`) || strings.Contains(after, `id="not-filed"`) {
		t.Errorf("/metrics after the filing: %.600s", after)
	}
	// A refused form answers with the error page, which leads back to the board.
	rec = submit(h, "/pages/file", formSends(t, page, `id="pick"`, url.Values{"category": {"Daily"}, "move": {"1"}}), "/metrics")
	if body := rec.Body.String(); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(body, "missing field ids") || !strings.Contains(body, `<a href="/metrics">Back</a>`) {
		t.Errorf("no box checked: %d %.600s", rec.Code, body)
	}
}

// The Metrics board folds (docs/plans/118-fold-and-find.md, rules 4 and 5); it has no list view
// (docs/plans/131-the-pareto-pass.md, part 3).
func TestTheMetricsBoardFolds(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	ids := cardsFixture(t, c)
	file := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/metrics")), "file-pages")
	apitest.Must(t)(c.Do(ctx, file, map[string]string{"ids": ids["Ferritin"], "category": "Iron"}))
	apitest.Must(t)(c.Do(ctx, file, map[string]string{"ids": ids["Weight"] + "," + ids["Walk"], "category": "Body"}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/pages?title=Iron")), "file-pages"), map[string]string{"category": "Body"}))

	cards := browse(t, h, "/metrics")
	for _, want := range []string{
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
	if strings.Contains(cards, "view=list") || strings.Contains(cards, "metric-rows") {
		t.Error("/metrics draws a list view")
	}
	// The tools of a group are its foot: after its cards, and in Iron after Ferritin (docs/plans/119-the-finish.md, rule 3).
	iron := cards[strings.Index(cards, `<h3>Iron`):]
	if ferritin, tools := strings.Index(iron, `>Ferritin</a>`), strings.Index(iron, `<div class="group-tools">`); ferritin < 0 || tools < ferritin {
		t.Errorf("the tools of Iron come before its cards: Ferritin at %d, the tools at %d", ferritin, tools)
	}
}

// TestAReadingLinkWritesItsNumberAsNumDoes: the title of a reading's link, in a day and in a series, writes the value
// as every other place writes it: 1000000, never 1e+06, and 0.00001, never 1e-05.
func TestAReadingLinkWritesItsNumberAsNumDoes(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", "2026-10-13")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "steps"}))
	for _, v := range []string{"1000000", "0.00001"} {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "steps", "day": "2026-10-12", "value": v}))
	}
	for path, want := range map[string][]string{
		"/days/2026-10-12": {"steps 0.00001", "steps 1000000"},
		"/metrics/steps":   {"2026-10-12 0.00001", "2026-10-12 1000000"},
	} {
		var got []string
		for _, l := range apitest.Must(t)(c.Get(t.Context(), path)).Entities {
			if slices.Contains(l.Rel, "reading") {
				got = append(got, l.Title)
			}
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("%s: the reading links are %q, want %q", path, got, want)
		}
	}
}

// TestAReadingSentAgainLinksTheDayOfTheStoredReading: a reading sent again with the same import_key answers "Already
// recorded", and its day link is the day of the reading that stands, not the day of the request. The first send says
// day=today on 2026-10-13. The next ones come on the next day: one says today again, and one names another day.
func TestAReadingSentAgainLinksTheDayOfTheStoredReading(t *testing.T) {
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	h := api.New(&core.Store{DB: dbtest.New(t), Clock: func() time.Time { return now }, Zone: time.UTC})
	c := client.InProcess(h, "cli")
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "register-metric"), map[string]string{"name": "Weight", "unit": "kg"}))
	record := func(day string) *siren.Entity {
		t.Helper()
		return apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "record"), map[string]string{"metric": "Weight", "day": day, "value": "71.2", "import_key": "scale/2026-10-13"}))
	}
	if first := record("today"); apitest.Href(first, "day") != "/days/2026-10-13" {
		t.Fatalf("the first send links the day %q, want /days/2026-10-13", apitest.Href(first, "day"))
	}
	now = now.AddDate(0, 0, 1)
	for _, day := range []string{"today", "2026-09-01"} {
		again := record(day)
		if again.Title != "Already recorded" || apitest.Href(again, "day") != "/days/2026-10-13" {
			t.Errorf("sent again with day=%s: %q links the day %q, want Already recorded with /days/2026-10-13", day, again.Title, apitest.Href(again, "day"))
		}
	}
	// A browser's form sent again gets the redirect to the day with the toast, as a capture does.
	form := url.Values{"metric": {"Weight"}, "day": {"2026-10-14"}, "value": {"72"}, "import_key": {"scale/browser"}}
	if first := submit(h, "/measurements", form, ""); first.Code != http.StatusSeeOther {
		t.Fatalf("the first form: %d, want 303", first.Code)
	}
	again := submit(h, "/measurements", form, "")
	if again.Code != http.StatusSeeOther || withoutReceiptOrNot(again.Header().Get("Location")) != "/days/2026-10-14" {
		t.Fatalf("the form sent again: %d %q, want 303 to /days/2026-10-14", again.Code, again.Header().Get("Location"))
	}
	if body := browse(t, h, again.Header().Get("Location")); !strings.Contains(body, "Already added. Nothing changed.") {
		t.Errorf("the second send does not say Already added:\n%.400s", body)
	}
}
