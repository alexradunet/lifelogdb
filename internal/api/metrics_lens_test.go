package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The habits are a lens of Metrics in the browser (docs/plans/111-one-metrics-view.md). These tests say what the
// two lenses show, that the old address of the page still arrives, and that JSON keeps the resource.

// lensFixture registers the metrics "Walk" (a habit) and "Weight" (not one), and returns the handler.
func lensFixture(t *testing.T) http.Handler {
	t.Helper()
	c, h := freshOn(t, fixedToday)
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "Walk"}))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "Weight", "unit": "kg"}))
	must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/metrics/Walk")), "start-habit"), map[string]string{"start_day": "2031-05-01"}))
	return h
}

func TestTheHabitsLensShowsTheDaysHabitsAndNotTheCategories(t *testing.T) {
	h := lensFixture(t)
	body := browse(t, h, "/metrics?show=habits&day="+fixedToday)
	for _, want := range []string{`<caption>Every habit active on this day, and what it was</caption>`,
		`<caption>How often each habit was recorded in this window</caption>`, `>Done</button>`,
		`<a href="/metrics">By category</a>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the habits lens lacks %s", want)
		}
	}
	if !strings.Contains(body, `<a href="/metrics?show=habits" aria-current="true">Habits</a>`) {
		t.Error("the habits lens does not mark its own link current")
	}
	if strings.Contains(body, `class="group metric-group"`) {
		t.Error("the habits lens shows the groups of categories")
	}
	if strings.Contains(body, "Record a reading") {
		t.Error("the habits lens carries the Record button of the other lens")
	}
}

func TestTheCategoryLensShowsTheGroupsAndTheSwitch(t *testing.T) {
	h := lensFixture(t)
	body := browse(t, h, "/metrics")
	for _, want := range []string{`class="group metric-group"`, `<a href="/metrics" aria-current="true">By category</a>`,
		`<a href="/metrics?show=habits">Habits</a>`, "Record a reading"} {
		if !strings.Contains(body, want) {
			t.Errorf("the category lens lacks %s", want)
		}
	}
	if strings.Contains(body, "Every habit active on this day") {
		t.Error("the category lens shows the habits table")
	}
}

func TestTheOldHabitsAddressRedirectsABrowserAndKeepsItsQuery(t *testing.T) {
	h := lensFixture(t)
	req := httptest.NewRequest("GET", "/habits?day=2031-05-02&from=2031-05-01&to=2031-05-02", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	want := "/metrics?day=2031-05-02&from=2031-05-01&show=habits&to=2031-05-02"
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
		t.Errorf("GET /habits in a browser: %d to %q, want 303 to %q", rec.Code, rec.Header().Get("Location"), want)
	}
}

func TestJSONKeepsTheHabitsResource(t *testing.T) {
	c, _ := freshOn(t, fixedToday)
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "Walk"}))
	must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/metrics/Walk")), "start-habit"), map[string]string{"start_day": "2031-05-01"}))
	e := must(c.Get(t.Context(), "/habits?day=2031-05-02"))
	if e.Class[0] != "habits" {
		t.Errorf("/habits as JSON has class %v, want habits", e.Class)
	}
	if find(e, "check-in").Name == "" {
		t.Error("/habits as JSON offers no check-in")
	}
}

func TestAMetricNamedHabitsKeepsItsOwnPage(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/")), "register-metric"), map[string]string{"name": "habits", "unit": "n"}))
	if body := browse(t, h, "/metrics/habits"); !strings.Contains(body, "<h1>habits</h1>") {
		t.Errorf("/metrics/habits is not the page of the metric: %.200s", body)
	}
}
