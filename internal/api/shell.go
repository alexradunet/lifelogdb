package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// The shell is the frame of every page a browser gets (docs/plans/092-the-app-shell.md): the sidebar, with today's
// date, the count of what is due and this month. It reads what every client gets from the root's due and from
// GET /months/{month}, once a page, and only for a browser: a JSON answer never runs it.

// shellView is what the sidebar shows: today, the open occurrences overdue or due today, and the weeks of this
// month with the days that have a day page.
type shellView struct {
	Today     string
	Zone      *time.Location // the zone of today: the words for a time use it
	Month     string         // "October 2026"
	MonthHref string
	Due       int
	Written   int
	Days      []any // the days of this month as the month resource gives them (day, page): the monthbox template draws them
}

// shell reads the sidebar's view. due is the count of what is open, overdue or due today when the page already read
// it (the root carries it), nil when the shell reads it itself.
func (h *server) shell(ctx context.Context, due *int) (*shellView, error) {
	if h.s == nil || h.localDay == nil {
		return nil, errors.New("no store or clock to read the shell from")
	}
	today := h.localDay()
	t, ok := dayOf(today)
	if !ok {
		return nil, fmt.Errorf("today %q is not a day", today)
	}
	count := 0
	if due != nil {
		count = *due
	} else {
		open, err := h.overdueAndDue(ctx, today)
		if err != nil {
			return nil, err
		}
		count = len(open)
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
	sv := &shellView{Today: today, Zone: h.zone, Month: first.Format(layoutMonth), MonthHref: monthHref(first), Due: count, Written: len(entries)}
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
	var due *int // the root already read what is due: the shell does not read it again
	if len(e.Class) > 0 && e.Class[0] == "root" {
		if props, ok := e.Properties.(map[string]any); ok {
			if open, ok := props["due"].([]occurrenceView); ok {
				n := len(open)
				due = &n
			}
		}
	}
	sv, err := h.shell(r.Context(), due)
	if err != nil {
		sv = &shellView{Zone: h.zone}
		if h.localDay != nil {
			sv.Today = h.localDay()
		}
	}
	renderHTML(w, r, status, e, sv)
}
