package api_test

import (
	"bytes"
	"log"
	"net/url"
	"strings"
	"testing"

	"lifelog/internal/api"
)

// TestAWrongLoginLeavesOneLogLine: a wrong token on /login is sent back to the form, and the owner can see it in the
// log: one line with the route. The line never has the token that was sent, and a right token leaves no line.
func TestAWrongLoginLeavesOneLogLine(t *testing.T) {
	h, _ := publicHandler(t, api.PublicOptions{Token: testToken})
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged) // no test of this package runs in parallel
	t.Cleanup(func() { log.SetOutput(prev) })
	const wrong = "synthetic-wrong-token-9876543210"
	rec := request(h, "POST", "/login", "token="+url.QueryEscape(wrong), map[string]string{"Sec-Fetch-Site": "same-origin"})
	if rec.Code != 303 || rec.Header().Get("Location") != "/login?failed=1" {
		t.Fatalf("wrong login: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	out := logged.String()
	if n := strings.Count(out, "\n"); n != 1 || !strings.Contains(out, "POST /login") {
		t.Errorf("want one line about POST /login, got %q", out)
	}
	if strings.Contains(out, wrong) || strings.Contains(out, testToken) {
		t.Errorf("the log has a token: %q", out)
	}
	logged.Reset()
	if rec := request(h, "POST", "/login", "token="+url.QueryEscape(testToken), map[string]string{"Sec-Fetch-Site": "same-origin"}); rec.Code != 303 || rec.Header().Get("Location") != "/" {
		t.Fatalf("right login: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if logged.Len() != 0 {
		t.Errorf("a right login was logged: %q", logged.String())
	}
}
