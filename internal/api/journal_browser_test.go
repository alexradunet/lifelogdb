package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/client"
)

// journalFixture is a small life around fixedToday: one-off tasks overdue, due today (one done), due on a past
// day (done there) and due later; a habit; a mood; two earlier day pages, one written as a mood alone.
func journalFixture(t *testing.T) (*client.Client, http.Handler) {
	t.Helper()
	c, h := freshOn(t, fixedToday)
	root := must(c.Get(t.Context(), "/"))
	task := func(label, due string) string {
		e := must(c.Do(t.Context(), find(root, "create-task"), map[string]string{"label": label, "due_day": due}))
		return strings.SplitN(hrefOf(e, "self"), "?", 2)[0]
	}
	done := func(href string) {
		must(c.Do(t.Context(), find(must(c.Get(t.Context(), href+"/occurrences/once")), "edit-occurrence"), map[string]string{"state": "done", "completed_at": "now"}))
	}
	task("Book the dentist", dayOf(-10))
	task("Call the bank", fixedToday)
	done(task("Pay the rent", fixedToday))
	done(task("Return the books", dayOf(-20)))
	task("Renew the passport", dayOf(30))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "evening_walk"}))
	must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/metrics/evening_walk")), "start-habit"), map[string]string{"start_day": dayOf(-30)}))
	capture := func(day string, v map[string]string) {
		must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/days/"+day)), "capture"), v))
	}
	capture(dayOf(-2), map[string]string{"text": "## Morning\nRan with [[Sam]] by the river."})
	capture(dayOf(-1), map[string]string{"mood": "2"})
	capture(fixedToday, map[string]string{"text": "Quiet day.", "mood": "4"})
	capture(fixedToday, map[string]string{"mood": "3"})
	return c, h
}

// labels are the label and state of each occurrence of a list, in order.
func labels(list any) string {
	var out []string
	for _, o := range list.([]any) {
		m := o.(map[string]any)
		out = append(out, fmt.Sprintf("%s/%s", m["label"], m["state"]))
	}
	return strings.Join(out, " ")
}

// actionsNamed are the hrefs of an entity's actions of that name.
func actionsNamed(e *api.Entity, name string) []string {
	var out []string
	for _, a := range e.Actions {
		if a.Name == name {
			out = append(out, a.Href)
		}
	}
	return out
}

func TestTheDayCarriesWhatIsDueItsMoodAndItsHabits(t *testing.T) {
	c, h := journalFixture(t)

	today := must(c.Get(t.Context(), "/days/"+fixedToday))
	p := props(today)
	if got := labels(p["due"]); got != "Book the dentist/open Call the bank/open Pay the rent/done" {
		t.Errorf("today's due: %s", got)
	}
	if fmt.Sprint(p["mood"]) != "[4 3]" || p["today"] != fixedToday {
		t.Errorf("today's mood %v, today %v", p["mood"], p["today"])
	}
	if hs := p["habits"].([]any); len(hs) != 1 || hs[0].(map[string]any)["metric"] != "evening_walk" || hs[0].(map[string]any)["state"] != "not recorded" {
		t.Errorf("today's habits: %v", hs)
	}
	if got := actionsNamed(today, "check-in"); len(got) != 1 || got[0] != "/metrics/evening_walk/check-in" {
		t.Errorf("check-ins: %v", got)
	}
	if got := actionsNamed(today, "edit-occurrence"); len(got) != 3 {
		t.Errorf("today's outcomes: %v", got)
	}
	save := find(today, "save-body")
	page := must(c.Get(t.Context(), hrefOf(today, "page")))
	if save.Href != hrefOf(today, "page")+"/body" || fieldOf(save, "version") != fmt.Sprint(props(page)["version"]) ||
		fieldOf(save, "body") != props(page)["body"] {
		t.Errorf("the day's save-body: %+v, page %v", save, props(page))
	}

	// A past day: what was due on it, in every state, and nothing of the days before it.
	past := props(must(c.Get(t.Context(), "/days/"+dayOf(-20))))
	if got := labels(past["due"]); got != "Return the books/done" {
		t.Errorf("a past day's due: %s", got)
	}
	if fmt.Sprint(past["mood"]) != "[]" {
		t.Errorf("a day with no mood: %v", past["mood"])
	}
	quiet := must(c.Get(t.Context(), "/days/"+dayOf(-5)))
	if got := labels(props(quiet)["due"]); got != "" || find(quiet, "save-body").Name != "" {
		t.Errorf("a quiet day: due %q, save-body %v", got, find(quiet, "save-body").Href)
	}

	// The browser: Done on the day returns to the day; a check-in too, and it is recorded.
	body := browse(t, h, "/days/"+fixedToday)
	for _, want := range []string{`<span class="mood" title="Mood">mood 4, 3</span>`, "<summary>Edit the day page</summary>", "<h2>Due</h2>",
		"Book the dentist", "Sun 4 May"} {
		if !strings.Contains(body, want) {
			t.Errorf("the day view lacks %q", want)
		}
	}
	if strings.Contains(body, "Renew the passport") || strings.Contains(body, "Return the books") {
		t.Error("the day view shows work of another day")
	}
	if strings.Contains(body, `<th scope="col">scope</th>`) {
		t.Error("the readings show a scope column with no session or time to show")
	}
	walk := formFor(t, body, "Done", "/metrics/evening_walk/check-in")
	if walk.Fields.Get("day") != fixedToday || walk.Fields.Get("done") != "1" || walk.Fields.Get("back") != "/days/"+fixedToday {
		t.Fatalf("the habit's Done: %v", walk.Fields)
	}
	if rec := submit(h, walk.Action, walk.Fields, ""); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/days/"+fixedToday {
		t.Fatalf("check-in: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if hs := props(must(c.Get(t.Context(), "/days/"+fixedToday)))["habits"].([]any); hs[0].(map[string]any)["state"] != "done" {
		t.Errorf("after the check-in: %v", hs)
	}
}

func TestTheRootIsTodayAtAGlance(t *testing.T) {
	c, h := journalFixture(t)
	root := must(c.Get(t.Context(), "/"))
	p := props(root)
	if got := labels(p["due"]); got != "Book the dentist/open Call the bank/open" {
		t.Errorf("the root's due: %s", got)
	}
	if hs := p["habits"].([]any); len(hs) != 1 || hs[0].(map[string]any)["metric"] != "evening_walk" {
		t.Errorf("the root's habits: %v", hs)
	}
	var recent []string
	for _, r := range p["recent"].([]any) {
		m := r.(map[string]any)
		recent = append(recent, fmt.Sprintf("%v %v %v", m["day"], m["mood"], m["first_line"]))
	}
	if got, want := strings.Join(recent, " | "), fixedToday+" [4 3] Quiet day. | "+dayOf(-1)+" [2] <nil> | "+dayOf(-2)+" <nil> Morning"; got != want {
		t.Errorf("recent:\n got %s\nwant %s", got, want)
	}
	if got := actionsNamed(root, "edit-occurrence"); len(got) != 2 || len(actionsNamed(root, "check-in")) != 1 {
		t.Errorf("the root's outcomes %v, check-ins %v", got, actionsNamed(root, "check-in"))
	}

	body := browse(t, h, "/")
	for _, want := range []string{"<h2>Due</h2>", "<h2>Habits</h2>", "<h2>Last entries</h2>", "Quiet day."} {
		if !strings.Contains(body, want) {
			t.Errorf("the root lacks %q", want)
		}
	}
	done := formFor(t, body, "Done", "/tasks/")
	if done.Fields.Get("back") != "/" {
		t.Fatalf("the root's Done returns to %q", done.Fields.Get("back"))
	}
	if rec := submit(h, done.Action, done.Fields, ""); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("Done from the root: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if got := labels(props(must(c.Get(t.Context(), "/")))["due"]); got != "Call the bank/open" {
		t.Errorf("after Done: %s", got)
	}
}
