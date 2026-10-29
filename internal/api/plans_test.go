package api_test

import (
	"fmt"
	"lifelog/client"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
	"lifelog/siren"
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
	if tick := apitest.Action(t, ticked, "tick"); res["href"] != tick.Href {
		t.Errorf("the result of tick has href %v, the tick action of the page has %s", res["href"], tick.Href)
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
	tick := apitest.Action(t, week, "tick") // templated: the line names its page
	weekID := fmt.Sprint(props(week)["id"])
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
		if _, err := c.Do(t.Context(), tick, mergeID(tc.v, weekID)); apitest.Status(err) != tc.want {
			t.Errorf("tick with %s: %v, want %d", tc.name, err, tc.want)
		}
	}
	_, err := c.Do(t.Context(), apitest.Action(t, root, "plan"), map[string]string{"page": "2027-W53", "text": "Never"})
	if apitest.Status(err) != 422 || !strings.Contains(err.Error(), "entities_period_exists") {
		t.Errorf("plan on 2027-W53: %v, want 422 naming entities_period_exists", err)
	}
}

// A repeat with no day is refused in plain words, and the plan form of a day has its on filled with that day.
func TestARepeatNeedsADayAndTheFormOfADayFillsOn(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	_, err := c.Do(t.Context(), apitest.Action(t, root, "plan"), map[string]string{"page": "Garden", "text": "Water", "every": "week"})
	if apitest.Status(err) != 422 || !strings.Contains(err.Error(), "a repeat needs a day: on (Planned for) or due (Must be done by)") {
		t.Errorf("a repeat with no day: %v, want 422 with the plain words", err)
	}
	day := apitest.Must(t)(c.Get(t.Context(), "/days/2031-05-14"))
	plan := apitest.Action(t, day, "plan")
	for _, f := range plan.Fields {
		if f.Name == "on" && f.Value != "2031-05-14" {
			t.Errorf("the plan form of a day has on = %v, want 2031-05-14", f.Value)
		}
	}
	if _, err := c.Do(t.Context(), apitest.Action(t, day, "plan"), map[string]string{"text": "Water", "every": "week"}); err != nil {
		t.Errorf("a repeat from the form of a day: %v", err)
	}
	page := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "plan"), map[string]string{"page": "Garden", "text": "Water"}))
	for _, f := range apitest.Action(t, page, "plan").Fields {
		if f.Name == "on" && f.Value != nil {
			t.Errorf("the plan form of a plain page has on = %v, want none", f.Value)
		}
	}
}

// plansFor plans the lines of the TestPlans* tests: two on a project page and one on today. Today is fixedToday.
func plansFor(t *testing.T) (*client.Client, http.Handler, *siren.Entity) {
	t.Helper()
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	for _, l := range []map[string]string{
		{"page": "Project", "text": "Call the bank"},
		{"page": "Project", "text": "Pay rent", "due": "2031-05-10"},
		{"page": "today", "text": "Walk", "on": "today"},
	} {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "plan"), l))
	}
	return c, h, root
}

func TestPlansListsEveryOpenLineByDayAndByPage(t *testing.T) {
	c, _, root := plansFor(t)
	if apitest.Href(root, "plans") != "/plans" {
		t.Errorf("the root links plans to %q", apitest.Href(root, "plans"))
	}
	byDay := apitest.Must(t)(c.Get(t.Context(), "/plans"))
	if len(byDay.Class) != 1 || byDay.Class[0] != "plans" || apitest.Href(byDay, "self") != "/plans" {
		t.Errorf("class %v, self %q", byDay.Class, apitest.Href(byDay, "self"))
	}
	p := props(byDay)
	if p["order"] != "day" || p["today"] != fixedToday || fmt.Sprint(p["total"]) != "3" {
		t.Errorf("properties %v", p)
	}
	// Past its day (10 May), today (14 May), then the line with no day.
	if got, want := plannedTitles(t, byDay, "lines"), `Project:3:"Pay rent",`+fixedToday+`:1:"Walk",Project:1:"Call the bank"`; got != want {
		t.Errorf("by day: %s, want %s", got, want)
	}
	for _, name := range []string{"plan", "tick", "carry", "schedule"} {
		apitest.Action(t, byDay, name)
	}
	if got := apitest.Action(t, byDay, "plan"); fmt.Sprint(got.Fields[0].Value) != fixedToday {
		t.Errorf("plan is filled with page %v, want today", got.Fields[0].Value)
	}

	byPage := apitest.Must(t)(c.Get(t.Context(), "/plans?order=page"))
	if props(byPage)["order"] != "page" || apitest.Href(byPage, "self") != "/plans?order=page" {
		t.Errorf("order %v, self %q", props(byPage)["order"], apitest.Href(byPage, "self"))
	}
	if got, want := plannedTitles(t, byPage, "lines"), fixedToday+`:1:"Walk",Project:1:"Call the bank",Project:3:"Pay rent"`; got != want {
		t.Errorf("by page: %s, want %s", got, want)
	}
}

func TestPlansRefusesWhatItDoesNotTake(t *testing.T) {
	c, _, _ := plansFor(t)
	for _, path := range []string{"/plans?order=week", "/plans?limit=0", "/plans?limit=501", "/plans?offset=-1"} {
		if _, err := c.Get(t.Context(), path); apitest.Status(err) != http.StatusUnprocessableEntity {
			t.Errorf("%s: %v, want 422", path, err)
		}
	}
}

func TestPlansPagesLinkNextAndPrev(t *testing.T) {
	c, _, _ := plansFor(t)
	first := apitest.Must(t)(c.Get(t.Context(), "/plans?limit=1"))
	if fmt.Sprint(props(first)["total"]) != "3" || len(props(first)["lines"].([]any)) != 1 || apitest.Href(first, "prev") != "" || apitest.Href(first, "next") != "/plans?limit=1&offset=1" {
		t.Errorf("first page: props %v, prev %q, next %q", props(first), apitest.Href(first, "prev"), apitest.Href(first, "next"))
	}
	last := apitest.Must(t)(c.Get(t.Context(), "/plans?limit=1&offset=2&order=page"))
	if apitest.Href(last, "next") != "" || apitest.Href(last, "prev") != "/plans?limit=1&offset=1&order=page" {
		t.Errorf("last page: prev %q, next %q", apitest.Href(last, "prev"), apitest.Href(last, "next"))
	}
}

func TestScheduleChangesTheDatesAndAnswersBackToABrowser(t *testing.T) {
	c, h, _ := plansFor(t)
	plans := apitest.Must(t)(c.Get(t.Context(), "/plans"))
	lines := props(plans)["lines"].([]any)
	rent := lines[0].(map[string]any) // Pay rent, due 10 May
	id := fmt.Sprint(rent["page_id"])
	e, err := c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/pages/"+id)), "schedule"),
		map[string]string{"line": "3", "text": "Pay rent", "on": "today", "due": ""})
	if err != nil {
		t.Fatal(err)
	}
	if res := e.Result.(map[string]any); res["written"] != "- [ ] Pay rent on:"+fixedToday || res["on"] != fixedToday || res["due"] != "" {
		t.Errorf("result %v", res)
	}
	// A date the client leaves out stays: a due alone keeps the on, then an on alone keeps the due.
	for _, step := range []struct {
		fields map[string]string
		body   string
	}{
		{map[string]string{"due": "2031-06-05"}, "- [ ] Call the bank\n\n- [ ] Pay rent on:" + fixedToday + " due:2031-06-05"},
		{map[string]string{"on": "2031-06-01"}, "- [ ] Call the bank\n\n- [ ] Pay rent on:2031-06-01 due:2031-06-05"},
	} {
		step.fields["line"], step.fields["text"] = "3", "Pay rent"
		if _, err := c.Do(t.Context(), apitest.Action(t, e, "schedule"), step.fields); err != nil {
			t.Fatalf("%v: %v", step.fields, err)
		}
		if body := props(apitest.Must(t)(c.Get(t.Context(), "/pages/"+id)))["body"]; body != step.body {
			t.Errorf("after %v: body %q, want %q", step.fields, body, step.body)
		}
	}
	if _, err := c.Do(t.Context(), apitest.Action(t, e, "schedule"), map[string]string{"line": "3", "text": "Pay rent"}); apitest.Status(err) != http.StatusUnprocessableEntity {
		t.Errorf("no date: %v, want 422", err)
	}
	if _, err := c.Do(t.Context(), apitest.Action(t, e, "schedule"), map[string]string{"line": "3", "text": "Pay rent x", "on": "2031-06-02"}); apitest.Status(err) != http.StatusConflict {
		t.Errorf("a changed line: %v, want 409", err)
	}
	// A browser form answers with a redirect to the page it came from, as tick and carry do.
	for _, form := range []struct {
		path  string
		extra url.Values
	}{{"/pages/" + id + "/schedule", url.Values{"on": {"2031-06-03"}}}, {"/pages/" + id + "/tick", url.Values{"outcome": {"dropped"}}}} {
		v := url.Values{"line": {"3"}, "text": {"Pay rent"}, "back": {"/plans"}}
		for k, vs := range form.extra {
			v[k] = vs
		}
		if rec := submit(h, form.path, v, ""); rec.Code != http.StatusSeeOther || !feedbackLocation(rec.Header().Get("Location"), "/plans") {
			t.Errorf("%s with back=/plans: %d, Location %q", form.path, rec.Code, rec.Header().Get("Location"))
		}
	}
	// Both forms were written, not refused: the line has the new on and is dropped.
	if body := props(apitest.Must(t)(c.Get(t.Context(), "/pages/"+id)))["body"]; body != "- [ ] Call the bank\n\n- [-] Pay rent on:2031-06-03 due:2031-06-05" {
		t.Errorf("after the forms: body %q", body)
	}
}

// TestATickOfACRLFBodyKeepsItsLineEnds: the body goes through the form fields and the JSON of the page, and a tick
// edits its line only. The body that GET /pages/{id} gives back keeps every "\r\n".
func TestATickOfACRLFBodyKeepsItsLineEnds(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	made := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"),
		map[string]string{"title": "Week", "body": "Week\r\n\r\n- [ ] a on:2026-10-20\r\n- [ ] b\r\n"}))
	ticked := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, made, "tick"),
		map[string]string{"line": "3", "text": "a", "outcome": "done", "done": "2026-10-21"}))
	want := "Week\r\n\r\n- [x] a on:2026-10-20 done:2026-10-21\r\n- [ ] b\r\n"
	if got := props(ticked)["body"]; got != want {
		t.Errorf("the body after the tick: %q, want %q", got, want)
	}
	page := apitest.Must(t)(c.Get(t.Context(), apitest.Href(ticked, "self")))
	got, _ := props(page)["body"].(string)
	if got != want || !strings.Contains(got, "\r\n") || !strings.Contains(got, "done:") {
		t.Errorf("the page read back: %q, want %q", got, want)
	}
}

// mergeID is the values with the path field id: the templated tick of a period page needs the page of the line.
func mergeID(v map[string]string, id string) map[string]string {
	out := map[string]string{"id": id}
	for k, x := range v {
		out[k] = x
	}
	return out
}

// TestTheDeleteOfOnePageCarriesTheAddressOfItsRevive: the result of tombstone-pages for one page names the href of
// the revive action of that page, so a client posts the undo there; for two pages there is no such field.
func TestTheDeleteOfOnePageCarriesTheAddressOfItsRevive(t *testing.T) {
	c, _, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	var pages []*siren.Entity
	for _, title := range []string{"Bank", "Garden", "Orchard"} {
		pages = append(pages, apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "plan"), map[string]string{"page": title, "text": "Call"})))
	}
	one := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, pages[0], "tombstone-pages"), nil))
	gone := apitest.Must(t)(c.Get(t.Context(), "/pages/"+fmt.Sprint(props(pages[0])["id"])))
	if revive := apitest.Action(t, gone, "revive"); one.Result.(map[string]any)["revive"] != revive.Href {
		t.Errorf("the result has revive %v, the action has %s", one.Result, revive.Href)
	}
	ids := fmt.Sprintf("%v %v", props(pages[1])["id"], props(pages[2])["id"])
	two := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, pages[1], "tombstone-pages"), map[string]string{"ids": ids}))
	if _, has := two.Result.(map[string]any)["revive"]; has {
		t.Errorf("a delete of two pages carries revive: %v", two.Result)
	}
}
