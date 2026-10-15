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
	actions := must(c.Get(t.Context(), "/actions"))
	metric := must(c.Do(t.Context(), find(actions, "register-metric"), map[string]string{"name": "Steps", "unit": "steps"}))
	days := []string{"0000-01-01", "0000-02-29", "0001-01-01", "0001-01-02"}
	var plain []string
	values := map[string]string{}
	for i, day := range days {
		fact := must(c.Do(t.Context(), find(actions, "record"), map[string]string{"metric": "Steps", "day": day, "value": fmt.Sprint(10 + i)}))
		id := href(fact, "self")
		values[id] = fmt.Sprint(10 + i)
		plain = append(plain, id)
	}
	metricPage := must(c.Get(t.Context(), href(metric, "page")))
	must(c.Do(t.Context(), find(metricPage, "rename"), map[string]string{"title": "Stride count"}))
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
	// The all-readings link drops the lower bound and keeps the upper one; following it again changes nothing.
	q := url.Values{"from": {"0000-12-31"}, "to": {"0001-01-01"}}
	bounded := must(c.Get(t.Context(), "/metrics/Steps?"+q.Encode())) // retained alias
	link := href(bounded, "all-readings")
	expanded := must(c.Get(t.Context(), link))
	assertRows(t, expanded, plain[:3])
	u, err := url.Parse(href(expanded, "self"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("all_history") != "1" || u.Query().Has("from") || u.Query().Get("to") != "0001-01-01" {
		t.Fatalf("history navigation=%s", u)
	}
	assertRows(t, must(c.Get(t.Context(), href(expanded, "self"))), plain[:3])
	assertRows(t, must(c.Get(t.Context(), href(expanded, "all-readings"))), plain[:3])
	if body := browse(t, h, link); !strings.Contains(body, "All history") {
		t.Fatal("missing explicit history label")
	}
	// Explicit ranges retain their exclusive lower and inclusive upper limits.
	bounded = must(c.Get(t.Context(), "/metrics/Stride%20count?from=0000-01-01&to=0000-02-29"))
	rows := bounded.Properties.(map[string]any)["readings"].([]any)
	if len(rows) != 1 || fmt.Sprint("/measurements/", rows[0].(map[string]any)["id"]) != plain[1] || fmt.Sprint(rows[0].(map[string]any)["value"]) != "11" {
		t.Fatal("bounded range semantics changed")
	}
	// A default window near the minimum must not manufacture negative-year input.
	assertRows(t, must(c.Get(t.Context(), "/metrics/Steps?to=0000-01-01")), plain[:1])
	for _, q := range []string{"all_history=2", "all_history=1&from=0000-01-01", "all_history=1&to=-001-01-01"} {
		if _, err := c.Get(t.Context(), "/metrics/Steps?"+q); err == nil {
			t.Fatalf("accepted invalid history query %s", q)
		}
	}
}
