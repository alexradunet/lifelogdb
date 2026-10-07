package api_test

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBrowserSessionEvidenceAndScopeLabels(t *testing.T) {
	c, h := fresh(t)
	root := must(c.Get("/actions"))
	must(c.Do(find(root, "create-page"), map[string]string{"title": "Workout"}))
	must(c.Do(find(root, "register-metric"), map[string]string{"name": "Steps", "unit": "steps"}))
	form := url.Values{"kind": {"Workout"}, "day": {"2020-01-02"}, "start_local": {"2020-01-01T23:00:00.000"}, "end_local": {"2020-01-02T07:00:00.000"}, "start_zone_unverified": {"Claimed/Zone"}}
	request := httptest.NewRequest("POST", "/sessions", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, request)
	if w.Code != 303 {
		t.Fatalf("browser capture %d %s", w.Code, w.Body.String())
	}
	location := w.Header().Get("Location")
	body := browse(t, h, location)
	for _, text := range []string{"unverified", "Claimed/Zone", "unresolved", "2020-01-02"} {
		if !strings.Contains(body, text) {
			t.Fatalf("missing %q", text)
		}
	}
	must(c.Do(find(root, "record"), map[string]string{"metric": "Steps", "day": "2020-01-02", "value": "10000"}))
	scoped := must(c.Do(find(root, "record"), map[string]string{"metric": "Steps", "day": "2020-01-02", "value": "4000", "session_id": strings.TrimPrefix(location, "/sessions/")}))
	readingBody := browse(t, h, href(scoped, "self"))
	if !strings.Contains(readingBody, "Scope: session") || !strings.Contains(readingBody, location) {
		t.Fatal("reading omitted scope association")
	}
	all := browse(t, h, "/metrics/Steps?from=2020-01-01&to=2020-01-02&scope=all")
	if !strings.Contains(all, "unassociated") || !strings.Contains(all, location) || strings.Contains(all, "<svg class=\"chart\"") {
		t.Fatal("all-scope presentation misleading")
	}
}
