package api_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestTheDayCarriesItsTrends: for each metric with a current reading on the day, its unit and its current readings
// in the 14 days through the day, oldest first (docs/plans/093-words-phone-and-today.md). A reading outside the
// window, a corrected value and a retracted reading stay out of it.
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

	dayProps := props(must(c.Get(t.Context(), "/days/"+fixedToday)))
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
	} {
		if !strings.Contains(body, w) {
			t.Errorf("the day's readings lack %s", w)
		}
	}
}

// TestAPastDaysTrendSaysItsDaysAgainstToday: the line of a past day places its readings by that day, the last one at
// the end, and its label says their days against today, not against the day the page shows.
func TestAPastDaysTrendSaysItsDaysAgainstToday(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	for _, r := range []struct{ day, value string }{{dayOf(-5), "70"}, {dayOf(-3), "71"}} {
		must(c.Do(t.Context(), find(root, "record"), map[string]string{"metric": "weight", "day": r.day, "value": r.value}))
	}
	body := browse(t, h, "/days/"+dayOf(-3))
	want := `<svg class="trend" viewBox="0 0 120 28" role="img" aria-label="weight from 70 kg 5 days ago · Fri 9 May to 71 kg 3 days ago · Sun 11 May">` +
		`<polyline points="98.8,24.0 116.0,4.0 "/><circle cx="116.0" cy="4.0" r="2.5"/></svg>`
	if !strings.Contains(body, want) {
		i := strings.Index(body, `<svg class="trend"`)
		t.Errorf("the line of %s:\n got %.300s\nwant %s", dayOf(-3), body[max(i, 0):], want)
	}
}

// TestTheDayAndHomeCarryOnThisDay: the day pages of earlier years on the month and day, the newest first, in the
// property and in the margin; none, an empty list and no group. The capture box carries its quiet prompt.
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
	}
}
