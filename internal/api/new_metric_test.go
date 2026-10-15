package api_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// New metric, new category and a new reading from /metrics (docs/plans/113-new-metric-on-metrics.md).

func TestRegisterMetricTakesACategoryAndAHabit(t *testing.T) {
	c, _ := freshOn(t, fixedToday)
	ctx := t.Context()
	register := find(must(c.Get(ctx, "/metrics")), "register-metric")
	if !register.Owner || register.Href != "/metrics" {
		t.Fatalf("/metrics offers register-metric as %+v", register)
	}
	var fields []string
	for _, f := range register.Fields {
		fields = append(fields, f.Name)
		if (f.Name == "category" || f.Name == "habit_from") && f.Required {
			t.Errorf("%s is required, want optional", f.Name)
		}
	}
	if !slices.Equal(fields, []string{"name", "unit", "note", "category", "habit_from"}) {
		t.Errorf("register-metric fields: %v", fields)
	}

	// "today" is the server's day, as a check-in reads it.
	walk := must(c.Do(ctx, register, map[string]string{"name": "Walk", "category": "Sport", "habit_from": "today"}))
	if walk.Result != "New metric Walk, in Sport, a habit from "+fixedToday+"." {
		t.Errorf("the answer's sentence: %v", walk.Result)
	}
	if got, want := groupsOf(t, c), "Habits:Walk Not filed:Mood"; got != want {
		t.Errorf("groups %q, want %q", got, want)
	}
	if got := props(must(c.Get(ctx, "/metrics/Walk")))["categories"]; !slices.Equal(anyStrings(got), []string{"Sport"}) {
		t.Errorf("Walk's categories %v, want [Sport]", got)
	}

	// A refused habit writes nothing: neither the metric nor its category.
	if _, err := c.Do(ctx, register, map[string]string{"name": "Run", "unit": "km", "category": "Running", "habit_from": "today"}); err == nil {
		t.Fatal("a habit of a metric with a unit was written")
	}
	if got, want := groupsOf(t, c), "Habits:Walk Not filed:Mood"; got != want {
		t.Errorf("after a refusal, groups %q, want %q", got, want)
	}
	if _, err := c.Get(ctx, "/pages?title=Running"); err == nil {
		t.Error("a refused register-metric made the category Running")
	}
}

func anyStrings(x any) []string {
	var out []string
	l, _ := x.([]any)
	for _, s := range l {
		out = append(out, s.(string))
	}
	return out
}

func TestTheMetricsPageMakesAMetricACategoryAndAReading(t *testing.T) {
	c, h := freshOn(t, fixedToday)

	page := browse(t, h, "/metrics")
	for _, want := range []string{`<details class="action new-metric" id="new-metric">`, `<form class="kind-number"`, `<form class="kind-score"`,
		`<form class="kind-habit"`, `name="habit_from" value="` + fixedToday + `"`, `<a class="button" href="/metrics?new=category#file-bar">New category</a>`,
		`<input type="radio" name="new-kind" value="number" checked>`} {
		if !strings.Contains(page, want) {
			t.Errorf("/metrics lacks %s", want)
		}
	}
	if home := browse(t, h, "/"); strings.Contains(home, `data-action="register-metric"`) {
		t.Error("Home still draws the New metric form")
	}
	if p := browse(t, h, "/metrics?new=metric&category=Sport"); !strings.Contains(p, `id="new-metric" open>`) ||
		!strings.Contains(p, `name="category" list="metric-categories" value="Sport"`) {
		t.Error("?new=metric&category=Sport does not open New metric with Sport filled in")
	}
	if p := browse(t, h, "/metrics?new=category"); !strings.Contains(p, `id="file-bar" open>`) {
		t.Error("?new=category does not open the bar")
	}

	// The habit form and the number form, as a browser sends them.
	for _, form := range []url.Values{
		{"kind": {"habit"}, "name": {"Walk"}, "unit": {""}, "habit_from": {fixedToday}, "category": {"Sport"}, "note": {""}, "back": {"/metrics"}},
		{"kind": {"number"}, "name": {"Weight"}, "unit": {"kg"}, "category": {"Body"}, "note": {"Before breakfast"}, "back": {"/metrics"}},
	} {
		rec := submit(h, "/metrics", form, "")
		if rec.Code != http.StatusSeeOther || !feedbackLocation(rec.Header().Get("Location"), "/metrics") {
			t.Fatalf("New metric %s: %d %q %.300s", form.Get("name"), rec.Code, rec.Header().Get("Location"), rec.Body.String())
		}
		if form.Get("name") == "Walk" && !strings.Contains(browse(t, h, rec.Header().Get("Location")), "New metric Walk, in Sport, a habit from "+fixedToday+".") {
			t.Error("the toast does not say what New metric made")
		}
	}
	page = browse(t, h, "/metrics")
	for _, want := range []string{`href="/metrics?new=metric&amp;category=Body#new-metric">+ New metric here</a>`,
		`aria-label="Add a reading of Weight"`, `aria-label="Check in Walk"`} {
		if !strings.Contains(page, want) {
			t.Errorf("/metrics lacks %s", want)
		}
	}
	if strings.Contains(page, `aria-label="Add a reading of Walk"`) {
		t.Error("a habit card has the + of a number card")
	}

	// The card's +: one value, for today.
	rec := submit(h, "/measurements", url.Values{"metric": {"Weight"}, "day": {fixedToday}, "value": {"80.5"}, "back": {"/metrics"}}, "")
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || (loc != "/metrics" && !feedbackLocation(loc, "/metrics")) {
		t.Fatalf("the card's +: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	var weight []any
	for _, x := range props(must(c.Get(t.Context(), "/metrics")))["metrics"].([]any) {
		if m := x.(map[string]any); m["name"] == "Weight" {
			weight = m["recent"].([]any)
		}
	}
	if len(weight) != 1 || weight[0].(map[string]any)["day"] != fixedToday || weight[0].(map[string]any)["value"] != json.Number("80.5") {
		t.Errorf("Weight's readings %v, want 80.5 on %s", weight, fixedToday)
	}

	// A refused form opens again on its own kind, with what it sent.
	submit(h, "/metrics", url.Values{"kind": {"score"}, "name": {"Mood"}, "unit": {"1-5"}, "back": {"/metrics"}}, "")
	rec = submit(h, "/metrics", url.Values{"kind": {"score"}, "name": {"Mood"}, "unit": {"1-10"}, "category": {"Mind"}, "back": {"/metrics"}}, "")
	body := rec.Body.String()
	if rec.Code != http.StatusConflict || !strings.Contains(body, `id="new-metric" open>`) || !strings.Contains(body, `<input type="radio" name="new-kind" value="score" checked>`) ||
		!strings.Contains(body, `value="1-10"`) || !strings.Contains(body, `value="Mind"`) {
		t.Errorf("a refused New metric: %d %.600s", rec.Code, body)
	}
}
