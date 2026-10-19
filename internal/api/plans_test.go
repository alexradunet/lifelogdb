package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
	"lifelog/internal/api/siren"
)

// plannedTitles are the page and the text of each line of a list of plan lines (planned of a day, lines of a month).
func plannedTitles(t *testing.T, e *siren.Entity, key string) string {
	t.Helper()
	var out []string
	list, _ := props(e)[key].([]any)
	for _, l := range list {
		l := l.(map[string]any)
		out = append(out, fmt.Sprintf("%v:%v:%q", l["title"], l["line"], l["text"]))
	}
	return strings.Join(out, ",")
}

// TestPlanTickAndCarryWriteALineAndShowItWhereItBelongs: plan makes the day page of today on the first write, the day
// shows the line, tick closes it and a repeat adds the next one, and carry moves a line to another day. The
// expected bodies and lists are written by hand.
func TestPlanTickAndCarryWriteALineAndShowItWhereItBelongs(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	do := func(name string, e *siren.Entity, v map[string]string) *siren.Entity {
		t.Helper()
		return apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, e, name), v))
	}
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	e := do("plan", root, map[string]string{"page": "today", "text": "Pay rent", "due": "2031-05-14", "every": "month"})
	if e.Title != fixedToday || props(e)["body"] != "- [ ] Pay rent due:2031-05-14 every:month" {
		t.Fatalf("plan on today: %q %v", e.Title, props(e)["body"])
	}
	if res := e.Result.(map[string]any); fmt.Sprint(res["line"]) != "1" || res["written"] != "- [ ] Pay rent due:2031-05-14 every:month" {
		t.Errorf("the result of plan: %v", res)
	}
	day := apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday))
	if got := plannedTitles(t, day, "planned"); got != fixedToday+`:1:"Pay rent"` || fmt.Sprint(props(day)["planned_more"]) != "0" {
		t.Errorf("planned on the day: %s", got)
	}

	ticked := do("tick", e, map[string]string{"line": "1", "text": "Pay rent", "outcome": "done"})
	res := ticked.Result.(map[string]any)
	if res["appended"] != "- [ ] Pay rent due:2031-06-14 every:month" || fmt.Sprint(res["appended_line"]) != "3" {
		t.Errorf("the result of tick: %v", res)
	}
	want := "- [x] Pay rent due:2031-05-14 every:month done:2031-05-14\n\n- [ ] Pay rent due:2031-06-14 every:month"
	if props(ticked)["body"] != want {
		t.Errorf("the body after the tick: %q, want %q", props(ticked)["body"], want)
	}
	if _, err := c.Do(t.Context(), apitest.Action(t, ticked, "tick"), map[string]string{"line": "1", "text": "Pay rent", "outcome": "done"}); apitest.Status(err) != http.StatusConflict {
		t.Errorf("a tick of a closed line: %v, want 409", err)
	}

	if res["text"] != "Pay rent" || fmt.Sprint(res["page_id"]) == "" || res["outcome"] != "done" {
		t.Errorf("a tick result lacks the text or the page for an undo: %v", res)
	}
	if _, err := c.Do(t.Context(), apitest.Action(t, ticked, "tick"), map[string]string{"line": "1", "text": "Pay rent", "outcome": "reopen", "done": "2031-05-14"}); apitest.Status(err) != 422 || !strings.Contains(err.Error(), "done") {
		t.Errorf("a reopen with a done field: %v, want 422 naming done", err)
	}
	reopened := do("tick", ticked, map[string]string{"line": "1", "text": "Pay rent", "outcome": "reopen"})
	if rr := reopened.Result.(map[string]any); rr["outcome"] != "reopen" || rr["removed"] != "- [ ] Pay rent due:2031-06-14 every:month" || fmt.Sprint(rr["removed_line"]) != "3" {
		t.Errorf("the result of a reopen: %v", rr)
	}
	if props(reopened)["body"] != "- [ ] Pay rent due:2031-05-14 every:month" {
		t.Errorf("the body after the reopen: %q", props(reopened)["body"])
	}
	ticked = do("tick", reopened, map[string]string{"line": "1", "text": "Pay rent", "outcome": "done"})

	carried := do("carry", ticked, map[string]string{"line": "3", "text": "Pay rent", "to": "2031-05-20"})
	if res := carried.Result.(map[string]any); fmt.Sprint(res["line"]) != "3" || fmt.Sprint(res["to_line"]) != "1" {
		t.Errorf("the result of carry: %v", res)
	}
	later := apitest.Must(t)(c.Get(t.Context(), "/days/2031-05-20"))
	if got := plannedTitles(t, later, "planned"); got != "2031-05-20:1:\"Pay rent\"" {
		t.Errorf("planned on the later day: %s", got)
	}
	if body := props(apitest.Must(t)(c.Get(t.Context(), "/pages?title=2031-05-20")))["body"]; body != "- [ ] Pay rent on:2031-05-20 due:2031-06-14 every:month" {
		t.Errorf("the copy: %q", body)
	}
	month := apitest.Must(t)(c.Get(t.Context(), "/months/2031-05"))
	if got := plannedTitles(t, month, "lines"); !strings.Contains(got, `2031-05-14:1:"Pay rent"`) {
		t.Errorf("the lines of the month: %s", got)
	}
}

// TestAPeriodPageShowsItsLinesAndAPlanIsRefusedWithReasons: a week page is made on the first plan, it carries
// period and the lines of its days; a bad line number, an outcome and a week the year does not have are 422.
func TestAPeriodPageShowsItsLinesAndAPlanIsRefusedWithReasons(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	week := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "plan"), map[string]string{"page": "2031-W20", "text": "Review the week", "on": "2031-05-14"}))
	if p := props(week); week.Title != "2031-W20" || p["period"] != "week" || plannedTitles(t, week, "lines") != `2031-W20:1:"Review the week"` {
		t.Errorf("the week page: %q %v %v", week.Title, p["period"], p["lines"])
	}
	if !hasAction(week, "plan") || !hasAction(week, "tick") || !hasAction(week, "carry") {
		t.Errorf("a week page offers %s", names(week))
	}
	tick := apitest.Action(t, week, "tick")
	for _, tc := range []struct {
		name string
		v    map[string]string
		want int
	}{
		{"line 0", map[string]string{"line": "0", "text": "Review the week", "outcome": "done"}, 422},
		{"line 1.5", map[string]string{"line": "1.5", "text": "Review the week", "outcome": "done"}, 422},
		{"line x", map[string]string{"line": "x", "text": "Review the week", "outcome": "done"}, 422},
		{"an outcome", map[string]string{"line": "1", "text": "Review the week", "outcome": "later"}, 422},
		{"a changed text", map[string]string{"line": "1", "text": "Review", "outcome": "done"}, 409},
	} {
		if _, err := c.Do(t.Context(), tick, tc.v); apitest.Status(err) != tc.want {
			t.Errorf("tick with %s: %v, want %d", tc.name, err, tc.want)
		}
	}
	_, err := c.Do(t.Context(), apitest.Action(t, root, "plan"), map[string]string{"page": "2027-W53", "text": "Never"})
	if apitest.Status(err) != 422 || !strings.Contains(err.Error(), "entities_period_exists") {
		t.Errorf("plan on 2027-W53: %v, want 422 naming entities_period_exists", err)
	}
}
