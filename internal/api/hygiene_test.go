package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http/httptest"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/db/dbtest"
)

// codeOf is the code and status an error answer carries.
func codeOf(t *testing.T, err error) (string, int) {
	t.Helper()
	var ce *client.Error
	if !errors.As(err, &ce) {
		t.Fatalf("not an API error: %v", err)
	}
	p, _ := ce.Entity.Properties.(map[string]any)
	code, _ := p["code"].(string)
	return code, ce.Status
}

func TestErrorAnswersCarryACode(t *testing.T) {
	c, h := fresh(t)
	root := must(c.Get(t.Context(), "/"))
	page := must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Taken", "body": "first"}))
	for _, tc := range []struct {
		name   string
		err    error
		code   string
		status int
	}{
		{"stale save", func() error {
			_, err := c.Do(t.Context(), find(page, "save-body"), map[string]string{"body": "second", "version": "0"})
			return err
		}(), "stale_version", 409},
		{"taken title", func() error {
			_, err := c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "taken"})
			return err
		}(), "exists", 409},
		{"unknown page", func() error { _, err := c.Get(t.Context(), "/pages/999999"); return err }(), "not_found", 404},
		{"bad day", func() error { _, err := c.Get(t.Context(), "/days/2026-13-01"); return err }(), "invalid", 422},
		{"missing field", func() error {
			_, err := c.Do(t.Context(), find(root, "create-page"), map[string]string{"body": "no title"})
			return err
		}(), "invalid", 422},
		{"bad limit", func() error { _, err := c.Get(t.Context(), "/days?limit=0"); return err }(), "invalid", 422},
	} {
		code, status := codeOf(t, tc.err)
		if code != tc.code || status != tc.status {
			t.Errorf("%s: code=%q status=%d want %q/%d (%v)", tc.name, code, status, tc.code, tc.status, tc.err)
		}
	}
	// The browser guard and the public listener name their refusals too.
	req := httptest.NewRequest("POST", "/pages", strings.NewReader("title=Cross"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), `"code": "cross_origin"`) {
		t.Errorf("cross-origin refusal: %d %.200s", rec.Code, rec.Body.String())
	}
	pub, _ := publicHandler(t, api.PublicOptions{Token: testToken})
	rec = request(pub, "GET", "/", "", nil)
	if rec.Code != 401 || !strings.Contains(rec.Body.String(), `"code": "token_required"`) {
		t.Errorf("token refusal: %d %.200s", rec.Code, rec.Body.String())
	}
}

// TestAnInternalErrorNamesNoCause: an error that core did not type is a fault of the server. Here the database is
// closed under the store. The answer is 500 internal with the message "internal error" and no word of the cause, as
// JSON and as a page. The server's log has the cause one time, with the pattern of the route and not the path,
// because a path can name a metric.
func TestAnInternalErrorNamesNoCause(t *testing.T) {
	d, err := db.Open(t.Context(), dbtest.File(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	h := api.New(&core.Store{DB: d})
	const cause = "database is closed"
	for _, accept := range []string{"application/vnd.siren+json", "text/html"} {
		t.Run(accept, func(t *testing.T) {
			var logged bytes.Buffer
			prev := log.Writer()
			log.SetOutput(&logged) // no test of this package runs in parallel
			t.Cleanup(func() { log.SetOutput(prev) })
			rec := request(h, "GET", "/metrics/weight", "", map[string]string{"Accept": accept})
			body := rec.Body.String()
			if rec.Code != 500 {
				t.Fatalf("status %d, want 500: %.300s", rec.Code, body)
			}
			if strings.Contains(body, cause) || strings.Contains(body, "sql:") {
				t.Errorf("the answer names the cause: %.600s", body)
			}
			if accept == "text/html" {
				if !strings.Contains(body, "internal error") {
					t.Errorf("the page does not say internal error: %.600s", body)
				}
			} else {
				var e api.Entity
				if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
					t.Fatalf("not an entity: %v", err)
				}
				if got := props(&e); got["code"] != "internal" || got["message"] != "internal error" || got["status"] != float64(500) {
					t.Errorf("properties %v, want 500 internal with the message internal error", got)
				}
			}
			if n := strings.Count(logged.String(), cause); n != 1 {
				t.Errorf("the log has the cause %d times, want 1: %q", n, logged.String())
			}
			if !strings.Contains(logged.String(), "GET /metrics/{name}") || strings.Contains(logged.String(), "weight") {
				t.Errorf("the log does not name the route's pattern, or names the path: %q", logged.String())
			}
		})
	}
}

// TestACancelledRequestIsNotLogged: a request whose context is cancelled answers 500 internal with the message
// "internal error", as a fault does, but the log stays empty: the client went away, or the server stopped it.
func TestACancelledRequestIsNotLogged(t *testing.T) {
	_, h := fresh(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged) // no test of this package runs in parallel
	t.Cleanup(func() { log.SetOutput(prev) })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, "GET", "/days", nil))
	var e api.Entity
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("not an entity: %v: %.300s", err, rec.Body.String())
	}
	if got := props(&e); rec.Code != 500 || got["code"] != "internal" || got["message"] != "internal error" {
		t.Errorf("answer %d %v, want 500 internal with the message internal error", rec.Code, got)
	}
	if logged.Len() != 0 {
		t.Errorf("a cancelled request is logged: %q", logged.String())
	}
}

// walk follows a list's next links from first, returning every item title and the page sizes seen.
func walk(t *testing.T, c *client.Client, first string) (titles []string, sizes []int) {
	t.Helper()
	for href, n := first, 0; href != ""; n++ {
		if n > 20 {
			t.Fatal("more than 20 pages")
		}
		e := must(c.Get(t.Context(), href))
		items := 0
		for _, l := range e.Entities {
			if l.Rel[0] == "item" {
				titles = append(titles, l.Title)
				items++
			}
		}
		sizes = append(sizes, items)
		prev, next := hrefOf(e, "prev"), hrefOf(e, "next")
		if (n == 0) != (prev == "") {
			t.Errorf("page %d of %s: prev=%q", n, first, prev)
		}
		href = next
	}
	return titles, sizes
}

func hrefOf(e *api.Entity, rel string) string {
	for _, l := range e.Links {
		for _, r := range l.Rel {
			if r == rel {
				return l.Href
			}
		}
	}
	return ""
}

func TestListsPage(t *testing.T) {
	c, h := fresh(t)
	root := must(c.Get(t.Context(), "/"))
	catalog := must(c.Get(t.Context(), "/actions"))
	for i := 1; i <= 7; i++ {
		must(c.Do(t.Context(), find(root, "create-person"), map[string]string{"title": fmt.Sprintf("Person %d", i)}))
		must(c.Do(t.Context(), find(catalog, "capture"), map[string]string{"day": fmt.Sprintf("2031-03-%02d", i), "text": fmt.Sprintf("needleword [[Ghost %d]]", i)}))
	}
	for _, tc := range []struct {
		list   string
		titles []string
	}{
		{"/browse?kind=person&limit=3", []string{"Person 1", "Person 2", "Person 3", "Person 4", "Person 5", "Person 6", "Person 7"}},
		{"/days?limit=3", []string{"2031-03-07", "2031-03-06", "2031-03-05", "2031-03-04", "2031-03-03", "2031-03-02", "2031-03-01"}},
	} {
		titles, sizes := walk(t, c, tc.list)
		if strings.Join(titles, ",") != strings.Join(tc.titles, ",") || fmt.Sprint(sizes) != "[3 3 1]" {
			t.Errorf("%s: titles=%v sizes=%v", tc.list, titles, sizes)
		}
	}
	// Ghost pages (empty, unlinked, older than 30 days) are paged by the same reader; the core test builds them.
	// Search pages by rank then id, every day page once; the q survives in the links.
	titles, sizes := walk(t, c, "/search?q=needleword&limit=3")
	if len(titles) != 7 || fmt.Sprint(sizes) != "[3 3 1]" {
		t.Errorf("search: titles=%v sizes=%v", titles, sizes)
	}
	seen := map[string]bool{}
	for _, x := range titles {
		if seen[x] {
			t.Errorf("search repeated %s", x)
		}
		seen[x] = true
	}
	second := must(c.Get(t.Context(), "/search?q=needleword&limit=3"))
	if next := hrefOf(second, "next"); !strings.Contains(next, "q=needleword") || !strings.Contains(next, "offset=3") {
		t.Errorf("search next=%q", next)
	}
	// Defaults are the counts of before; the properties say what page this is.
	for _, tc := range []struct {
		list  string
		limit int
	}{{"/days", 60}, {"/browse", 200}, {"/browse?kind=person", 200}, {"/browse?kind=media", 200}, {"/browse?ghosts=1", 200}, {"/search?q=needleword", 50}} {
		e := must(c.Get(t.Context(), tc.list))
		p, _ := e.Properties.(map[string]any)
		if fmt.Sprint(p["limit"]) != fmt.Sprint(tc.limit) || fmt.Sprint(p["offset"]) != "0" || hrefOf(e, "next") != "" || hrefOf(e, "prev") != "" {
			t.Errorf("%s: limit=%v offset=%v next=%q prev=%q", tc.list, p["limit"], p["offset"], hrefOf(e, "next"), hrefOf(e, "prev"))
		}
	}
	// The page past the end is empty with a prev link; bad values are refused.
	last := must(c.Get(t.Context(), "/browse?kind=person&limit=3&offset=9"))
	if len(last.Entities) != 0 || hrefOf(last, "prev") != "/browse?kind=person&limit=3&offset=6" || hrefOf(last, "next") != "" {
		t.Errorf("past the end: %d items prev=%q next=%q", len(last.Entities), hrefOf(last, "prev"), hrefOf(last, "next"))
	}
	for _, bad := range []string{"/browse?limit=0", "/browse?limit=501", "/browse?limit=x", "/browse?offset=-1", "/browse?offset=x", "/search?q=a&limit=0"} {
		if _, err := c.Get(t.Context(), bad); !clientStatus(err, 422) {
			t.Errorf("%s: %v", bad, err)
		}
	}
	// The browser sees the links.
	body := browse(t, h, "/browse?kind=person&limit=3&offset=3")
	if !strings.Contains(body, `href="/browse?kind=person&amp;limit=3&amp;offset=0" rel="prev"`) || !strings.Contains(body, `href="/browse?kind=person&amp;limit=3&amp;offset=6" rel="next"`) {
		t.Errorf("people page 2: %.600s", body)
	}
	if body := browse(t, h, "/search?q=needleword&limit=3"); !strings.Contains(body, `rel="next"`) || strings.Contains(body, `rel="prev"`) {
		t.Errorf("search page 1 pager: %.600s", body)
	}
}
