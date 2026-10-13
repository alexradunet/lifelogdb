package api

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"lifelog/internal/core"
)

// A month is the calendar of the browser (docs/plans/091-the-browser-for-daily-use.md): each day of a month, whether
// it has a day page, the first line of that page and the day's mood, and every other thing that names the day
// (docs/plans/101-the-calendar-holds-every-dated-thing.md). open-day goes to a day by its date.

// monthDay is one day of a month as the calendar shows it.
type monthDay struct {
	Day       string         `json:"day"`
	Page      bool           `json:"page"`
	FirstLine string         `json:"first_line,omitempty"`
	Mood      []float64      `json:"mood,omitempty"`
	Items     []calendarItem `json:"items,omitempty"`
	MorePages int            `json:"more_pages,omitempty"`
	Readings  int            `json:"readings,omitempty"`
}

// calendarItem is a thing that names a day besides its day page: its kind, its title and the address of its own
// resource. State is a task's, Years an anniversary's, Boundary and Qualifier a period's, At a session's start.
type calendarItem struct {
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Href      string `json:"href"`
	State     string `json:"state,omitempty"`
	Years     int    `json:"years,omitempty"`
	Boundary  string `json:"boundary,omitempty"`
	Qualifier string `json:"qualifier,omitempty"`
	At        string `json:"at,omitempty"`
}

// dayPages is how many pages and files of a day the calendar lists; more_pages counts the rest, and the day lists
// them all. An import can write thousands of pages on one day.
const dayPages = 20

// monthOf parses a month, YYYY-MM; ok is false for anything else.
func monthOf(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01", s)
	return t, err == nil && t.Format("2006-01") == s
}

func monthHref(t time.Time) string { return "/months/" + t.Format("2006-01") }

// currentMonth answers with this month's address, as /days/today does with today's.
func (h *server) currentMonth(*http.Request) (*Entity, error) {
	t, _ := dayOf(h.localDay())
	return nil, redirect{monthHref(t)}
}

func (h *server) month(r *http.Request) (*Entity, error) {
	ctx := r.Context()
	first, ok := monthOf(r.PathValue("month"))
	if !ok {
		return nil, core.Invalid("a month is YYYY-MM")
	}
	last := first.AddDate(0, 1, -1)
	from, through, today := first.Format(time.DateOnly), last.Format(time.DateOnly), h.localDay()
	cal, err := h.calendarOf(ctx, from, through)
	if err != nil {
		return nil, err
	}
	// The periods a day of the month can fall in. An ongoing end is read as of today, which the month gives: the
	// contract asks for an explicit day (docs/contract/period-boundaries.md).
	periods := []core.LifePeriod{}
	for _, p := range cal.periods {
		m, err := core.PeriodMembershipIn(p.Start, p.End, from, through, today)
		if err != nil {
			return nil, err
		}
		if m != "outside" {
			p.Membership = m
			periods = append(periods, p)
		}
	}
	name := first.Format(layoutMonth)
	prev, next := first.AddDate(0, -1, 0), first.AddDate(0, 1, 0)
	e := &Entity{Class: []string{"month"}, Title: name,
		Links: []Link{link("self", monthHref(first), name), link("prev", monthHref(prev), prev.Format(layoutMonth)),
			link("next", monthHref(next), next.Format(layoutMonth)), link("days", "/days", "Days"), link("index", "/", "Home")},
		Actions: []Action{action("open-day", nil, nil)}}
	for _, d := range cal.days {
		e.Entities = append(e.Entities, link("day", dayHref(d.Day), d.Day))
	}
	e.Properties = map[string]any{"month": first.Format("2006-01"), "today": today, "days": cal.days, "periods": periods}
	return e, nil
}

// openDay answers with the address of the day the form names.
func (h *server) openDay(r *http.Request) (*Entity, error) {
	day := r.URL.Query().Get("day")
	if !core.IsDay(day) {
		return nil, core.Invalid("day must be a day, YYYY-MM-DD")
	}
	return nil, redirect{dayHref(day)}
}

// calendar is what a window of days holds: its days as a calendar shows them, the occurrences due in it, and every
// live period (a boundary of one marks its day).
type calendar struct {
	days        []monthDay
	occurrences []occurrenceView
	periods     []core.LifePeriod
}

// calendarOf reads the days from from through through: whether each has a day page, its first line, its mood, its
// readings other than the Mood's (the mood says those) and its items. A month and a week read it; a window is at
// most 31 days.
func (h *server) calendarOf(ctx context.Context, from, through string) (*calendar, error) {
	entries, err := h.s.DayEntries(ctx, from, through, false, 31)
	if err != nil {
		return nil, err
	}
	moods, err := h.s.Moods(ctx, from, through)
	if err != nil {
		return nil, err
	}
	readings, err := h.s.ReadingsByDay(ctx, from, through)
	if err != nil {
		return nil, err
	}
	occurrences, err := h.dueIn(ctx, from, through, "all", false)
	if err != nil {
		return nil, err
	}
	anniversaries, err := h.s.Anniversaries(ctx, from, through)
	if err != nil {
		return nil, err
	}
	periods, err := h.s.LifePeriods(ctx, "", "", false)
	if err != nil {
		return nil, err
	}
	dated, err := h.s.DatedIn(ctx, from, through, dayPages)
	if err != nil {
		return nil, err
	}

	// The items of each day, in the order of plan 101: the tasks (open, then done, then skipped), the anniversaries,
	// the period boundaries, then the sessions, the places and the pages as DatedIn orders them.
	items, more := map[string][]calendarItem{}, map[string]int{}
	add := func(day string, it calendarItem) { items[day] = append(items[day], it) }
	rank := map[string]int{core.StateOpen: 0, core.StateDone: 1, core.StateSkipped: 2}
	tasks := slices.Clone(occurrences)
	slices.SortStableFunc(tasks, func(a, b occurrenceView) int {
		return cmp.Or(cmp.Compare(a.DueDay, b.DueDay), cmp.Compare(rank[a.State], rank[b.State]))
	})
	for _, o := range tasks {
		add(o.DueDay, calendarItem{Kind: "task", Title: o.Label, Href: occurrenceHref(o.TaskID, o.OccurrenceKey), State: o.State})
	}
	for _, a := range anniversaries {
		add(a.Day, calendarItem{Kind: a.Kind, Title: a.Title, Href: pageHref(a.ID), Years: a.Years})
	}
	for _, p := range periods {
		for _, b := range []struct {
			name  string
			value *string
		}{{"start", p.Start}, {"end", p.End}} {
			if day, q, ok := boundaryDay(b.value); ok && from <= day && day <= through {
				add(day, calendarItem{Kind: "period", Title: p.Title, Href: pageHref(p.ID), Boundary: b.name, Qualifier: q})
			}
		}
	}
	for _, d := range dated {
		it := calendarItem{Kind: d.Kind, Title: d.Title, Href: pageHref(d.ID)}
		if d.Kind == "session" {
			it.Href, it.At = sessionHref(d.ID), d.At
		}
		add(d.Day, it)
		if d.Pages > dayPages {
			more[d.Day] = d.Pages - dayPages
		}
	}

	pages := map[string]core.DayEntry{}
	for _, d := range entries {
		pages[d.Day] = d
	}
	days := []monthDay{}
	first, _ := dayOf(from)
	last, _ := dayOf(through)
	for t := first; !t.After(last); t = t.AddDate(0, 0, 1) {
		day := t.Format(time.DateOnly)
		p, written := pages[day]
		days = append(days, monthDay{Day: day, Page: written, FirstLine: p.FirstLine, Mood: moods[day],
			Items: items[day], MorePages: more[day], Readings: readings[day] - len(moods[day])})
	}
	return &calendar{days: days, occurrences: occurrences, periods: periods}, nil
}

// boundaryDay is the day a period boundary names, and its qualifier (?, ~ or %, "" for none); ok is false for a
// boundary that names a year or a month, an unknown one and an ongoing end.
func boundaryDay(b *string) (day, qualifier string, ok bool) {
	if b == nil {
		return "", "", false
	}
	day = strings.TrimRight(*b, "?~%")
	if day != *b {
		qualifier = (*b)[len(day):]
	}
	return day, qualifier, core.IsDay(day)
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
	t, ok := dayOf(first)
	if !ok {
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
