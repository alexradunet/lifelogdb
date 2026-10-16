package api

import (
	"context"
	"errors"
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

// shell reads the sidebar's view.
func (h *server) shell(ctx context.Context) (*shellView, error) {
	if h.s == nil {
		return nil, errors.New("no store to read the shell from")
	}
	today := h.s.Today()
	t, ok := core.ParseDay(today)
	if !ok {
		return nil, fmt.Errorf("today %q is not a day", today)
	}
	first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1)
	entries, err := h.s.DayEntries(ctx, first.Format(time.DateOnly), last.Format(time.DateOnly), false, 31)
	if err != nil {
		return nil, err
	}
	written := map[string]bool{}
	for _, d := range entries {
		written[d.Day] = true
	}
	sv := &shellView{Today: today, Zone: h.s.Now().Location(), Month: first.Format(layoutMonth), MonthHref: monthHref(first), Written: len(entries)}
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		day := d.Format(time.DateOnly)
		sv.Days = append(sv.Days, map[string]any{"day": day, "page": written[day]})
	}
	return sv, nil
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
	sv, err := h.shell(r.Context())
	if err != nil {
		sv = &shellView{}
		if h.s != nil {
			sv.Today, sv.Zone = h.s.Today(), h.s.Now().Location()
		}
	}
	renderHTML(w, r, status, e, sv)
}
