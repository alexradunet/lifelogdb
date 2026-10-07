package api_test

import (
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestTemporalCollectionsRetainLinksAndEvidence(t *testing.T) {
	c, h := fresh(t)
	actions := must(c.Get("/actions"))
	must(c.Do(find(actions, "create-page"), map[string]string{"title": "Workout"}))
	first := must(c.Do(find(actions, "capture-session"), map[string]string{"kind": "Workout", "day": "2020-01-01", "start_at": "2020-01-01T12:00:00.000Z"}))
	second := must(c.Do(find(actions, "capture-session"), map[string]string{"kind": "Workout", "day": "2020-01-02", "start_local": "2020-01-01T23:00:00.000", "end_local": "2020-01-02T07:00:00.000", "start_zone_unverified": "Claimed/Zone"}))
	period := must(c.Do(find(actions, "create-period"), map[string]string{"title": "Synthetic study", "start_boundary": "2018-09", "end_boundary": ".."}))
	for _, tc := range []struct{ collection, member string }{{"/sessions", href(first, "self")}, {"/sessions", href(second, "self")}, {"/periods", href(period, "self")}} {
		t.Run(tc.collection+tc.member, func(t *testing.T) {
			list := must(c.Get(tc.collection))
			found := false
			for _, link := range list.Entities {
				found = found || link.Href == tc.member
			}
			for _, link := range list.Links {
				found = found || link.Href == tc.member
			}
			if !found {
				t.Errorf("collection %s offers no member link to %s", tc.collection, tc.member)
			}
			body := browse(t, h, tc.collection)
			if !strings.Contains(body, `href="`+tc.member+`"`) {
				t.Errorf("browser collection %s has no member navigation to %s", tc.collection, tc.member)
			}
		})
	}
	body := browse(t, h, "/sessions")
	for _, want := range []string{"2020-01-01T23:00:00.000", "2020-01-02T07:00:00.000", "Claimed/Zone"} {
		if !strings.Contains(body, want) {
			t.Errorf("mixed-basis session collection silently omits %q", want)
		}
	}
}

func TestAllReadingsLinkPreservesScope(t *testing.T) {
	c, h := fresh(t)
	actions := must(c.Get("/actions"))
	must(c.Do(find(actions, "create-page"), map[string]string{"title": "Workout"}))
	must(c.Do(find(actions, "register-metric"), map[string]string{"name": "Steps", "unit": "steps"}))
	session := must(c.Do(find(actions, "capture-session"), map[string]string{"kind": "Workout", "day": "2020-01-02", "start_at": "2020-01-02T12:00:00.000Z"}))
	id := strings.TrimPrefix(href(session, "self"), "/sessions/")

	must(c.Do(find(actions, "record"), map[string]string{"metric": "Steps", "day": "2020-01-02", "value": "10000"}))
	fact := must(c.Do(find(actions, "record"), map[string]string{"metric": "Steps", "day": "2020-01-02", "value": "4000", "session_id": id}))
	must(c.Do(find(session, "tombstone-session"), nil))
	body := browse(t, h, "/metrics/Steps?from=2020-01-01&to=2020-01-02&scope=session&session_id="+id+"&include_deleted=1")
	match := regexp.MustCompile(`<a href="([^"]+)">All readings</a>`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatal("missing existing all-readings range link")
	}
	u, err := url.Parse(html.UnescapeString(match[1]))
	if err != nil {
		t.Fatal(err)
	}
	expanded := must(c.Get(u.String()))
	readings := expanded.Properties.(map[string]any)["readings"].([]any)
	if len(readings) != 1 || readings[0].(map[string]any)["id"] != fact.Properties.(map[string]any)["id"] {
		t.Fatal("range navigation changed historical scope")
	}
	q := u.Query()
	if q.Get("scope") != "session" || q.Get("session_id") != id || q.Get("include_deleted") != "1" {
		t.Errorf("all-readings link silently changes scope/history: %s", u)
	}
}
