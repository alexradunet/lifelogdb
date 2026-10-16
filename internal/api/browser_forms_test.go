package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api"
)

// formRE finds a POST form of a page: its action and what it holds. This application's own markup is regular enough
// to read this way; a form never nests. hiddenRE finds a hidden field of a form: its name and its value.
var (
	formRE   = regexp.MustCompile(`(?s)<form method="POST" action="([^"]+)"[^>]*>(.*?)</form>`)
	hiddenRE = regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)">`)
)

// withoutReceipt is a Location without the feedback receipt a write adds, so a test compares the path it returned to.
func withoutReceipt(t *testing.T, location string) string {
	t.Helper()
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("Location %q: %v", location, err)
	}
	q := u.Query()
	if len(q["feedback"]) != 1 {
		t.Errorf("Location %q carries no receipt", location)
	}
	q.Del("feedback")
	u.RawQuery = q.Encode()
	return u.String()
}

// fieldOf is the value of an action's field, "" when it has none.
func fieldOf(a api.Action, name string) string {
	for _, f := range a.Fields {
		if f.Name == name && f.Value != nil {
			return fmt.Sprint(f.Value)
		}
	}
	return ""
}

// fixedToday is the server's local day in the tests of a day: a Wednesday.
const fixedToday = "2031-05-14"

// dayOf is the local day n days from fixedToday.
func dayOf(n int) string {
	t, _ := time.Parse(time.DateOnly, fixedToday)
	return t.AddDate(0, 0, n).Format(time.DateOnly)
}

// TestABrowserFormReturnsToThePageThatSentIt: a form that names a path on this server returns there with the
// feedback; any other value is ignored and the browser goes to what it changed, as before; a JSON client never
// follows back.
func TestABrowserFormReturnsToThePageThatSentIt(t *testing.T) {
	c, h := fresh(t)
	for _, tc := range []struct{ back, want string }{
		{"/days?limit=5&offset=10", "/days?limit=5&offset=10"},
		{"/", "/"},
		{"/days/2026-09-29?x=%2F%2F", "/days/2026-09-29?x=%2F%2F"},
		{"", "/days/2026-09-29"},
		{"//evil.example/", "/days/2026-09-29"},
		{"/\\evil.example/", "/days/2026-09-29"},
		{"https://evil.example/", "/days/2026-09-29"},
		{"javascript:alert(1)", "/days/2026-09-29"},
		{"evil", "/days/2026-09-29"},
		{"/a\r\nSet-Cookie: x=1", "/days/2026-09-29"},
		{"/page#part", "/days/2026-09-29"},
		{"/" + strings.Repeat("a", 2048), "/days/2026-09-29"},
	} {
		rec := submit(h, "/days/2026-09-29/capture", url.Values{"text": {"a line"}, "back": {tc.back}}, "")
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("back %q: %d", tc.back, rec.Code)
		}
		if got := withoutReceipt(t, rec.Header().Get("Location")); got != tc.want {
			t.Errorf("back %q: Location %q, want %q", tc.back, got, tc.want)
		}
	}
	// The feedback of a write that has some goes to the page returned to, with its receipt.
	rec := submit(h, "/pages", url.Values{"title": {"Notes"}, "body": {"[[bad/name]]"}, "back": {"/browse?kind=person"}}, "")
	if loc := rec.Header().Get("Location"); !feedbackLocation(loc, "/browse?kind=person") || !strings.Contains(browse(t, h, loc), "bad/name") {
		t.Errorf("feedback did not follow the return path: %q", loc)
	}
	// A JSON client: the answer is the resource, whatever back says.
	req := httptest.NewRequest("POST", "/days/2026-09-29/capture", strings.NewReader(url.Values{"text": {"json"}, "back": {"/browse?kind=person"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != http.StatusOK || out.Header().Get("Location") != "/days/2026-09-29" {
		t.Errorf("a JSON client followed back: %d %q", out.Code, out.Header().Get("Location"))
	}
	if n := len(must(c.Get(t.Context(), "/days/2026-09-29")).Properties.(map[string]any)["view"].([]any)); n == 0 {
		t.Error("the writes were not done")
	}
}
