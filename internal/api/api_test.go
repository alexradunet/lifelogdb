package api_test

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/importer"
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

func freshWorkspace(t *testing.T) (*client.Client, http.Handler) {
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
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Notebook"), 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := importer.Open(filepath.Join(root, "Notebook.lifelog"))
	if err != nil {
		t.Fatal(err)
	}
	h := api.New(&core.Store{DB: d}, ws)
	return client.InProcess(h, "cli"), h
}

func TestCatalogNamesAreUnique(t *testing.T) {
	for _, tc := range []struct {
		name      string
		client    func(*testing.T) *client.Client
		workspace bool
	}{
		{"ordinary", func(t *testing.T) *client.Client { c, _ := fresh(t); return c }, false},
		{"workspace", func(t *testing.T) *client.Client { c, _ := freshWorkspace(t); return c }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actions, err := tc.client(t).Catalog()
			if err != nil {
				t.Fatal(err)
			}
			seen, normalized := map[string]api.Action{}, map[string]api.Action{}
			for _, a := range actions {
				if prev, ok := seen[a.Name]; ok {
					t.Errorf("catalog action name %q is used by both %s and %s", a.Name, prev.Href, a.Href)
				}
				seen[a.Name] = a
				tool := strings.ReplaceAll(a.Name, "-", "_")
				if prev, ok := normalized[tool]; ok {
					t.Errorf("MCP tool name %q is used by both actions %q and %q", tool, prev.Name, a.Name)
				}
				normalized[tool] = a
			}
			page := seen["find"]
			if page.Href != "/pages" || len(page.Fields) != 1 || page.Fields[0].Name != "title" {
				t.Errorf("page find = %+v, want /pages with title", page)
			}
			importFind, ok := seen["import-find"]
			if tc.workspace {
				if !ok || importFind.Href != "/import/find" || len(importFind.Fields) != 1 || importFind.Fields[0].Name != "text" {
					t.Errorf("import-find = %+v, present %v; want /import/find with text", importFind, ok)
				}
			} else if ok {
				t.Errorf("ordinary catalog includes workspace-only %q", importFind.Name)
			}
		})
	}
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
	if got := names(ana); got != "save-body,promote,promote-period,rename,link,unlink,tombstone" {
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

func TestCorrectionDomainActions(t *testing.T) {
	c, _ := fresh(t)
	catalog, err := c.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	actions := &api.Entity{Actions: catalog}
	record := find(actions, "record")
	correct := find(actions, "correct")
	retract := find(actions, "retract")

	mood := must(c.Do(record, map[string]string{"metric": "Mood", "day": "2026-10-01", "value": "3"}))
	if _, err := c.Do(correct, map[string]string{"id": measurementID(mood), "value": "2.5"}); !clientStatus(err, 422) {
		t.Fatalf("fractional mood correction: %v, want 422", err)
	}
	if _, err := c.Do(correct, map[string]string{"id": measurementID(mood), "value": "5"}); err != nil {
		t.Fatalf("valid mood correction: %v", err)
	}

	if _, err := c.Do(find(actions, "register-metric"), map[string]string{"name": "api_walk"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Do(find(actions, "start-habit"), map[string]string{"name": "api_walk", "start_day": "2026-10-01"}); err != nil {
		t.Fatal(err)
	}
	habit := must(c.Do(record, map[string]string{"metric": "api_walk", "day": "2026-10-01", "value": "1"}))
	if _, err := c.Do(correct, map[string]string{"id": measurementID(habit), "value": "2"}); !clientStatus(err, 422) {
		t.Fatalf("non-binary habit correction: %v, want 422", err)
	}
	if _, err := c.Do(retract, map[string]string{"id": measurementID(habit)}); err != nil {
		t.Fatalf("habit retraction: %v", err)
	}
}

func clientStatus(err error, want int) bool {
	var ce *client.Error
	return errors.As(err, &ce) && ce.Status == want
}

func measurementID(e *api.Entity) string {
	return strings.TrimPrefix(href(e, "self"), "/measurements/")
}

func TestBrowserGetsHTMLAndSourceUI(t *testing.T) {
	c, h := fresh(t)
	req := httptest.NewRequest("POST", "/days/2026-09-29/capture", strings.NewReader("text=hello"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || !feedbackLocation(rec.Header().Get("Location"), "/days/2026-09-29") {
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

func TestQueryBoundaryIsReportedAs422(t *testing.T) {
	c, _ := fresh(t)
	query := find(must(c.Get("/")), "query")
	if _, err := c.Do(query, map[string]string{"sql": "SELECT 1; SELECT 2"}); !clientStatus(err, 422) {
		t.Fatalf("script query error = %v, want 422", err)
	}
	if got := fmtRows(must(c.Do(query, map[string]string{"sql": "SELECT ';' AS semi"}))); got != ";" {
		t.Fatalf("literal semicolon query rows = %q, want ;", got)
	}
}

func TestBrowserOriginProtection(t *testing.T) {
	c, h := fresh(t)
	postForm := func(path string, vals url.Values, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("POST", "http://lifelog.local"+path, strings.NewReader(vals.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	sameOrigin := postForm("/days/2026-10-10/capture", url.Values{"text": {"same origin"}}, map[string]string{
		"Accept":         "text/html,application/xhtml+xml",
		"Origin":         "http://lifelog.local",
		"Sec-Fetch-Site": "same-origin",
	})
	if sameOrigin.Code != http.StatusSeeOther || !feedbackLocation(sameOrigin.Header().Get("Location"), "/days/2026-10-10") {
		t.Fatalf("same-origin browser form got %d, Location %q", sameOrigin.Code, sameOrigin.Header().Get("Location"))
	}

	headerless := postForm("/days/2026-10-11/capture", url.Values{"text": {"cli"}}, map[string]string{
		"Accept": "application/vnd.siren+json",
	})
	if headerless.Code != http.StatusOK {
		t.Fatalf("headerless in-process-style POST got %d: %.200s", headerless.Code, headerless.Body.String())
	}

	for _, method := range []string{"GET", "HEAD"} {
		req := httptest.NewRequest(method, "http://lifelog.local/", nil)
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("safe cross-origin %s got %d", method, rec.Code)
		}
	}

	denied := []struct {
		name    string
		path    string
		vals    url.Values
		headers map[string]string
	}{
		{
			name: "cross-site day capture",
			path: "/days/2026-10-12/capture",
			vals: url.Values{"text": {"bad"}},
			headers: map[string]string{
				"Accept":         "application/vnd.siren+json",
				"Origin":         "https://evil.example",
				"Sec-Fetch-Site": "cross-site",
			},
		},
		{
			name: "cross-site measurement",
			path: "/measurements",
			vals: url.Values{"metric": {"Mood"}, "day": {"2026-10-13"}, "value": {"4"}},
			headers: map[string]string{
				"Accept":         "application/vnd.siren+json",
				"Origin":         "https://evil.example",
				"Sec-Fetch-Site": "cross-site",
			},
		},
		{
			name: "malformed origin day capture",
			path: "/days/2026-10-14/capture",
			vals: url.Values{"text": {"bad origin"}},
			headers: map[string]string{
				"Accept": "application/vnd.siren+json",
				"Origin": "http://[::1",
			},
		},
	}
	for _, tc := range denied {
		rec := postForm(tc.path, tc.vals, tc.headers)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s got %d, want 403: %.200s", tc.name, rec.Code, rec.Body.String())
		}
	}

	root := must(c.Get("/"))
	if got := fmtRows(must(c.Do(find(root, "query"), map[string]string{"sql": "SELECT CAST(count(*) AS text) FROM entity_names WHERE title IN ('2026-10-10','2026-10-11')"}))); got != "2" {
		t.Errorf("allowed day pages count = %s, want 2", got)
	}
	if got := fmtRows(must(c.Do(find(root, "query"), map[string]string{"sql": "SELECT CAST(count(*) AS text) FROM entity_names WHERE title IN ('2026-10-12','2026-10-14')"}))); got != "0" {
		t.Errorf("denied day pages count = %s, want 0", got)
	}
	if got := fmtRows(must(c.Do(find(root, "query"), map[string]string{"sql": "SELECT CAST(count(*) AS text) FROM measurements WHERE day='2026-10-13'"}))); got != "0" {
		t.Errorf("denied readings count = %s, want 0", got)
	}

	importHandler, target := replayFixture(t)
	rec := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://lifelog.local/import/replay", strings.NewReader(url.Values{"to": {target}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/vnd.siren+json")
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		rec := httptest.NewRecorder()
		importHandler.ServeHTTP(rec, req)
		return rec
	}()
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-site import replay got %d, want 403: %.200s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("denied import replay target exists or could not be checked: %v", err)
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

func TestTombstonedHabits(t *testing.T) {
	c, _ := fresh(t)
	root := must(c.Get("/"))
	must(c.Do(find(root, "register-metric"), map[string]string{"name": "Walk"}))
	walk := must(c.Get("/metrics/Walk"))
	must(c.Do(find(walk, "start-habit"), map[string]string{"start_day": "2026-09-01"}))
	page := must(c.Get("/pages?title=Walk"))
	for i, hidden := range []bool{false, true, false} {
		if hidden {
			page = must(c.Do(find(page, "tombstone"), nil))
		} else if i > 0 {
			page = must(c.Do(find(page, "revive"), nil))
		}
		h := must(c.Get("/habits?day=2026-09-01&from=2026-09-01&to=2026-09-02"))
		if hidden {
			if find(h, "check-in").Name != "" || len(h.Entities) != 0 {
				t.Errorf("hidden check-in offered: %+v", h)
			}
		} else if find(h, "check-in").Name == "" || len(h.Entities) != 1 {
			t.Errorf("live check-in missing: %+v", h)
		}
	}
}

func TestMetricCanonicalActions(t *testing.T) {
	for _, tc := range []struct{ stored, equivalent, unit, state string }{
		{"Café", "Cafe\u0301", "", "unitless"},
		{"Straße", "STRASSE", "", "unitless"},
		{"Café mass", "Cafe\u0301 mass", "kg", "unitful"},
		{"Straße mass", "STRASSE mass", "kg", "unitful"},
		{"Café habit", "Cafe\u0301 habit", "", "habit"},
		{"Straße habit", "STRASSE habit", "", "habit"},
		{"Café hidden", "Cafe\u0301 hidden", "", "deleted"},
		{"Straße hidden", "STRASSE hidden", "", "deleted"},
		{"Café absent", "Cafe\u0301 absent", "", "unknown"},
		{"Straße absent", "STRASSE absent", "", "unknown"},
		{"ASCII metric", "ascii METRIC", "", "unitless"},
	} {
		t.Run(tc.stored, func(t *testing.T) {
			c, _ := fresh(t)
			if tc.state != "unknown" {
				must(c.Do(find(must(c.Get("/")), "register-metric"), map[string]string{"name": tc.stored, "unit": tc.unit}))
				if tc.state == "habit" {
					must(c.Do(find(must(c.Get("/metrics/"+url.PathEscape(tc.stored))), "start-habit"), map[string]string{"start_day": "2026-01-01"}))
				}
				if tc.state == "deleted" {
					must(c.Do(find(must(c.Get("/pages?title="+url.QueryEscape(tc.stored))), "tombstone"), nil))
				}
			}
			stored := must(c.Get("/metrics/" + url.PathEscape(tc.stored)))
			equivalent := must(c.Get("/metrics/" + url.PathEscape(tc.equivalent)))
			if names(stored) != names(equivalent) {
				t.Errorf("actions: stored %s, equivalent %s", names(stored), names(equivalent))
			}
			if href(stored, "page") != href(equivalent, "page") {
				t.Errorf("page identity differs: %q vs %q", href(stored, "page"), href(equivalent, "page"))
			}
			for _, e := range []*api.Entity{stored, equivalent} {
				if href(e, "self") == "" {
					t.Error("missing self link")
				} else if names(must(c.Get(href(e, "self")))) != names(e) {
					t.Error("self link changes actions")
				}
				restricted := tc.state == "unknown" || tc.state == "deleted"
				if restricted && (href(e, "page") != "" || find(e, "link").Name != "" || find(e, "start-habit").Name != "" || find(e, "check-in").Name != "") {
					t.Errorf("restricted metric offers actions: %s", names(e))
				}
				if tc.unit != "" && find(e, "start-habit").Name != "" {
					t.Error("unitful metric offers habit")
				}
			}
			if tc.state == "unitless" {
				a := find(equivalent, "start-habit")
				if a.Name == "" {
					t.Fatal("equivalent spelling lacks start action")
				}
				must(c.Do(a, map[string]string{"start_day": "2026-01-01"}))
			}
			if tc.state == "habit" {
				a := find(equivalent, "check-in")
				if a.Name == "" {
					t.Fatal("equivalent spelling lacks check-in")
				}
				must(c.Do(a, map[string]string{"day": "2026-01-01", "done": "1"}))
			}
			if tc.state != "unknown" && tc.state != "deleted" {
				page := must(c.Get(href(equivalent, "page")))
				if page.Title != tc.stored {
					t.Errorf("stored spelling changed: %q", page.Title)
				}
			}
		})
	}
}

func TestRenamedIdentityRetainsActionsAndAliasWrites(t *testing.T) {
	c, _ := fresh(t)
	root := must(c.Get("/"))
	p := must(c.Do(find(root, "create-page"), map[string]string{"title": "Typo", "body": "prose"}))
	oldHref := href(p, "self")
	renamed := must(c.Do(find(p, "rename"), map[string]string{"title": "Correct"}))
	if href(renamed, "self") != oldHref {
		t.Fatal("rename changed id")
	}
	other := must(c.Do(find(root, "create-page"), map[string]string{"title": "Other", "body": "[[Typo]]"}))
	current := must(c.Get(oldHref))
	if current.Title != "Correct" || names(current) != "save-body,promote,promote-period,rename,link,unlink,tombstone" {
		t.Fatalf("renamed actions: %+v", current)
	}
	version := current.Properties.(map[string]any)["version"].(string)
	must(c.Do(find(current, "save-body"), map[string]string{"body": "#REDIRECT [[Other]]", "version": version}))
	must(c.Do(find(other, "link"), map[string]string{"to": "Typo", "kind": "related"}))
	must(c.Do(find(must(c.Get(oldHref)), "unlink"), map[string]string{"to": "Other", "kind": "related"}))
	alias := must(c.Get("/pages?title=Typo"))
	if href(alias, "self") != oldHref || alias.Title != "Correct" || alias.Properties.(map[string]any)["body"] != "#REDIRECT [[Other]]" {
		t.Fatalf("retained alias: %+v", alias)
	}
	if _, err := c.Do(find(alias, "link"), map[string]string{"to": "Other", "kind": "redirect"}); !clientStatus(err, 422) {
		t.Fatalf("removed kind: %v", err)
	}
}

func feedbackLocation(location, path string) bool {
	u, err := url.Parse(location)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Path != path || u.Fragment != "" {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	values := q["feedback"]
	if len(q) != 1 || len(values) != 1 || len(values[0]) != 64 {
		return false
	}
	_, err = hex.DecodeString(values[0])
	return err == nil
}

// Another writer's file can hold a daily habit check-in outside 0/1; every presentation of the habits labels
// that day invalid and counts it (cookbook/habits.md). The CLI and MCP serve this same JSON.
func TestHabitsShowInvalidCheckIns(t *testing.T) {
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
	c := client.InProcess(h, "cli")
	root := must(c.Get("/"))
	must(c.Do(find(root, "register-metric"), map[string]string{"name": "Walk"}))
	must(c.Do(find(must(c.Get("/metrics/Walk")), "start-habit"), map[string]string{"start_day": "2026-10-01"}))
	must(c.Do(find(root, "record"), map[string]string{"metric": "Walk", "day": "2026-10-02", "value": "1"}))
	if _, err := d.W.Exec(`INSERT INTO measurements(metric_id, day, value, created_at, source)
	                       SELECT entity_id, '2026-10-01', 2, '2026-10-07T00:00:00.000Z', 'other-writer'
	                         FROM entity_names WHERE title = 'Walk'`); err != nil {
		t.Fatal(err)
	}

	habits := must(c.Get("/habits?day=2026-10-01&from=2026-10-01&to=2026-10-02"))
	props := habits.Properties.(map[string]any)
	if got := props["habits"].([]any); len(got) != 1 || got[0].(map[string]any)["state"] != "invalid" {
		t.Errorf("habits of the day: %v", got)
	}
	comp := props["completion"].([]any)
	if len(comp) != 1 {
		t.Fatalf("completion: %v", comp)
	}
	for field, want := range map[string]string{"active_days": "2", "done": "1", "not_done": "0", "not_recorded": "0", "invalid": "1"} {
		if got := fmt.Sprint(comp[0].(map[string]any)[field]); got != want {
			t.Errorf("completion %s = %v, want %v", field, got, want)
		}
	}
	listed := false
	for _, v := range must(c.Get("/days/2026-10-01")).Properties.(map[string]any)["view"].([]any) {
		row := v.(map[string]any)
		listed = listed || row["what"] == "habit" && row["detail"] == "Walk: invalid"
	}
	if !listed {
		t.Error("the day view does not label the habit invalid")
	}
	body := browse(t, h, "/habits?day=2026-10-01&from=2026-10-01&to=2026-10-02")
	for _, w := range []string{`<span class="notdone">invalid</span>`, `<th scope="col" class="num">invalid</th>`} {
		if !strings.Contains(body, w) {
			t.Errorf("the habits page does not show %s", w)
		}
	}
}
