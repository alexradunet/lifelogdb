package api_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestHomeAndADayLinkOnlyToServedPaths: every link of Home and of a day page opens a page, and no link sends a query
// that its page does not read (docs/plans/117-one-browse-view.md, step 0).
func TestHomeAndADayLinkOnlyToServedPaths(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/days/2031-05-03")), "capture"), map[string]string{"text": "Synthetic entry"}))
	for _, page := range []string{"/", "/days/2031-05-03", "/days/" + fixedToday} {
		body := browse(t, h, page)
		for _, m := range regexp.MustCompile(`href="(/[^"]*)"`).FindAllStringSubmatch(body, -1) {
			href, _, _ := strings.Cut(strings.ReplaceAll(m[1], "&amp;", "&"), "#") // a browser sends no fragment
			if strings.HasPrefix(href, "/metrics?show=") {
				t.Errorf("%s links %s: /metrics reads no show", page, href)
			}
			req := httptest.NewRequest("GET", href, nil)
			req.Header.Set("Accept", "text/html")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code == http.StatusNotFound {
				t.Errorf("%s links %s, which answers 404", page, href)
			}
		}
	}
}

// TestActionDescriptionsNameOnlyTheirFields: a field name in an action's description (a word with an underscore) is a
// field of that action, so a client is never told to send a field the action does not take.
func TestActionDescriptionsNameOnlyTheirFields(t *testing.T) {
	c, _ := fresh(t)
	cat := must(c.Get(t.Context(), "/actions"))
	for _, a := range cat.Actions {
		var fields []string
		for _, f := range a.Fields {
			fields = append(fields, f.Name)
		}
		for _, w := range regexp.MustCompile(`\b[a-z]+_[a-z_]+\b`).FindAllString(a.Description, -1) {
			if !slices.Contains(fields, w) {
				t.Errorf("%s: the description names %s, which is not one of its fields %v", a.Name, w, fields)
			}
		}
	}
}
