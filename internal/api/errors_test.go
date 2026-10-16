package api_test

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http/httptest"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
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
	c, h, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	page := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Taken", "body": "first"}))
	for _, tc := range []struct {
		name   string
		err    error
		code   string
		status int
	}{
		{"stale save", func() error {
			_, err := c.Do(t.Context(), apitest.Action(t, page, "save-body"), map[string]string{"body": "second", "version": "0"})
			return err
		}(), "stale_version", 409},
		{"taken title", func() error {
			_, err := c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "taken"})
			return err
		}(), "exists", 409},
		{"bad day", func() error { _, err := c.Get(t.Context(), "/days/2026-13-01"); return err }(), "invalid", 422},
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
// closed under the store. The answer is 500 with no word of the cause, as JSON and as a page, and the page says
// internal error; the entity of the answer is TestErrorEntity's. The server's log has the cause one time, with the
// pattern of the route and not the path, because a path can name a metric.
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
			if accept == "text/html" && !strings.Contains(body, "internal error") {
				t.Errorf("the page does not say internal error: %.600s", body)
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

// TestACancelledRequestIsNotLogged: a request whose context is cancelled answers 500, as a fault does, but the log
// stays empty: the client went away, or the server stopped it. The entity of the answer is TestErrorEntity's.
func TestACancelledRequestIsNotLogged(t *testing.T) {
	_, h, _ := apitest.Fresh(t, "cli")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged) // no test of this package runs in parallel
	t.Cleanup(func() { log.SetOutput(prev) })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, "GET", "/days", nil))
	if rec.Code != 500 {
		t.Errorf("answer %d, want 500: %.300s", rec.Code, rec.Body.String())
	}
	if logged.Len() != 0 {
		t.Errorf("a cancelled request is logged: %q", logged.String())
	}
}

// code is the error code a refused write answered with, "" for a success.
func code(err error) string {
	var ce *client.Error
	if !errors.As(err, &ce) {
		return ""
	}
	props, _ := ce.Entity.Properties.(map[string]any)
	c, _ := props["code"].(string)
	return c
}

// A write with an old version answers stale_version on every resource that carries one (API.md: "a save with an old
// version"), and a 409 that is not a version's keeps the general code conflict
// (docs/plans/098-the-codebase-made-cohesive.md).
func TestStaleVersionIsTheSameCodeOnEveryResource(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	page := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Garden", "body": "the beds"}))
	save := apitest.Action(t, page, "save-body")
	apitest.Must(t)(c.Do(t.Context(), save, map[string]string{"body": "the beds, dug"}))
	if _, err := c.Do(t.Context(), save, map[string]string{"body": "the beds, dug again"}); code(err) != "stale_version" {
		t.Errorf("a page saved with an old version: code %q, want stale_version (%v)", code(err), err)
	}

	// Not a version's 409: a file kept under the title of a page that has words of its own.
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Scan.pdf", "body": "my own words"}))
	if _, err := c.Do(t.Context(), apitest.Action(t, root, "add-file"), map[string]string{"title": "Scan.pdf", "sha256": strings.Repeat("a", 64), "mime": "application/pdf", "body": "OCR text"}); code(err) != "conflict" {
		t.Errorf("a file over a page with words: code %q, want conflict (%v)", code(err), err)
	}
}
