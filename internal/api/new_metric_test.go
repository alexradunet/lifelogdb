package api_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// New metric, new category and a new reading from /metrics (docs/plans/113-new-metric-on-metrics.md).

func TestRegisterMetricTakesACategoryAndAHabit(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	ctx := t.Context()
	register := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/metrics")), "register-metric")
	if !register.Owner || register.Href != "/metrics" {
		t.Fatalf("/metrics offers register-metric as %+v", register)
	}
	var fields []string
	for _, f := range register.Fields {
		fields = append(fields, f.Name)
		if f.Name == "category" && f.Required {
			t.Errorf("%s is required, want optional", f.Name)
		}
	}
	if !slices.Equal(fields, []string{"name", "unit", "note", "category"}) {
		t.Errorf("register-metric fields: %v", fields)
	}

	walk := apitest.Must(t)(c.Do(ctx, register, map[string]string{"name": "Walk", "category": "Sport"}))
	if walk.Result != "New metric Walk, in Sport." {
		t.Errorf("the answer's sentence: %v", walk.Result)
	}
	if got, want := groupsOf(t, c), "Sport:Walk Not filed:Mood"; got != want {
		t.Errorf("groups %q, want %q", got, want)
	}
	if got := props(apitest.Must(t)(c.Get(ctx, "/metrics/Walk")))["categories"]; !slices.Equal(anyStrings(got), []string{"Sport"}) {
		t.Errorf("Walk's categories %v, want [Sport]", got)
	}

	// A refused metric writes nothing: neither the metric nor its category.
	if _, err := c.Do(ctx, register, map[string]string{"name": "Run", "unit": "km", "category": "2031-05-01"}); err == nil {
		t.Fatal("a metric filed in a day was written")
	}
	if got, want := groupsOf(t, c), "Sport:Walk Not filed:Mood"; got != want {
		t.Errorf("after a refusal, groups %q, want %q", got, want)
	}
	if _, err := c.Get(ctx, "/pages?title=Run"); err == nil {
		t.Error("a refused register-metric made the metric Run")
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
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)

	page := browse(t, h, "/metrics")
	for _, want := range []string{`<details class="action new-metric" id="new-metric">`, `<form class="kind-number"`, `<form class="kind-score"`,
		`<form class="kind-habit"`, `<a class="button" href="/metrics?new=file-pages#pick">New category</a>`,
		`<input type="radio" name="new-kind" value="number" checked>`} {
		if !strings.Contains(page, want) {
			t.Errorf("/metrics lacks %s", want)
		}
	}
	if home := browse(t, h, "/"); strings.Contains(home, `method="POST" action="/metrics"`) {
		t.Error("Home still draws the New metric form")
	}
	if p := browse(t, h, "/metrics?new=register-metric&category=Sport"); !strings.Contains(p, `id="new-metric" open>`) ||
		!strings.Contains(p, `name="category" list="metric-categories" value="Sport"`) {
		t.Error("?new=register-metric&category=Sport does not open New metric with Sport filled in")
	}
	if p := browse(t, h, "/metrics?new=file-pages"); !strings.Contains(p, `<input type="checkbox" class="pick-on" checked>`) {
		t.Error("?new=file-pages does not turn Select on")
	}

	// The habit form and the number form, as a browser sends them.
	for _, form := range []url.Values{
		{"kind": {"habit"}, "name": {"Walk"}, "unit": {""}, "category": {"Sport"}, "note": {""}, "back": {"/metrics"}},
		{"kind": {"number"}, "name": {"Weight"}, "unit": {"kg"}, "category": {"Body"}, "note": {"Before breakfast"}, "back": {"/metrics"}},
	} {
		rec := submit(h, "/metrics", form, "")
		if rec.Code != http.StatusSeeOther || !feedbackLocation(rec.Header().Get("Location"), "/metrics") {
			t.Fatalf("New metric %s: %d %q %.300s", form.Get("name"), rec.Code, rec.Header().Get("Location"), rec.Body.String())
		}
		if form.Get("name") == "Walk" && !strings.Contains(browse(t, h, rec.Header().Get("Location")), "New metric Walk, in Sport.") {
			t.Error("the toast does not say what New metric made")
		}
	}
	page = browse(t, h, "/metrics")
	for _, want := range []string{`href="/metrics?new=register-metric&amp;category=Body#new-metric">+ New metric here</a>`,
		`aria-label="Add a reading of Weight"`, `aria-label="Add a reading of Walk"`} {
		if !strings.Contains(page, want) {
			t.Errorf("/metrics lacks %s", want)
		}
	}

	// The card's +: one value, for today.
	rec := submit(h, "/measurements", url.Values{"metric": {"Weight"}, "day": {fixedToday}, "value": {"80.5"}, "back": {"/metrics"}}, "")
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || (loc != "/metrics" && !feedbackLocation(loc, "/metrics")) {
		t.Fatalf("the card's +: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	var weight []any
	for _, x := range props(apitest.Must(t)(c.Get(t.Context(), "/metrics")))["metrics"].([]any) {
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
