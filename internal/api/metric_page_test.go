package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"lifelog/internal/client"
)

// The one page of a metric (docs/plans/100-one-page-a-metric.md): Record, every reading and the metric's own page.

// metricFixture is Weight (kg) with a note, a reading of 2024 and two of 2026, and today's day page, which names it.
// The server's day is 2026-10-13. It returns the three readings' addresses, oldest first.
func metricFixture(t *testing.T) (*client.Client, http.Handler, []string) {
	t.Helper()
	c, h := freshOn(t, "2026-10-13")
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "Weight", "unit": "kg", "note": "Before breakfast."}))
	var readings []string
	for _, r := range [][2]string{{"2024-03-01", "81.5"}, {"2026-10-01", "77.4"}, {"2026-10-12", "77.1"}} {
		m := must(c.Do(t.Context(), find(root, "record"), map[string]string{"metric": "Weight", "day": r[0], "value": r[1]}))
		readings = append(readings, href(m, "self"))
	}
	must(c.Do(t.Context(), find(root, "capture"), map[string]string{"text": "Weighed in: [[Weight]] is down."}))
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
	page := must(c.Get(t.Context(), "/pages?title=2026-10-13"))
	for _, want := range []string{"Before breakfast.", `href="` + href(page, "self") + `"`, "Mentioned in"} {
		if !strings.Contains(body, want) {
			t.Errorf("the metric page does not show %s", want)
		}
	}

	all := must(c.Get(t.Context(), "/metrics/Weight"))
	for _, name := range []string{"save-body", "rename", "link", "unlink", "tombstone"} {
		if a := find(all, name); a.Name == "" || !strings.Contains(body, `<form method="POST" action="`+a.Href+`"`) {
			t.Errorf("the metric page has no form of %s: %+v", name, a)
		}
	}
	if n := len(all.Properties.(map[string]any)["readings"].([]any)); n != 3 {
		t.Errorf("the series reads %d readings with no from, want every one (3)", n)
	}
	if href(all, "self") != "/metrics/Weight" {
		t.Errorf("the series asked with no query names %s as itself", href(all, "self"))
	}
	window := must(c.Get(t.Context(), "/metrics/Weight?from=2026-01-01"))
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
	series := must(c.Get(t.Context(), "/metrics/Weight"))
	self := href(series, "page")

	req := httptest.NewRequest("GET", self, nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/metrics/Weight" {
		t.Fatalf("a browser at the metric's page: %d to %q, want 303 to /metrics/Weight", rec.Code, rec.Header().Get("Location"))
	}
	if p := must(c.Get(t.Context(), self)); p.Class[0] != "metric" || p.Properties.(map[string]any)["body"] != "Before breakfast." {
		t.Errorf("JSON at the metric's page is not the page: %v %v", p.Class, p.Properties)
	}

	// A save with no page to return to ends on the metric, with its toast.
	save := find(series, "save-body")
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
	must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/metrics/Weight")), "tombstone"), nil))
	body := browse(t, h, self)
	if !strings.Contains(body, "Deleted") || !strings.Contains(body, `action="`+self+`/revive"`) || strings.Contains(body, "<h2>Properties</h2>") {
		t.Error("a deleted metric's page is not the page view with Revive")
	}
}

func TestTheSeriesCarriesItsPage(t *testing.T) {
	c, _, _ := metricFixture(t)
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Body"}))
	series := must(c.Get(t.Context(), "/metrics/weight"))
	must(c.Do(t.Context(), find(series, "link"), map[string]string{"to": "Body", "kind": "part-of"}))

	series = must(c.Get(t.Context(), "/metrics/weight"))
	p := series.Properties.(map[string]any)
	if series.Title != "Weight" || p["unit"] != "kg" || p["body"] != "Before breakfast." || fmt.Sprint("/pages/", p["id"]) != href(series, "page") {
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
	link := find(series, "link")
	if link.Title != "Add a link" || !hasOption(link, "kind", "part-of") {
		t.Errorf("link is not Add a link with every kind: %+v", link)
	}
	for _, name := range []string{"save-body", "rename", "unlink", "tombstone"} {
		if find(series, name).Name == "" {
			t.Errorf("the series does not offer its page's %s: %s", name, names(series))
		}
	}

	must(c.Do(t.Context(), find(series, "rename"), map[string]string{"title": "Body weight"}))
	for _, path := range []string{"/metrics/Body%20weight", "/metrics/Weight"} {
		s := must(c.Get(t.Context(), path))
		if s.Title != "Body weight" || len(s.Properties.(map[string]any)["readings"].([]any)) != 3 {
			t.Errorf("%s after the rename: %q with %v", path, s.Title, s.Properties.(map[string]any)["readings"])
		}
	}
}

// TestAYearShowsItsNewestReadingsFirst: a year's fold shows its newest ten readings, and the rest of the year are in
// a closed fold under them that says how many it holds, so the page's other groups stay near; every reading is
// still on the page, newest first.
func TestAYearShowsItsNewestReadingsFirst(t *testing.T) {
	c, h := freshOn(t, "2026-10-13")
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "Weight", "unit": "kg"}))
	for d := 1; d <= 15; d++ {
		must(c.Do(t.Context(), find(root, "record"), map[string]string{"metric": "Weight", "day": fmt.Sprintf("2026-09-%02d", d), "value": fmt.Sprint(70 + d)}))
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
	c, h := freshOn(t, "2026-10-13")
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "Weight", "unit": "kg"}))
	for d := 1; d <= 11; d++ {
		must(c.Do(t.Context(), find(root, "record"), map[string]string{"metric": "Weight", "day": fmt.Sprintf("2026-09-%02d", d), "value": fmt.Sprint(70 + d)}))
	}
	body := browse(t, h, "/metrics/Weight")
	if want := `<details class="readings-more"><summary>1 more reading of 2026</summary>`; !strings.Contains(body, want) {
		t.Errorf("the fold of one older reading is not %s", want)
	}
}
