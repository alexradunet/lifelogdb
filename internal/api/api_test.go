package api_test

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
	"lifelog/internal/api/siren"
	"lifelog/internal/client"
	"lifelog/internal/core"
)

// scalar reads one text value from the file: a probe, never the behaviour under test.
func scalar(t *testing.T, s *core.Store, query string) string {
	t.Helper()
	var v string
	if err := s.DB.R.QueryRowContext(t.Context(), query).Scan(&v); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return v
}

func names(e *siren.Entity) string {
	var out []string
	for _, a := range e.Actions {
		out = append(out, a.Name)
	}
	return strings.Join(out, ",")
}

func hasAction(e *siren.Entity, name string) bool {
	for _, a := range e.Actions {
		if a.Name == name {
			return true
		}
	}
	return false
}

func TestCatalogNamesAreUnique(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	actions, err := c.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	seen, normalized := map[string]siren.Action{}, map[string]siren.Action{}
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

// TestEveryCatalogActionHasItsRouteAndItsOwnerRule: each action of the catalog has a route with the action's method
// and path, and that route refuses an agent exactly when the action is the owner's alone. The mux answers a missing
// route with a plain-text 404 or 405; an action's handler answers with a Siren entity, or with a 303 whose Location is
// the resource that the action names (open-day with no day goes to today). The mux redirects only with a 301.
func TestEveryCatalogActionHasItsRouteAndItsOwnerRule(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	p := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Route probe"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	m := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "weight", "day": "2026-09-01", "value": "71"}))
	page, reading := strings.TrimPrefix(apitest.Href(p, "self"), "/pages/"), strings.TrimPrefix(apitest.Href(m, "self"), "/measurements/")
	if page == "" || strings.Contains(page, "/") || reading == "" || strings.Contains(reading, "/") {
		t.Fatalf("no page id (%q) or reading id (%q) to fill a path with", page, reading)
	}
	actions, err := c.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	owners := 0
	for _, a := range actions {
		id := page
		if strings.HasPrefix(a.Href, "/measurements/") {
			id = reading
		}
		path := strings.NewReplacer("{id}", id, "{day}", "2026-09-01").Replace(a.Href)
		if strings.Contains(path, "{") {
			t.Fatalf("%s: the test fills no value for a path field of %s", a.Name, a.Href)
		}
		req := httptest.NewRequest(a.Method, path, strings.NewReader(""))
		if a.Method == "POST" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		req.Header.Set(siren.SourceHeader, "agent:x")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		ct := rec.Header().Get("Content-Type")
		answered := ct == "application/vnd.siren+json" || rec.Code == http.StatusSeeOther && rec.Header().Get("Location") != ""
		if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed || !answered {
			t.Errorf("%s %s (%s): %d %s %q, not an answer of the action: %.200s", a.Method, path, a.Name, rec.Code, ct,
				rec.Header().Get("Location"), rec.Body.String())
		}
		if refused := rec.Code == http.StatusForbidden; refused != a.Owner {
			t.Errorf("%s %s (%s, owner %v): an agent got %d", a.Method, path, a.Name, a.Owner, rec.Code)
		}
		if a.Owner {
			owners++
		}
	}
	if owners == 0 {
		t.Error("the catalog has no owner action: the test does not reach the owner rule")
	}
}

// TestARequestWithoutARequiredFieldIsRefusedAndWritesNothing: for each action of the catalog and each of its required
// fields that is not a path field, a request that leaves the field out, or sends it empty, answers 422 "missing field
// <name>" and changes nothing in the file. The other required fields carry values that a write accepts, so only the
// one field is missing. An action with a file field is sent as multipart too, with a file. search is the one
// exception: GET /search with no q is the search page, which TestEveryViewRenders draws. Before each request the
// probe page gets a body that no other request gave it, so a save of an empty body or of other words always changes
// the file. Before each request the test also records a new probe reading, and the /measurements/ actions get its id,
// so a correct always has a reading that no case corrected before (a reading is corrected one time only). Thus a case
// cannot hide a write because a case before it wrote the same thing or used up the probe.
func TestARequestWithoutARequiredFieldIsRefusedAndWritesNothing(t *testing.T) {
	c, _, s := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	p := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Required probe", "body": "first words"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	page := strings.TrimPrefix(apitest.Href(p, "self"), "/pages/")
	if page == "" {
		t.Fatal("no page id to send")
	}
	reading := "" // the id of the probe reading of the case: each case records a new one
	// version is the probe page's version now: a case that wrote by error, or the new body of each case, does not
	// make the next save stale.
	version := func() string {
		for _, f := range apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), apitest.Href(p, "self"))), "save-body").Fields {
			if f.Name == "version" {
				v, _ := f.Value.(string)
				return v
			}
		}
		t.Fatal("the probe page offers no save-body with a version")
		return ""
	}
	original := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(original, []byte("synthetic notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A value that a write accepts, for each required field; a field with options takes its first option.
	valid := map[string]string{"title": "Probe title", "body": "new words", "to": "Required probe",
		"kind": "part-of", "ids": page, "category": "Probe category", "metric": "weight", "day": "2026-09-02",
		"value": "70", "name": "Probe metric", "q": "words"}
	// state is the file in short: the rows of each table, and the sum of the edit revisions, so an edit shows too.
	state := func() string {
		return scalar(t, s, `SELECT (SELECT count(*) || ' ' || total(revision) FROM entities) || ' ' ||
			(SELECT count(*) FROM entity_names) || ' ' || (SELECT count(*) FROM links) || ' ' ||
			(SELECT count(*) FROM measurements) || ' ' || (SELECT count(*) FROM files) || ' ' ||
			(SELECT count(*) FROM people) || ' ' || (SELECT count(*) FROM metrics)`)
	}
	actions, err := c.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cases := 0
	for _, a := range actions {
		file := "" // the action's first file field
		for _, f := range a.Fields {
			if f.Type == "file" && file == "" {
				file = f.Name
			}
		}
		for _, missing := range a.Fields {
			if !missing.Required || missing.In == "path" || a.Name == "search" {
				continue
			}
			for _, empty := range []bool{false, true} {
				// values reads the version when the request goes, after the probe page got the body of its case.
				values := func() map[string]string {
					valid["version"] = version()
					values := map[string]string{}
					for _, f := range a.Fields {
						switch {
						case f.In == "path" && f.Name == "id" && strings.HasPrefix(a.Href, "/measurements/"):
							values[f.Name] = reading
						case f.In == "path" && f.Name == "id":
							values[f.Name] = page
						case f.In == "path" && f.Name == "day":
							values[f.Name] = "2026-09-01"
						case f.In == "path":
							t.Fatalf("%s: the test has no value for the path field %s", a.Name, f.Name)
						case f.Name == missing.Name:
							if empty {
								values[f.Name] = ""
							}
						case f.Required && len(f.Options) > 0:
							values[f.Name] = f.Options[0]
						case f.Required:
							v, ok := valid[f.Name]
							if !ok {
								t.Fatalf("%s: the test has no value for the required field %s", a.Name, f.Name)
							}
							values[f.Name] = v
						}
					}
					return values
				}
				sends := map[string]func() (*siren.Entity, error){
					"a form": func() (*siren.Entity, error) { return c.Do(t.Context(), a, values()) },
				}
				if file != "" {
					sends["multipart"] = func() (*siren.Entity, error) {
						return c.DoFiles(t.Context(), a, values(), map[string]string{file: original})
					}
				}
				sent := "left out"
				if empty {
					sent = "empty"
				}
				for how, send := range sends {
					what := fmt.Sprintf("%s %s with %s %s, as %s", a.Method, a.Name, missing.Name, sent, how)
					apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), apitest.Href(p, "self"))), "save-body"),
						map[string]string{"body": fmt.Sprintf("the words before case %d", cases)}))
					m := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"),
						map[string]string{"metric": "weight", "day": "2026-09-01", "value": fmt.Sprint(60 + cases)}))
					if reading = strings.TrimPrefix(apitest.Href(m, "self"), "/measurements/"); reading == "" {
						t.Fatalf("%s: no reading id to send", what)
					}
					before := state()
					_, err := send()
					var ce *client.Error
					if !errors.As(err, &ce) {
						t.Errorf("%s: %v, want a refusal", what, err)
					} else if props, _ := ce.Entity.Properties.(map[string]any); ce.Status != http.StatusUnprocessableEntity ||
						props["code"] != "invalid" || props["message"] != "missing field "+missing.Name {
						t.Errorf("%s: %d %v, want 422 invalid \"missing field %s\"", what, ce.Status, props, missing.Name)
					}
					if after := state(); after != before {
						t.Errorf("%s: the file changed from %s to %s", what, before, after)
					}
					cases++
				}
			}
		}
	}
	if cases == 0 {
		t.Error("no action of the catalog has a required field: the test reaches nothing")
	}
}

func TestActionsAreOfferedOnlyWhereLegal(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	day, err := c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "capture"), map[string]string{"text": "with [[Ana]]"})
	if err != nil {
		t.Fatal(err)
	}
	dayPage := apitest.Must(t)(c.Get(t.Context(), apitest.Href(day, "page")))
	if got := names(dayPage); got != "save-body,link,unlink,tombstone" {
		t.Errorf("a day page offers %s (never promote)", got)
	}
	if !hasOption(apitest.Action(t, dayPage, "link"), "kind", "at") {
		t.Error("a day page does not offer an at link")
	}
	if hasOption(apitest.Action(t, dayPage, "link"), "kind", "part-of") {
		t.Error("a day page offers a part-of link (D26: a day page is never filed)")
	}
	ana := apitest.Must(t)(c.Get(t.Context(), dayPage.Entities[0].Href))
	if got := names(ana); got != "save-body,promote,rename,link,unlink,file-pages,tombstone" {
		t.Errorf("a plain page offers %s", got)
	}
	if hasOption(apitest.Action(t, ana, "link"), "kind", "at") {
		t.Error("a plain page offers an at link (D16: from a day page only)")
	}
	ana = apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, ana, "promote"), map[string]string{"to": "person"}))
	if got := names(ana); strings.Contains(got, "promote") || hasOption(apitest.Action(t, ana, "link"), "kind", "at") {
		t.Errorf("a person offers %s with kinds %v", got, apitest.Action(t, ana, "link").Fields)
	}
	gone := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, ana, "tombstone"), nil))
	if got := names(gone); got != "revive" {
		t.Errorf("a tombstoned page offers %s", got)
	}
}

func TestSaveCarriesItsVersion(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	p := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "create-page"), map[string]string{"title": "Diet", "body": "v1"}))
	save := apitest.Action(t, p, "save-body")
	p2 := apitest.Must(t)(c.Do(t.Context(), save, map[string]string{"body": "v2 [[Plan]]"}))
	if p2.Result == nil {
		t.Error("a save reports no result")
	}
	_, err := c.Do(t.Context(), save, map[string]string{"body": "v3"}) // the old form: its version is stale now
	if apitest.Status(err) != 409 {
		t.Errorf("a stale save: %v", err)
	}
}

func TestExistingTitleLinksToThePage(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	create := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "create-page")
	c.Do(t.Context(), create, map[string]string{"title": "Café"})
	e, err := c.Do(t.Context(), create, map[string]string{"title": "CAFÉ"})
	if err == nil || apitest.Href(e, "existing") == "" {
		t.Errorf("a duplicate title: %v, links %v", err, e.Links)
	}
}

// A link from an id that no page has is a missing row (404), not the foreign key's refusal (422).
func TestLinkFromAMissingPageIsNotFound(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Target"}))
	link := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/actions")), "link") // the templated action: the id is a path field
	_, err := c.Do(t.Context(), link, map[string]string{"id": "999999", "to": "Target", "kind": "part-of"})
	if code, status := codeOf(t, err); code != "not_found" || status != 404 {
		t.Errorf("link from a missing page: code=%q status=%d (%v)", code, status, err)
	}
}

func TestCorrectionDomainActions(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	catalog, err := c.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	actions := &siren.Entity{Actions: catalog}
	record := apitest.Action(t, actions, "record")
	correct := apitest.Action(t, actions, "correct")
	retract := apitest.Action(t, actions, "retract")

	mood := apitest.Must(t)(c.Do(t.Context(), record, map[string]string{"metric": "Mood", "day": "2026-10-01", "value": "3"}))
	if _, err := c.Do(t.Context(), correct, map[string]string{"id": measurementID(mood), "value": "2.5"}); apitest.Status(err) != 422 {
		t.Fatalf("fractional mood correction: %v, want 422", err)
	}
	if _, err := c.Do(t.Context(), correct, map[string]string{"id": measurementID(mood), "value": "5"}); err != nil {
		t.Fatalf("valid mood correction: %v", err)
	}

	if _, err := c.Do(t.Context(), apitest.Action(t, actions, "register-metric"), map[string]string{"name": "api_walk"}); err != nil {
		t.Fatal(err)
	}
	habit := apitest.Must(t)(c.Do(t.Context(), record, map[string]string{"metric": "api_walk", "day": "2026-10-01", "value": "1"}))
	if _, err := c.Do(t.Context(), retract, map[string]string{"id": measurementID(habit)}); err != nil {
		t.Fatalf("habit retraction: %v", err)
	}
}

// TestACorrectWithNoValueIsRefusedAndChangesNothing: a correct with no value is refused 422, because a correct with
// a nil value would retract the reading. The catalog's required value is the one guard of this. The reading reads back
// as it was: current, with its value, with no correction and no retraction, and the file has no new reading. Each case
// records a new reading first and corrects it: a reading is corrected one time only, so a case cannot fail for another
// reason because a case before it corrected the reading by error.
func TestACorrectWithNoValueIsRefusedAndChangesNothing(t *testing.T) {
	c, h, s := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	for i, body := range []string{"", "value="} {
		m := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "weight", "day": fmt.Sprintf("2026-09-%02d", i+1), "value": "71"}))
		self := apitest.Href(m, "self")
		req := httptest.NewRequest("POST", self+"/correct", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var e siren.Entity
		if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
			t.Fatalf("correct with %q: the answer is not an entity: %v: %s", body, err, rec.Body.String())
		}
		if p := props(&e); rec.Code != http.StatusUnprocessableEntity || p["code"] != "invalid" || p["message"] != "missing field value" {
			t.Errorf("correct with %q: %d %v, want 422 invalid \"missing field value\"", body, rec.Code, p)
		}
		back := props(apitest.Must(t)(c.Get(t.Context(), self)))
		if back["current"] != true || back["retraction"] != false || back["value"] != json.Number("71") || back["superseded_by"] != nil {
			t.Errorf("correct with %q: the reading reads back as %v, want current 71, not retracted, not corrected", body, back)
		}
		if n, want := scalar(t, s, "SELECT CAST(count(*) AS text) FROM measurements"), fmt.Sprint(i+1); n != want {
			t.Errorf("correct with %q: %s readings in the file, want %s (one for each case so far)", body, n, want)
		}
	}
}

func measurementID(e *siren.Entity) string {
	return strings.TrimPrefix(apitest.Href(e, "self"), "/measurements/")
}

func TestBrowserGetsHTMLAndSourceUI(t *testing.T) {
	_, h, s := apitest.Fresh(t, "cli")
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
	_, h, s := apitest.Fresh(t, "cli")
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

func hasOption(a siren.Action, field, opt string) bool {
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
	c, _, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	for _, m := range []map[string]string{{"name": "weight", "unit": "kg"}, {"name": "ldl", "unit": "mg/dL"}} {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), m))
	}
	day := "2026-06-01"
	for _, r := range []map[string]string{
		{"metric": "weight", "day": "2026-05-20", "value": "71.5"},
		{"metric": "ldl", "day": "2025-12-01", "value": "96"}, // a lab marker, read twice a year
	} {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"), r))
	}
	dayView := apitest.Must(t)(c.Get(t.Context(), "/days/"+day))
	rec := apitest.Action(t, dayView, "record")
	if !hasOption(rec, "metric", "weight") || hasOption(rec, "metric", "ldl") || hasOption(rec, "metric", "Mood") {
		t.Errorf("the day view offers %v: only what was read in its last 60 days", rec.Fields[0].Options)
	}
	if apitest.Href(dayView, "metrics") != "/metrics" {
		t.Error("the day view does not link the whole registry")
	}
	if all := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/metrics")), "record"); !hasOption(all, "metric", "ldl") || !hasOption(all, "metric", "Mood") {
		t.Errorf("/metrics offers %v: every registered metric", all.Fields[0].Options)
	}
	if ldl := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/metrics/ldl")), "record"); !hasOption(ldl, "metric", "ldl") || ldl.Fields[0].Value != "ldl" {
		t.Errorf("a series offers %v with %v: its own metric, selected", ldl.Fields[0].Options, ldl.Fields[0].Value)
	}
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, dayView, "record"), map[string]string{"metric": "ldl", "day": day, "value": "92"}))
	if !hasOption(apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/"+day)), "record"), "metric", "ldl") {
		t.Error("a metric read on the day is not offered")
	}
}

func TestMetricsAreGroupedByCategory(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	for _, m := range []map[string]string{{"name": "TSH", "unit": "µUI/mL", "note": "thyroid-stimulating hormone"}, {"name": "Weight", "unit": "kg"},
		{"name": "Walk"}, {"name": "Steps", "unit": "n"}} {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), m))
	}
	for _, p := range []string{"Biomarkers", "Thyroid", "Body"} {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": p}))
	}
	thyroid := apitest.Must(t)(c.Get(t.Context(), "/pages?title=Thyroid"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, thyroid, "link"), map[string]string{"to": "Biomarkers", "kind": "part-of"}))
	tsh := apitest.Must(t)(c.Get(t.Context(), "/metrics/tsh"))
	if apitest.Href(tsh, "page") == "" || !hasAction(tsh, "file-pages") || apitest.Action(t, tsh, "link").Title != "Add a link" {
		t.Fatalf("a series links its metric's page and offers to file it: %v", names(tsh))
	}
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, tsh, "link"), map[string]string{"to": "Thyroid", "kind": "part-of"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/metrics/Weight")), "link"), map[string]string{"to": "Body", "kind": "part-of"}))
	walk := apitest.Must(t)(c.Get(t.Context(), "/metrics/Walk"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, walk, "link"), map[string]string{"to": "Body", "kind": "part-of"}))
	if _, err := c.Do(t.Context(), apitest.Action(t, walk, "link"), map[string]string{"to": "Nowhere", "kind": "part-of"}); err == nil {
		t.Error("a metric is filed in a category page that does not exist")
	}

	var got []string
	for _, g := range apitest.Must(t)(c.Get(t.Context(), "/metrics")).Properties.(map[string]any)["groups"].([]any) {
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
	for _, s := range []string{"<h3>Thyroid <small>1</small></h3>", `<datalist id="pick-category"><option value="Biomarkers"><option value="Thyroid"><option value="Body"></datalist>`,
		"<h2>Not filed", `title="thyroid-stimulating hormone"`} {
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
			c, _, _ := apitest.Fresh(t, "cli")
			if tc.state != "unknown" {
				apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "register-metric"), map[string]string{"name": tc.stored, "unit": tc.unit}))
				if tc.state == "deleted" {
					apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/pages?title="+url.QueryEscape(tc.stored))), "tombstone"), nil))
				}
			}
			stored := apitest.Must(t)(c.Get(t.Context(), "/metrics/"+url.PathEscape(tc.stored)))
			equivalent := apitest.Must(t)(c.Get(t.Context(), "/metrics/"+url.PathEscape(tc.equivalent)))
			if names(stored) != names(equivalent) {
				t.Errorf("actions: stored %s, equivalent %s", names(stored), names(equivalent))
			}
			if apitest.Href(stored, "page") != apitest.Href(equivalent, "page") {
				t.Errorf("page identity differs: %q vs %q", apitest.Href(stored, "page"), apitest.Href(equivalent, "page"))
			}
			for _, e := range []*siren.Entity{stored, equivalent} {
				if apitest.Href(e, "self") == "" {
					t.Error("missing self link")
				} else if names(apitest.Must(t)(c.Get(t.Context(), apitest.Href(e, "self")))) != names(e) {
					t.Error("self link changes actions")
				}
				restricted := tc.state == "unknown" || tc.state == "deleted"
				if restricted && (apitest.Href(e, "page") != "" || hasAction(e, "link")) {
					t.Errorf("restricted metric offers actions: %s", names(e))
				}
			}
			if tc.state == "unitless" {
				apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, equivalent, "record"), map[string]string{"day": "2026-01-01", "value": "1"}))
			}
			if tc.state != "unknown" && tc.state != "deleted" {
				page := apitest.Must(t)(c.Get(t.Context(), apitest.Href(equivalent, "page")))
				if page.Title != tc.stored {
					t.Errorf("stored spelling changed: %q", page.Title)
				}
			}
		})
	}
}

func TestRenamedIdentityRetainsActionsAndAliasWrites(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	p := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Typo", "body": "prose"}))
	oldHref := apitest.Href(p, "self")
	renamed := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, p, "rename"), map[string]string{"title": "Correct"}))
	if apitest.Href(renamed, "self") != oldHref {
		t.Fatal("rename changed id")
	}
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Other", "body": "[[Typo]]"}))
	current := apitest.Must(t)(c.Get(t.Context(), oldHref))
	if current.Title != "Correct" || names(current) != "save-body,promote,rename,link,unlink,file-pages,tombstone" {
		t.Fatalf("renamed actions: %+v", current)
	}
	version := current.Properties.(map[string]any)["version"].(string)
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, current, "save-body"), map[string]string{"body": "#REDIRECT [[Other]]", "version": version}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, current, "link"), map[string]string{"to": "Other", "kind": "part-of"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), oldHref)), "unlink"), map[string]string{"to": "Other", "kind": "part-of"}))
	alias := apitest.Must(t)(c.Get(t.Context(), "/pages?title=Typo"))
	if apitest.Href(alias, "self") != oldHref || alias.Title != "Correct" || alias.Properties.(map[string]any)["body"] != "#REDIRECT [[Other]]" {
		t.Fatalf("retained alias: %+v", alias)
	}
	if _, err := c.Do(t.Context(), apitest.Action(t, alias, "link"), map[string]string{"to": "Other", "kind": "redirect"}); apitest.Status(err) != 422 {
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
