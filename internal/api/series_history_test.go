package api_test

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"lifelog/internal/api"
)

func TestAllReadingsIncludesEntireAdmittedCalendar(t *testing.T) {
	c, h := fresh(t)
	actions := must(c.Get("/actions"))
	metric := must(c.Do(find(actions, "register-metric"), map[string]string{"name": "Steps", "unit": "steps"}))
	must(c.Do(find(actions, "create-page"), map[string]string{"title": "Workout"}))
	session := must(c.Do(find(actions, "capture-session"), map[string]string{"kind": "Workout", "day": "0000-01-01", "start_local": "0000-01-01T12:00:00.000"}))
	sid := strings.TrimPrefix(href(session, "self"), "/sessions/")
	days := []string{"0000-01-01", "0000-02-29", "0001-01-01", "0001-01-02"}
	var plain, scoped []string
	values := map[string]string{}
	for i, day := range days {
		for _, scope := range []string{"", sid} {
			fact := must(c.Do(find(actions, "record"), map[string]string{"metric": "Steps", "day": day, "value": fmt.Sprint(10 + i), "session_id": scope}))
			id := href(fact, "self")
			values[id] = fmt.Sprint(10 + i)
			if scope == "" {
				plain = append(plain, id)
			} else {
				scoped = append(scoped, id)
			}
		}
	}
	metricPage := must(c.Get(href(metric, "page")))
	must(c.Do(find(metricPage, "rename"), map[string]string{"title": "Stride count"}))
	must(c.Do(find(session, "tombstone-session"), nil))
	assertRows := func(t *testing.T, e *api.Entity, want []string) {
		t.Helper()
		rows := e.Properties.(map[string]any)["readings"].([]any)
		got := []string{}
		for _, row := range rows {
			r := row.(map[string]any)
			got = append(got, fmt.Sprint("/measurements/", r["id"]))
			id := got[len(got)-1]
			if fmt.Sprint(r["value"]) != values[id] {
				t.Fatalf("row %s value=%v want=%v", id, r["value"], values[id])
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ordered identities=%v want=%v", got, want)
		}
	}
	for _, tc := range []struct {
		scope, history string
		want           []string
	}{
		{"unassociated", "0", plain[:3]}, {"session", "0", []string{}}, {"session", "1", scoped[:3]},
		{"all", "0", plain[:3]}, {"all", "1", []string{plain[0], scoped[0], plain[1], scoped[1], plain[2], scoped[2]}},
	} {
		t.Run(tc.scope+tc.history, func(t *testing.T) {
			q := url.Values{"from": {"0000-12-31"}, "to": {"0001-01-01"}, "scope": {tc.scope}, "include_deleted": {tc.history}}
			q.Set("session_id", "0")
			if tc.scope == "session" {
				q.Set("session_id", sid)
			}
			bounded := must(c.Get("/metrics/Steps?" + q.Encode())) // retained alias
			link := href(bounded, "all-readings")
			expanded := must(c.Get(link))
			assertRows(t, expanded, tc.want)
			u, err := url.Parse(href(expanded, "self"))
			if err != nil {
				t.Fatal(err)
			}
			if u.Query().Get("all_history") != "1" || u.Query().Has("from") || u.Query().Get("scope") != tc.scope || u.Query().Get("include_deleted") != tc.history || u.Query().Get("session_id") != q.Get("session_id") {
				t.Fatalf("history navigation=%s", u)
			}
			assertRows(t, must(c.Get(href(expanded, "self"))), tc.want)
			assertRows(t, must(c.Get(href(expanded, "all-readings"))), tc.want)
			if body := browse(t, h, link); !strings.Contains(body, "All history") {
				t.Fatal("missing explicit history label")
			}
		})
	}
	// Explicit ranges retain their exclusive lower and inclusive upper limits.
	bounded := must(c.Get("/metrics/Stride%20count?from=0000-01-01&to=0000-02-29"))
	rows := bounded.Properties.(map[string]any)["readings"].([]any)
	if len(rows) != 1 || fmt.Sprint("/measurements/", rows[0].(map[string]any)["id"]) != plain[1] || fmt.Sprint(rows[0].(map[string]any)["value"]) != "11" {
		t.Fatal("bounded range semantics changed")
	}
	// A default window near the minimum must not manufacture negative-year input.
	assertRows(t, must(c.Get("/metrics/Steps?to=0000-01-01")), plain[:1])
	for _, q := range []string{"all_history=2", "all_history=1&from=0000-01-01", "all_history=1&to=-001-01-01"} {
		if _, err := c.Get("/metrics/Steps?" + q); err == nil {
			t.Fatalf("accepted invalid history query %s", q)
		}
	}
}
