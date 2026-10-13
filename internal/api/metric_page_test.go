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
	for _, want := range []string{"Before breakfast.", `href="` + href(page, "self") + `"`, "Mentioned in",
		`data-action="save-body"`, `data-action="rename"`, `data-action="link"`, `data-action="unlink"`, `data-action="tombstone"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the metric page does not show %s", want)
		}
	}

	all := must(c.Get(t.Context(), "/metrics/Weight"))
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
	must(c.Do(t.Context(), find(series, "link"), map[string]string{"to": "Body"}))

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
	if link.Title != "File in category" || !hasOption(link, "kind", "related") {
		t.Errorf("link is not File in category with the other kinds: %+v", link)
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

func TestAHabitsPageLeadsWithItsCheckIn(t *testing.T) {
	c, h := freshOn(t, "2026-10-13")
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "Walk"}))
	must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/metrics/Walk")), "start-habit"), map[string]string{"start_day": "2026-10-01"}))
	walk := must(c.Get(t.Context(), "/metrics/Walk"))
	for day, done := range map[string]string{"2026-10-11": "0", "2026-10-12": "1"} {
		must(c.Do(t.Context(), find(walk, "check-in"), map[string]string{"day": day, "done": done}))
	}
	body := browse(t, h, "/metrics/Walk")

	forms := postForms(body)
	if len(forms) < 2 || forms[0].Button != "Done" || forms[1].Button != "Not done" ||
		forms[0].Action != "/metrics/Walk/check-in" || forms[0].Fields.Get("day") != "2026-10-13" || forms[0].Fields.Get("done") != "1" {
		t.Fatalf("a habit's page does not lead with today's check-in: %+v", forms)
	}
	for _, want := range []string{`<span class="kind">habit</span>`, `<td class="num"><a href="/measurements/`, ">done</a>", ">not done</a>"} {
		if !strings.Contains(body, want) {
			t.Errorf("a habit's page does not show %s", want)
		}
	}
	if strings.Contains(body, `<svg class="chart"`) {
		t.Error("a habit is charted as a line of 0 and 1")
	}
	more := strings.Index(body, `aria-label="More"`)
	if rec := strings.Index(body, `data-action="record"`); more < 0 || rec < more {
		t.Errorf("a habit's Record is not under More (more at %d, record at %d)", more, rec)
	}
}
