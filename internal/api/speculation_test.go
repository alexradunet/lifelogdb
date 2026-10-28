package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
)

// TestAShellPageAsksForPrerenderAndEveryAnswerVariesByAccept: a page of the shell names the speculation rules, a
// Siren answer does not, and the rules file is JSON that keeps the prerender away from a query string and from the
// assets (plan 149).
func TestAShellPageAsksForPrerenderAndEveryAnswerVariesByAccept(t *testing.T) {
	_, h, _ := apitest.Fresh(t, "cli")
	get := func(path, accept string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Accept", accept)
		h.ServeHTTP(rec, req)
		return rec
	}
	page := get("/actions", "text/html")
	if got := page.Header().Get("Speculation-Rules"); got != `"/assets/speculation.json"` {
		t.Errorf("shell page Speculation-Rules = %q", got)
	}
	if got := page.Header().Get("Vary"); got != "Accept" {
		t.Errorf("shell page Vary = %q", got)
	}
	siren := get("/actions", "application/vnd.siren+json")
	if got := siren.Header().Get("Speculation-Rules"); got != "" {
		t.Errorf("Siren answer Speculation-Rules = %q, want none", got)
	}
	if got := siren.Header().Get("Vary"); got != "Accept" {
		t.Errorf("Siren answer Vary = %q", got)
	}

	rules := get("/assets/speculation.json", "*/*")
	if rules.Code != http.StatusOK || rules.Header().Get("Content-Type") != "application/speculationrules+json" {
		t.Fatalf("rules: %d %q", rules.Code, rules.Header().Get("Content-Type"))
	}
	var doc struct {
		Prerender []json.RawMessage `json:"prerender"`
	}
	if err := json.Unmarshal(rules.Body.Bytes(), &doc); err != nil || len(doc.Prerender) == 0 {
		t.Fatalf("rules are not JSON with a prerender rule: %v", err)
	}
	first := string(doc.Prerender[0])
	for _, want := range []string{`/*\\?*=*`, `/assets/*`} {
		if !strings.Contains(first, want) {
			t.Errorf("the first prerender rule does not exclude %s: %s", want, first)
		}
	}
}

// TestTheLoginPageAsksForNoPrerender: the login page is no page of the shell.
func TestTheLoginPageAsksForNoPrerender(t *testing.T) {
	h, _ := publicHandler(t, api.PublicOptions{Token: testToken})
	rec := request(h, "GET", "/login", "", map[string]string{"Accept": "text/html"})
	if rec.Code != 200 || rec.Header().Get("Speculation-Rules") != "" {
		t.Errorf("login: %d Speculation-Rules=%q", rec.Code, rec.Header().Get("Speculation-Rules"))
	}
}
