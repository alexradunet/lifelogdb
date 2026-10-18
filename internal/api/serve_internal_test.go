package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"lifelog/internal/api/siren"
	"lifelog/internal/core"
)

// backCases are the values of the field back that a browser form can send, and what backOf keeps of each: the value
// when it is a path on this server, "" otherwise. They are also the seed corpus of FuzzBackOf.
var backCases = []struct{ back, want string }{
	{"/days?limit=5&offset=10", "/days?limit=5&offset=10"},
	{"/", "/"},
	{"/days/2026-09-29?x=%2F%2F", "/days/2026-09-29?x=%2F%2F"},
	{"/" + strings.Repeat("a", 2047), "/" + strings.Repeat("a", 2047)}, // 2048 bytes: maxBack
	{"", ""},
	{"/" + strings.Repeat("a", 2048), ""}, // 2049 bytes
	{"//evil.example/", ""},
	{"///evil.example/", ""},
	{"/\\evil.example/", ""},
	{"https://evil.example/", ""},
	{"javascript:alert(1)", ""},
	{"evil", ""},
	{" /days", ""},
	{"/a\r\nSet-Cookie: x=1", ""},
	{"/\t/evil.example/", ""},
	{"/a\x7fb", ""},
	{"/page#part", ""},
	{"/%zz", ""}, // not a URL: the escape is not hex
}

// requestWithBack is a POST whose form has the field back.
func requestWithBack(back string) *http.Request {
	r := httptest.NewRequest("POST", "/days/2026-09-29/capture", nil)
	r.PostForm = url.Values{"back": {back}}
	return r
}

// TestBackOf: backOf keeps a path on this server and drops every other value (docs/plans/091-the-browser-for-daily-use.md).
func TestBackOf(t *testing.T) {
	for _, tc := range backCases {
		if got := backOf(requestWithBack(tc.back)); got != tc.want {
			t.Errorf("backOf(%q) = %q, want %q", tc.back, got, tc.want)
		}
	}
}

// FuzzBackOf: whatever a form sends, backOf gives the fallback "" or the value itself, and a value it keeps stays on
// this server when a browser follows it. The check reads the value as a browser does (the WHATWG URL standard): it
// removes a tab and a newline, and it reads a backslash as a slash. It then resolves the value against a page of this
// server, and the host must not change. A kept value has no control character either: it goes into the header
// Location.
func FuzzBackOf(f *testing.F) {
	for _, tc := range backCases {
		f.Add(tc.back)
	}
	base, err := url.Parse("http://lifelog.local/days/2026-09-29")
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, back string) {
		got := backOf(requestWithBack(back))
		if got == "" {
			return
		}
		if got != back {
			t.Fatalf("backOf(%q) = %q, want the value or \"\"", back, got)
		}
		if strings.ContainsFunc(got, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			t.Fatalf("backOf kept %q, which has a control character", got)
		}
		read := strings.NewReplacer("\t", "", "\n", "", "\r", "", "\\", "/").Replace(got)
		ref, err := url.Parse(read)
		if err != nil {
			t.Fatalf("backOf kept %q, which a browser reads as %q, not a URL: %v", got, read, err)
		}
		if u := base.ResolveReference(ref); u.Scheme != base.Scheme || u.Host != base.Host {
			t.Fatalf("backOf kept %q, and a browser goes to %s", got, u)
		}
	})
}

// TestErrorEntity: each error becomes one error entity, with the status, the code and the message. An error that
// core did not type is a fault: the client gets "internal error", and the log gets the cause one time, under the
// pattern of the route. A cancelled request is not logged.
func TestErrorEntity(t *testing.T) {
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged) // no test of this package runs in parallel
	t.Cleanup(func() { log.SetOutput(prev) })
	r := httptest.NewRequest("GET", "/metrics/weight", nil)
	r.Pattern = "GET /metrics/{name}"
	const home = "index:/:Home"
	for _, tc := range []struct {
		name string
		err  error
		want string // status code message, then each link as rel:href:title
		log  bool   // the log has the cause
	}{
		{"a 409 with its code", &core.Error{Status: 409, Msg: "the version is stale", Code: "stale_version"}, "409 stale_version the version is stale " + home, false},
		{"a title in use", &core.ExistsError{ID: 7, Title: "Taken"}, "409 exists a page titled Taken already exists " + home + " existing:/pages/7:Taken", false},
		{"a 404", core.NotFound("no page %d", 9), "404 not_found no page 9 " + home, false},
		{"a 422", core.Invalid("value %q is not a number", "x"), `422 invalid value "x" is not a number ` + home, false},
		{"a 409 with no code", core.Conflict("the page is deleted"), "409 conflict the page is deleted " + home, false},
		{"a 400", &core.Error{Status: 400, Msg: "bad"}, "400 bad_request bad " + home, false},
		{"a 401", &core.Error{Status: 401, Msg: "who"}, "401 unauthorized who " + home, false},
		{"a 403", &core.Error{Status: 403, Msg: "the owner's"}, "403 forbidden the owner's " + home, false},
		{"a 413", &core.Error{Status: 413, Msg: "big"}, "413 too_large big " + home, false},
		{"a 415", &core.Error{Status: 415, Msg: "multipart"}, "415 unsupported_media multipart " + home, false},
		{"a status with no general code", &core.Error{Status: 418, Msg: "odd"}, "418 internal odd " + home, false},
		{"a wrapped refusal", fmt.Errorf("save: %w", core.Invalid("no")), "422 invalid save: no " + home, false},
		{"a timeout", fmt.Errorf("read: %w", context.DeadlineExceeded), "504 timeout read: context deadline exceeded " + home, false},
		{"a refusal that wraps a timeout", &core.Error{Status: 422, Msg: "slow", Err: context.DeadlineExceeded}, "504 timeout slow " + home, false},
		{"a fault", errors.New("disk I/O error in /private/life.db"), "500 internal internal error " + home, true},
		{"a cancelled request", fmt.Errorf("read: %w", context.Canceled), "500 internal internal error " + home, false},
	} {
		logged.Reset()
		e, status := errorEntity(r, tc.err)
		p, _ := e.Properties.(map[string]any)
		got := fmt.Sprint(p["status"], " ", p["code"], " ", p["message"])
		for _, l := range e.Links {
			got += fmt.Sprintf(" %s:%s:%s", strings.Join(l.Rel, ","), l.Href, l.Title)
		}
		if got != tc.want || status != p["status"] || e.Class[0] != "error" || e.Title != "Error" {
			t.Errorf("%s: %d %v %q %s, want %s", tc.name, status, e.Class, e.Title, got, tc.want)
		}
		if tc.log != (logged.Len() > 0) {
			t.Errorf("%s: the log is %q", tc.name, logged.String())
		}
		if tc.log && (strings.Count(logged.String(), tc.err.Error()) != 1 || !strings.Contains(logged.String(), "GET /metrics/{name}")) {
			t.Errorf("%s: the log %q does not have the cause one time under the pattern of the route", tc.name, logged.String())
		}
	}
}

// TestAnAnswerThatJSONCannotCarryIsAFault: when an entity has a value that JSON cannot carry (a NaN, an infinity), a
// program gets 500 and the error entity with the general message, not 200 and a cut body. The server's log has the
// cause one time, with the pattern of the route and not the path.
func TestAnAnswerThatJSONCannotCarryIsAFault(t *testing.T) {
	for name, e := range map[string]*siren.Entity{
		"properties that are not JSON": {Class: []string{"thing"}, Title: "Thing", Properties: map[string]any{"value": math.NaN()}},
		"a result that is not JSON":    {Class: []string{"thing"}, Title: "Thing", Properties: map[string]any{"value": 1}, Result: map[string]any{"value": math.Inf(1)}},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/things/1", nil)
			req.Pattern = "GET /things/{id}" // as the routes set it
			req.Header.Set("Accept", "application/vnd.siren+json")
			rec := httptest.NewRecorder()
			var logged bytes.Buffer
			prev := log.Writer()
			log.SetOutput(&logged) // no test of this package runs in parallel
			t.Cleanup(func() { log.SetOutput(prev) })
			answer(rec, req, http.StatusOK, e, frame{})
			if n := strings.Count(logged.String(), "cannot be encoded"); n != 1 || !strings.Contains(logged.String(), "GET /things/{id}") ||
				strings.Contains(logged.String(), "/things/1") {
				t.Errorf("the log does not have the cause one time with the route's pattern, or names the path: %q", logged.String())
			}
			var got struct {
				Class      []string
				Properties struct {
					Status        int
					Code, Message string
				}
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("the body is not JSON: %v: %.300s", err, rec.Body.String())
			}
			if rec.Code != http.StatusInternalServerError || rec.Header().Get("Content-Type") != "application/vnd.siren+json" ||
				!slices.Equal(got.Class, []string{"error"}) || got.Properties.Status != http.StatusInternalServerError ||
				got.Properties.Code != "internal" || got.Properties.Message != "internal error" {
				t.Errorf("status %d %q, want 500 and the error entity with the general message: %.300s",
					rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
			}
		})
	}
}

// TestAnswerDrawsByTheFrame: answer gives a program JSON and a browser a page; a page-only frame gives a program the
// page too. A page with no shell has no sidebar. The GET of a refused form gets the entity and the status, no bytes.
func TestAnswerDrawsByTheFrame(t *testing.T) {
	e := &siren.Entity{Class: []string{"preview"}, Title: "Preview", Properties: map[string]any{"body": "words"}}
	for _, c := range []struct {
		name, accept string
		fr           frame
		want         string // the Content-Type
	}{
		{"a program", "application/vnd.siren+json", frame{}, "application/vnd.siren+json"},
		{"a browser", "text/html", frame{}, "text/html; charset=utf-8"},
		{"a program, a page-only frame", "application/vnd.siren+json", frame{pageOnly: true}, "text/html; charset=utf-8"},
	} {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/preview", nil)
			req.Header.Set("Accept", c.accept)
			rec := httptest.NewRecorder()
			answer(rec, req, http.StatusAccepted, e, c.fr)
			body := rec.Body.String()
			if rec.Code != http.StatusAccepted || rec.Header().Get("Content-Type") != c.want {
				t.Errorf("%d %q, want 202 %q", rec.Code, rec.Header().Get("Content-Type"), c.want)
			}
			if strings.HasPrefix(c.want, "text/html") && (!strings.Contains(body, "<h1>Preview</h1>") || strings.Contains(body, `class="rail"`)) {
				t.Errorf("not the page with no sidebar: %.300s", body)
			}
			if !strings.HasPrefix(c.want, "text/html") && !strings.Contains(body, `"title": "Preview"`) {
				t.Errorf("not the entity: %.300s", body)
			}
		})
	}
}

// TestAPageThatCannotBeDrawnNamesNoCause: when a page cannot be drawn (its properties or its result do not encode as
// JSON, or its template fails on them), the browser gets a 500 that says no word of the internal cause. The server's
// log has the cause one time, with the pattern of the route and not the path.
func TestAPageThatCannotBeDrawnNamesNoCause(t *testing.T) {
	for name, e := range map[string]*siren.Entity{
		"properties that are not JSON": {Class: []string{"thing"}, Title: "Thing", Properties: map[string]any{"value": math.NaN()}},
		"a result that is not JSON":    {Class: []string{"thing"}, Title: "Thing", Properties: map[string]any{"value": 1}, Result: map[string]any{"value": math.Inf(1)}},
		"a template that fails":        {Class: []string{"error"}, Title: "Error", Properties: "not an object"},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/things/1", nil)
			req.Pattern = "GET /things/{id}" // as the routes set it
			req.Header.Set("Accept", "text/html")
			rec := httptest.NewRecorder()
			var logged bytes.Buffer
			prev := log.Writer()
			log.SetOutput(&logged) // no test of this package runs in parallel
			t.Cleanup(func() { log.SetOutput(prev) })
			answer(rec, req, http.StatusOK, e, frame{})
			body := rec.Body.String()
			if n := strings.Count(logged.String(), "cannot be drawn"); n != 1 || !strings.Contains(logged.String(), "GET /things/{id}") ||
				strings.Contains(logged.String(), "/things/1") {
				t.Errorf("the log does not have the cause one time with the route's pattern, or names the path: %q", logged.String())
			}
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status %d, want 500", rec.Code)
			}
			if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(body, "</html>") {
				t.Errorf("the answer is not a page: %q %.200s", rec.Header().Get("Content-Type"), body)
			}
			for _, cause := range []string{"template:", "executing", "evaluate", "json:", "unsupported value"} {
				if strings.Contains(body, cause) {
					t.Errorf("the page names the cause (%q): %.300s", cause, body)
				}
			}
		})
	}
}
