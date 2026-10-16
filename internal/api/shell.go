package api

import (
	"context"
	"time"

	"lifelog/internal/core"
)

// The shell is the frame of every page a browser gets (docs/plans/092-the-app-shell.md): the sidebar, with today's
// date and this month. It reads the day pages of this month, as GET /months/{month} gives them to every client, once
// a page, and only for a page (answer): a JSON answer never runs it.

// shellView is what the sidebar shows: today, and the weeks of this month with the days that have a day page.
type shellView struct {
	Today     string
	Zone      *time.Location // the zone of today: the words for a time use it
	Month     string         // "October 2026"
	MonthHref string
	Written   int
	Days      []any // the days of this month as the month resource gives them (day, page): the monthbox template draws them
}

// shellOf is the sidebar's read of the store s: today, then the month of today and its days that have a day page.
// The page is drawn even when the read of the month fails: the sidebar then has its sections and today's date but no
// count or month, and the read that the page is about has already answered on its own.
func shellOf(ctx context.Context, s *core.Store) *shellView {
	now := s.Local() // one read of the clock, so today and the zone of the words agree
	sv := &shellView{Today: now.Format(time.DateOnly), Zone: now.Location()}
	t, ok := core.ParseDay(sv.Today)
	if !ok { // a clock beyond the years that a day can name: the sidebar has no month
		return sv
	}
	first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1)
	entries, err := s.DayEntries(ctx, first.Format(time.DateOnly), last.Format(time.DateOnly), false, 31)
	if err != nil { // the page is drawn without the month
		return sv
	}
	sv.Month, sv.MonthHref, sv.Written = first.Format(layoutMonth), monthHref(first), len(entries)
	for _, d := range writtenDays(entries, first, last) {
		sv.Days = append(sv.Days, map[string]any{"day": d.Day, "page": d.Page})
	}
	return sv
}
