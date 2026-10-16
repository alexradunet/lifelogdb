package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"lifelog/internal/core"
)

// The shell is the frame of every page a browser gets (docs/plans/092-the-app-shell.md): the sidebar, with today's
// date and this month. It reads the day pages of this month, as GET /months/{month} gives them to every client, once
// a page, and only for a browser: a JSON answer never runs it.

// shellView is what the sidebar shows: today, and the weeks of this month with the days that have a day page.
type shellView struct {
	Today     string
	Zone      *time.Location // the zone of today: the words for a time use it
	Month     string         // "October 2026"
	MonthHref string
	Written   int
	Days      []any // the days of this month as the month resource gives them (day, page): the monthbox template draws them
}

// shell adds the month of today and its days that have a day page to sv, which has today. When the read fails, shell
// adds nothing and returns the error.
func (h *server) shell(ctx context.Context, sv *shellView) error {
	t, ok := core.ParseDay(sv.Today)
	if !ok {
		return fmt.Errorf("today %q is not a day", sv.Today)
	}
	first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1)
	entries, err := h.s.DayEntries(ctx, first.Format(time.DateOnly), last.Format(time.DateOnly), false, 31)
	if err != nil {
		return err
	}
	sv.Month, sv.MonthHref, sv.Written = first.Format(layoutMonth), monthHref(first), len(entries)
	for _, d := range writtenDays(entries, first, last) {
		sv.Days = append(sv.Days, map[string]any{"day": d.Day, "page": d.Page})
	}
	return nil
}

// write answers with the entity, as the free function write does; a browser gets it in the shell. The page is
// drawn even when the shell's read fails: the sidebar then has its sections and today's date but no count or
// month, and the read that the page is about has already answered on its own. A page drawn by the free write (the
// login, a refusal before the handler) has no shell, and so no sidebar: it shows nothing of the database.
func (h *server) write(w http.ResponseWriter, r *http.Request, status int, e *Entity) {
	if !wantsHTML(r) {
		writeEntity(w, r, status, e)
		return
	}
	h.inShell(w, r, status, e)
}

// inShell draws the entity as a page in the shell, for any client: the read of the shell may fail, as write says.
func (h *server) inShell(w http.ResponseWriter, r *http.Request, status int, e *Entity) {
	sv := &shellView{}
	if h.s != nil {
		now := h.s.Local() // one read of the clock, so today and the zone of the words agree
		sv.Today, sv.Zone = now.Format(time.DateOnly), now.Location()
		_ = h.shell(r.Context(), sv) // the page is drawn without the month when the read fails
	}
	renderHTML(w, r, status, e, sv)
}
