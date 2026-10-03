package api_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
)

func fresh(t *testing.T) (*client.Client, http.Handler) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(p); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	h := api.New(&core.Store{DB: d}, nil)
	return client.InProcess(h, "cli"), h
}

func names(e *api.Entity) string {
	var out []string
	for _, a := range e.Actions {
		out = append(out, a.Name)
	}
	return strings.Join(out, ",")
}

func find(e *api.Entity, name string) api.Action {
	for _, a := range e.Actions {
		if a.Name == name {
			return a
		}
	}
	return api.Action{}
}

func TestActionsAreOfferedOnlyWhereLegal(t *testing.T) {
	c, _ := fresh(t)
	day, err := c.Do(find(must(c.Get("/")), "capture"), map[string]string{"text": "with [[Ana]]"})
	if err != nil {
		t.Fatal(err)
	}
	dayPage := must(c.Get(href(day, "page")))
	if got := names(dayPage); got != "save-body,link,unlink,tombstone" {
		t.Errorf("a day page offers %s (never promote)", got)
	}
	if !hasOption(find(dayPage, "link"), "kind", "at") {
		t.Error("a day page does not offer an at link")
	}
	ana := must(c.Get(dayPage.Entities[0].Href))
	if got := names(ana); got != "save-body,promote,rename,link,unlink,tombstone" {
		t.Errorf("a plain page offers %s", got)
	}
	if hasOption(find(ana, "link"), "kind", "at") {
		t.Error("a plain page offers an at link (D16: from a day page only)")
	}
	ana = must(c.Do(find(ana, "promote"), map[string]string{"to": "person"}))
	if got := names(ana); strings.Contains(got, "promote") || hasOption(find(ana, "link"), "kind", "located-in") {
		t.Errorf("a person offers %s with kinds %v", got, find(ana, "link").Fields)
	}
	gone := must(c.Do(find(ana, "tombstone"), nil))
	if got := names(gone); got != "revive" {
		t.Errorf("a tombstoned page offers %s", got)
	}
}

func TestSaveCarriesItsVersion(t *testing.T) {
	c, _ := fresh(t)
	p := must(c.Do(find(must(c.Get("/")), "create-page"), map[string]string{"title": "Diet", "body": "v1"}))
	save := find(p, "save-body")
	p2 := must(c.Do(save, map[string]string{"body": "v2 [[Plan]]"}))
	if p2.Result == nil {
		t.Error("a save reports no result")
	}
	_, err := c.Do(save, map[string]string{"body": "v3"}) // the old form: its version is stale now
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != 409 {
		t.Errorf("a stale save: %v", err)
	}
}

func TestExistingTitleLinksToThePage(t *testing.T) {
	c, _ := fresh(t)
	create := find(must(c.Get("/")), "create-page")
	c.Do(create, map[string]string{"title": "Café"})
	e, err := c.Do(create, map[string]string{"title": "CAFÉ"})
	if err == nil || href(e, "existing") == "" {
		t.Errorf("a duplicate title: %v, links %v", err, e.Links)
	}
}

func TestBrowserGetsHTMLAndSourceUI(t *testing.T) {
	c, h := fresh(t)
	req := httptest.NewRequest("POST", "/days/2026-09-29/capture", strings.NewReader("text=hello"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/days/2026-09-29" {
		t.Errorf("a browser form is answered %d, Location %q: post, then redirect", rec.Code, rec.Header().Get("Location"))
	}
	if body := browse(t, h, "/days/2026-09-29"); !strings.Contains(body, `<form method="POST" action="/days/2026-09-29/capture">`) {
		t.Errorf("HTML: %.300s", body)
	}
	q := must(c.Do(find(must(c.Get("/")), "query"), map[string]string{"sql": "SELECT source FROM entities"}))
	if !strings.Contains(fmtRows(q), "ui") {
		t.Errorf("a browser form wrote as %s", fmtRows(q))
	}
}

func fmtRows(e *api.Entity) string {
	p, _ := e.Properties.(map[string]any)
	rows, _ := p["rows"].([]any)
	var out []string
	for _, r := range rows {
		out = append(out, r.([]any)[0].(string))
	}
	return strings.Join(out, ",")
}

func must(e *api.Entity, err error) *api.Entity {
	if err != nil {
		panic(err)
	}
	return e
}

func href(e *api.Entity, rel string) string {
	for _, l := range e.Links {
		if l.Rel[0] == rel {
			return l.Href
		}
	}
	return ""
}

func hasOption(a api.Action, field, opt string) bool {
	for _, f := range a.Fields {
		if f.Name == field {
			for _, o := range f.Options {
				if o == opt {
					return true
				}
			}
		}
	}
	return false
}

func TestViewsOfferTheMetricsInUse(t *testing.T) {
	c, _ := fresh(t)
	root := must(c.Get("/"))
	for _, m := range []map[string]string{{"name": "weight", "unit": "kg"}, {"name": "ldl", "unit": "mg/dL"}} {
		must(c.Do(find(root, "register-metric"), m))
	}
	day := "2026-06-01"
	for _, r := range []map[string]string{
		{"metric": "weight", "day": "2026-05-20", "value": "71.5"},
		{"metric": "ldl", "day": "2025-12-01", "value": "96"}, // a lab marker, read twice a year
	} {
		must(c.Do(find(root, "record"), r))
	}
	dayView := must(c.Get("/days/" + day))
	rec := find(dayView, "record")
	if !hasOption(rec, "metric", "weight") || hasOption(rec, "metric", "ldl") || hasOption(rec, "metric", "Mood") {
		t.Errorf("the day view offers %v: only what was read in its last 60 days", rec.Fields[0].Options)
	}
	if href(dayView, "metrics") != "/metrics" {
		t.Error("the day view does not link the whole registry")
	}
	if all := find(must(c.Get("/metrics")), "record"); !hasOption(all, "metric", "ldl") || !hasOption(all, "metric", "Mood") {
		t.Errorf("/metrics offers %v: every registered metric", all.Fields[0].Options)
	}
	if ldl := find(must(c.Get("/metrics/ldl")), "record"); !hasOption(ldl, "metric", "ldl") || ldl.Fields[0].Value != "ldl" {
		t.Errorf("a series offers %v with %v: its own metric, selected", ldl.Fields[0].Options, ldl.Fields[0].Value)
	}
	must(c.Do(find(dayView, "record"), map[string]string{"metric": "ldl", "day": day, "value": "92"}))
	if !hasOption(find(must(c.Get("/days/"+day)), "record"), "metric", "ldl") {
		t.Error("a metric read on the day is not offered")
	}
}

func TestMetricsAreGroupedByCategory(t *testing.T) {
	c, h := fresh(t)
	root := must(c.Get("/"))
	for _, m := range []map[string]string{{"name": "TSH", "unit": "µUI/mL", "note": "thyroid-stimulating hormone"}, {"name": "Weight", "unit": "kg"},
		{"name": "Walk"}, {"name": "Steps", "unit": "n"}} {
		must(c.Do(find(root, "register-metric"), m))
	}
	for _, p := range []string{"Biomarkers", "Thyroid", "Body"} {
		must(c.Do(find(root, "create-page"), map[string]string{"title": p}))
	}
	thyroid := must(c.Get("/pages?title=Thyroid"))
	must(c.Do(find(thyroid, "link"), map[string]string{"to": "Biomarkers", "kind": "part-of"}))
	tsh := must(c.Get("/metrics/tsh"))
	if href(tsh, "page") == "" || find(tsh, "link").Title != "File in category" {
		t.Fatalf("a series links its metric's page and offers to file it: %v", names(tsh))
	}
	must(c.Do(find(tsh, "link"), map[string]string{"to": "Thyroid", "kind": "part-of"}))
	must(c.Do(find(must(c.Get("/metrics/Weight")), "link"), map[string]string{"to": "Body", "kind": "part-of"}))
	walk := must(c.Get("/metrics/Walk"))
	must(c.Do(find(walk, "link"), map[string]string{"to": "Body", "kind": "part-of"}))
	must(c.Do(find(walk, "start-habit"), map[string]string{"start_day": "2026-09-20"}))
	if _, err := c.Do(find(walk, "link"), map[string]string{"to": "Nowhere", "kind": "part-of"}); err == nil {
		t.Error("a metric is filed in a category page that does not exist")
	}

	var got []string
	for _, g := range must(c.Get("/metrics")).Properties.(map[string]any)["groups"].([]any) {
		g := g.(map[string]any)
		var ms []string
		for _, m := range g["metrics"].([]any) {
			ms = append(ms, m.(map[string]any)["name"].(string))
		}
		got = append(got, g["title"].(string)+":"+strings.Join(ms, ","))
	}
	if want := "Habits:Walk Biomarkers: Thyroid:TSH Body:Weight Not filed:Mood,Steps"; strings.Join(got, " ") != want {
		t.Errorf("groups %q, want %q: habits first, each category in tree order with those under it, the rest last", strings.Join(got, " "), want)
	}
	page := browse(t, h, "/metrics")
	for _, s := range []string{"<h2>Habits", `<span class="badge">Body</span>`, ">Thyroid</a>", "Biomarkers/Thyroid", "<h2>Not filed", "thyroid-stimulating hormone"} {
		if !strings.Contains(page, s) {
			t.Errorf("/metrics lacks %q", s)
		}
	}
	if s := browse(t, h, "/metrics/TSH"); !strings.Contains(s, `<span class="badge">Biomarkers/Thyroid</span>`) {
		t.Error("a series does not show its category")
	}
	if s := browse(t, h, href(tsh, "page")); !strings.Contains(s, "thyroid-stimulating hormone") {
		t.Error("a metric's page does not hold its note")
	}
}
