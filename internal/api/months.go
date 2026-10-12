package api

import (
	"net/http"
	"time"

	"lifelog/internal/core"
)

// A month is the calendar of the browser (docs/plans/091-the-browser-for-daily-use.md): each day of a month, whether
// it has a day page, the first line of that page and the day's mood. open-day goes to a day by its date.

// monthDay is one day of a month as the calendar shows it.
type monthDay struct {
	Day       string    `json:"day"`
	Page      bool      `json:"page"`
	FirstLine string    `json:"first_line,omitempty"`
	Mood      []float64 `json:"mood,omitempty"`
}

// monthOf parses a month, YYYY-MM; ok is false for anything else.
func monthOf(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01", s)
	return t, err == nil && t.Format("2006-01") == s
}

func monthHref(t time.Time) string { return "/months/" + t.Format("2006-01") }

// currentMonth answers with this month's address, as /days/today does with today's.
func (h *server) currentMonth(*http.Request) (*Entity, error) {
	t, _ := time.Parse(time.DateOnly, h.localDay())
	return nil, redirect{monthHref(t)}
}

func (h *server) month(r *http.Request) (*Entity, error) {
	ctx := r.Context()
	first, ok := monthOf(r.PathValue("month"))
	if !ok {
		return nil, &core.Error{Status: 422, Msg: "a month is YYYY-MM"}
	}
	last := first.AddDate(0, 1, -1)
	from, through := first.Format(time.DateOnly), last.Format(time.DateOnly)
	entries, err := h.s.DayEntries(ctx, from, through, false, 31)
	if err != nil {
		return nil, err
	}
	moods, err := h.s.Moods(ctx, from, through)
	if err != nil {
		return nil, err
	}
	pages := map[string]core.DayEntry{}
	for _, d := range entries {
		pages[d.Day] = d
	}
	name := first.Format("January 2006")
	prev, next := first.AddDate(0, -1, 0), first.AddDate(0, 1, 0)
	e := &Entity{Class: []string{"month"}, Title: name,
		Links: []Link{link("self", monthHref(first), name), link("prev", monthHref(prev), prev.Format("January 2006")),
			link("next", monthHref(next), next.Format("January 2006")), link("days", "/days", "Days"), link("index", "/", "Home")},
		Actions: []Action{action("open-day", nil, nil)}}
	days := []monthDay{}
	for t := first; !t.After(last); t = t.AddDate(0, 0, 1) {
		day := t.Format(time.DateOnly)
		p, written := pages[day]
		days = append(days, monthDay{Day: day, Page: written, FirstLine: p.FirstLine, Mood: moods[day]})
		e.Entities = append(e.Entities, link("day", dayHref(day), day))
	}
	e.Properties = map[string]any{"month": first.Format("2006-01"), "today": h.localDay(), "days": days}
	return e, nil
}

// openDay answers with the address of the day the form names.
func (h *server) openDay(r *http.Request) (*Entity, error) {
	day := r.URL.Query().Get("day")
	if !core.IsDay(day) {
		return nil, &core.Error{Status: 422, Msg: "day must be a day, YYYY-MM-DD"}
	}
	return nil, redirect{dayHref(day)}
}

// weeks puts the days of a month into the rows of a calendar, Monday first: nil fills a cell before the first day and
// after the last.
func weeks(list any) [][]any {
	days, _ := list.([]any)
	if len(days) == 0 {
		return nil
	}
	m, _ := days[0].(map[string]any)
	first, _ := m["day"].(string)
	t, err := time.Parse(time.DateOnly, first)
	if err != nil {
		return nil
	}
	var out [][]any
	row := make([]any, (int(t.Weekday())+6)%7)
	for _, d := range days {
		if row = append(row, d); len(row) == 7 {
			out, row = append(out, row), nil
		}
	}
	if len(row) > 0 {
		out = append(out, append(row, make([]any, 7-len(row))...))
	}
	return out
}
