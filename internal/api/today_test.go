package api_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestTheDayCarriesItsTrends: for each metric with a current reading of no session on the day, its unit and its
// current readings of no session in the 14 days through the day, oldest first (docs/plans/093-words-phone-and-today.md).
// A reading outside the window, a session's reading, a corrected value and a retracted reading stay out of it, and
// so does a habit: its strip tells it.
func TestTheDayCarriesItsTrends(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "steps", "unit": "steps"}))
	record := func(day, value string, extra ...string) string {
		v := map[string]string{"metric": "weight", "day": day, "value": value}
		for i := 0; i+1 < len(extra); i += 2 {
			v[extra[i]] = extra[i+1]
		}
		return href(must(c.Do(t.Context(), find(root, "record"), v)), "self")
	}
	record(dayOf(-14), "72.0") // the day before the window
	record(dayOf(-13), "71.4")
	record(dayOf(-5), "71.2")
	must(c.Do(t.Context(), find(must(c.Get(t.Context(), record(dayOf(-2), "80"))), "correct"), map[string]string{"value": "71.3"}))
	must(c.Do(t.Context(), find(must(c.Get(t.Context(), record(dayOf(-3), "90"))), "retract"), nil))
	today := record(fixedToday, "71.1")
	must(c.Do(t.Context(), find(root, "record"), map[string]string{"metric": "steps", "day": fixedToday, "value": "5000"}))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "walk"}))
	must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/metrics/walk")), "start-habit"), map[string]string{"start_day": dayOf(-4)}))
	must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/metrics/walk")), "check-in"), map[string]string{"day": fixedToday, "done": "1"}))
	must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Run"}))
	session := must(c.Do(t.Context(), find(root, "capture-session"), map[string]string{"kind": "Run", "day": fixedToday, "start_at": fixedToday + "T05:00:00.000Z"}))
	record(fixedToday, "70.9", "session_id", strings.TrimPrefix(href(session, "self"), "/sessions/"))

	dayProps := props(must(c.Get(t.Context(), "/days/"+fixedToday)))
	var habitNames []string
	for _, x := range dayProps["habits"].([]any) {
		habitNames = append(habitNames, x.(map[string]any)["metric"].(string))
	}
	if strings.Join(habitNames, ",") != "walk" {
		t.Errorf("habits = %v, want [walk]", habitNames)
	}
	var got []string
	for _, x := range dayProps["trends"].([]any) {
		tr := x.(map[string]any)
		var points []string
		for _, p := range tr["readings"].([]any) {
			m := p.(map[string]any)
			points = append(points, fmt.Sprintf("%v=%v", m["day"], m["value"]))
		}
		got = append(got, fmt.Sprintf("%v %v [%s]", tr["metric"], tr["unit"], strings.Join(points, " ")))
	}
	want := []string{
		"weight kg [" + dayOf(-13) + "=71.4 " + dayOf(-5) + "=71.2 " + dayOf(-2) + "=71.3 " + fixedToday + "=71.1]",
		"steps steps [" + fixedToday + "=5000]",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("trends:\n got %s\nwant %s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if trends := props(must(c.Get(t.Context(), "/days/"+dayOf(-1))))["trends"].([]any); len(trends) != 0 {
		t.Errorf("a day with no reading has trends: %v", trends)
	}

	body := browse(t, h, "/days/"+fixedToday)
	for _, w := range []string{
		`<a class="reading" href="` + today + `">71.1 <span class="unit">kg</span></a>`,
		`<svg class="trend" viewBox="0 0 120 28" role="img" aria-label="weight from 71.4 kg Thu 1 May to 71.1 kg today">`,
		`aria-label="steps: one reading in 14 days, 5000 steps today"`,
		`<span class="meta">session ` + strings.TrimPrefix(href(session, "self"), "/sessions/"),
	} {
		if !strings.Contains(body, w) {
			t.Errorf("the day's readings lack %s", w)
		}
	}
}

// TestAHabitCarriesItsLastDays: each habit of the day and of the root has its state on each of the 14 days through
// the day, oldest first, inactive on a day no period of it holds.
func TestAHabitCarriesItsLastDays(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "walk"}))
	walk := must(c.Get(t.Context(), "/metrics/walk"))
	must(c.Do(t.Context(), find(walk, "start-habit"), map[string]string{"start_day": dayOf(-20), "end_day": dayOf(-10)}))
	must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/metrics/walk")), "start-habit"), map[string]string{"start_day": dayOf(-4)}))
	for day, done := range map[string]string{dayOf(-12): "1", dayOf(-11): "0", dayOf(-2): "1"} {
		must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/metrics/walk")), "check-in"), map[string]string{"day": day, "done": done}))
	}
	want := "not recorded,done,not done,not recorded,inactive,inactive,inactive,inactive,inactive,not recorded,not recorded,done,not recorded,not recorded"
	for _, path := range []string{"/days/" + fixedToday, "/"} {
		hs := props(must(c.Get(t.Context(), path)))["habits"].([]any)
		if len(hs) != 1 {
			t.Fatalf("%s habits: %v", path, hs)
		}
		var recent []string
		for _, s := range hs[0].(map[string]any)["recent"].([]any) {
			recent = append(recent, s.(string))
		}
		if got := strings.Join(recent, ","); got != want {
			t.Errorf("%s recent:\n got %s\nwant %s", path, got, want)
		}
	}
	body := browse(t, h, "/days/"+fixedToday)
	for _, w := range []string{`<span class="strip" role="img" aria-label="done on 2 of its 9 active days of the last 14">`,
		`<i class="not-recorded" title="Thu 1 May: not recorded"></i><i class="done" title="Fri 2 May: done"></i><i class="not-done" title="Sat 3 May: not done"></i>`,
		`<i class="inactive" title="Mon 5 May: inactive"></i>`, `<span class="meta">2 of 9 days</span>`} {
		if !strings.Contains(body, w) {
			t.Errorf("the habit's strip lacks %s", w)
		}
	}
}

// TestASeriesDoesNotFillItsOwnPage: on a task's page the group Later shows its next 5 slots and says how many more;
// a series with fewer shows them all.
func TestASeriesDoesNotFillItsOwnPage(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	root := must(c.Get(t.Context(), "/"))
	often := must(c.Do(t.Context(), find(root, "create-task"), map[string]string{"label": "Water the plants", "repeat_unit": "day", "repeat_every": "3", "anchor_day": fixedToday}))
	rare := must(c.Do(t.Context(), find(root, "create-task"), map[string]string{"label": "Clean the gutter", "repeat_unit": "month", "repeat_every": "1", "anchor_day": dayOf(1)}))
	body := browse(t, h, strings.SplitN(hrefOf(often, "self"), "?", 2)[0])
	later := body[strings.Index(body, `aria-label="Later"`):]
	if n := strings.Count(later, `class="occ"`); n != 5 {
		t.Errorf("Later shows %d slots, want 5", n)
	}
	if !strings.Contains(later, " more in this window</span>") || strings.Contains(later, `">and `) {
		t.Errorf("Later does not say how many more, in words and with no link to its own page")
	}
	rareBody := browse(t, h, strings.SplitN(hrefOf(rare, "self"), "?", 2)[0])
	if strings.Contains(rareBody, "more in this window") {
		t.Error("a series with few slots is folded")
	}
}

// TestTheDayAndHomeCarryOnThisDay: the day pages of earlier years on the month and day, the newest first, in the
// property and in the margin after Due; none, an empty list and no group. The capture box carries its quiet prompt.
func TestTheDayAndHomeCarryOnThisDay(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	root := must(c.Get(t.Context(), "/"))
	older, old := strings.Replace(fixedToday, fixedToday[:4], "2029", 1), strings.Replace(fixedToday, fixedToday[:4], "2027", 1)
	if got := props(root)["on_this_day"].([]any); len(got) != 0 {
		t.Fatalf("on_this_day with no page = %v, want an empty list", got)
	}
	if body := browse(t, h, "/"); strings.Contains(body, "On this day") {
		t.Error("Home draws On this day with no entry")
	}
	for day, text := range map[string]string{older: "Two years ago.", old: "Four years ago.", dayOf(-1): "Yesterday."} {
		must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/days/"+day)), "capture"), map[string]string{"text": text}))
	}
	for _, route := range []string{"/", "/days/" + fixedToday} {
		var got []string
		for _, x := range props(must(c.Get(t.Context(), route)))["on_this_day"].([]any) {
			m := x.(map[string]any)
			got = append(got, fmt.Sprintf("%v %v", m["day"], m["first_line"]))
		}
		if want := []string{older + " Two years ago.", old + " Four years ago."}; strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%s on_this_day = %q, want %q", route, got, want)
		}
		body := browse(t, h, route)
		for _, w := range []string{`aria-label="On this day"`, `<a class="entry" href="/days/` + older + `">`, `placeholder="What happened? Who was there?"`} {
			if !strings.Contains(body, w) {
				t.Errorf("%s lacks %s", route, w)
			}
		}
		if i, j := strings.Index(body, `aria-label="Due"`), strings.Index(body, `aria-label="On this day"`); i < 0 || j < i {
			t.Errorf("%s: On this day is not after Due", route)
		}
	}
}
