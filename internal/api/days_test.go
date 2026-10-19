package api_test

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
	"lifelog/internal/api/siren"
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

// TestTheRootIsTodayAtAGlance: a browser's Home is the page of today (docs/plans/129-the-ux-pass.md, part 10): GET /
// answers with the day of today. The JSON root, the entry point of the API, does not change.
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
	if p["today"] != fixedToday || !hasAction(root, "snapshot") {
		t.Errorf("the JSON root: today %v, actions %s", p["today"], names(root))
	}

	for _, accept := range []string{"text/html,application/xhtml+xml", "text/html"} {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Accept", accept)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `<span class="leaf-day">14</span>`) || !strings.Contains(rec.Body.String(), `<section class="capture"`) {
			t.Errorf("GET / as %q: %d, want the page of today: %.300s", accept, rec.Code, rec.Body.String())
		}
	}
	for _, accept := range []string{"application/vnd.siren+json", "application/json", "text/html,application/json", ""} {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Accept", accept)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"class": [`) {
			t.Errorf("GET / as %q: %d, want the JSON root: %.200s", accept, rec.Code, rec.Body.String())
		}
	}
}

// TestAFormThatReturnsHomeLandsOnToday: the write that answers "/" (a snapshot, a capture sent with back=/) is sent
// home, which is the page of today, and its one-use receipt is read there, once.
func TestAFormThatReturnsHomeLandsOnToday(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	rec := submit(h, "/days/"+fixedToday+"/capture", url.Values{"text": {"Walked with [[Dana Example]]."}, "back": {"/"}}, "")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/?feedback=") {
		t.Fatalf("capture: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	to := rec.Header().Get("Location")
	if body := browse(t, h, to); !strings.Contains(body, `class="toast"`) || !strings.Contains(body, "Dana Example") || !strings.Contains(body, `<h1>Wednesday</h1>`) {
		t.Errorf("the page of today does not carry the receipt of the capture:\n%.600s", body)
	}
	if strings.Contains(browse(t, h, to), `class="toast"`) {
		t.Error("the receipt came back on a reload")
	}

}

// TestTheDayOfTodayCarriesTheLastEntries: Home is the page of today, so "Last entries", the list the root had, is in
// the margin of today and of no other day (docs/plans/129-the-ux-pass.md, part 10).
func TestTheDayOfTodayCarriesTheLastEntries(t *testing.T) {
	c, h := journalFixture(t)
	today := props(apitest.Must(t)(c.Get(t.Context(), "/days/"+fixedToday)))
	var recent []string
	list, _ := today["recent"].([]any)
	for _, r := range list {
		m := r.(map[string]any)
		recent = append(recent, fmt.Sprintf("%v %v %v", m["day"], m["mood"], m["first_line"]))
	}
	if got, want := strings.Join(recent, " | "), fixedToday+" [4 3] Quiet day. | "+dayOf(-1)+" [2] <nil> | "+dayOf(-2)+" <nil> Morning"; got != want {
		t.Errorf("recent of today:\n got %s\nwant %s", got, want)
	}
	if _, ok := props(apitest.Must(t)(c.Get(t.Context(), "/days/"+dayOf(-1))))["recent"]; ok {
		t.Error("a day that is not today carries recent")
	}

	body := browse(t, h, "/days/"+fixedToday)
	for _, want := range []string{`aria-label="Last entries"`, "<h2>Last entries</h2>", `<a href="/days">Every day</a>`,
		`<a class="entry" href="/days/` + dayOf(-2) + `">`, "Morning"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page of today lacks %q", want)
		}
	}
	if strings.Contains(browse(t, h, "/days/"+dayOf(-1)), "Last entries") {
		t.Error("a day that is not today draws Last entries")
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
		`<label title="awful"><input type="radio" name="mood" value="1"><span>1 <small>awful</small></span></label>`,
		`<label title="okay"><input type="radio" name="mood" value="3"><span>3 <small>okay</small></span></label>`,
		`<label title="great"><input type="radio" name="mood" value="5"><span>5 <small>great</small></span></label>`, `<input type="hidden" name="back" value="/days/` + fixedToday + `">`} {
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

// TestCaptureReturnsToThePageThatSentIt: the Capture of a day, today's too, returns to the day. Home is the page of
// today, so no form returns to "/".
func TestCaptureReturnsToThePageThatSentIt(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	for _, day := range []string{fixedToday, dayOf(-3)} {
		page := "/days/" + day
		if form := captureForm(t, browse(t, h, page), page+"/capture"); !strings.Contains(form, `name="back" value="`+page+`"`) {
			t.Errorf("Capture on %s does not return to it:\n%s", page, form)
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
		`<p class="sub"><span class="tag open">today</span>`,
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

func TestLiveDayView(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "Walk"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "Walk", "day": "2026-09-01", "value": "1"}))
	page := apitest.Must(t)(c.Get(t.Context(), "/pages?title=Walk"))
	for i, hidden := range []bool{false, true, false} {
		if hidden {
			apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, page, "tombstone-pages"), nil))
			page = apitest.Must(t)(c.Get(t.Context(), apitest.Href(page, "self")))
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

// hiddenKey is the value of the hidden import_key of a form, "" when the form has none.
func hiddenKey(form string) string {
	m := regexp.MustCompile(`<input type="hidden" name="import_key" value="([^"]*)">`).FindStringSubmatch(form)
	if m == nil {
		return ""
	}
	return m[1]
}

// TestTheCaptureFormCarriesAFreshKeyEachTimeItIsDrawn (issue 0085, D28): the browser sends the key that the form was
// drawn with, so a form sent twice (a double click, a reload that posts again) is one capture. The form of the next
// draw has another key, so the next entry is a new capture.
func TestTheCaptureFormCarriesAFreshKeyEachTimeItIsDrawn(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	action := "/days/" + fixedToday + "/capture"
	first := hiddenKey(captureForm(t, browse(t, h, "/days/"+fixedToday), action))
	second := hiddenKey(captureForm(t, browse(t, h, "/days/"+fixedToday), action))
	if len(first) < 16 || first == second {
		t.Errorf("the keys of two draws are %q and %q: want two different keys of 16 characters or more", first, second)
	}
}

// TestACaptureFormSentAgainIsAddedOnce: the same form sent twice answers 303 to the day both times, the second time
// with "Already added" and no second paragraph. A JSON client gets the answer "Already added" with the day, as a
// reading sent again gets "Already recorded".
func TestACaptureFormSentAgainIsAddedOnce(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	form := url.Values{"text": {"Walked the dog"}, "mood": {"4"}, "import_key": {"form-key-1"}}
	toDay := "/days/" + fixedToday
	first := submit(h, toDay+"/capture", form, "")
	if first.Code != http.StatusSeeOther || withoutReceiptOrNot(first.Header().Get("Location")) != toDay {
		t.Fatalf("first send: %d %q", first.Code, first.Header().Get("Location"))
	}
	if body := browse(t, h, first.Header().Get("Location")); strings.Contains(body, "Already added") {
		t.Errorf("the first send says Already added:\n%.400s", body)
	}
	again := submit(h, toDay+"/capture", form, "")
	if again.Code != http.StatusSeeOther || withoutReceiptOrNot(again.Header().Get("Location")) != toDay {
		t.Fatalf("send again: %d %q, want 303 to the day", again.Code, again.Header().Get("Location"))
	}
	if body := browse(t, h, again.Header().Get("Location")); !strings.Contains(body, "Already added") {
		t.Errorf("the second send does not say Already added:\n%.400s", body)
	}
	day := apitest.Must(t)(c.Get(t.Context(), toDay))
	if got := fmt.Sprint(props(day)["mood"]); got != "[4]" {
		t.Errorf("the moods of the day after a send again: %s, want [4]", got)
	}
	if page := apitest.Must(t)(c.Get(t.Context(), apitest.Href(day, "page"))); props(page)["body"] != "Walked the dog" {
		t.Errorf("the page after a send again: %q", props(page)["body"])
	}

	// a JSON client sends the field import_key of the catalog
	capture := apitest.Action(t, day, "capture")
	if capture.Field("import_key") == nil {
		t.Fatal("the capture action has no import_key field")
	}
	fields := map[string]string{"text": "By JSON", "import_key": "json-key-1"}
	apitest.Must(t)(c.Do(t.Context(), capture, fields))
	answer := apitest.Must(t)(c.Do(t.Context(), capture, fields))
	if answer.Title != "Already added" || apitest.Href(answer, "day") != toDay {
		t.Errorf("sent again by JSON: %q links the day %q, want Already added with %s", answer.Title, apitest.Href(answer, "day"), toDay)
	}
	if page := apitest.Must(t)(c.Get(t.Context(), apitest.Href(day, "page"))); props(page)["body"] != "Walked the dog\n\nBy JSON" {
		t.Errorf("the page after the JSON sends: %q", props(page)["body"])
	}
}

// withoutReceiptOrNot is a Location with its feedback receipt cut: the receipt of an answer with a result is part of
// it, and a test of the destination does not read it.
func withoutReceiptOrNot(location string) string {
	u, err := url.Parse(location)
	if err != nil {
		return location
	}
	q := u.Query()
	q.Del("feedback")
	u.RawQuery = q.Encode()
	return u.String()
}

// TestTheStreamPagesByDay: /days is the days with something, the newest first (docs/plans/130-the-explore-pass.md),
// paged by limit and offset as every list: next is the older days, prev the newer days or the top. A day after today
// is not in it.
func TestTheStreamPagesByDay(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday) // 2031-05-14
	for day, text := range map[string]string{fixedToday: "Today.", dayOf(-1): "Yesterday.", dayOf(-2): "Two days ago.", "2030-05-14": "A year ago.", "2031-05-20": "Not yet."} {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/"+day)), "capture"), map[string]string{"text": text}))
	}
	cards := func(e *siren.Entity) string {
		var out []string
		for _, x := range props(e)["cards"].([]any) {
			m := x.(map[string]any)
			out = append(out, fmt.Sprintf("%v:%v", m["day"], m["words"]))
		}
		return strings.Join(out, " ")
	}
	top := apitest.Must(t)(c.Get(t.Context(), "/days?limit=2"))
	if got, want := cards(top), "2031-05-14:Today. 2031-05-13:Yesterday."; got != want {
		t.Errorf("the top page: %s, want %s", got, want)
	}
	if apitest.Href(top, "next") != "/days?limit=2&offset=2" || apitest.Href(top, "prev") != "" {
		t.Errorf("the top page links next %q, prev %q", apitest.Href(top, "next"), apitest.Href(top, "prev"))
	}
	for rel, want := range map[string]string{"days": "/days", "week": "/weeks/2031-W20", "month": "/months/2031-05", "year": "/years/2031"} {
		if got := apitest.Href(top, rel); got != want {
			t.Errorf("the zoom link %s is %q, want %q", rel, got, want)
		}
	}
	older := apitest.Must(t)(c.Get(t.Context(), apitest.Href(top, "next")))
	if got, want := cards(older), "2031-05-12:Two days ago. 2030-05-14:A year ago."; got != want {
		t.Errorf("the older page: %s, want %s", got, want)
	}
	if apitest.Href(older, "next") != "" || apitest.Href(older, "prev") != "/days?limit=2" {
		t.Errorf("the older page links next %q, prev %q", apitest.Href(older, "next"), apitest.Href(older, "prev"))
	}
	if _, err := c.Get(t.Context(), "/days?limit=0"); apitest.Status(err) != 422 {
		t.Errorf("/days?limit=0: %v, want a 422", err)
	}

	body := browse(t, h, "/days")
	for _, want := range []string{`<article class="day-card" aria-label="2031-05-14">`, `<p class="card-words">Today.</p>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the stream lacks %s", want)
		}
	}
	if strings.Contains(body, "Not yet.") {
		t.Error("the stream shows a day after today")
	}
}
