package api_test

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestTheMoodIsAChoiceOfFive: capture's mood offers the options 1 to 5 (docs/plans/092-the-app-shell.md), on the
// day's action and in the catalog; the values the API takes do not change, and the browser draws the field as a row
// of radio buttons that works with no script.
func TestTheMoodIsAChoiceOfFive(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	day := must(c.Get(t.Context(), "/days/"+fixedToday))
	for _, a := range []string{"the day's", "the catalog's"} {
		capture := find(day, "capture")
		if a == "the catalog's" {
			capture = find(must(c.Get(t.Context(), "/actions")), "capture")
		}
		var mood string
		for _, f := range capture.Fields {
			if f.Name == "mood" {
				mood = fmt.Sprintf("%s %v %q", f.Type, f.Required, f.Options)
			}
		}
		if mood != `number false ["1" "2" "3" "4" "5"]` {
			t.Errorf("%s capture's mood: %s", a, mood)
		}
	}

	capture := find(day, "capture")
	must(c.Do(t.Context(), capture, map[string]string{"text": "No mood today."}))
	must(c.Do(t.Context(), capture, map[string]string{"mood": "4"}))
	for _, bad := range []string{"6", "0", "2.5"} {
		if _, err := c.Do(t.Context(), capture, map[string]string{"mood": bad}); !clientStatus(err, 422) {
			t.Errorf("mood %s: %v, want 422", bad, err)
		}
	}
	if got := fmt.Sprint(props(must(c.Get(t.Context(), "/days/"+fixedToday)))["mood"]); got != "[4]" {
		t.Errorf("the day's mood after the captures: %s", got)
	}

	body := browse(t, h, "/days/"+fixedToday)
	form := captureForm(t, body, "/days/"+fixedToday+"/capture")
	for _, want := range []string{`<fieldset class="segmented"><legend>Mood 1-5</legend>`, `<input type="radio" name="mood" value="1">`,
		`<input type="radio" name="mood" value="5">`, `<input type="hidden" name="back" value="/days/` + fixedToday + `">`} {
		if !strings.Contains(form, want) {
			t.Errorf("the day's Capture lacks %s:\n%s", want, form)
		}
	}
	if strings.Contains(form, `<select name="mood"`) || strings.Contains(form, " required") {
		t.Errorf("the mood is a list, or is required:\n%s", form)
	}
	rec := submit(h, "/days/"+fixedToday+"/capture", url.Values{"text": {"From the browser."}, "mood": {"3"}, "back": {"/days/" + fixedToday}}, "")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/days/"+fixedToday) {
		t.Fatalf("Capture from the browser: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if got := fmt.Sprint(props(must(c.Get(t.Context(), "/days/"+fixedToday)))["mood"]); got != "[4 3]" {
		t.Errorf("the day's mood after the browser's capture: %s", got)
	}
}

// TestCaptureReturnsToThePageThatSentIt: Home's Capture returns to Home, a day's to the day.
func TestCaptureReturnsToThePageThatSentIt(t *testing.T) {
	_, h := freshOn(t, fixedToday)
	for page, want := range map[string]string{"/": "/", "/days/" + dayOf(-3): "/days/" + dayOf(-3)} {
		action := "/days/" + fixedToday + "/capture"
		if page != "/" {
			action = page + "/capture"
		}
		if form := captureForm(t, browse(t, h, page), action); !strings.Contains(form, `name="back" value="`+want+`"`) {
			t.Errorf("Capture on %s does not return to %s:\n%s", page, want, form)
		}
	}
}

// captureForm is the form of a page that posts to action.
func captureForm(t *testing.T, body, action string) string {
	t.Helper()
	for _, m := range regexp.MustCompile(`(?s)<form method="POST" action="([^"]+)"[^>]*>.*?</form>`).FindAllStringSubmatch(body, -1) {
		if html.UnescapeString(m[1]) == action {
			return m[0]
		}
	}
	t.Fatalf("no form posts to %s", action)
	return ""
}

// TestTheDayIsALeafAndItsMargin (docs/plans/095-the-leaf-and-its-margin.md): the head of a day is a calendar leaf
// that opens its month, beside the weekday, the week and "today"; the previous and next day keep their whole name for
// a reader; the sheet (the journal, then Capture) comes before the margin (Due first) in the source, so a narrow
// screen shows the writing first; Record a reading is in the margin's Readings, its optional fields folded.
// 14 May 2031 is a Wednesday in ISO week 20 (1 January 2031 is a Wednesday, so week 1 starts on 30 December 2030).
func TestTheDayIsALeafAndItsMargin(t *testing.T) {
	_, h := freshOn(t, fixedToday)
	body := browse(t, h, "/days/"+fixedToday)
	for _, want := range []string{
		`<a class="leaf" href="/months/2031-05" aria-label="May 2031, the calendar of the month"><span class="leaf-month">May 2031</span><span class="leaf-day">14</span></a>`,
		`<h1>Wednesday</h1>`,
		`<a class="date" href="/weeks/2031-W20">week 20</a><span class="tag open">today</span>`,
		`href="/days/2031-05-13" rel="prev" aria-label="Previous day, Tue 13 May"><span aria-hidden="true">‹</span><span class="step-day">Tue 13 May</span></a>`,
		`href="/days/2031-05-15" rel="next" aria-label="Next day, Thu 15 May"><span class="step-day">Thu 15 May</span><span aria-hidden="true">›</span></a>`,
		`<p class="empty">Nothing written today yet. <a href="#capture">Add to today</a> below.</p>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the day lacks %s", want)
		}
	}
	order := []string{`<div class="sheet">`, `<section class="journal"`, `<section class="capture"`, `<div class="margin">`,
		`<section class="group" aria-label="Due">`, `<section class="group" aria-label="Readings">`}
	at, prev := -1, "the start"
	for _, mark := range order {
		i := strings.Index(body, mark)
		if i <= at {
			t.Fatalf("%s is not after %s in the source (at %d, after %d)", mark, prev, i, at)
		}
		at, prev = i, mark
	}
	readings := body[strings.Index(body, `aria-label="Readings"`):]
	readings = readings[:strings.Index(readings, "</section>")]
	more := regexp.MustCompile(`(?s)<details class="more-fields"><summary>4 more fields</summary>(.*?)</details>`).FindStringSubmatch(readings)
	if !strings.Contains(readings, `<summary class="button">Record a reading</summary>`) || more == nil ||
		strings.Count(more[1], "<label>") != 4 || !strings.Contains(more[1], `name="taken_at"`) {
		t.Errorf("Record a reading is not in the Readings, with 4 folded fields:\n%s", readings)
	}
	if !strings.Contains(readings, `<p class="empty">No readings on this day. <a href="#record">Record one</a> below.</p>`) {
		t.Errorf("a day with no reading does not say so:\n%s", readings)
	}

	past := browse(t, h, "/days/"+dayOf(-3))
	for _, want := range []string{`<span class="leaf-day">11</span>`, `<h1>Sunday</h1>`, `<a href="/days/today">Go to today</a>`,
		`<p class="empty">Nothing written on this day. <a href="#capture">Add to it</a> below.</p>`, `<h2>Add to Sun 11 May</h2>`} {
		if !strings.Contains(past, want) {
			t.Errorf("a past day lacks %s", want)
		}
	}
	if strings.Contains(past, `<span class="tag open">today</span>`) {
		t.Error("a past day says today")
	}
}

// TestTheHabitsStepToANamedDay says the previous and next day of the habits page carry their names, as the day's
// own steps do: an arrow alone gives a reader nothing to read.
func TestTheHabitsStepToANamedDay(t *testing.T) {
	_, h := freshOn(t, fixedToday)
	body := browse(t, h, "/habits?day="+fixedToday)
	for _, want := range []string{
		`rel="prev" aria-label="Previous day, Tue 13 May"><span aria-hidden="true">‹</span><span class="step-day">Tue 13 May</span></a>`,
		`rel="next" aria-label="Next day, Thu 15 May"><span class="step-day">Thu 15 May</span><span aria-hidden="true">›</span></a>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the habits page lacks %s", want)
		}
	}
}
