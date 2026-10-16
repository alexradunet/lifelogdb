package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
)

// TestTheMonthIsACalendar: each day of a month, whether it has a live day page, its first line and its mood; the
// months around it, across a year; the current month; and a month that is not one.
func TestTheMonthIsACalendar(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	capture := func(day string, v map[string]string) {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/"+day)), "capture"), v))
	}
	capture("2031-05-01", map[string]string{"text": "May day: a walk by the lake.", "mood": "4"})
	capture("2031-05-14", map[string]string{"mood": "3"})
	capture("2031-05-31", map[string]string{"text": "The last day."})
	gone := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/2031-05-20")), "capture"), map[string]string{"text": "Scratch"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), apitest.Href(gone, "page"))), "tombstone"), nil))

	for _, tc := range []struct {
		month, title, prev, next string
		days                     int
	}{
		{"2031-05", "May 2031", "/months/2031-04", "/months/2031-06", 31},
		{"2031-06", "June 2031", "/months/2031-05", "/months/2031-07", 30},
		{"2028-02", "February 2028", "/months/2028-01", "/months/2028-03", 29},
		{"2031-02", "February 2031", "/months/2031-01", "/months/2031-03", 28},
		{"2031-12", "December 2031", "/months/2031-11", "/months/2032-01", 31},
		{"2031-01", "January 2031", "/months/2030-12", "/months/2031-02", 31},
	} {
		m := apitest.Must(t)(c.Get(t.Context(), "/months/"+tc.month))
		days := props(m)["days"].([]any)
		if m.Title != tc.title || apitest.Href(m, "prev") != tc.prev || apitest.Href(m, "next") != tc.next || len(days) != tc.days {
			t.Errorf("%s: %q, prev %q, next %q, %d days", tc.month, m.Title, apitest.Href(m, "prev"), apitest.Href(m, "next"), len(days))
		}
		if first := days[0].(map[string]any)["day"]; first != tc.month+"-01" {
			t.Errorf("%s starts on %v", tc.month, first)
		}
		if len(m.Entities) != tc.days || m.Entities[0].Href != "/days/"+tc.month+"-01" {
			t.Errorf("%s embeds %d day links", tc.month, len(m.Entities))
		}
	}

	may := props(apitest.Must(t)(c.Get(t.Context(), "/months/2031-05")))
	var marked []string
	for _, d := range may["days"].([]any) {
		m := d.(map[string]any)
		if m["page"] == true || m["mood"] != nil {
			marked = append(marked, fmt.Sprintf("%v %v %v %v", m["day"], m["page"], m["mood"], m["first_line"]))
		}
	}
	want := "2031-05-01 true [4] May day: a walk by the lake. | 2031-05-14 true [3] <nil> | 2031-05-31 true <nil> The last day."
	if got := strings.Join(marked, " | "); got != want {
		t.Errorf("May:\n got %s\nwant %s", got, want)
	}
	if may["month"] != "2031-05" || may["today"] != fixedToday {
		t.Errorf("May's month %v, today %v", may["month"], may["today"])
	}

	for _, bad := range []string{"2031-13", "2031-5", "x", "2031-05-01"} {
		if _, err := c.Get(t.Context(), "/months/"+bad); apitest.Status(err) != 422 {
			t.Errorf("/months/%s: %v", bad, err)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/months/current", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/months/2031-05" {
		t.Errorf("/months/current: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	body := browse(t, h, "/months/2031-05")
	for _, w := range []string{`<table class="calendar">`,
		`<td class="written" aria-current="date"><a href="/days/2031-05-14"><span class="n">14</span><span class="meta">mood 3</span></a></td>`,
		`<td class="written"><a href="/days/2031-05-01"><span class="n">1</span><span class="first">May day: a walk by the lake.</span><span class="meta">mood 4</span></a></td>`,
		`<td><a href="/days/2031-05-20"><span class="n">20</span></a></td>`,
		`aria-label="Previous month, April 2031"`, `aria-label="Next month, June 2031"`, `action="/days/go"`} {
		if !strings.Contains(body, w) {
			t.Errorf("the calendar lacks %s", w)
		}
	}
	// May 2031 starts on a Thursday: three empty cells before it.
	if !strings.Contains(body, "<tr><td></td><td></td><td></td><td") {
		t.Error("the first week does not start on Monday")
	}
}

func TestOpenDayGoesToTheDay(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	for _, tc := range []struct {
		day  string
		code int
	}{
		{"2031-05-14", http.StatusSeeOther}, {"1999-12-31", http.StatusSeeOther},
		{"2031-02-30", http.StatusUnprocessableEntity}, {"x", http.StatusUnprocessableEntity}, {"", http.StatusUnprocessableEntity},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/days/go?day="+tc.day, nil))
		if rec.Code != tc.code || tc.code == http.StatusSeeOther && rec.Header().Get("Location") != "/days/"+tc.day {
			t.Errorf("open-day %q: %d %q", tc.day, rec.Code, rec.Header().Get("Location"))
		}
	}
	// The catalog action, as every client sends it, opens the day.
	day := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "open-day"), map[string]string{"day": "2031-04-30"}))
	if day.Title != "2031-04-30" || day.Class[0] != "day" {
		t.Errorf("open-day through the client: %s %v", day.Title, day.Class)
	}
}

// TestTheCalendarHoldsEveryDatedThing (docs/plans/101-the-calendar-holds-every-dated-thing.md): a month gives each
// day what names it, written through the catalog as every client writes it, and the browser draws the cells and Day by
// day. The expected items are worked out by hand.
func TestTheCalendarHoldsEveryDatedThing(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday) // 2031-05-14
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	do := func(e *api.Entity, action string, v map[string]string) *api.Entity {
		t.Helper()
		return apitest.Must(t)(c.Do(ctx, apitest.Action(t, e, action), v))
	}

	day := do(apitest.Must(t)(c.Get(ctx, "/days/2031-05-14")), "capture", map[string]string{"text": "A walk by the lake.", "mood": "4"})
	do(root, "register-metric", map[string]string{"name": "weight", "unit": "kg"})
	do(apitest.Must(t)(c.Get(ctx, "/")), "record", map[string]string{"metric": "weight", "day": "2031-05-14", "value": "80"})

	do(root, "create-person", map[string]string{"title": "Ana", "birth_day": "1990-05-14"})
	do(root, "create-person", map[string]string{"title": "Ion", "birth_day": "1930-03-01", "death_day": "2020-05-21"})

	do(root, "create-place", map[string]string{"title": "Lake Park"})
	do(apitest.Must(t)(c.Get(ctx, apitest.Href(day, "page"))), "link", map[string]string{"to": "Lake Park", "kind": "at"})
	do(root, "add-file", map[string]string{"title": "Scan.pdf", "sha256": strings.Repeat("a", 64), "mime": "application/pdf", "day": "2031-05-14"})
	for i := 1; i <= 21; i++ {
		do(root, "add-file", map[string]string{"title": fmt.Sprintf("Import %02d.pdf", i), "sha256": fmt.Sprintf("%064x", i), "mime": "application/pdf", "day": "2031-05-20"})
	}

	month := apitest.Must(t)(c.Get(ctx, "/months/2031-05"))
	got, extra := map[string][]string{}, map[string]string{}
	for _, d := range props(month)["days"].([]any) {
		m := d.(map[string]any)
		on := m["day"].(string)
		for _, it := range anyItems(m["items"]) {
			line := fmt.Sprintf("%v %v", it["kind"], it["title"])
			for _, k := range []string{"state", "years"} {
				if v, ok := it[k]; ok {
					line += fmt.Sprintf(" %s=%v", k, v)
				}
			}
			got[on] = append(got[on], line)
			// every item leads to its own resource, and that resource is the thing named
			e, err := c.Get(ctx, it["href"].(string))
			switch {
			case err != nil:
				t.Errorf("%s: %s: %v", on, it["href"], err)
			case e.Title != it["title"]:
				t.Errorf("%s: %s is %q, want %q", on, it["href"], e.Title, it["title"])
			}
		}
		if m["more_pages"] != nil || m["readings"] != nil {
			extra[on] = fmt.Sprintf("more %v readings %v", m["more_pages"], m["readings"])
		}
	}
	imports := []string{}
	for i := 1; i <= 20; i++ {
		imports = append(imports, fmt.Sprintf("file Import %02d.pdf", i))
	}
	want := map[string][]string{
		"2031-05-14": {"birthday Ana years=41", "place Lake Park", "file Scan.pdf"},
		"2031-05-20": imports,
		"2031-05-21": {"death Ion years=11"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("the items of May:\n got %v\nwant %v", got, want)
	}
	// The mood is not counted with the readings; 20 files of 20 May are items, and the 21st is counted.
	if fmt.Sprint(extra) != "map[2031-05-14:more <nil> readings 1 2031-05-20:more 1 readings <nil>]" {
		t.Errorf("more pages and readings: %v", extra)
	}
	// The week of 20 May has the same days.
	y, w := time.Date(2031, 5, 20, 0, 0, 0, 0, time.UTC).ISOWeek()
	week := props(apitest.Must(t)(c.Get(ctx, fmt.Sprintf("/weeks/%04d-W%02d", y, w))))
	for _, d := range week["days"].([]any) {
		if m := d.(map[string]any); m["day"] == "2031-05-20" && (len(anyItems(m["items"])) != 20 || fmt.Sprint(m["more_pages"]) != "1") {
			t.Errorf("the week's 20 May: %d items, more %v", len(anyItems(m["items"])), m["more_pages"])
		}
	}

	body := browse(t, h, "/months/2031-05")
	for _, s := range []string{
		// a cell: three items, each with a square, the mood and the readings; a phone's squares
		`<span class="item">Ana <small>birthday</small></span><span class="item">Lake Park <small>place</small></span><span class="item">Scan.pdf</span></span>`,
		`<span class="marks" aria-hidden="true"><i></i><i></i><i></i></span><span class="meta">mood 4 · 1 reading</span>`,
		`<span class="more">and 18 more</span>`,
		// Day by day: a day a heading, each item a tag, a link to the thing and a note
		`<h3><a href="/days/2031-05-14">`,
		`<span class="tag">birthday</span> <a href="/pages/`,
		`>Ana</a> <small class="agenda-note">41 years</small>`,
		`>Ion</a> <small class="agenda-note">11 years ago</small>`,
		`<a href="/days/2031-05-20">and 1 more page</a>`,
	} {
		if !strings.Contains(body, s) {
			t.Errorf("the calendar lacks %s", s)
		}
	}

	// A month with nothing says so: a fresh file's.
	_, none, _ := apitest.FreshOn(t, "cli", fixedToday)
	empty := browse(t, none, "/months/2031-08")
	for _, s := range []string{"No page, place or birthday has a day in August 2031."} {
		if !strings.Contains(empty, s) {
			t.Errorf("an empty month lacks %q", s)
		}
	}
}

// anyItems are the items of a day of the calendar as the JSON client reads them.
func anyItems(v any) []map[string]any {
	list, _ := v.([]any)
	out := make([]map[string]any, 0, len(list))
	for _, x := range list {
		out = append(out, x.(map[string]any))
	}
	return out
}

// TestTheFirstMonthOfTheDaysIsACalendar: January 0000 holds the first day that a date can name. Its calendar has its
// 31 days, a day page on its first day and a reading on its third day.
func TestTheFirstMonthOfTheDaysIsACalendar(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/0000-01-01")), "capture"), map[string]string{"text": "The first day."}))
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "weight", "day": "0000-01-03", "value": "70"}))
	m, err := c.Get(t.Context(), "/months/0000-01")
	if err != nil {
		t.Fatalf("/months/0000-01: %v", err)
	}
	days := props(m)["days"].([]any)
	if m.Title != "January 0000" || len(days) != 31 {
		t.Fatalf("%q, %d days", m.Title, len(days))
	}
	first, third := days[0].(map[string]any), days[2].(map[string]any)
	if first["day"] != "0000-01-01" || first["page"] != true || first["first_line"] != "The first day." {
		t.Errorf("the first day: %v", first)
	}
	if third["day"] != "0000-01-03" || third["page"] != false || said(third["readings"]) != "1" {
		t.Errorf("the third day: %v", third)
	}
	browse(t, h, "/months/0000-01")
}
