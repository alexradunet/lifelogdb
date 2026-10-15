package api

// The days: the root (today at a glance), the list of days, a day and its view, and capture.

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"lifelog/internal/core"
)

// recentDays is how many of the newest day pages the root shows.
const recentDays = 5

// recentDay is a day page as the root lists it: the day, the first line of its body, and its mood.
type recentDay struct {
	core.DayEntry
	Mood []float64 `json:"mood,omitempty"`
}

// onThisDayMax is how many earlier years a day lists.
const onThisDayMax = 10

// withMoods adds to each day entry the day's moods.
func (h *server) withMoods(ctx context.Context, entries []core.DayEntry) ([]recentDay, error) {
	out := []recentDay{}
	if len(entries) == 0 {
		return out, nil
	}
	lo, hi := entries[0].Day, entries[0].Day
	for _, d := range entries {
		lo, hi = min(lo, d.Day), max(hi, d.Day)
	}
	moods, err := h.s.Moods(ctx, lo, hi)
	if err != nil {
		return nil, err
	}
	for _, d := range entries {
		out = append(out, recentDay{DayEntry: d, Mood: moods[d.Day]})
	}
	return out, nil
}

// onThisDay are the day pages of earlier years on the month and day of a day, the newest first.
func (h *server) onThisDay(ctx context.Context, day string) ([]recentDay, error) {
	entries, err := h.s.OnThisDay(ctx, day, onThisDayMax)
	if err != nil {
		return nil, err
	}
	return h.withMoods(ctx, entries)
}

// The root is today at a glance (docs/plans/091-the-browser-for-daily-use.md): what is due today and is overdue,
// today's habits, the newest day pages, and every action that starts something.
func (h *server) root(r *http.Request) (*Entity, error) {
	ctx := r.Context()
	today := h.localDay()
	due, err := h.overdueAndDue(ctx, today)
	if err != nil {
		return nil, err
	}
	hs, states, err := h.habitsOn(ctx, today)
	if err != nil {
		return nil, err
	}
	entries, err := h.s.DayEntries(ctx, "0000-01-01", today, true, recentDays)
	if err != nil {
		return nil, err
	}
	recent, err := h.withMoods(ctx, entries)
	if err != nil {
		return nil, err
	}
	onThisDay, err := h.onThisDay(ctx, today)
	if err != nil {
		return nil, err
	}
	record, err := h.recordAction(ctx, "", today)
	if err != nil {
		return nil, err
	}
	actions := []Action{
		action("capture", map[string]string{"day": today}, nil), action("search", nil, nil), action("find", nil, nil), action("open-day", nil, nil), action("deadlines", nil, nil), action("create-task", nil, nil),
		action("create-page", nil, nil), action("create-person", nil, nil), action("create-place", nil, nil), action("add-file", nil, nil),
		record, action("register-metric", nil, nil), action("query", nil, nil), action("snapshot", nil, nil), action("ask", nil, nil)}
	return &Entity{
		Class: []string{"root"}, Title: "Lifelog",
		Properties: map[string]any{"today": today, "due": due, "habits": hs, "recent": recent, "on_this_day": onThisDay},
		Links: []Link{link("self", "/", "Lifelog"), link("today", dayHref(today), "Today"), link("days", "/days", "Days"),
			link("categories", "/categories", "Categories"), link("people", "/people", "People"), link("places", "/places", "Places"), link("media", "/media", "Media"), link("files", "/files", "Files"), link("metrics", "/metrics", "Metrics"),
			link("habits", "/habits", "Habits"), link("tasks", "/tasks", "Tasks"), link("deadlines", "/deadlines", "Deadlines"),
			link("ghosts", "/ghosts", "Ghost pages"), link("actions", "/actions", "All actions"), link("calendar", "/months/current", "Calendar"),
			link("week", "/weeks/current", "This week"), link("year", "/years/current", "This year")},
		Actions: append(append(actions, checkIns(states, today)...), outcomesOf(due, today)...),
	}, nil
}

// overdueAndDue are the open occurrences due before today in the deadlines' default window, then the open ones due
// today.
func (h *server) overdueAndDue(ctx context.Context, today string) ([]occurrenceView, error) {
	open, _, err := h.dueToday(ctx, today)
	return open, err
}

// dueToday reads the deadlines from the default window back through today once, and splits them: the open ones (the
// overdue first, then today's, as the deadlines order them), and today's settled ones.
func (h *server) dueToday(ctx context.Context, today string) (open, settled []occurrenceView, err error) {
	t, _ := dayOf(today)
	all, err := h.dueIn(ctx, t.AddDate(0, 0, -windowBack).Format(time.DateOnly), today, "all", false)
	if err != nil {
		return nil, nil, err
	}
	open = []occurrenceView{}
	for _, v := range all {
		switch {
		case v.State == core.StateOpen:
			open = append(open, v)
		case v.DueDay == today:
			settled = append(settled, v)
		}
	}
	return open, settled, nil
}

func (h *server) today(*http.Request) (*Entity, error) { return nil, redirect{dayHref(h.localDay())} }

func (h *server) days(r *http.Request) (*Entity, error) {
	limit, offset, err := pageOf(r, 60)
	if err != nil {
		return nil, err
	}
	days, err := h.s.RecentDays(r.Context(), limit+1, offset)
	if err != nil {
		return nil, err
	}
	e := &Entity{Class: []string{"days"}, Title: "Days", Links: []Link{link("self", "/days", "Days"), link("index", "/", "Home")}}
	days = paged(e, days, "/days", nil, limit, offset)
	for _, d := range days {
		e.Entities = append(e.Entities, link("item", dayHref(d.Title), d.Title))
	}
	return e, nil
}

// dayView is a day as the API shows it: the cookbook's day view, and beside it what is due that day (on today, the
// open work of the days before too), the day's mood, its habits and the day the server read as today
// (docs/plans/091-the-browser-for-daily-use.md).
type dayView struct {
	core.Day
	Due    []occurrenceView `json:"due"`
	Mood   []float64        `json:"mood"`
	Habits []habitView      `json:"habits"`
	Trends []trend          `json:"trends"`
	// OnThisDay are the day pages of earlier years on this month and day.
	OnThisDay []recentDay `json:"on_this_day"`
	Today     string      `json:"today"`
}

func (h *server) day(r *http.Request) (*Entity, error) {
	return h.dayEntity(r.Context(), r.PathValue("day"))
}

// dayEntity is the resource /days/{day} serves.
func (h *server) dayEntity(ctx context.Context, day string) (*Entity, error) {
	d, err := h.s.Day(ctx, day)
	if err != nil {
		return nil, err
	}
	today := h.localDay()
	var due []occurrenceView
	if day == today { // the open work of the days before, then all of today's: one read
		var settled []occurrenceView
		due, settled, err = h.dueToday(ctx, today)
		due = append(due, settled...)
	} else {
		due, err = h.dueIn(ctx, day, day, "all", false)
	}
	if err != nil {
		return nil, err
	}
	moods, err := h.s.Moods(ctx, day, day)
	if err != nil {
		return nil, err
	}
	hs, states, err := h.habitsOn(ctx, day)
	if err != nil {
		return nil, err
	}
	trends, err := h.trends(ctx, day, d.Readings, hs)
	if err != nil {
		return nil, err
	}
	onThisDay, err := h.onThisDay(ctx, day)
	if err != nil {
		return nil, err
	}
	mood := moods[day]
	if mood == nil {
		mood = []float64{}
	}
	record, err := h.recordAction(ctx, "", day)
	if err != nil {
		return nil, err
	}
	t, _ := dayOf(day)
	e := &Entity{
		Class: []string{"day"}, Title: day, Properties: dayView{Day: *d, Due: due, Mood: mood, Habits: hs, Trends: trends, OnThisDay: onThisDay, Today: today},
		Links: []Link{link("self", dayHref(day), day), link("prev", dayHref(t.AddDate(0, 0, -1).Format(time.DateOnly)), "Previous day"),
			link("next", dayHref(t.AddDate(0, 0, 1).Format(time.DateOnly)), "Next day"), link("metrics", "/metrics", "Metrics"), link("index", "/", "Home"),
			link("month", monthHref(t), t.Format(layoutMonth))},
		Actions: []Action{action("capture", map[string]string{"day": day}, nil), record, action("open-day", nil, nil)},
	}
	if d.PageID != 0 {
		e.Links = append(e.Links, link("page", pageHref(d.PageID), "The day page"))
		p, err := h.s.PageByID(ctx, d.PageID)
		if err != nil {
			return nil, err
		}
		e.Actions = append(e.Actions, saveBodyAction(d.PageID, p.Body, p.Version))
	}
	e.Actions = append(append(e.Actions, checkIns(states, day)...), outcomesOf(due, today)...)
	for _, m := range d.Readings {
		e.Entities = append(e.Entities, link("reading", measurementHref(m.ID), fmt.Sprintf("%s %g", m.Metric, m.Value)))
	}
	return e, nil
}

func (h *server) capture(r *http.Request, src string) (*Entity, error) {
	v, note, err := captureForm(r)
	if err != nil {
		return nil, err
	}
	mood, err := number(v, "mood")
	if err != nil {
		return nil, err
	}
	day := r.PathValue("day")
	if day == "today" { // the share target of the manifest cannot know the day
		day = h.localDay()
	}
	entry, err := h.entryOf(r.Context(), v.Get("text"), note)
	if err != nil {
		return nil, err
	}
	_, sync, err := h.s.Capture(r.Context(), src, day, entry, mood)
	if err != nil {
		return nil, err
	}
	return withResult(h.dayEntity(r.Context(), day))(sync)
}
