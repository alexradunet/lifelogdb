package api_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
	"lifelog/internal/client"
)

// lifeOf makes a fresh life around fixedToday, and a function that does an action of a page as the CLI would.
func lifeOf(t *testing.T) (http.Handler, func(from, action string, v map[string]string)) {
	t.Helper()
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	return h, func(from, action string, v map[string]string) {
		t.Helper()
		doAction(t, c, from, action, v)
	}
}

func doAction(t *testing.T, c *client.Client, from, action string, v map[string]string) {
	t.Helper()
	must := apitest.Must(t)
	must(c.Do(t.Context(), apitest.Action(t, must(c.Get(t.Context(), from)), action), v))
}

// TestTheTodayStripReadsTheLife: three days written in a row ending today, a mood today, and three habits in use:
// Walk done today, Stretch with a reading ten days ago and none today, Smoke recorded 0 today. A metric with a unit
// is no habit. The strip counts the entry, the mood and Walk: 3 of 5. The head of today shows the same streak.
func TestTheTodayStripReadsTheLife(t *testing.T) {
	h, do := lifeOf(t)
	for _, n := range []string{"Walk", "Stretch", "Smoke"} {
		do("/", "register-metric", map[string]string{"name": n})
	}
	do("/", "register-metric", map[string]string{"name": "Weight", "unit": "kg"})
	for _, n := range []int{-2, -1, 0} {
		do("/days/"+dayOf(n), "capture", map[string]string{"text": "Synthetic words."})
	}
	do("/days/"+fixedToday, "capture", map[string]string{"mood": "4"})
	for _, r := range []struct{ metric, day, value string }{
		{"Walk", fixedToday, "1"}, {"Stretch", dayOf(-10), "1"}, {"Smoke", fixedToday, "0"}, {"Weight", fixedToday, "70"},
	} {
		do("/days/"+r.day, "record", map[string]string{"metric": r.metric, "value": r.value})
	}

	body := browse(t, h, "/days/"+fixedToday)
	strip := regexp.MustCompile(`(?s)<section class="rail-today".*?</section>`).FindString(body)
	for _, want := range []string{
		"3 of 5 done", "3 days in a row",
		`<a class="chip done" href="/days/today#capture">✓ entry</a>`, `<a class="chip done" href="/days/today#capture">✓ mood</a>`,
		`<span class="chip done">✓ Walk</span>`, `<a class="chip" href="/?back=%2Fdays%2F2031-05-14&amp;metric=Stretch&amp;new=record&amp;value=1#record">Stretch</a>`,
		`<span class="chip">Smoke · no</span>`,
	} {
		if !strings.Contains(strip, want) {
			t.Errorf("the strip lacks %s:\n%s", want, strip)
		}
	}
	if strings.Contains(strip, "Weight") {
		t.Errorf("a metric with a unit is in the strip:\n%s", strip)
	}
	if !strings.Contains(body, `<span class="tag open">today</span> <span class="tag">3 days in a row</span>`) {
		t.Errorf("the head of today does not show the streak:\n%.1500s", body)
	}
	// a day that is not today has the strip in its rail, and no streak tag in its head
	if past := browse(t, h, "/days/"+dayOf(-1)); !strings.Contains(past, "3 days in a row</div>") || strings.Contains(past, `<span class="tag">3 days in a row</span>`) {
		t.Errorf("the page of a past day: the rail must show the streak and the head must not")
	}
}

// TestTheLinkOfAHabitChipFillsTheRecordForm: the chip leads to Home with the form of the reading open, the habit
// chosen and the value 1 filled in, from the query. No other form is involved.
func TestTheLinkOfAHabitChipFillsTheRecordForm(t *testing.T) {
	h, do := lifeOf(t)
	do("/", "register-metric", map[string]string{"name": "Walk"})
	do("/days/"+dayOf(-1), "record", map[string]string{"metric": "Walk", "value": "1"})
	body := browse(t, h, "/?new=record&metric=Walk&value=1")
	form := regexp.MustCompile(`(?s)<div id="record">.*?</form>`).FindString(body)
	if !strings.Contains(form, `<option selected>Walk</option>`) || !regexp.MustCompile(`name="value" value="1"`).MatchString(form) || !strings.Contains(form, "<details class=\"action\" open>") {
		t.Errorf("the form is not open and filled with Walk and 1:\n%s", form)
	}
}

// TestOnThisDayListsTheSameDayOfEarlierYears: the margin of a day has a group with its day pages of the years before,
// the nearest year first, while a page exists. The group is on every day, not only on today, and absent when no page
// exists.
func TestOnThisDayListsTheSameDayOfEarlierYears(t *testing.T) {
	h, do := lifeOf(t)
	group := regexp.MustCompile(`(?s)<section class="group" aria-label="On this day">.*?</section>`)
	if g := group.FindString(browse(t, h, "/days/"+fixedToday)); g != "" {
		t.Errorf("a day with no earlier year has a group:\n%s", g)
	}
	do("/days/2030-05-14", "capture", map[string]string{"text": "A year ago.", "mood": "5"})
	do("/days/2029-05-14", "capture", map[string]string{"text": "Two years ago."})
	do("/days/2027-05-14", "capture", map[string]string{"text": "Four years ago, after a gap."})
	g := group.FindString(browse(t, h, "/days/"+fixedToday))
	if g == "" {
		t.Fatal("no group on today")
	}
	for _, want := range []string{`href="/days/2030-05-14"`, `A year ago.`, `mood 5`, `href="/days/2029-05-14"`, `Two years ago.`} {
		if !strings.Contains(g, want) {
			t.Errorf("the group lacks %s:\n%s", want, g)
		}
	}
	if strings.Contains(g, "2027-05-14") || strings.Index(g, "2030-05-14") > strings.Index(g, "2029-05-14") {
		t.Errorf("the list goes past a missing year, or the nearest year is not first:\n%s", g)
	}
	// a past day has its own earlier years
	if g := group.FindString(browse(t, h, "/days/2030-05-14")); !strings.Contains(g, `href="/days/2029-05-14"`) || strings.Contains(g, "2030-05-14") {
		t.Errorf("the group of 2030-05-14:\n%s", g)
	}
}

// TestOnThisDayStopsAtFiveYears: at most five earlier years.
func TestOnThisDayStopsAtFiveYears(t *testing.T) {
	h, do := lifeOf(t)
	for y := 2025; y <= 2030; y++ {
		do("/days/"+fmt.Sprintf("%d-05-14", y), "capture", map[string]string{"text": "Synthetic words."})
	}
	body := browse(t, h, "/days/"+fixedToday)
	g := regexp.MustCompile(`(?s)<section class="group" aria-label="On this day">.*?</section>`).FindString(body)
	if n := strings.Count(g, `class="entry`); n != 5 || strings.Contains(g, "2025-05-14") {
		t.Errorf("%d entries, want the five nearest years (2026 to 2030):\n%s", n, g)
	}
}
