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

// TestTheCatalogNamesNoRemovedFeature: no action's description or field title tells a client of a feature the
// docs removed: a photo's position (D21) or a task and what is due (D23). An MCP agent reads these texts as the
// truth. Each pattern names a removed feature, not a bare word, so a true text such as "due to" or the position of a
// row stays free.
func TestTheCatalogNamesNoRemovedFeature(t *testing.T) {
	c, _ := fresh(t)
	removed := regexp.MustCompile(`(?i)\b(` + strings.Join([]string{
		`tasks?`, `overdue`, `what (is|was) due`, `due (days?|dates?)`, // D23
		`(a|the) photo's position`, `positions? of (a|the) photos?`, `and position`, `positions? near`,
		`position as its point`, `the place it was taken`, // D21
	}, "|") + `)\b`)
	// The texts that the catalog said before plan 121 removed them, and two true texts.
	for s, want := range map[string]bool{
		"Open the day view of a local day (YYYY-MM-DD): its journal, what was due and its readings.": true,
		"A photo's own day (its EXIF date) and position are read from the original":                  true,
		"its day page gets an at link to the place it was taken in, and shows it":                    true,
		"a position near no place is reported":                                                       true,
		"at names the place, which takes the position as its point":                                  true,
		"Refused due to a stale version.":                                                            false,
		"The position of a row in the list.":                                                         false,
	} {
		if got := removed.MatchString(s); got != want {
			t.Fatalf("the pattern finds a removed feature in %q: %v, want %v", s, got, want)
		}
	}
	for _, a := range must(c.Get(t.Context(), "/actions")).Actions {
		texts := []string{a.Title, a.Description}
		for _, f := range a.Fields {
			texts = append(texts, f.Title)
		}
		for _, s := range texts {
			if w := removed.FindString(s); w != "" {
				t.Errorf("%s says %q: %s", a.Name, w, s)
			}
		}
	}
}
