package api

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"net/http"
	"strings"
	"time"
)

// The shell is the frame of every page a browser gets (docs/plans/092-the-app-shell.md): the sidebar, with today's
// date, the count of what is due and this month. It reads what every client gets from the root's due and from
// GET /months/{month}, once a page, and only for a browser: a JSON answer never runs it.

// shellView is what the sidebar shows: today, the open occurrences overdue or due today, and the weeks of this
// month with the days that have a day page.
type shellView struct {
	Today     string
	Month     string // "October 2026"
	MonthHref string
	Due       int
	Written   int
	Weeks     [][]*shellDay // Monday first; nil fills a cell before the first day and after the last
}

// shellDay is one day of the sidebar's month.
type shellDay struct {
	Day  string
	N    int
	Page bool
}

func (h *server) shell(ctx context.Context) (*shellView, error) {
	if h.s == nil || h.localDay == nil {
		return nil, errors.New("no store or clock to read the shell from")
	}
	today := h.localDay()
	t, err := time.Parse(time.DateOnly, today)
	if err != nil {
		return nil, err
	}
	due, err := h.overdueAndDue(ctx, today)
	if err != nil {
		return nil, err
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
	sv := &shellView{Today: today, Month: first.Format("January 2006"), MonthHref: monthHref(first), Due: len(due), Written: len(entries)}
	row := make([]*shellDay, (int(first.Weekday())+6)%7)
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		day := d.Format(time.DateOnly)
		if row = append(row, &shellDay{Day: day, N: d.Day(), Page: written[day]}); len(row) == 7 {
			sv.Weeks, row = append(sv.Weeks, row), nil
		}
	}
	if len(row) > 0 {
		sv.Weeks = append(sv.Weeks, append(row, make([]*shellDay, 7-len(row))...))
	}
	return sv, nil
}

// write answers with the entity, as the free function write does; a browser gets it in the shell. The page is
// drawn even when the shell's read fails: the sidebar then has its sections and today's date but no count or
// month, and the read that the page is about has already answered on its own. A page drawn by the free write (the
// login, a refusal before the handler) has no shell, and so no sidebar: it shows nothing of the database.
func (h *server) write(w http.ResponseWriter, r *http.Request, status int, e *Entity) {
	if !wantsHTML(r) {
		write(w, r, status, e)
		return
	}
	sv, err := h.shell(r.Context())
	if err != nil {
		sv = &shellView{}
		if h.localDay != nil {
			sv.Today = h.localDay()
		}
	}
	renderHTML(w, r, status, e, sv)
}

// fontTypes are the files the font route serves, by their extension: the faces and their licences.
var fontTypes = map[string]string{"woff2": "font/woff2", "txt": "text/plain; charset=utf-8"}

// fonts is GET /assets/fonts/{file}: an embedded face or licence, by its exact name (html/fonts/README.md). It is
// presentation, like /render, and not in the catalog. A file never changes under its name, so a browser keeps it
// for a year.
func fonts(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	typ := fontTypes[name[strings.LastIndex(name, ".")+1:]]
	if typ == "" || strings.ContainsAny(name, `/\`) {
		http.NotFound(w, r)
		return
	}
	b, err := views.ReadFile("html/fonts/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(b))
}

// icons are the sidebar's line icons (the Icons of the design system): 16 px, a 1.5 px stroke in the colour of
// their text.
var icons = map[string]string{
	"today":    `<circle cx="8" cy="8" r="3"/><path d="M8 1v2M8 13v2M1 8h2M13 8h2M3 3l1.4 1.4M11.6 11.6 13 13M3 13l1.4-1.4M11.6 4.4 13 3"/>`,
	"calendar": `<rect x="2" y="3" width="12" height="11" rx="2"/><path d="M2 6.5h12M5 1.5v3M11 1.5v3"/>`,
	"due":      `<circle cx="8" cy="8" r="6"/><path d="M5.5 8.2l1.8 1.8 3.3-3.6"/>`,
	"tasks":    `<path d="M6 4h8M6 8h8M6 12h8M2 4h1M2 8h1M2 12h1"/>`,
	"people":   `<circle cx="8" cy="5.5" r="2.5"/><path d="M3 14c.6-2.8 2.6-4.2 5-4.2s4.4 1.4 5 4.2"/>`,
	"places":   `<path d="M8 14.5s4.5-4.2 4.5-8a4.5 4.5 0 0 0-9 0c0 3.8 4.5 8 4.5 8z"/><circle cx="8" cy="6.5" r="1.6"/>`,
	"files":    `<path d="M4 1.5h5l3 3v10H4z"/><path d="M9 1.5v3h3"/>`,
	"metrics":  `<path d="M1.5 9h3l2-5 3 9 2-4h3"/>`,
	"habits":   `<path d="M3 6a5 5 0 0 1 9-1.5M13 10a5 5 0 0 1-9 1.5M12 1.5v3h-3M4 14.5v-3h3"/>`,
	"sessions": `<circle cx="8" cy="8.5" r="5.5"/><path d="M8 5.5v3l2 1.5M6.5 1.5h3"/>`,
	"periods":  `<path d="M4 3H2.5v10H4M12 3h1.5v10H12M5.5 8h5"/>`,
	"ghosts":   `<path d="M4 1.5h5l3 3v10H4z" stroke-dasharray="2 2"/>`,
	"search":   `<circle cx="7" cy="7" r="4.5"/><path d="M10.5 10.5 14 14"/>`,
	"new":      `<path d="M8 3v10M3 8h10"/>`,
}

// icon is an icon of the sidebar as inline SVG, hidden from a reader: an icon is always beside its word.
func icon(name string) template.HTML {
	p, ok := icons[name]
	if !ok {
		return ""
	}
	return template.HTML(`<svg class="icon" viewBox="0 0 16 16" aria-hidden="true">` + p + `</svg>`)
}

// shortDay is a day as the sidebar and a list write it: "Mon 12 Oct".
func shortDay(day string) string {
	t, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return day
	}
	return t.Format("Mon 2 Jan")
}
