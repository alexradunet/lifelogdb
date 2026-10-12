package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
		`← April 2031`, `June 2031 →`, `action="/days/go"`} {
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
