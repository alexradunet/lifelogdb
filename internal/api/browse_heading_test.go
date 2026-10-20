package api_test

import (
	"regexp"
	"testing"

	"lifelog/internal/api/apitest"
)

// TestTheHeadingOfBrowseNamesTheKind: with no category chosen, the heading is the kind's name, not Everything.
func TestTheHeadingOfBrowseNamesTheKind(t *testing.T) {
	_, h, _ := apitest.Fresh(t, "cli")
	h1 := regexp.MustCompile(`<h1>([^<]*)</h1>`)
	for kind, want := range map[string]string{"page": "Pages", "person": "People", "place": "Places", "media": "Media", "document": "Documents", "metric": "Metrics"} {
		m := h1.FindStringSubmatch(browse(t, h, "/browse?kind="+kind))
		if m == nil || m[1] != want {
			t.Errorf("kind %s: the heading is %v, want %s", kind, m, want)
		}
	}
}
