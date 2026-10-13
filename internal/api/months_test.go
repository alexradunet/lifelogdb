package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api"
)

// TestTheMonthIsACalendar: each day of a month, whether it has a live day page, its first line and its mood; the
// months around it, across a year; the current month; and a month that is not one.
func TestTheMonthIsACalendar(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	capture := func(day string, v map[string]string) {
		must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/days/"+day)), "capture"), v))
	}
	capture("2031-05-01", map[string]string{"text": "May day: a walk by the lake.", "mood": "4"})
	capture("2031-05-14", map[string]string{"mood": "3"})
	capture("2031-05-31", map[string]string{"text": "The last day."})
	gone := must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/days/2031-05-20")), "capture"), map[string]string{"text": "Scratch"}))
	must(c.Do(t.Context(), find(must(c.Get(t.Context(), hrefOf(gone, "page"))), "tombstone"), nil))

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
		m := must(c.Get(t.Context(), "/months/"+tc.month))
		days := props(m)["days"].([]any)
		if m.Title != tc.title || hrefOf(m, "prev") != tc.prev || hrefOf(m, "next") != tc.next || len(days) != tc.days {
			t.Errorf("%s: %q, prev %q, next %q, %d days", tc.month, m.Title, hrefOf(m, "prev"), hrefOf(m, "next"), len(days))
		}
		if first := days[0].(map[string]any)["day"]; first != tc.month+"-01" {
			t.Errorf("%s starts on %v", tc.month, first)
		}
		if len(m.Entities) != tc.days || m.Entities[0].Href != "/days/"+tc.month+"-01" {
			t.Errorf("%s embeds %d day links", tc.month, len(m.Entities))
		}
	}

	may := props(must(c.Get(t.Context(), "/months/2031-05")))
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
		if _, err := c.Get(t.Context(), "/months/"+bad); !clientStatus(err, 422) {
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
	c, h := freshOn(t, fixedToday)
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
	day := must(c.Do(t.Context(), find(must(c.Get(t.Context(), "/")), "open-day"), map[string]string{"day": "2031-04-30"}))
	if day.Title != "2031-04-30" || day.Class[0] != "day" {
		t.Errorf("open-day through the client: %s %v", day.Title, day.Class)
	}
}

// TestTheCalendarHoldsEveryDatedThing (docs/plans/101-the-calendar-holds-every-dated-thing.md): a month gives each
// day what names it, written through the catalog as every client writes it, and the browser draws the cells, Day by
// day and the periods. The expected items are worked out by hand.
func TestTheCalendarHoldsEveryDatedThing(t *testing.T) {
	c, h := freshOn(t, fixedToday) // 2031-05-14
	ctx := t.Context()
	root := must(c.Get(ctx, "/"))
	do := func(e *api.Entity, action string, v map[string]string) *api.Entity {
		t.Helper()
		return must(c.Do(ctx, find(e, action), v))
	}

	day := do(must(c.Get(ctx, "/days/2031-05-14")), "capture", map[string]string{"text": "A walk by the lake.", "mood": "4"})
	do(root, "register-metric", map[string]string{"name": "weight", "unit": "kg"})
	do(must(c.Get(ctx, "/")), "record", map[string]string{"metric": "weight", "day": "2031-05-14", "value": "80"})

	do(root, "create-task", map[string]string{"label": "Pay rent", "due_day": "2031-05-14"})
	do(root, "create-task", map[string]string{"label": "Call the bank", "due_day": "2031-05-10"})
	old := do(root, "create-task", map[string]string{"label": "Old errand", "due_day": "2031-05-14"})
	do(old, "tombstone-task", nil)
	plants := do(root, "create-task", map[string]string{"label": "Water the plants", "repeat_unit": "week", "repeat_every": "1", "anchor_day": "2031-05-06"})
	settle := func(key, state string) {
		t.Helper()
		slot := must(c.Get(ctx, strings.SplitN(hrefOf(plants, "self"), "?", 2)[0]+"/occurrences/"+key))
		do(do(slot, "capture-occurrence", nil), "edit-occurrence", map[string]string{"state": state})
	}
	settle("2031-05-06", "done")
	settle("2031-05-13", "skipped")

	do(root, "create-person", map[string]string{"title": "Ana", "birth_day": "1990-05-14"})
	do(root, "create-person", map[string]string{"title": "Ion", "birth_day": "1930-03-01", "death_day": "2020-05-21"})
	for _, p := range [][3]string{
		{"Trip to Rome", "2031-05-20", "2031-05-24"}, // definite; it marks both its days
		{"Living in Berlin", "2018-09", ".."},        // definite, read as of today
		{"Spring", "2031-05", "2031-05"},             // possible on every day
		{"Study", "2031-05-02~", "2031-06"},          // incomparable; it marks its approximate start
		{"Old job", "2010", "2015"},                  // outside
	} {
		do(root, "create-period", map[string]string{"title": p[0], "start_boundary": p[1], "end_boundary": p[2]})
	}

	do(root, "create-page", map[string]string{"title": "Run"})
	do(root, "capture-session", map[string]string{"kind": "Run", "day": "2031-05-14", "start_local": "2031-05-14T07:30:00.000"})
	do(root, "create-place", map[string]string{"title": "Lake Park"})
	do(must(c.Get(ctx, hrefOf(day, "page"))), "link", map[string]string{"to": "Lake Park", "kind": "at"})
	do(root, "add-file", map[string]string{"title": "Scan.pdf", "sha256": strings.Repeat("a", 64), "mime": "application/pdf", "day": "2031-05-14"})
	for i := 1; i <= 21; i++ {
		do(root, "add-file", map[string]string{"title": fmt.Sprintf("Import %02d.pdf", i), "sha256": fmt.Sprintf("%064x", i), "mime": "application/pdf", "day": "2031-05-20"})
	}

	month := must(c.Get(ctx, "/months/2031-05"))
	got, extra := map[string][]string{}, map[string]string{}
	for _, d := range props(month)["days"].([]any) {
		m := d.(map[string]any)
		on := m["day"].(string)
		for _, it := range anyItems(m["items"]) {
			line := fmt.Sprintf("%v %v", it["kind"], it["title"])
			for _, k := range []string{"state", "years", "boundary", "qualifier", "at"} {
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
			case it["kind"] == "task" && e.Class[0] != "occurrence", it["kind"] == "session" && e.Class[0] != "session":
				t.Errorf("%s: %s is a %v", on, it["href"], e.Class)
			case it["kind"] != "task" && it["kind"] != "session" && e.Title != it["title"]:
				t.Errorf("%s: %s is %q, want %q", on, it["href"], e.Title, it["title"])
			}
		}
		if m["more_pages"] != nil || m["readings"] != nil {
			extra[on] = fmt.Sprintf("more %v readings %v", m["more_pages"], m["readings"])
		}
	}
	imports := []string{"task Water the plants state=open", "period Trip to Rome boundary=start"}
	for i := 1; i <= 20; i++ {
		imports = append(imports, fmt.Sprintf("file Import %02d.pdf", i))
	}
	want := map[string][]string{
		"2031-05-02": {"period Study boundary=start qualifier=~"},
		"2031-05-06": {"task Water the plants state=done"},
		"2031-05-10": {"task Call the bank state=open"},
		"2031-05-13": {"task Water the plants state=skipped"},
		"2031-05-14": {"task Pay rent state=open", "birthday Ana years=41", "session Run at=2031-05-14T07:30:00.000", "place Lake Park", "file Scan.pdf"},
		"2031-05-20": imports,
		"2031-05-21": {"death Ion years=11"},
		"2031-05-24": {"period Trip to Rome boundary=end"},
		"2031-05-27": {"task Water the plants state=open"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("the items of May:\n got %v\nwant %v", got, want)
	}
	// The mood is not counted with the readings; 20 files of 20 May are items, and the 21st is counted.
	if fmt.Sprint(extra) != "map[2031-05-14:more <nil> readings 1 2031-05-20:more 1 readings <nil>]" {
		t.Errorf("more pages and readings: %v", extra)
	}
	var periods []string
	for _, p := range props(month)["periods"].([]any) {
		m := p.(map[string]any)
		periods = append(periods, fmt.Sprintf("%v %v", m["title"], m["membership"]))
	}
	if got := strings.Join(periods, ", "); got != "Living in Berlin definite, Spring possible, Study incomparable, Trip to Rome definite" {
		t.Errorf("the periods of May: %s", got)
	}

	// The week of 20 May has the same days, and its occurrences are as before.
	y, w := time.Date(2031, 5, 20, 0, 0, 0, 0, time.UTC).ISOWeek()
	week := props(must(c.Get(ctx, fmt.Sprintf("/weeks/%04d-W%02d", y, w))))
	for _, d := range week["days"].([]any) {
		if m := d.(map[string]any); m["day"] == "2031-05-20" && (len(anyItems(m["items"])) != 22 || fmt.Sprint(m["more_pages"]) != "1") {
			t.Errorf("the week's 20 May: %d items, more %v", len(anyItems(m["items"])), m["more_pages"])
		}
	}
	var labels []string
	for _, o := range week["occurrences"].([]any) {
		labels = append(labels, fmt.Sprint(o.(map[string]any)["label"], " ", o.(map[string]any)["due_day"]))
	}
	if got := strings.Join(labels, ", "); got != "Water the plants 2031-05-20" {
		t.Errorf("the week's occurrences: %s", got)
	}

	body := browse(t, h, "/months/2031-05")
	for _, s := range []string{
		// a cell: three items, each with a square the colour of a task's state, then the rest counted, the mood and
		// the readings; a phone's squares
		`<span class="item open">Pay rent</span><span class="item">Ana <small>birthday</small></span><span class="item">Run <small>07:30</small></span></span>`,
		`<span class="marks" aria-hidden="true"><i class="open"></i><i></i><i></i><i></i></span><span class="more">and 2 more</span><span class="meta">mood 4 · 1 reading</span>`,
		`<span class="item overdue">Call the bank</span>`,
		`<span class="item done">Water the plants</span>`,
		`<span class="item">Study <small>starts</small></span>`,
		`<span class="more">and 20 more</span>`,
		// Day by day: a day a heading, each item a tag, a link to the thing and a note
		`<h3><a href="/days/2031-05-14">`,
		`<span class="tag overdue">overdue</span> <a href="/tasks/`,
		`<span class="tag">birthday</span> <a href="/pages/`,
		`>Ana</a> <small class="agenda-note">41 years</small>`,
		`>Ion</a> <small class="agenda-note">11 years ago</small>`,
		`>Study</a> <small class="agenda-note">approximate</small>`,
		`<span class="tag">session</span> <a href="/sessions/`,
		`<a href="/days/2031-05-20">and 1 more page</a>`,
		// Periods: the placed ones with their boundaries, the possible one tagged, the incomparable one counted
		`>Living in Berlin</a> <small class="agenda-note">2018-09 to ongoing</small>`,
		`>Spring</a> <span class="tag">possibly</span>`,
		`1 period cannot be placed in this month`,
	} {
		if !strings.Contains(body, s) {
			t.Errorf("the calendar lacks %s", s)
		}
	}
	if strings.Contains(body, "Old job") || strings.Contains(body, "Old errand") {
		t.Error("the calendar shows a period outside the month or a deleted task")
	}

	// A month with nothing says so: a fresh file's.
	_, none := freshOn(t, fixedToday)
	empty := browse(t, none, "/months/2031-08")
	for _, s := range []string{"No task, session, page, place, birthday or period has a day in August 2031.", "No period falls in August 2031."} {
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
