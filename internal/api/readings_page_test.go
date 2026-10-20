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
	// A link that names a metric selects it, and a name that no metric has selects nothing: the browser then
	// refuses the empty prompt, and the server refuses the name (404), as before.
	if got := browserSends(t, browse(t, h, "/?new=record&metric=Nothing&value=1")).Get("metric"); got != "" {
		t.Errorf("an unknown metric in the link is replaced by %q", got)
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
	do("/", "register-metric", map[string]string{"name": "Stretch"})
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

// TestHabitsAndUnitsAreSaid: a metric with no unit says how a habit comes and goes, with the days of the core, and
// its value box shows what to type; a metric with a unit says neither. The tiles of a day show the unit, and a
// metric read again on the day links to its page.
func TestHabitsAndUnitsAreSaid(t *testing.T) {
	h, do := lifeOf(t)
	do("/", "register-metric", map[string]string{"name": "Stretch"})
	do("/", "register-metric", map[string]string{"name": "Weight", "unit": "kg"})
	do("/", "register-metric", map[string]string{"name": "Mood score", "unit": "1-5"})
	for _, v := range []string{"70", "71"} {
		do("/days/"+fixedToday, "record", map[string]string{"metric": "Weight", "value": v})
	}
	do("/days/"+fixedToday, "record", map[string]string{"metric": "Mood score", "value": "4"})
	do("/days/"+fixedToday, "record", map[string]string{"metric": "Stretch", "value": "1"})

	habit := browse(t, h, "/metrics/Stretch")
	for _, want := range []string{"shows in Today as a habit after its first reading, and leaves Today 14 days after its last", `placeholder="a count, or 1 done and 0 not done"`} {
		if !strings.Contains(habit, want) {
			t.Errorf("the page of a habit lacks %q", want)
		}
	}
	if weight := browse(t, h, "/metrics/Weight"); strings.Contains(weight, "days after its last") || strings.Contains(weight, "1 done and 0 not done") {
		t.Error("the page of a metric with a unit speaks of habits")
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
