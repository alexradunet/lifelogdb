package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/db/dbtest"
)

// TestTheFontsAreServedByExactName: every face base.css names is served as a font that a browser keeps; the
// licences are text; any other name is 404, and the route is a GET (HEAD too) and nothing else.
func TestTheFontsAreServedByExactName(t *testing.T) {
	_, h, _ := apitest.Fresh(t, "cli")
	get := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec
	}
	urls := regexp.MustCompile(`url\((/assets/fonts/[^)]+)\)`).FindAllStringSubmatch(browse(t, h, "/actions"), -1)
	if len(urls) != 12 {
		t.Fatalf("base.css names %d font files, want 12 (6 faces, latin and latin-ext)", len(urls))
	}
	for _, u := range urls {
		rec := get("GET", u[1])
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "font/woff2" || !strings.HasPrefix(rec.Body.String(), "wOF2") ||
			rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("GET %s: %d %q %q", u[1], rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("Cache-Control"))
		}
	}
	for _, licence := range []string{"OFL-atkinson-hyperlegible.txt", "OFL-ibm-plex-mono.txt", "OFL-jersey-15.txt"} {
		if rec := get("GET", "/assets/fonts/"+licence); rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") ||
			!strings.Contains(rec.Body.String(), "SIL Open Font License, Version 1.1") {
			t.Errorf("the licence %s: %d %q", licence, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	for _, path := range []string{"/assets/fonts/unknown.woff2", "/assets/fonts/README.md", "/assets/fonts/..%2fbase.css",
		"/assets/fonts/%2e%2e%2fbase.css", "/assets/fonts/", "/assets/fonts/a%2Fb.woff2", "/assets/fonts/woff2", "/assets/base.css"} {
		if rec := get("GET", path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d, want 404", path, rec.Code)
		}
	}
	if rec := get("HEAD", urls[0][1]); rec.Code != http.StatusOK || rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "font/woff2" {
		t.Errorf("HEAD: %d, %d bytes", rec.Code, rec.Body.Len())
	}
	if rec := get("POST", urls[0][1]); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d, want 405", rec.Code)
	}

	pub, _ := publicHandler(t, api.PublicOptions{Token: testToken})
	if rec := request(pub, "GET", urls[0][1], "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("a public listener serves a font with no token: %d", rec.Code)
	}
	if rec := request(pub, "GET", urls[0][1], "", map[string]string{"Authorization": "Bearer " + testToken}); rec.Code != http.StatusOK {
		t.Errorf("a public listener refuses a font with the token: %d", rec.Code)
	}
}

// TestTheManifestAndTheIconAreServed: a browser can install the app. The manifest and the icon have their types
// and a cache; a public listener refuses both without the token; the manifest's share target posts to a route that
// writes on the local day.
func TestTheManifestAndTheIconAreServed(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	get := func(h http.Handler, method, path string, headers map[string]string) *httptest.ResponseRecorder {
		return request(h, method, path, "", headers)
	}
	rec := get(h, "GET", "/manifest.webmanifest", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/manifest+json" ||
		rec.Header().Get("Cache-Control") != "public, max-age=3600" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("the manifest: %d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("Cache-Control"))
	}
	var m struct {
		Name        string `json:"name"`
		StartURL    string `json:"start_url"`
		Display     string `json:"display"`
		ThemeColor  string `json:"theme_color"`
		Icons       []struct{ Src, Purpose string }
		Shortcuts   []struct{ Name, URL string }
		ShareTarget struct {
			Action, Method, Enctype string
			Params                  struct {
				Text  string
				Files []struct {
					Name   string
					Accept []string
				}
			}
		} `json:"share_target"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("the manifest is not JSON: %v", err)
	}
	if m.Name != "Lifelog" || m.StartURL != "/" || m.Display != "standalone" || m.ThemeColor != "#3a2210" || len(m.Shortcuts) != 2 || len(m.Icons) != 2 ||
		m.ShareTarget.Action != "/days/today/capture" || m.ShareTarget.Method != "POST" || m.ShareTarget.Enctype != "multipart/form-data" ||
		m.ShareTarget.Params.Text != "text" || len(m.ShareTarget.Params.Files) != 0 { // a phone's share sheet sends text to capture
		t.Errorf("the manifest says %+v", m)
	}
	for _, i := range m.Icons {
		if i.Src != "/assets/icon.svg" {
			t.Errorf("the manifest names the icon %q", i.Src)
		}
	}

	icon := get(h, "GET", "/assets/icon.svg", nil)
	if icon.Code != http.StatusOK || icon.Header().Get("Content-Type") != "image/svg+xml" ||
		icon.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || icon.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("the icon: %d %q %q", icon.Code, icon.Header().Get("Content-Type"), icon.Header().Get("Cache-Control"))
	}
	if b := icon.Body.String(); !strings.HasPrefix(b, "<svg ") || strings.Contains(b, "<script") || strings.Contains(b, "<text") || len(b) > 1024 {
		t.Errorf("the icon is not a small SVG with no text and no script: %d bytes", len(b))
	}
	for _, path := range []string{"/manifest.webmanifest", "/assets/icon.svg"} {
		if r := get(h, "HEAD", path, nil); r.Code != http.StatusOK || r.Body.Len() != 0 {
			t.Errorf("HEAD %s: %d, %d bytes", path, r.Code, r.Body.Len())
		}
		if r := get(h, "POST", path, nil); r.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s: %d, want 405", path, r.Code)
		}
	}

	pub, _ := publicHandler(t, api.PublicOptions{Token: testToken})
	for _, path := range []string{"/manifest.webmanifest", "/assets/icon.svg"} {
		if r := get(pub, "GET", path, nil); r.Code != http.StatusUnauthorized {
			t.Errorf("a public listener serves %s with no token: %d", path, r.Code)
		}
		if r := get(pub, "GET", path, map[string]string{"Authorization": "Bearer " + testToken}); r.Code != http.StatusOK {
			t.Errorf("a public listener refuses %s with the token: %d", path, r.Code)
		}
	}

	// The share target: a POST of text to /days/today/capture writes on the local day.
	r := submit(h, "/days/today/capture", url.Values{"text": {"Shared from a phone"}}, "")
	if r.Code != http.StatusSeeOther {
		t.Fatalf("POST /days/today/capture: %d %.200s", r.Code, r.Body.String())
	}
	day := apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday))
	if apitest.Href(day, "page") == "" || !strings.Contains(browse(t, h, "/days/"+fixedToday), "Shared from a phone") {
		t.Errorf("the shared text is not on %s", fixedToday)
	}
	if page := apitest.Must(t)(c.Get(t.Context(), apitest.Href(day, "page"))); !strings.Contains(props(page)["body"].(string), "Shared from a phone") {
		t.Errorf("the day page does not hold the shared text: %v", props(page)["body"])
	}
}

// TestTheStylePageReadsNothing: the style page is a whole page, it is not in the catalog, and it reads nothing from
// the database: with the store closed the shell's read fails and the page is still drawn. A public listener refuses
// it without the token. What it shows is TestTheStylePageRunsTheRealTemplates.
func TestTheStylePageReadsNothing(t *testing.T) {
	d, err := db.Open(t.Context(), dbtest.File(t))
	if err != nil {
		t.Fatal(err)
	}
	h := api.New(&core.Store{DB: d})
	if err := d.Close(); err != nil { // every query of the store now fails
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/styleguide", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Fatalf("GET /styleguide with the store closed: %d %q %.300s", rec.Code, rec.Header().Get("Content-Type"), body)
	}
	for _, id := range []string{`id="refused"`, `role="alert"`} {
		if strings.Contains(body, id) {
			t.Errorf("the style page carries %s: a second live region for the refused alert", id)
		}
	}

	c, live, _ := apitest.Fresh(t, "cli")
	catalog, err := c.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range catalog {
		if strings.Contains(a.Href, "styleguide") {
			t.Errorf("the catalog names the style page: %s", a.Name)
		}
	}
	if rec := request(live, "GET", "/styleguide", "", map[string]string{"Accept": "text/html"}); rec.Code != http.StatusOK {
		t.Errorf("GET /styleguide on an open store: %d", rec.Code)
	}
	if rec := request(live, "POST", "/styleguide", "", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /styleguide: %d, want 405", rec.Code)
	}

	pub, _ := publicHandler(t, api.PublicOptions{Token: testToken})
	if rec := request(pub, "GET", "/styleguide", "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("a public listener serves the style page with no token: %d", rec.Code)
	}
	if rec := request(pub, "GET", "/styleguide", "", map[string]string{"Authorization": "Bearer " + testToken, "Accept": "text/html"}); rec.Code != http.StatusOK {
		t.Errorf("a public listener refuses the style page with the token: %d", rec.Code)
	}
}
