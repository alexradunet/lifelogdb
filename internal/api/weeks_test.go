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
// read on 1 October (week 40), 6 and 9 October.
func weekScenario(t *testing.T) (*client.Client, http.Handler) {
	t.Helper()
	c, h := freshOn(t, weekToday)
	for day, text := range map[string]string{"2026-10-05": "Monday: the long meeting.", "2026-10-07": "Wednesday: a walk by the river.", "2026-10-11": "Sunday: rest."} {
		must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/days/"+day)), "capture"), map[string]string{"text": text}))
	}
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	for day, value := range map[string]string{"2026-10-01": "71.9", "2026-10-06": "71.6", "2026-10-09": "71.2"} {
		must(c.Do(t.Context(), find(root, "record"), map[string]string{"metric": "weight", "day": day, "value": value}))
	}
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
// readings are checked against the scenario by hand.
func TestTheWeekLooksBackOverSevenDays(t *testing.T) {
	c, h := weekScenario(t)
	w := must(c.Get(t.Context(), "/weeks/2026-W41"))
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

	metrics := objects(p, "metrics")
	if len(metrics) != 1 || metrics[0]["metric"] != "weight" || metrics[0]["unit"] != "kg" || said(metrics[0]["readings"]) != "2" ||
		said(metrics[0]["mean"]) != "71.4" || said(metrics[0]["previous_mean"]) != "71.9" {
		t.Errorf("metrics %v: weight, 2 readings, mean (71.6+71.2)/2 = 71.4, the week before 71.9", metrics)
	}
	for rel, want := range map[string]string{"self": "/weeks/2026-W41", "prev": "/weeks/2026-W40", "next": "/weeks/2026-W42", "month": "/months/2026-10", "year": "/years/2026"} {
		if got := hrefOf(w, rel); got != want {
			t.Errorf("link %s = %q, want %q", rel, got, want)
		}
	}
	if len(w.Actions) != 1 || find(w, "open-day").Name != "open-day" {
		t.Errorf("actions: %s", names(w))
	}

	// The week before has one reading and no week before it: no previous mean.
	p40 := props(must(c.Get(t.Context(), "/weeks/2026-W40")))
	m40 := objects(p40, "metrics")
	if len(m40) != 1 || said(m40[0]["readings"]) != "1" || said(m40[0]["mean"]) != "71.9" {
		t.Fatalf("week 40 metrics %v", m40)
	}
	if _, has := m40[0]["previous_mean"]; has {
		t.Error("week 40 has a previous mean, and week 39 has no reading")
	}
	// A week with nothing in it is still a week.
	p42 := props(must(c.Get(t.Context(), "/weeks/2026-W42")))
	if len(objects(p42, "metrics")) != 0 || len(objects(p42, "days")) != 7 {
		t.Errorf("week 42: %v", p42)
	}
	// 2026 and 2020 have 53 weeks. Week 1 of 2026 starts in 2025, and its Thursday is in January.
	for week, want := range map[string]string{"2026-W53": "2026-12-28", "2020-W53": "2020-12-28", "2027-W01": "2027-01-04", "2026-W01": "2025-12-29"} {
		if got := props(must(c.Get(t.Context(), "/weeks/"+week)))["from"]; got != want {
			t.Errorf("%s starts on %v, want %s", week, got, want)
		}
	}
	w1 := must(c.Get(t.Context(), "/weeks/2026-W01"))
	if hrefOf(w1, "month") != "/months/2026-01" || hrefOf(w1, "year") != "/years/2026" || hrefOf(w1, "prev") != "/weeks/2025-W52" {
		t.Errorf("week 1 of 2026 links: %s %s %s", hrefOf(w1, "month"), hrefOf(w1, "year"), hrefOf(w1, "prev"))
	}

	for _, bad := range []string{"2026-W60", "2026-10", "2026-W00", "2026-W54", "2025-W53", "2026-W1", "x", "2026-w41", "2026-W41x"} {
		if _, err := c.Get(t.Context(), "/weeks/"+bad); !clientStatus(err, 422) {
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
		"Monday: the long meeting.", "Sunday: rest.", "71.4 kg", "down from 71.9",
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
	empty := browse(t, h, "/weeks/2026-W20")
	for _, want := range []string{"No reading this week."} {
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
		must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/days/"+day)), "capture"), map[string]string{"text": "A page."}))
	}
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	for day, value := range map[string]string{"2025-12-31": "70", "2026-10-01": "71.9", "2026-10-06": "71.6", "2026-10-09": "71.2"} {
		must(c.Do(t.Context(), find(root, "record"), map[string]string{"metric": "weight", "day": day, "value": value}))
	}
	y := must(c.Get(t.Context(), "/years/2026"))
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
	if len(objects(props(must(c.Get(t.Context(), "/years/2028"))), "months")[1]["days"].([]any)) != 29 {
		t.Error("February 2028 has not 29 days")
	}
	for _, bad := range []string{"20", "abcd", "0000", "20260", "2026-10"} {
		if _, err := c.Get(t.Context(), "/years/"+bad); !clientStatus(err, 422) {
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
	home := must(c.Get(t.Context(), "/"))
	if hrefOf(home, "week") != "/weeks/current" || hrefOf(home, "year") != "/years/current" {
		t.Errorf("Home links: week %q, year %q", hrefOf(home, "week"), hrefOf(home, "year"))
	}
}

// said is a property as the client decodes it, written out: a number is a json.Number.
func said(v any) string { return fmt.Sprint(v) }

// TestTheFirstWeekOfTheDaysHasNoLowerBound: week 1 of 0000 runs from Monday 3 to Sunday 9 January 0000. The week
// before it starts in the year -1, before the first day that a date can name, so the week reads its series with no
// lower bound: the reading of 0000-01-01 is the week before. Week 2 of 0000 has a lower bound again: the reading of
// 0000-01-01 is not in it or in the week before it.
func TestTheFirstWeekOfTheDaysHasNoLowerBound(t *testing.T) {
	c, h := freshOn(t, weekToday)
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	for day, value := range map[string]string{"0000-01-01": "70", "0000-01-03": "71", "0000-01-09": "73", "0000-01-10": "74"} {
		must(c.Do(t.Context(), find(root, "record"), map[string]string{"metric": "weight", "day": day, "value": value}))
	}
	for _, tc := range []struct{ week, from, through, readings, mean, previous string }{
		{"0000-W01", "0000-01-03", "0000-01-09", "2", "72", "70"},
		{"0000-W02", "0000-01-10", "0000-01-16", "1", "74", "72"},
	} {
		w, err := c.Get(t.Context(), "/weeks/"+tc.week)
		if err != nil {
			t.Errorf("/weeks/%s: %v", tc.week, err)
			continue
		}
		p := props(w)
		if p["from"] != tc.from || p["through"] != tc.through || len(objects(p, "days")) != 7 {
			t.Errorf("%s: from %v, through %v, %d days", tc.week, p["from"], p["through"], len(objects(p, "days")))
		}
		m := objects(p, "metrics")
		if len(m) != 1 || said(m[0]["readings"]) != tc.readings || said(m[0]["mean"]) != tc.mean || said(m[0]["previous_mean"]) != tc.previous {
			t.Errorf("%s metrics %v: want %s readings, mean %s, the week before %s", tc.week, m, tc.readings, tc.mean, tc.previous)
		}
		browse(t, h, "/weeks/"+tc.week)
	}
}

// TestTheEdgesOfTheCalendarLinkOnlyToPages: the days run from 0000-01-01 to 9999-12-31 (lifelog_meta.days), and a
// year has a page from 0001 to 9999. The first and the last week, month and year answer, and each of their links
// leads to a page that answers: a link to a period that has no page is absent, in the links of the entity and in its
// HTML. A week of the year 0000 has no year link.
func TestTheEdgesOfTheCalendarLinkOnlyToPages(t *testing.T) {
	c, h := freshOn(t, weekToday)
	calendarHref := regexp.MustCompile(`href="(/(?:weeks|months|years)/[^"]+)"`)
	answers := func(path, accept string) int {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Accept", accept)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, tc := range []struct{ page, rels string }{
		{"/weeks/0000-W01", "next,month"},
		{"/weeks/0000-W02", "prev,next,month"},
		{"/weeks/9999-W52", "prev,month,year"},
		{"/months/0000-01", "next"},
		{"/months/9999-12", "prev"},
		{"/years/0001", "next"},
		{"/years/9999", "prev"},
	} {
		e, err := c.Get(t.Context(), tc.page)
		if err != nil {
			t.Errorf("%s: %v", tc.page, err)
			continue
		}
		var rels []string
		for _, rel := range []string{"prev", "next", "up", "month", "year"} {
			href := hrefOf(e, rel)
			if href == "" {
				continue
			}
			rels = append(rels, rel)
			if code := answers(href, "application/vnd.siren+json"); code != http.StatusOK {
				t.Errorf("%s: the %s link %s answers %d, want 200", tc.page, rel, href, code)
			}
		}
		if got := strings.Join(rels, ","); got != tc.rels {
			t.Errorf("%s links %s, want %s", tc.page, got, tc.rels)
		}
		for _, m := range calendarHref.FindAllStringSubmatch(browse(t, h, tc.page), -1) {
			if code := answers(m[1], "text/html"); code != http.StatusOK && code != http.StatusSeeOther {
				t.Errorf("%s: its HTML links %s, which answers %d", tc.page, m[1], code)
			}
		}
	}
}

// TestTheLastWeekOfTheDaysEndsOnTheLastDay: week 52 of 9999 runs from Monday 27 to Friday 31 December 9999, the last
// day that a date can name. The week reads its five days, with a reading in it and with none.
func TestTheLastWeekOfTheDaysEndsOnTheLastDay(t *testing.T) {
	c, h := freshOn(t, weekToday)
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	for _, reading := range []string{"", "9999-12-31"} {
		if reading != "" {
			must(c.Do(t.Context(), find(root, "record"), map[string]string{"metric": "weight", "day": reading, "value": "70"}))
		}
		w, err := c.Get(t.Context(), "/weeks/9999-W52")
		if err != nil {
			t.Errorf("/weeks/9999-W52 (a reading on %q): %v", reading, err)
			continue
		}
		p := props(w)
		days := objects(p, "days")
		if p["from"] != "9999-12-27" || p["through"] != "9999-12-31" || len(days) != 5 || days[4]["day"] != "9999-12-31" {
			t.Errorf("9999-W52: from %v, through %v, %d days", p["from"], p["through"], len(days))
		}
		if m := objects(p, "metrics"); (reading == "") != (len(m) == 0) || reading != "" && said(m[0]["readings"]) != "1" {
			t.Errorf("9999-W52 metrics %v, with a reading on %q", m, reading)
		}
		browse(t, h, "/weeks/9999-W52")
	}
}
