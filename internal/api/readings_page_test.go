package api_test

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestTheRecordFormAsksForAMetricWhenNoneIsNamed: with no metric named, the first option is an empty prompt and the
// select is required, so the browser makes the owner choose; every live metric is offered, read lately or not.
func TestTheRecordFormAsksForAMetricWhenNoneIsNamed(t *testing.T) {
	h, do := lifeOf(t)
	for _, n := range []string{"Evening walk", "Meditate"} {
		do("/", "register-metric", map[string]string{"name": n})
	}
	body := browse(t, h, "/days/"+fixedToday)
	want := `<select name="metric" required><option value="">Pick a metric</option><option>Evening walk</option><option>Meditate</option>`
	if !strings.Contains(body, want) {
		t.Errorf("the record form lacks %s:\n%s", want, regexp.MustCompile(`(?s)<div id="record">.*?</form>`).FindString(body))
	}
	if sent := browserSends(t, body); sent.Get("metric") != "" {
		t.Errorf("the browser sends metric %q before the owner chose", sent.Get("metric"))
	}
	// A link that names a metric selects it, and a name that no metric has is kept and selected: the browser sends
	// it, and the server refuses it (404) with its reason. It is never dropped in silence.
	if got := browserSends(t, browse(t, h, "/?new=record&metric=Nothing&value=1")).Get("metric"); got != "Nothing" {
		t.Errorf("an unknown metric in the link is sent as %q, want Nothing", got)
	}
	rec := submit(h, "/measurements", url.Values{"metric": {"Nothing"}, "day": {fixedToday}, "value": {"1"}}, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("record of an unknown metric: %d, want 404", rec.Code)
	}
}

// TestARecordGoesBackToTheDayItWasMadeOn: the form of Home and of a day returns to that page with the answer; the
// key is filled, so a form sent twice is one reading.
func TestARecordGoesBackToTheDayItWasMadeOn(t *testing.T) {
	h, do := lifeOf(t)
	do("/", "register-metric", map[string]string{"name": "Walk"})
	for _, page := range []string{"/", "/days/" + dayOf(-2)} {
		body := browse(t, h, page+"?new=record&metric=Walk&value=1")
		sent := browserSends(t, body)
		if sent.Get("import_key") == "" || !strings.Contains(body, `name="import_key" value="`+sent.Get("import_key")+`"`) {
			t.Fatalf("%s: no key filled: %v", page, sent)
		}
		rec := submit(h, "/measurements", sent, "")
		if rec.Code != http.StatusSeeOther || returnedTo(t, rec.Header().Get("Location")) != page {
			t.Errorf("%s: %d, returned to %q, want %q", page, rec.Code, rec.Header().Get("Location"), page)
		}
		if again := submit(h, "/measurements", sent, ""); again.Code != http.StatusSeeOther {
			t.Errorf("%s: the same form sent twice: %d", page, again.Code)
		}
	}
	// two forms, each sent twice: one reading on each day
	for _, day := range []string{fixedToday, dayOf(-2)} {
		if got := browse(t, h, "/days/"+day); strings.Contains(got, "more this day") {
			t.Errorf("%s: a form sent twice made two readings", day)
		}
	}
}

// TestTheChipOfAHabitReturnsToThePageItWasOn: the write returns to the page whose rail drew the chip.
func TestTheChipOfAHabitReturnsToThePageItWasOn(t *testing.T) {
	h, do := lifeOf(t)
	do("/", "register-metric", map[string]string{"name": "Stretch", "daily": "1"})
	do("/days/"+dayOf(-3), "record", map[string]string{"metric": "Stretch", "value": "1"})
	page := "/days/" + dayOf(-1)
	chip := regexp.MustCompile(`<a class="chip" href="([^"]+)">Stretch</a>`).FindStringSubmatch(browse(t, h, page))
	if chip == nil {
		t.Fatal("no chip for Stretch")
	}
	link, err := url.Parse(html.UnescapeString(chip[1]))
	if err != nil {
		t.Fatal(err)
	}
	sent := browserSends(t, browse(t, h, link.RequestURI()))
	if sent.Get("metric") != "Stretch" || sent.Get("value") != "1" {
		t.Fatalf("the form sends %v", sent)
	}
	rec := submit(h, "/measurements", sent, "")
	if got := returnedTo(t, rec.Header().Get("Location")); got != page {
		t.Errorf("returned to %q, want %q", got, page)
	}
}

// TestACorrectionStartsFromTheValueAndARetractionHasNone: Correct holds the current value; a retraction offers no
// Correct; the page says how to fix a wrong day or metric, with a link to the record form filled.
func TestACorrectionStartsFromTheValueAndARetractionHasNone(t *testing.T) {
	h, do := lifeOf(t)
	do("/", "register-metric", map[string]string{"name": "Weight", "unit": "kg"})
	do("/days/"+fixedToday, "record", map[string]string{"metric": "Weight", "value": "71.5"})
	reading := regexp.MustCompile(`href="(/measurements/\d+)"`).FindStringSubmatch(browse(t, h, "/metrics/Weight"))[1]

	body := browse(t, h, reading)
	if !regexp.MustCompile(`(?s)aria-label="Correct".*name="value" value="71.5"`).MatchString(body) {
		t.Errorf("Correct is not filled with 71.5:\n%s", body)
	}
	for _, want := range []string{"Wrong day or wrong metric? Retract it, then",
		`href="/?day=` + fixedToday + `&amp;metric=Weight&amp;new=record&amp;value=71.5#record">add it again</a>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the reading lacks %s:\n%s", want, body)
		}
	}

	rec := submit(h, reading+"/retract", url.Values{}, "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("retract: %d", rec.Code)
	}
	retracted := browse(t, h, returnedTo(t, rec.Header().Get("Location")))
	if strings.Contains(retracted, `aria-label="Correct"`) || strings.Contains(retracted, "Wrong day") || strings.Contains(retracted, "Retract</summary>") {
		t.Errorf("a retraction offers an action:\n%s", retracted)
	}
	if !strings.Contains(retracted, "This row retracts the reading before it") {
		t.Errorf("the retraction is not explained:\n%s", retracted)
	}
}

// TestHabitsAndUnitsAreSaid: a metric with no unit says what a reading is, and its value box shows what to type; a
// metric with a unit says neither. The mark of a daily metric is one open form on its view, ticked or not. The tiles of a day show the unit, and a metric read again on the day links to its page.
func TestHabitsAndUnitsAreSaid(t *testing.T) {
	h, do := lifeOf(t)
	do("/", "register-metric", map[string]string{"name": "Stretch"})
	do("/", "register-metric", map[string]string{"name": "Meditate", "daily": "1"})
	do("/", "register-metric", map[string]string{"name": "Weight", "unit": "kg"})
	do("/", "register-metric", map[string]string{"name": "Mood score", "unit": "1-5"})
	for _, v := range []string{"70", "71"} {
		do("/days/"+fixedToday, "record", map[string]string{"metric": "Weight", "value": v})
	}
	do("/days/"+fixedToday, "record", map[string]string{"metric": "Mood score", "value": "4"})
	do("/days/"+fixedToday, "record", map[string]string{"metric": "Stretch", "value": "1"})

	habit := browse(t, h, "/metrics/Stretch")
	for _, want := range []string{"With no unit, a reading is a count, or 1 for done and 0 for not done.", `placeholder="a count, or 1 done and 0 not done"`} {
		if !strings.Contains(habit, want) {
			t.Errorf("the page of a habit lacks %q", want)
		}
	}
	if strings.Contains(habit, "Not asked for on every day") || strings.Contains(habit, "days after its last") {
		t.Error("the view of a metric says the old sentences of the mark, or speaks of 14 days")
	}
	// The mark is one open form on the metric's view, in both states: the box is ticked for a daily metric.
	mark := regexp.MustCompile(`(?s)<section class="daily-mark".*?</section>`)
	if form := mark.FindString(habit); !strings.Contains(form, `name="daily"`) || strings.Contains(form, "checked") || strings.Contains(form, "<details") {
		t.Errorf("the view of a metric that is not daily lacks the open form with an unticked box:\n%s", form)
	}
	if form := mark.FindString(browse(t, h, "/metrics/Meditate")); !strings.Contains(form, `name="daily" value="1" checked`) || strings.Contains(form, "<details") {
		t.Errorf("the view of a daily metric lacks the open form with a ticked box:\n%s", form)
	}
	if weight := browse(t, h, "/metrics/Weight"); strings.Contains(weight, "1 done and 0 not done") || !mark.MatchString(weight) {
		t.Error("the view of a metric with a unit speaks of habits, or lacks the form of the mark")
	}

	day := browse(t, h, "/days/"+fixedToday)
	for _, want := range []string{`71 <span class="unit">kg</span>`, `4 <span class="unit">of 5</span>`,
		`<a class="meta" href="/metrics/Weight">and 1 more this day</a>`} {
		if !strings.Contains(day, want) {
			t.Errorf("the day lacks %s", want)
		}
	}
	if strings.Contains(day, `>1 <span class="unit">`) {
		t.Error("a habit reading has a unit")
	}
}

// TestTheRecordFieldsSayWhatToType: the folded fields have plain titles with an example.
func TestTheRecordFieldsSayWhatToType(t *testing.T) {
	h, _ := lifeOf(t)
	body := browse(t, h, "/days/"+fixedToday)
	for _, want := range []string{"Time taken, in UTC (2026-06-09T21:14:03.482Z)", "Time zone (Europe/Bucharest)"} {
		if !strings.Contains(body, want) {
			t.Errorf("the record form lacks %q", want)
		}
	}
}

// returnedTo is a Location without the receipt of feedback, when it has one.
func returnedTo(t *testing.T, location string) string {
	t.Helper()
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("Location %q: %v", location, err)
	}
	q := u.Query()
	q.Del("feedback")
	u.RawQuery = q.Encode()
	return u.String()
}

// TestTheViewOfAMetricDrawsTheDailyForm: the view of a metric offers edit-metric with the current mark, once and not
// under More, and the form of the browser sets it. An unticked box sends nothing, which is 0. The page of a person has
// no such form.
func TestTheViewOfAMetricDrawsTheDailyForm(t *testing.T) {
	h, do := lifeOf(t)
	do("/", "register-metric", map[string]string{"name": "Meditate"})
	do("/", "create-person", map[string]string{"title": "Sam (barber)", "name": "Samuel Ortiz"})
	view := "/metrics/Meditate"
	page := regexp.MustCompile(`action="(/pages/\d+)/metric"`).FindStringSubmatch(browse(t, h, view))[1]

	body := browse(t, h, view)
	if n := strings.Count(body, `action="`+page+`/metric"`); n != 1 || strings.Contains(body, `name="daily" value="1" checked`) {
		t.Errorf("the view of a metric that is not daily has %d forms of the mark, or a ticked box", n)
	}
	if rec := submit(h, page+"/metric", url.Values{"daily": {"1"}, "back": {view}}, view); rec.Code != http.StatusSeeOther || returnedTo(t, rec.Header().Get("Location")) != view {
		t.Fatalf("the form answered %d to %q, want 303 to the view", rec.Code, rec.Header().Get("Location"))
	}
	if !strings.Contains(browse(t, h, view), `name="daily" value="1" checked`) {
		t.Error("after the form, the box is not ticked")
	}
	if rec := submit(h, page+"/metric", url.Values{"back": {view}}, view); rec.Code != http.StatusSeeOther {
		t.Fatalf("the form with an empty box answered %d", rec.Code)
	}
	if strings.Contains(browse(t, h, view), `name="daily" value="1" checked`) {
		t.Error("after an empty box, the mark is still set")
	}
	person := regexp.MustCompile(`href="(/pages/\d+)"`).FindStringSubmatch(browse(t, h, "/browse?kind=person"))[1]
	if strings.Contains(browse(t, h, person), "Ask for it every day") {
		t.Error("the page of a person draws the daily form")
	}
}

// TestADailyMetricAsksInItsTileOnEveryDay: a day draws one tile for each daily metric, in name order, then the other
// metrics read that day. A daily metric with no reading draws the record form fixed to it; a save returns to the day,
// and the tile then shows the reading once.
func TestADailyMetricAsksInItsTileOnEveryDay(t *testing.T) {
	h, do := lifeOf(t)
	do("/", "register-metric", map[string]string{"name": "Sleep", "unit": "h", "daily": "1"})
	do("/", "register-metric", map[string]string{"name": "Meditate", "daily": "1"})
	do("/", "register-metric", map[string]string{"name": "Weight", "unit": "kg"})
	page := "/days/" + fixedToday
	do(page, "record", map[string]string{"metric": "Weight", "value": "70", "day": fixedToday})

	body := browse(t, h, page)
	names := regexp.MustCompile(`<a class="name" href="/metrics/[^"]*">([^<]+)</a>`).FindAllStringSubmatch(body, -1)
	var got []string
	for _, n := range names {
		got = append(got, n[1])
	}
	if want := "Meditate Sleep Weight"; strings.Join(got, " ") != want {
		t.Errorf("tiles %v, want %s", got, want)
	}
	sleep := regexp.MustCompile(`(?s)<form\b[^>]*id="tile-Sleep".*?</form>`).FindString(body)
	for _, want := range []string{`name="metric" value="Sleep"`, `name="day" value="` + fixedToday + `"`, `type="number"`, `placeholder="h"`, `>Save</button>`} {
		if !strings.Contains(sleep, want) {
			t.Errorf("the Sleep form lacks %s:\n%s", want, sleep)
		}
	}
	meditate := regexp.MustCompile(`(?s)<form\b[^>]*id="tile-Meditate".*?</form>`).FindString(body)
	if strings.Contains(meditate, `type="number"`) {
		t.Errorf("a metric with no unit has a number box:\n%s", meditate)
	}
	for _, want := range []string{`type="radio" name="value" value="1"`, `type="radio" name="value" value="0"`, `Done`, `Not done`} {
		if !strings.Contains(meditate, want) {
			t.Errorf("the Meditate form lacks %s:\n%s", want, meditate)
		}
	}
	if strings.Index(meditate, `value="1"`) > strings.Index(meditate, `value="0"`) {
		t.Errorf("Done is not the first radio:\n%s", meditate)
	}
	if strings.Contains(body, "No readings on this day") || strings.Contains(body, `id="tile-Weight"`) {
		t.Errorf("a day with tiles shows the empty state, or the read Weight asks again")
	}

	sent := formSends(t, body, `id="tile-Sleep"`, url.Values{"value": {"7.5"}})
	rec := submit(h, "/measurements", sent, "")
	if rec.Code != http.StatusSeeOther || returnedTo(t, rec.Header().Get("Location")) != page {
		t.Fatalf("save: %d, returned to %q, want %q", rec.Code, rec.Header().Get("Location"), page)
	}
	after := browse(t, h, page)
	if strings.Contains(after, `id="tile-Sleep"`) || !strings.Contains(after, "7.5") {
		t.Errorf("after the save the Sleep tile still asks, or lacks 7.5")
	}
	if n := strings.Count(after, `class="name" href="/metrics/Sleep"`); n != 1 {
		t.Errorf("Sleep has %d tiles, want 1", n)
	}

	// a past day asks the same
	past := browse(t, h, "/days/"+dayOf(-1))
	if !strings.Contains(past, `id="tile-Sleep"`) || !strings.Contains(past, `id="tile-Meditate"`) {
		t.Errorf("a past day lacks the daily tiles")
	}
}

// TestTheEmptyStateOfReadingsNeedsNoTile: with no daily metric and no reading a day says so; with a daily metric it
// does not, as the tile asks.
func TestTheEmptyStateOfReadingsNeedsNoTile(t *testing.T) {
	h, do := lifeOf(t)
	page := "/days/" + fixedToday
	if !strings.Contains(browse(t, h, page), "No readings on this day") {
		t.Error("an empty day lacks the empty state")
	}
	do("/", "register-metric", map[string]string{"name": "Meditate", "daily": "1"})
	if strings.Contains(browse(t, h, page), "No readings on this day") {
		t.Error("a day with a daily metric shows the empty state")
	}
}
