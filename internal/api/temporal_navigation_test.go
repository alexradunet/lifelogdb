package api_test

import (
	"strings"
	"testing"
)

func TestTemporalCollectionsRetainLinksAndEvidence(t *testing.T) {
	c, h := fresh(t)
	actions := must(c.Get(t.Context(), "/actions"))
	period := must(c.Do(t.Context(), find(actions, "create-period"), map[string]string{"title": "Synthetic study", "start_boundary": "2018-09", "end_boundary": ".."}))
	member := href(period, "self")
	list := must(c.Get(t.Context(), "/periods"))
	found := false
	for _, link := range list.Entities {
		found = found || link.Href == member
	}
	for _, link := range list.Links {
		found = found || link.Href == member
	}
	if !found {
		t.Errorf("/periods offers no member link to %s", member)
	}
	if body := browse(t, h, "/periods"); !strings.Contains(body, `href="`+member+`"`) {
		t.Errorf("browser /periods has no member navigation to %s", member)
	}
}
