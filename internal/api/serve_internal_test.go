package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

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
