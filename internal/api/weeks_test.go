package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/client"
)

// weekToday is the local day of the week and year tests: a Monday, the first day of ISO week 42 of 2026.
const weekToday = "2026-10-12"

// weekScenario writes, through the catalog, week 41 of 2026 and the week before it. The tests check the answers by
// hand: pages on Mon 5, Wed 7 and Sun 11 October; the habit walk since 20 September, done on 6 and 8 October; weight
// read on 1 October (week 40), 6 and 9 October; a task due on 7 October and done, a task due on 10 October and open.
func weekScenario(t *testing.T) (*client.Client, http.Handler) {
	t.Helper()
	c, h := freshOn(t, weekToday)
	for day, text := range map[string]string{"2026-10-05": "Monday: the long meeting.", "2026-10-07": "Wednesday: a walk by the river.", "2026-10-11": "Sunday: rest."} {
		must(c.Do(find(must(c.Get("/days/"+day)), "capture"), map[string]string{"text": text}))
	}
	root := must(c.Get("/"))
	must(c.Do(find(root, "register-metric"), map[string]string{"name": "walk"}))
	must(c.Do(find(root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	must(c.Do(find(must(c.Get("/metrics/walk")), "start-habit"), map[string]string{"start_day": "2026-09-20"}))
	for _, day := range []string{"2026-10-06", "2026-10-08"} {
		must(c.Do(find(must(c.Get("/habits?day="+day)), "check-in"), map[string]string{"day": day, "done": "1"}))
	}
	for day, value := range map[string]string{"2026-10-01": "71.9", "2026-10-06": "71.6", "2026-10-09": "71.2"} {
		must(c.Do(find(root, "record"), map[string]string{"metric": "weight", "day": day, "value": value}))
	}
	done := must(c.Do(find(root, "create-task"), map[string]string{"label": "Send the form", "due_day": "2026-10-07"}))
	once := strings.SplitN(hrefOf(done, "self"), "?", 2)[0] + "/occurrences/once"
	must(c.Do(find(must(c.Get(once)), "edit-occurrence"), map[string]string{"state": "done", "completed_at": "now"}))
	must(c.Do(find(root, "create-task"), map[string]string{"label": "Call the bank", "due_day": "2026-10-10"}))
	return c, h
}

// objects is a list property as a list of objects.
func objects(p map[string]any, key string) []map[string]any {
	var out []map[string]any
	for _, x := range p[key].([]any) {
		out = append(out, x.(map[string]any))
	}
	return out
}

// TestTheWeekLooksBackOverSevenDays: week 41 of 2026 runs from Monday 5 to Sunday 11 October. Its days, habits,
// readings and occurrences are checked against the scenario by hand.
func TestTheWeekLooksBackOverSevenDays(t *testing.T) {
	c, h := weekScenario(t)
	w := must(c.Get("/weeks/2026-W41"))
	p := props(w)
	if w.Class[0] != "week" || w.Title != "Week 41 of 2026" || p["week"] != "2026-W41" || p["from"] != "2026-10-05" || p["through"] != "2026-10-11" || p["today"] != weekToday {
		t.Errorf("the week: %v %q %v", w.Class, w.Title, p)
	}
	week := []string{"2026-10-05", "2026-10-06", "2026-10-07", "2026-10-08", "2026-10-09", "2026-10-10", "2026-10-11"}
	lines := map[string]string{"2026-10-05": "Monday: the long meeting.", "2026-10-07": "Wednesday: a walk by the river.", "2026-10-11": "Sunday: rest."}
	days := objects(p, "days")
	if len(days) != 7 || len(w.Entities) != 7 {
		t.Fatalf("%d days, %d day links", len(days), len(w.Entities))
	}
	for i, day := range week {
		line, written := lines[day]
		d := days[i]
		if d["day"] != day || (d["page"] == true) != written || w.Entities[i].Href != "/days/"+day {
			t.Errorf("day %d: %v, want %s, written %v", i, d, day, written)
		}
		if written && d["first_line"] != line {
			t.Errorf("%s first line %v, want %q", day, d["first_line"], line)
		}
		if _, has := d["first_line"]; !written && has {
			t.Errorf("%s has a first line and no page", day)
		}
	}

	habits := objects(p, "habits")
	if len(habits) != 1 || habits[0]["metric"] != "walk" || said(habits[0]["done"]) != "2" || said(habits[0]["active_days"]) != "7" {
		t.Errorf("habits %v: walk done on 2 of its 7 active days", habits)
	}
	metrics := objects(p, "metrics")
	if len(metrics) != 1 || metrics[0]["metric"] != "weight" || metrics[0]["unit"] != "kg" || said(metrics[0]["readings"]) != "2" ||
		said(metrics[0]["mean"]) != "71.4" || said(metrics[0]["previous_mean"]) != "71.9" {
		t.Errorf("metrics %v: weight, 2 readings, mean (71.6+71.2)/2 = 71.4, the week before 71.9", metrics)
	}
	occ := objects(p, "occurrences")
	if len(occ) != 2 || occ[0]["label"] != "Send the form" || occ[0]["state"] != "done" || occ[1]["label"] != "Call the bank" || occ[1]["state"] != "open" {
		t.Errorf("occurrences %v: one done (due 7 October), one open (due 10 October)", occ)
	}
	for rel, want := range map[string]string{"self": "/weeks/2026-W41", "prev": "/weeks/2026-W40", "next": "/weeks/2026-W42", "month": "/months/2026-10", "year": "/years/2026"} {
		if got := hrefOf(w, rel); got != want {
			t.Errorf("link %s = %q, want %q", rel, got, want)
		}
	}
	if len(w.Actions) != 1 || find(w, "open-day").Name != "open-day" {
		t.Errorf("actions: %s", names(w))
	}

	// The week before has one reading and no week before it: no previous mean. Its habit was active on all 7 days and done on none.
	p40 := props(must(c.Get("/weeks/2026-W40")))
	m40 := objects(p40, "metrics")
	if len(m40) != 1 || said(m40[0]["readings"]) != "1" || said(m40[0]["mean"]) != "71.9" {
		t.Fatalf("week 40 metrics %v", m40)
	}
	if _, has := m40[0]["previous_mean"]; has {
		t.Error("week 40 has a previous mean, and week 39 has no reading")
	}
	if h40 := objects(p40, "habits"); len(h40) != 1 || said(h40[0]["done"]) != "0" || said(h40[0]["active_days"]) != "7" {
		t.Errorf("week 40 habits %v", h40)
	}
	// A week with nothing in it is still a week.
	p42 := props(must(c.Get("/weeks/2026-W42")))
	if len(objects(p42, "metrics")) != 0 || len(objects(p42, "occurrences")) != 0 || len(objects(p42, "days")) != 7 {
		t.Errorf("week 42: %v", p42)
	}
	// 2026 and 2020 have 53 weeks. Week 1 of 2026 starts in 2025, and its Thursday is in January.
	for week, want := range map[string]string{"2026-W53": "2026-12-28", "2020-W53": "2020-12-28", "2027-W01": "2027-01-04", "2026-W01": "2025-12-29"} {
		if got := props(must(c.Get("/weeks/" + week)))["from"]; got != want {
			t.Errorf("%s starts on %v, want %s", week, got, want)
		}
	}
	w1 := must(c.Get("/weeks/2026-W01"))
	if hrefOf(w1, "month") != "/months/2026-01" || hrefOf(w1, "year") != "/years/2026" || hrefOf(w1, "prev") != "/weeks/2025-W52" {
		t.Errorf("week 1 of 2026 links: %s %s %s", hrefOf(w1, "month"), hrefOf(w1, "year"), hrefOf(w1, "prev"))
	}

	for _, bad := range []string{"2026-W60", "2026-10", "2026-W00", "2026-W54", "2025-W53", "2026-W1", "x", "2026-w41", "2026-W41x"} {
		if _, err := c.Get("/weeks/" + bad); !clientStatus(err, 422) {
			t.Errorf("/weeks/%s: %v", bad, err)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/weeks/current", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/weeks/2026-W42" {
		t.Errorf("/weeks/current: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	body := browse(t, h, "/weeks/2026-W41")
	for _, want := range []string{"Week 41 of 2026", "Mon 5 Oct to Sun 11 Oct", `href="/months/2026-10"`, `href="/years/2026"`,
		"Monday: the long meeting.", "Sunday: rest.", "walk", "2 of 7 days", "71.4 kg", "down from 71.9", "Send the form", "Call the bank",
		`rel="prev"`, `rel="next"`, `action="/days/go"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the week's page lacks %q", want)
		}
	}
	for _, day := range week {
		if !regexp.MustCompile(`<a class="entry[^"]*" href="/days/` + day + `">`).MatchString(body) {
			t.Errorf("the week's page has no row for %s", day)
		}
	}
	if n := strings.Count(body, `<a class="entry unwritten"`); n != 4 || strings.Count(body, "nothing written") != 4 {
		t.Errorf("%d unwritten rows, want 4", n)
	}
	if strings.Index(body, "Send the form") > strings.Index(body, "Call the bank") {
		t.Error("the done occurrence is not before the open one")
	}
	empty := browse(t, h, "/weeks/2026-W20")
	for _, want := range []string{"No habit was active this week.", "No reading this week.", "Nothing was due this week."} {
		if !strings.Contains(empty, want) {
			t.Errorf("an empty week lacks %q", want)
		}
	}
	if !strings.Contains(browse(t, h, "/weeks/2026-W40"), "first week with readings") {
		t.Error("week 40 does not say it is the first week with readings")
	}
	if !strings.Contains(browse(t, h, "/days/2026-10-07"), `<a class="date" href="/weeks/2026-W41">week 41</a>`) {
		t.Error("a day does not link to its week")
	}
}

// TestTheYearHoldsTwelveMonths: the day pages and the current readings of one year, month by month. A reading of the
// year before is not counted.
func TestTheYearHoldsTwelveMonths(t *testing.T) {
	c, h := freshOn(t, weekToday)
	for _, day := range []string{"2026-10-05", "2026-10-07", "2026-10-11"} {
		must(c.Do(find(must(c.Get("/days/"+day)), "capture"), map[string]string{"text": "A page."}))
	}
	root := must(c.Get("/"))
	must(c.Do(find(root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	for day, value := range map[string]string{"2025-12-31": "70", "2026-10-01": "71.9", "2026-10-06": "71.6", "2026-10-09": "71.2"} {
		must(c.Do(find(root, "record"), map[string]string{"metric": "weight", "day": day, "value": value}))
	}
	y := must(c.Get("/years/2026"))
	p := props(y)
	if y.Class[0] != "year" || y.Title != "2026" || p["year"] != "2026" || p["today"] != weekToday || said(p["written"]) != "3" || said(p["readings"]) != "3" {
		t.Errorf("the year: %v %q %v %v %v %v", y.Class, y.Title, p["year"], p["today"], p["written"], p["readings"])
	}
	months := objects(p, "months")
	if len(months) != 12 || len(y.Entities) != 12 || y.Entities[9].Href != "/months/2026-10" {
		t.Fatalf("%d months, %d links", len(months), len(y.Entities))
	}
	if months[9]["month"] != "2026-10" || said(months[9]["written"]) != "3" || said(months[0]["written"]) != "0" || len(months[1]["days"].([]any)) != 28 || len(months[9]["days"].([]any)) != 31 {
		t.Errorf("months: %v %v %v", months[9]["written"], months[0]["written"], len(months))
	}
	if d := months[9]["days"].([]any)[4].(map[string]any); d["day"] != "2026-10-05" || d["page"] != true {
		t.Errorf("5 October: %v", d)
	}
	for rel, want := range map[string]string{"self": "/years/2026", "prev": "/years/2025", "next": "/years/2027"} {
		if got := hrefOf(y, rel); got != want {
			t.Errorf("link %s = %q, want %q", rel, got, want)
		}
	}
	if len(y.Actions) != 1 || find(y, "open-day").Name != "open-day" {
		t.Errorf("actions: %s", names(y))
	}
	if len(objects(props(must(c.Get("/years/2028"))), "months")[1]["days"].([]any)) != 29 {
		t.Error("February 2028 has not 29 days")
	}
	for _, bad := range []string{"20", "abcd", "0000", "20260", "2026-10"} {
		if _, err := c.Get("/years/" + bad); !clientStatus(err, 422) {
			t.Errorf("/years/%s: %v", bad, err)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/years/current", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/years/2026" {
		t.Errorf("/years/current: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	body := browse(t, h, "/years/2026")
	grid, _, _ := strings.Cut(strings.SplitN(body, `<div class="year">`, 2)[1], `<p class="sub">`) // the sidebar has a month box of its own
	if n := strings.Count(grid, `<div class="month-box">`); n != 12 {
		t.Errorf("%d month boxes in the year, want 12", n)
	}
	for _, want := range []string{`<a href="/months/2026-01">January</a>`, `<a href="/months/2026-12">December</a>`, `<span class="meta">3 written</span>`,
		`<a href="/days/2026-10-12" aria-current="date"`, `<a href="/days/2026-10-05" class="written"`, "3 days written", "3 readings", `rel="prev"`, `rel="next"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the year's page lacks %q", want)
		}
	}
	if !strings.Contains(browse(t, h, "/years/2027"), "0 days written") {
		t.Error("a year with no page does not say that no day is written")
	}
	if !strings.Contains(browse(t, h, "/months/2026-10"), `<a href="/years/2026">The year</a>`) {
		t.Error("a month does not link to its year")
	}
}

// TestHomeLinksTheWeekAndTheYear: Home links this week and this year.
func TestHomeLinksTheWeekAndTheYear(t *testing.T) {
	c, _ := freshOn(t, weekToday)
	home := must(c.Get("/"))
	if hrefOf(home, "week") != "/weeks/current" || hrefOf(home, "year") != "/years/current" {
		t.Errorf("Home links: week %q, year %q", hrefOf(home, "week"), hrefOf(home, "year"))
	}
}

// said is a property as the client decodes it, written out: a number is a json.Number.
func said(v any) string { return fmt.Sprint(v) }
