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
	if !strings.Contains(rec.Body.String(), `<form method="POST" action="/days/2026-09-29/capture">`) {
		t.Errorf("HTML: %.300s", rec.Body.String())
	}
	if rec.Header().Get("Location") != "/days/2026-09-29" {
		t.Errorf("Location %q", rec.Header().Get("Location"))
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
	if !hasOption(rec, "metric", "weight") || hasOption(rec, "metric", "ldl") || hasOption(rec, "metric", "mood") {
		t.Errorf("the day view offers %v: only what was read in its last 60 days", rec.Fields[0].Options)
	}
	if href(dayView, "metrics") != "/metrics" {
		t.Error("the day view does not link the whole registry")
	}
	if all := find(must(c.Get("/metrics")), "record"); !hasOption(all, "metric", "ldl") || !hasOption(all, "metric", "mood") {
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
