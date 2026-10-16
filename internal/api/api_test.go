package api_test

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
)

func fresh(t *testing.T) (*client.Client, http.Handler) {
	t.Helper()
	c, h, _ := freshStore(t)
	return c, h
}

// freshStore is fresh with the store too: a test reads a row straight from the file, as a probe of what a surface wrote.
func freshStore(t *testing.T) (*client.Client, http.Handler, *core.Store) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s := &core.Store{DB: d}
	h := api.New(s)
	return client.InProcess(h, "cli"), h, s
}

// scalar reads one text value from the file: a probe, never the behaviour under test.
func scalar(t *testing.T, s *core.Store, query string) string {
	t.Helper()
	var v string
	if err := s.DB.R.QueryRowContext(t.Context(), query).Scan(&v); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return v
}

// freshOn is fresh with the server's local day fixed: a test of today, or of the words for a day, uses fixed dates.
func freshOn(t *testing.T, day string) (*client.Client, http.Handler) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	h := api.NewOn(&core.Store{DB: d}, func() string { return day }, time.UTC)
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

func TestCatalogNamesAreUnique(t *testing.T) {
	c, _ := fresh(t)
	actions, err := c.Catalog(t.Context())
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
}

func TestActionsAreOfferedOnlyWhereLegal(t *testing.T) {
	c, _ := fresh(t)
	day, err := c.Do(t.Context(), find(must(c.Get(t.Context(), "/")), "capture"), map[string]string{"text": "with [[Ana]]"})
	if err != nil {
		t.Fatal(err)
	}
	dayPage := must(c.Get(t.Context(), href(day, "page")))
	if got := names(dayPage); got != "save-body,link,unlink,tombstone" {
		t.Errorf("a day page offers %s (never promote)", got)
	}
	if !hasOption(find(dayPage, "link"), "kind", "at") {
		t.Error("a day page does not offer an at link")
	}
	if hasOption(find(dayPage, "link"), "kind", "part-of") {
		t.Error("a day page offers a part-of link (D26: a day page is never filed)")
	}
	ana := must(c.Get(t.Context(), dayPage.Entities[0].Href))
	if got := names(ana); got != "save-body,promote,rename,link,unlink,file-pages,tombstone" {
		t.Errorf("a plain page offers %s", got)
	}
	if hasOption(find(ana, "link"), "kind", "at") {
		t.Error("a plain page offers an at link (D16: from a day page only)")
	}
	ana = must(c.Do(t.Context(), find(ana, "promote"), map[string]string{"to": "person"}))
	if got := names(ana); strings.Contains(got, "promote") || hasOption(find(ana, "link"), "kind", "at") {
		t.Errorf("a person offers %s with kinds %v", got, find(ana, "link").Fields)
	}
	gone := must(c.Do(t.Context(), find(ana, "tombstone"), nil))
	if got := names(gone); got != "revive" {
		t.Errorf("a tombstoned page offers %s", got)
	}
}

func TestSaveCarriesItsVersion(t *testing.T) {
	c, _ := fresh(t)
	p := must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/")), "create-page"), map[string]string{"title": "Diet", "body": "v1"}))
	save := find(p, "save-body")
	p2 := must(c.Do(t.Context(), save, map[string]string{"body": "v2 [[Plan]]"}))
	if p2.Result == nil {
		t.Error("a save reports no result")
	}
	_, err := c.Do(t.Context(), save, map[string]string{"body": "v3"}) // the old form: its version is stale now
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != 409 {
		t.Errorf("a stale save: %v", err)
	}
}

func TestExistingTitleLinksToThePage(t *testing.T) {
	c, _ := fresh(t)
	create := find(must(c.Get(t.Context(), "/")), "create-page")
	c.Do(t.Context(), create, map[string]string{"title": "Café"})
	e, err := c.Do(t.Context(), create, map[string]string{"title": "CAFÉ"})
	if err == nil || href(e, "existing") == "" {
		t.Errorf("a duplicate title: %v, links %v", err, e.Links)
	}
}

// A link from an id that no page has is a missing row (404), not the foreign key's refusal (422).
func TestLinkFromAMissingPageIsNotFound(t *testing.T) {
	c, _ := fresh(t)
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Target"}))
	link := find(must(c.Get(t.Context(), "/actions")), "link") // the templated action: the id is a path field
	_, err := c.Do(t.Context(), link, map[string]string{"id": "999999", "to": "Target", "kind": "part-of"})
	if code, status := codeOf(t, err); code != "not_found" || status != 404 {
		t.Errorf("link from a missing page: code=%q status=%d (%v)", code, status, err)
	}
}

func TestCorrectionDomainActions(t *testing.T) {
	c, _ := fresh(t)
	catalog, err := c.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	actions := &api.Entity{Actions: catalog}
	record := find(actions, "record")
	correct := find(actions, "correct")
	retract := find(actions, "retract")

	mood := must(c.Do(t.Context(), record, map[string]string{"metric": "Mood", "day": "2026-10-01", "value": "3"}))
	if _, err := c.Do(t.Context(), correct, map[string]string{"id": measurementID(mood), "value": "2.5"}); !clientStatus(err, 422) {
		t.Fatalf("fractional mood correction: %v, want 422", err)
	}
	if _, err := c.Do(t.Context(), correct, map[string]string{"id": measurementID(mood), "value": "5"}); err != nil {
		t.Fatalf("valid mood correction: %v", err)
	}

	if _, err := c.Do(t.Context(), find(actions, "register-metric"), map[string]string{"name": "api_walk"}); err != nil {
		t.Fatal(err)
	}
	habit := must(c.Do(t.Context(), record, map[string]string{"metric": "api_walk", "day": "2026-10-01", "value": "1"}))
	if _, err := c.Do(t.Context(), retract, map[string]string{"id": measurementID(habit)}); err != nil {
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
	_, h, s := freshStore(t)
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
	if got := scalar(t, s, "SELECT CAST(count(*) AS text) FROM entities WHERE source = 'ui'"); got != "1" {
		t.Errorf("rows a browser form wrote as ui: %s, want 1", got)
	}
}

func TestBrowserOriginProtection(t *testing.T) {
	_, h, s := freshStore(t)
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

	if got := scalar(t, s, "SELECT CAST(count(*) AS text) FROM entity_names WHERE title IN ('2026-10-10','2026-10-11')"); got != "2" {
		t.Errorf("allowed day pages count = %s, want 2", got)
	}
	if got := scalar(t, s, "SELECT CAST(count(*) AS text) FROM entity_names WHERE title IN ('2026-10-12','2026-10-14')"); got != "0" {
		t.Errorf("denied day pages count = %s, want 0", got)
	}
	if got := scalar(t, s, "SELECT CAST(count(*) AS text) FROM measurements WHERE day='2026-10-13'"); got != "0" {
		t.Errorf("denied readings count = %s, want 0", got)
	}

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
	root := must(c.Get(t.Context(), "/"))
	for _, m := range []map[string]string{{"name": "weight", "unit": "kg"}, {"name": "ldl", "unit": "mg/dL"}} {
		must(c.Do(t.Context(), find(root, "register-metric"), m))
	}
	day := "2026-06-01"
	for _, r := range []map[string]string{
		{"metric": "weight", "day": "2026-05-20", "value": "71.5"},
		{"metric": "ldl", "day": "2025-12-01", "value": "96"}, // a lab marker, read twice a year
	} {
		must(c.Do(t.Context(), find(root, "record"), r))
	}
	dayView := must(c.Get(t.Context(), "/days/"+day))
	rec := find(dayView, "record")
	if !hasOption(rec, "metric", "weight") || hasOption(rec, "metric", "ldl") || hasOption(rec, "metric", "Mood") {
		t.Errorf("the day view offers %v: only what was read in its last 60 days", rec.Fields[0].Options)
	}
	if href(dayView, "metrics") != "/metrics" {
		t.Error("the day view does not link the whole registry")
	}
	if all := find(must(c.Get(t.Context(), "/metrics")), "record"); !hasOption(all, "metric", "ldl") || !hasOption(all, "metric", "Mood") {
		t.Errorf("/metrics offers %v: every registered metric", all.Fields[0].Options)
	}
	if ldl := find(must(c.Get(t.Context(), "/metrics/ldl")), "record"); !hasOption(ldl, "metric", "ldl") || ldl.Fields[0].Value != "ldl" {
		t.Errorf("a series offers %v with %v: its own metric, selected", ldl.Fields[0].Options, ldl.Fields[0].Value)
	}
	must(c.Do(t.Context(), find(dayView, "record"), map[string]string{"metric": "ldl", "day": day, "value": "92"}))
	if !hasOption(find(must(c.Get(t.Context(), "/days/"+day)), "record"), "metric", "ldl") {
		t.Error("a metric read on the day is not offered")
	}
}

func TestMetricsAreGroupedByCategory(t *testing.T) {
	c, h := fresh(t)
	root := must(c.Get(t.Context(), "/"))
	for _, m := range []map[string]string{{"name": "TSH", "unit": "µUI/mL", "note": "thyroid-stimulating hormone"}, {"name": "Weight", "unit": "kg"},
		{"name": "Walk"}, {"name": "Steps", "unit": "n"}} {
		must(c.Do(t.Context(), find(root, "register-metric"), m))
	}
	for _, p := range []string{"Biomarkers", "Thyroid", "Body"} {
		must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": p}))
	}
	thyroid := must(c.Get(t.Context(), "/pages?title=Thyroid"))
	must(c.Do(t.Context(), find(thyroid, "link"), map[string]string{"to": "Biomarkers", "kind": "part-of"}))
	tsh := must(c.Get(t.Context(), "/metrics/tsh"))
	if href(tsh, "page") == "" || find(tsh, "file-pages").Name == "" || find(tsh, "link").Title != "Add a link" {
		t.Fatalf("a series links its metric's page and offers to file it: %v", names(tsh))
	}
	must(c.Do(t.Context(), find(tsh, "link"), map[string]string{"to": "Thyroid", "kind": "part-of"}))
	must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/metrics/Weight")), "link"), map[string]string{"to": "Body", "kind": "part-of"}))
	walk := must(c.Get(t.Context(), "/metrics/Walk"))
	must(c.Do(t.Context(), find(walk, "link"), map[string]string{"to": "Body", "kind": "part-of"}))
	if _, err := c.Do(t.Context(), find(walk, "link"), map[string]string{"to": "Nowhere", "kind": "part-of"}); err == nil {
		t.Error("a metric is filed in a category page that does not exist")
	}

	var got []string
	for _, g := range must(c.Get(t.Context(), "/metrics")).Properties.(map[string]any)["groups"].([]any) {
		g := g.(map[string]any)
		var ms []string
		for _, m := range g["metrics"].([]any) {
			ms = append(ms, m.(map[string]any)["name"].(string))
		}
		got = append(got, g["title"].(string)+":"+strings.Join(ms, ","))
	}
	if want := "Biomarkers: Thyroid:TSH Body:Walk,Weight Not filed:Mood,Steps"; strings.Join(got, " ") != want {
		t.Errorf("groups %q, want %q: each category in tree order with those under it, the rest last", strings.Join(got, " "), want)
	}
	page := browse(t, h, "/metrics")
	for _, s := range []string{"<h3>Thyroid <small>1</small></h3>", "Biomarkers/Thyroid", "<h2>Not filed", `title="thyroid-stimulating hormone"`} {
		if !strings.Contains(page, s) {
			t.Errorf("/metrics lacks %q", s)
		}
	}
	// The metric's page opens this one in a browser (TestTheBrowserOpensAMetricsPageAtItsMetric), so it holds the note.
	s := browse(t, h, "/metrics/TSH")
	if !regexp.MustCompile(`<span class="badge category"><a href="/pages/\d+">Thyroid</a><form class="tiny" method="POST" action="/pages/\d+/unlink"`).MatchString(s) {
		t.Error("a series does not show its category as a badge with its x")
	}
	if !strings.Contains(s, "thyroid-stimulating hormone") {
		t.Error("a metric's one page does not hold its note")
	}
}

func TestMetricCanonicalActions(t *testing.T) {
	for _, tc := range []struct{ stored, equivalent, unit, state string }{
		{"Café", "Cafe\u0301", "", "unitless"},
		{"Straße", "STRASSE", "", "unitless"},
		{"Café mass", "Cafe\u0301 mass", "kg", "unitful"},
		{"Straße mass", "STRASSE mass", "kg", "unitful"},
		{"Café hidden", "Cafe\u0301 hidden", "", "deleted"},
		{"Straße hidden", "STRASSE hidden", "", "deleted"},
		{"Café absent", "Cafe\u0301 absent", "", "unknown"},
		{"Straße absent", "STRASSE absent", "", "unknown"},
		{"ASCII metric", "ascii METRIC", "", "unitless"},
	} {
		t.Run(tc.stored, func(t *testing.T) {
			c, _ := fresh(t)
			if tc.state != "unknown" {
				must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/")), "register-metric"), map[string]string{"name": tc.stored, "unit": tc.unit}))
				if tc.state == "deleted" {
					must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/pages?title="+url.QueryEscape(tc.stored))), "tombstone"), nil))
				}
			}
			stored := must(c.Get(t.Context(), "/metrics/"+url.PathEscape(tc.stored)))
			equivalent := must(c.Get(t.Context(), "/metrics/"+url.PathEscape(tc.equivalent)))
			if names(stored) != names(equivalent) {
				t.Errorf("actions: stored %s, equivalent %s", names(stored), names(equivalent))
			}
			if href(stored, "page") != href(equivalent, "page") {
				t.Errorf("page identity differs: %q vs %q", href(stored, "page"), href(equivalent, "page"))
			}
			for _, e := range []*api.Entity{stored, equivalent} {
				if href(e, "self") == "" {
					t.Error("missing self link")
				} else if names(must(c.Get(t.Context(), href(e, "self")))) != names(e) {
					t.Error("self link changes actions")
				}
				restricted := tc.state == "unknown" || tc.state == "deleted"
				if restricted && (href(e, "page") != "" || find(e, "link").Name != "") {
					t.Errorf("restricted metric offers actions: %s", names(e))
				}
			}
			if tc.state == "unitless" {
				a := find(equivalent, "record")
				if a.Name == "" {
					t.Fatal("equivalent spelling lacks record")
				}
				must(c.Do(t.Context(), a, map[string]string{"day": "2026-01-01", "value": "1"}))
			}
			if tc.state != "unknown" && tc.state != "deleted" {
				page := must(c.Get(t.Context(), href(equivalent, "page")))
				if page.Title != tc.stored {
					t.Errorf("stored spelling changed: %q", page.Title)
				}
			}
		})
	}
}

func TestRenamedIdentityRetainsActionsAndAliasWrites(t *testing.T) {
	c, _ := fresh(t)
	root := must(c.Get(t.Context(), "/"))
	p := must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Typo", "body": "prose"}))
	oldHref := href(p, "self")
	renamed := must(c.Do(t.Context(), find(p, "rename"), map[string]string{"title": "Correct"}))
	if href(renamed, "self") != oldHref {
		t.Fatal("rename changed id")
	}
	must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Other", "body": "[[Typo]]"}))
	current := must(c.Get(t.Context(), oldHref))
	if current.Title != "Correct" || names(current) != "save-body,promote,rename,link,unlink,file-pages,tombstone" {
		t.Fatalf("renamed actions: %+v", current)
	}
	version := current.Properties.(map[string]any)["version"].(string)
	must(c.Do(t.Context(), find(current, "save-body"), map[string]string{"body": "#REDIRECT [[Other]]", "version": version}))
	must(c.Do(t.Context(), find(current, "link"), map[string]string{"to": "Other", "kind": "part-of"}))
	must(c.Do(t.Context(), find(must(c.Get(t.Context(), oldHref)), "unlink"), map[string]string{"to": "Other", "kind": "part-of"}))
	alias := must(c.Get(t.Context(), "/pages?title=Typo"))
	if href(alias, "self") != oldHref || alias.Title != "Correct" || alias.Properties.(map[string]any)["body"] != "#REDIRECT [[Other]]" {
		t.Fatalf("retained alias: %+v", alias)
	}
	if _, err := c.Do(t.Context(), find(alias, "link"), map[string]string{"to": "Other", "kind": "redirect"}); !clientStatus(err, 422) {
		t.Fatalf("removed kind: %v", err)
	}
}

// feedbackLocation reports a Location that is the page back (a path, with its query when it has one) and one
// receipt of feedback.
func feedbackLocation(location, back string) bool {
	want, err := url.Parse(back)
	if err != nil {
		return false
	}
	u, err := url.Parse(location)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Path != want.Path || u.Fragment != "" {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	values := q["feedback"]
	q.Del("feedback")
	if q.Encode() != want.Query().Encode() || len(values) != 1 || len(values[0]) != 64 {
		return false
	}
	_, err = hex.DecodeString(values[0])
	return err == nil
}

// withoutTrigger runs one statement with a trigger dropped and restores it: deliberate damage, or a row a file
// written before the rule may hold.
func withoutTrigger(t *testing.T, w *sql.DB, trigger, statement string) {
	t.Helper()
	var definition string
	if err := w.QueryRow(`SELECT sql FROM sqlite_schema WHERE type='trigger' AND name=?`, trigger).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"DROP TRIGGER " + trigger, statement, definition} {
		if _, err := w.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}
