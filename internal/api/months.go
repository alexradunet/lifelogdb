package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"lifelog/internal/api/siren"
	"lifelog/internal/api/view"
	"lifelog/internal/core"
	"lifelog/internal/text"
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
	Picture   string         `json:"picture,omitempty"` // the address of the day page's cover
}

// calendarItem is a thing that names a day besides its day page: its kind, its title and the address of its own
// resource. Years is an anniversary's.
type calendarItem struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Href  string `json:"href"`
	Years int    `json:"years,omitempty"`
}

// dayPages is how many pages and files of a day the calendar lists; more_pages counts the rest, and the day lists
// them all. An import can write thousands of pages on one day.
const dayPages = 20

// layoutMonth is the layout of a month in words.
const layoutMonth = "January 2006"

// currentMonth answers with this month's address, as /days/today does with today's.
func (h *server) currentMonth(*http.Request) (*siren.Entity, error) {
	t, _ := text.ParseDay(h.s.Today())
	return nil, redirect{siren.MonthHref(t)}
}

func (h *server) month(r *http.Request) (*siren.Entity, error) {
	ctx := r.Context()
	first, ok := siren.MonthOf(r.PathValue("month"))
	if !ok {
		return nil, core.Invalid("a month is YYYY-MM")
	}
	last := first.AddDate(0, 1, -1)
	from, through, today := first.Format(time.DateOnly), last.Format(time.DateOnly), h.s.Today()
	cal, err := h.calendarOf(ctx, from, through)
	if err != nil {
		return nil, err
	}
	name := first.Format(layoutMonth)
	prev, next := first.AddDate(0, -1, 0), first.AddDate(0, 1, 0)
	e := &siren.Entity{Class: []string{"month"}, Title: name, Links: []siren.Link{link("self", siren.MonthHref(first), name)},
		Actions: []siren.Action{action("open-day", nil, nil)}}
	// A month has a page when its first day is a day.
	e.Links = append(e.Links, neighbourLinks(
		neighbour{prev, siren.MonthHref(prev), prev.Format(layoutMonth)},
		neighbour{next, siren.MonthHref(next), next.Format(layoutMonth)})...)
	e.Links = append(e.Links, zoomLinks(anchorIn(today, first, last), streamAfter(last))...)
	e.Links = append(e.Links, link("index", "/", "Home"))
	for _, d := range cal.days {
		e.Entities = append(e.Entities, link("day", siren.DayHref(d.Day), d.Day))
	}
	e.Properties = map[string]any{"month": first.Format("2006-01"), "today": today, "days": cal.days}
	return e, nil
}

// zoomLinks are the levels of the Calendar's zoom (docs/plans/130-the-explore-pass.md, part 3): the stream (days, at
// the address the caller gives), and the week, the month and the year of the anchor. A level whose page the anchor's
// year cannot have is left out.
func zoomLinks(anchor time.Time, days string) []siren.Link {
	out := []siren.Link{link("days", days, "Days")}
	if _, ok := weekOf(siren.WeekKey(anchor)); ok {
		out = append(out, link("week", siren.WeekHref(anchor), "Week"))
	}
	out = append(out, link("month", siren.MonthHref(anchor), anchor.Format(layoutMonth)))
	if siren.IsYear(anchor.Year()) {
		out = append(out, link("year", siren.YearHref(anchor.Year()), strconv.Itoa(anchor.Year())))
	}
	return out
}

// anchorIn is the day a period's zoom is anchored on: today when it is inside the period, else its first day.
func anchorIn(today string, first, last time.Time) time.Time {
	if t, ok := text.ParseDay(today); ok && !t.Before(first) && !t.After(last) {
		return t
	}
	return first
}

// streamAfter is the page of the stream that a period opens on: the days before the day after its last day, or the
// top of the stream when that day is not one (after 9999-12-31).
func streamAfter(last time.Time) string {
	if next := last.AddDate(0, 0, 1).Format(time.DateOnly); text.IsDay(next) {
		return streamHref(next, streamLimit, 0)
	}
	return "/days"
}

// openDay answers with the address of the day the form names, or of today when it names none or the word "today".
func (h *server) openDay(r *http.Request, act spec, _ string) (*siren.Entity, error) {
	v, err := fieldsOf(r, act)
	if err != nil {
		return nil, err
	}
	day := h.dayNamed(v.Get("day"))
	if day == "" { // the field is optional: no day is today
		day = h.s.Today()
	}
	if !text.IsDay(day) {
		return nil, core.Invalid("day must be a day, YYYY-MM-DD")
	}
	return nil, redirect{siren.DayHref(day)}
}

// calendar is what a window of days holds: its days as a calendar shows them.
type calendar struct {
	days []monthDay
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
	anniversaries, err := h.s.Anniversaries(ctx, from, through)
	if err != nil {
		return nil, err
	}
	dated, err := h.s.DatedIn(ctx, from, through, dayPages)
	if err != nil {
		return nil, err
	}

	// The items of each day, in the order of plan 101: the anniversaries, then the places and the pages as DatedIn
	// orders them.
	items, more := map[string][]calendarItem{}, map[string]int{}
	add := func(day string, it calendarItem) { items[day] = append(items[day], it) }
	for _, a := range anniversaries {
		add(a.Day, calendarItem{Kind: a.Kind, Title: a.Title, Href: siren.PageHref(a.ID), Years: a.Years})
	}
	for _, d := range dated {
		add(d.Day, calendarItem{Kind: d.Kind, Title: d.Title, Href: siren.PageHref(d.ID)})
		if d.Pages > dayPages {
			more[d.Day] = d.Pages - dayPages
		}
	}

	firstLines, pictures := map[string]string{}, map[string]string{}
	for _, d := range entries {
		firstLines[d.Day], pictures[d.Day] = d.FirstLine, pictureHref(d.Picture)
	}
	days := []monthDay{}
	first, _ := text.ParseDay(from)
	last, _ := text.ParseDay(through)
	for _, w := range writtenDays(entries, first, last) {
		day := w.Day
		days = append(days, monthDay{Day: day, Page: w.Page, FirstLine: firstLines[day], Mood: moods[day],
			Items: items[day], MorePages: more[day], Readings: readings[day] - len(moods[day]), Picture: pictures[day]})
	}
	return &calendar{days: days}, nil
}

// writtenDay is a day and whether it has a day page: the day is written.
type writtenDay struct {
	Day  string `json:"day"`
	Page bool   `json:"page"`
}

// writtenDays are the days from first through last, each with whether one of entries is its day page. The
// calendar, the months of a year and the month of the sidebar draw them.
func writtenDays(entries []core.DayEntry, first, last time.Time) []writtenDay {
	written := make(map[string]bool, len(entries))
	for _, d := range entries {
		written[d.Day] = true
	}
	days := []writtenDay{}
	for t := first; !t.After(last); t = t.AddDate(0, 0, 1) {
		day := t.Format(time.DateOnly)
		days = append(days, writtenDay{Day: day, Page: written[day]})
	}
	return days
}

// shellOf is the sidebar's read of the store s: today, then the month of today and its days that have a day page.
// The page is drawn even when the read of the month fails: the sidebar then has its sections and today's date but no
// count or month, and the read that the page is about has already answered on its own.
func shellOf(ctx context.Context, s *core.Store) *view.Shell {
	now := s.Local() // one read of the clock, so today and the zone of the words agree
	sv := &view.Shell{Today: now.Format(time.DateOnly), Zone: now.Location()}
	t, ok := text.ParseDay(sv.Today)
	if !ok { // a clock beyond the years that a day can name: the sidebar has no month
		return sv
	}
	first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1)
	entries, err := s.DayEntries(ctx, first.Format(time.DateOnly), last.Format(time.DateOnly), false, 31)
	if err != nil { // the page is drawn without the month
		return sv
	}
	sv.Month, sv.MonthHref, sv.Written = first.Format(layoutMonth), siren.MonthHref(first), len(entries)
	for _, d := range writtenDays(entries, first, last) {
		sv.Days = append(sv.Days, map[string]any{"day": d.Day, "page": d.Page})
	}
	return sv
}
