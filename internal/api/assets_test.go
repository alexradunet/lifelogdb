package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
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
// and a cache; the PNG icons have the sizes that the manifest declares; a public listener serves the manifest and
// every icon without the token (they hold nothing of the database); the manifest's share target posts to a route
// that writes on the local day.
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
		Icons       []struct{ Src, Sizes, Purpose string }
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
	if m.Name != "Lifelog" || m.StartURL != "/" || m.Display != "standalone" || m.ThemeColor != "#3a2210" || len(m.Shortcuts) != 2 || len(m.Icons) != 3 ||
		m.ShareTarget.Action != "/days/today/capture" || m.ShareTarget.Method != "POST" || m.ShareTarget.Enctype != "application/x-www-form-urlencoded" ||
		m.ShareTarget.Params.Text != "text" || len(m.ShareTarget.Params.Files) != 0 { // a phone's share sheet sends text to capture
		t.Errorf("the manifest says %+v", m)
	}
	wantIcons := [][3]string{{"/assets/icon-192.png", "192x192", "any"}, {"/assets/icon-512.png", "512x512", "any"}, {"/assets/icon-512.png", "512x512", "maskable"}}
	for n, i := range m.Icons {
		if n < len(wantIcons) && [3]string{i.Src, i.Sizes, i.Purpose} != wantIcons[n] {
			t.Errorf("the manifest icon %d is %+v, want %v", n, i, wantIcons[n])
		}
	}
	for _, i := range m.Icons {
		png := get(h, "GET", i.Src, nil)
		if png.Code != http.StatusOK || png.Header().Get("Content-Type") != "image/png" ||
			png.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || png.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("the icon %s: %d %q %q", i.Src, png.Code, png.Header().Get("Content-Type"), png.Header().Get("Cache-Control"))
			continue
		}
		cfg, err := png2config(png.Body.Bytes())
		if err != nil || fmt.Sprintf("%dx%d", cfg.Width, cfg.Height) != i.Sizes {
			t.Errorf("the icon %s decodes as %+v (%v), the manifest says %s", i.Src, cfg, err, i.Sizes)
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
	if r := get(pub, "GET", "/assets/fonts/OFL-jersey-15.txt", nil); r.Code == http.StatusOK { // the fonts stay behind the token
		t.Errorf("a public listener serves a font with no token: %d", r.Code)
	}
	for _, path := range []string{"/manifest.webmanifest", "/assets/icon.svg", "/assets/icon-192.png", "/assets/icon-512.png", "/assets/wood.png"} {
		for _, method := range []string{"GET", "HEAD"} {
			if r := get(pub, method, path, nil); r.Code != http.StatusOK {
				t.Errorf("a public listener refuses %s %s with no token: %d", method, path, r.Code)
			}
		}
		if r := get(pub, "POST", path, nil); r.Code != http.StatusUnauthorized {
			t.Errorf("a public listener takes POST %s with no token: %d, want 401", path, r.Code)
		}
		if r := get(pub, "GET", path, map[string]string{"Authorization": "Bearer " + testToken}); r.Code != http.StatusOK {
			t.Errorf("a public listener refuses %s with the token: %d", path, r.Code)
		}
	}

	// The share target: a POST of text to /days/today/capture writes on the local day.
	// It posts with the enctype that the manifest declares: a phone sends exactly that.
	req := httptest.NewRequest("POST", m.ShareTarget.Action, strings.NewReader(url.Values{"text": {"Shared from a phone"}}.Encode()))
	req.Header.Set("Content-Type", m.ShareTarget.Enctype)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	r := httptest.NewRecorder()
	h.ServeHTTP(r, req)
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

// TestAMultipartPostWithoutFileFieldsIsRefused: a multipart body to an action that takes no file is answered 415, never
// read as an empty form (the fields would be lost in silence).
func TestAMultipartPostWithoutFileFieldsIsRefused(t *testing.T) {
	_, h, s := apitest.Fresh(t, "cli")
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("text", "Lost words"); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/days/today/capture", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("multipart POST to capture: %d, want 415: %.200s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Not done.") {
		t.Errorf("the 415 page has no first words for its code: %.300s", rec.Body.String())
	}
	if n := scalar(t, s, "SELECT CAST(count(*) AS TEXT) FROM entities WHERE body LIKE '%Lost words%'"); n != "0" {
		t.Errorf("the refused request wrote %s pages", n)
	}
}

// png2config reads the size of a PNG without decoding it twice; it fails for a body that is not a PNG.
func png2config(b []byte) (image.Config, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return cfg, err
	}
	_, err = png.Decode(bytes.NewReader(b))
	return cfg, err
}
