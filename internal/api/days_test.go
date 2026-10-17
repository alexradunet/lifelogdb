package api_test

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
	"lifelog/internal/client"
)

// journalFixture is a small life around fixedToday: a mood; two earlier day pages, one written as a mood alone.
func journalFixture(t *testing.T) (*client.Client, http.Handler) {
	t.Helper()
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	capture := func(day string, v map[string]string) {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/"+day)), "capture"), v))
	}
	capture(dayOf(-2), map[string]string{"text": "## Morning\nRan with [[Sam]] by the river."})
	capture(dayOf(-1), map[string]string{"mood": "2"})
	capture(fixedToday, map[string]string{"text": "Quiet day.", "mood": "4"})
	capture(fixedToday, map[string]string{"mood": "3"})
	return c, h
}

func TestTheDayCarriesItsMood(t *testing.T) {
	c, h := journalFixture(t)

	today := apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday))
	p := props(today)
	if fmt.Sprint(p["mood"]) != "[4 3]" || p["today"] != fixedToday {
		t.Errorf("today's mood %v, today %v", p["mood"], p["today"])
	}
	save := apitest.Action(t, today, "save-body")
	page := apitest.Must(t)(c.Get(t.Context(), apitest.Href(today, "page")))
	if save.Href != apitest.Href(today, "page")+"/body" || fieldOf(save, "version") != fmt.Sprint(props(page)["version"]) ||
		fieldOf(save, "body") != props(page)["body"] {
		t.Errorf("the day's save-body: %+v, page %v", save, props(page))
	}

	// A past day with nothing on it.
	past := props(apitest.Must(t)(c.Get(t.Context(), "/days/"+dayOf(-20))))
	if fmt.Sprint(past["mood"]) != "[]" {
		t.Errorf("a day with no mood: %v", past["mood"])
	}
	quiet := apitest.Must(t)(c.Get(t.Context(), "/days/"+dayOf(-5)))
	if hasAction(quiet, "save-body") {
		t.Errorf("a quiet day offers save-body: %s", names(quiet))
	}

	// The browser.
	body := browse(t, h, "/days/"+fixedToday)
	for _, want := range []string{`<span class="mood" title="Mood">mood 4, 3</span>`, "<summary>Edit the day page</summary>"} {
		if !strings.Contains(body, want) {
			t.Errorf("the day view lacks %q", want)
		}
	}
	if strings.Contains(body, `<th scope="col">scope</th>`) {
		t.Error("the readings show a scope column")
	}
}

func TestTheRootIsTodayAtAGlance(t *testing.T) {
	c, h := journalFixture(t)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	p := props(root)
	var recent []string
	for _, r := range p["recent"].([]any) {
		m := r.(map[string]any)
		recent = append(recent, fmt.Sprintf("%v %v %v", m["day"], m["mood"], m["first_line"]))
	}
	if got, want := strings.Join(recent, " | "), fixedToday+" [4 3] Quiet day. | "+dayOf(-1)+" [2] <nil> | "+dayOf(-2)+" <nil> Morning"; got != want {
		t.Errorf("recent:\n got %s\nwant %s", got, want)
	}

	body := browse(t, h, "/")
	for _, want := range []string{"<h2>Last entries</h2>", "Quiet day."} {
		if !strings.Contains(body, want) {
			t.Errorf("the root lacks %q", want)
		}
	}
}

// TestTheMoodIsAChoiceOfFive: capture's mood offers the options 1 to 5 (docs/plans/092-the-app-shell.md), on the
// day's action and in the catalog; the values the API takes do not change, and the browser draws the field as a row
// of radio buttons that works with no script.
func TestTheMoodIsAChoiceOfFive(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	day := apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday))
	for _, a := range []string{"the day's", "the catalog's"} {
		capture := apitest.Action(t, day, "capture")
		if a == "the catalog's" {
			capture = apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/actions")), "capture")
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

	capture := apitest.Action(t, day, "capture")
	apitest.Must(t)(c.Do(t.Context(), capture, map[string]string{"text": "No mood today."}))
	apitest.Must(t)(c.Do(t.Context(), capture, map[string]string{"mood": "4"}))
	for _, bad := range []string{"6", "0", "2.5"} {
		if _, err := c.Do(t.Context(), capture, map[string]string{"mood": bad}); apitest.Status(err) != 422 {
			t.Errorf("mood %s: %v, want 422", bad, err)
		}
	}
	if got := fmt.Sprint(props(apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday)))["mood"]); got != "[4]" {
		t.Errorf("the day's mood after the captures: %s", got)
	}

	body := browse(t, h, "/days/"+fixedToday)
	form := captureForm(t, body, "/days/"+fixedToday+"/capture")
	for _, want := range []string{`<fieldset class="segmented"><legend>Mood</legend>`,
		`<label title="awful"><input type="radio" name="mood" value="1"><span aria-label="1, awful">1</span></label>`,
		`<label title="okay"><input type="radio" name="mood" value="3"><span aria-label="3, okay">3</span></label>`,
		`<label title="great"><input type="radio" name="mood" value="5"><span aria-label="5, great">5</span></label>`, `<input type="hidden" name="back" value="/days/` + fixedToday + `">`} {
		if !strings.Contains(form, want) {
			t.Errorf("the day's Capture lacks %s:\n%s", want, form)
		}
	}
	if strings.Contains(form, `<select name="mood"`) || strings.Contains(form, " required") {
		t.Errorf("the mood is a list, or is required:\n%s", form)
	}
	past := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/"+dayOf(-1))), "capture")
	apitest.Must(t)(c.Do(t.Context(), past, map[string]string{"mood": "2"}))
	if one := browse(t, h, "/days/"+dayOf(-1)); !strings.Contains(one, `<span class="mood" title="Mood">mood 2 · bad</span>`) {
		t.Errorf("a day with one mood does not say its word:\n%s", one)
	}
	rec := submit(h, "/days/"+fixedToday+"/capture", url.Values{"text": {"From the browser."}, "mood": {"3"}, "back": {"/days/" + fixedToday}}, "")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/days/"+fixedToday) {
		t.Fatalf("Capture from the browser: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if got := fmt.Sprint(props(apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday)))["mood"]); got != "[4 3]" {
		t.Errorf("the day's mood after the browser's capture: %s", got)
	}
}

// TestCaptureReturnsToThePageThatSentIt: Home's Capture returns to Home, a day's to the day.
func TestCaptureReturnsToThePageThatSentIt(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
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
// a reader; the sheet (the journal, then Capture) comes before the margin in the source, so a narrow screen shows
// the writing first; Record a reading is in the margin's Readings, its optional fields folded.
// 14 May 2031 is a Wednesday in ISO week 20 (1 January 2031 is a Wednesday, so week 1 starts on 30 December 2030).
func TestTheDayIsALeafAndItsMargin(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
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
		`<section class="group" aria-label="Readings">`}
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
	more := regexp.MustCompile(`(?s)<details class="more-fields"><summary>3 more fields</summary>(.*?)</details>`).FindStringSubmatch(readings)
	if !strings.Contains(readings, `<summary class="button">Add a reading</summary>`) || more == nil ||
		strings.Count(more[1], "<label>") != 3 || !strings.Contains(more[1], `name="taken_at"`) {
		t.Errorf("Add a reading is not in the Readings, with 3 folded fields:\n%s", readings)
	}
	if !strings.Contains(readings, `<p class="empty">No readings on this day. <a href="#record">Add one</a> below.</p>`) {
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

// TestTheDayAndHomeCarryOnThisDay: the day pages of earlier years on the month and day, the newest first, in the
// property and in the margin; none, an empty list and no group. The capture box carries its quiet prompt.
func TestTheDayAndHomeCarryOnThisDay(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	older, old := strings.Replace(fixedToday, fixedToday[:4], "2029", 1), strings.Replace(fixedToday, fixedToday[:4], "2027", 1)
	if got := props(root)["on_this_day"].([]any); len(got) != 0 {
		t.Fatalf("on_this_day with no page = %v, want an empty list", got)
	}
	if body := browse(t, h, "/"); strings.Contains(body, "On this day") {
		t.Error("Home draws On this day with no entry")
	}
	for day, text := range map[string]string{older: "Two years ago.", old: "Four years ago.", dayOf(-1): "Yesterday."} {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/"+day)), "capture"), map[string]string{"text": text}))
	}
	for _, route := range []string{"/", "/days/" + fixedToday} {
		var got []string
		for _, x := range props(apitest.Must(t)(c.Get(t.Context(), route)))["on_this_day"].([]any) {
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

func TestLiveDayView(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "Walk"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "Walk", "day": "2026-09-01", "value": "1"}))
	page := apitest.Must(t)(c.Get(t.Context(), "/pages?title=Walk"))
	for i, hidden := range []bool{false, true, false} {
		if hidden {
			page = apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, page, "tombstone"), nil))
		} else if i > 0 {
			page = apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, page, "revive"), nil))
		}
		day := apitest.Must(t)(c.Get(t.Context(), "/days/2026-09-01"))
		if hasOption(apitest.Action(t, day, "record"), "metric", "Walk") == hidden {
			t.Errorf("record options hidden=%v: %+v", hidden, day.Actions)
		}
		props := day.Properties.(map[string]any)
		listed := false
		for _, v := range props["view"].([]any) {
			row := v.(map[string]any)
			listed = listed || row["what"] == "Walk" && strings.HasPrefix(fmt.Sprint(row["detail"]), "1")
		}
		if listed == hidden {
			t.Errorf("reading visibility hidden=%v: %v", hidden, props["view"])
		}
		if len(props["readings"].([]any)) != 1 {
			t.Error("historical reading lost")
		}
	}
}
