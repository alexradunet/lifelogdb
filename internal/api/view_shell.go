package api

import (
	"context"
	"html/template"
	"strings"
	"time"

	"lifelog/internal/api/siren"
	"lifelog/internal/core"
	"lifelog/internal/text"
)

// The shell is the frame of every page a browser gets (docs/plans/092-the-app-shell.md): the sidebar, with today's
// date and this month. It reads the day pages of this month, as GET /months/{month} gives them to every client, once
// a page, and only for a page (answer): a JSON answer never runs it.

// Shell is what the sidebar shows: today, and the weeks of this month with the days that have a day page.
type Shell struct {
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
func shellOf(ctx context.Context, s *core.Store) *Shell {
	now := s.Local() // one read of the clock, so today and the zone of the words agree
	sv := &Shell{Today: now.Format(time.DateOnly), Zone: now.Location()}
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

// navItems are the sidebar's links, in order, by group: "home" is the brand, "day" the sections of every day (on a
// phone, the bottom bar with More), "owner" the links at its foot. Each stands for the pages under its paths, so the
// one the browser is on is marked (Nav). A page's own neighbours need no entry here, since every template places them
// from the entity's links. Browse is the one list of the record (docs/plans/117-one-browse-view.md, rule 7). Key is
// the access key of an item, "" for none: t is Today.
var navItems = []struct {
	Title, Href, Icon string
	Paths             []string // the pages it stands for: these paths and the paths under them
	Group, Key        string
}{
	{"Lifelog", "/", "", []string{"/"}, "home", ""},
	{"Today", "/days/today", "today", []string{"/days/today"}, "day", "t"},
	{"Calendar", "/months/current", "calendar", []string{"/months", "/weeks", "/years", "/days"}, "day", ""},
	{"Metrics", "/metrics", "metrics", []string{"/metrics"}, "day", ""},
	{"Browse", "/browse", "browse", []string{"/browse"}, "day", ""},
	{"Every action", "/actions", "", []string{"/actions"}, "owner", ""},
	{"Integrity checks", "/integrity", "", []string{"/integrity"}, "owner", ""},
}

// navItem is one sidebar link, marked when the browser is on a page it stands for.
type navItem struct {
	Title, Href, Icon, Group, Key string
	Current                       bool
}

// newKinds are the items of the rail's New menu (docs/plans/114-one-way-to-do-a-thing.md): each kind is made on its
// list, and the link opens the list with its New form open. A page, a person, a place and a file are made on their
// Browse list (docs/plans/117-one-browse-view.md, rule 7); a file on the media list. A metric is made on /metrics,
// and a reading is added on its metric's card.
var newKinds = []struct{ Title, Href, Icon string }{
	{"Page", "/browse?kind=page&new=create-page", "new"},
	{"Person", "/browse?kind=person&new=create-person", "people"},
	{"Place", "/browse?kind=place&new=create-place", "places"},
	{"Metric", "/metrics?new=register-metric", "metrics"},
	{"Reading", "/metrics", "metrics"},
	{"File", "/browse?kind=media&new=add-file", "files"},
}

// NewKinds are the items of the rail's New menu.
func (v *view) NewKinds() []struct{ Title, Href, Icon string } { return newKinds }

// Nav is the sidebar with the link of this page marked, so a reader knows where they are: the item whose path is
// the page's path, else the one with the longest path the page is under. Today's day is Today, not Calendar.
func (v *view) Nav() []navItem {
	path := v.Path
	if today := v.Today(); today != "" && path == siren.DayHref(today) {
		path = "/days/today"
	}
	out := make([]navItem, len(navItems))
	best, at, length := -1, 0, 0
	for i, it := range navItems {
		out[i] = navItem{Title: it.Title, Href: it.Href, Icon: it.Icon, Group: it.Group, Key: it.Key}
		for _, p := range it.Paths {
			if n := navMatch(path, p); n > at || n == at && n > 0 && len(p) > length {
				best, at, length = i, n, len(p)
			}
		}
	}
	if best >= 0 {
		out[best].Current = true
	}
	return out
}

// InMore reports a page that is one of the owner's links at the foot of More: the button More is then marked, as
// the place that holds the page.
func (v *view) InMore() bool {
	for _, it := range v.Nav() {
		if it.Current && it.Group == "owner" {
			return true
		}
	}
	return false
}

// navMatch is how well a path an item stands for fits the request path: 2 when it is the path, 1 when the path is
// under it, 0 when it is not.
func navMatch(path, p string) int {
	switch {
	case path == p:
		return 2
	case p != "/" && strings.HasPrefix(path, p+"/"):
		return 1
	}
	return 0
}

// icons are the sidebar's line icons (the Icons of the design system): 16 px, a 1.5 px stroke in the colour of
// their text.
var icons = map[string]string{
	"today":    `<circle cx="8" cy="8" r="3"/><path d="M8 1v2M8 13v2M1 8h2M13 8h2M3 3l1.4 1.4M11.6 11.6 13 13M3 13l1.4-1.4M11.6 4.4 13 3"/>`,
	"calendar": `<rect x="2" y="3" width="12" height="11" rx="2"/><path d="M2 6.5h12M5 1.5v3M11 1.5v3"/>`,
	"browse":   `<path d="M2 3.5h4l1.5 1.5H14v8.5H2z"/><path d="M2 7.5h12"/>`,
	"people":   `<circle cx="8" cy="5.5" r="2.5"/><path d="M3 14c.6-2.8 2.6-4.2 5-4.2s4.4 1.4 5 4.2"/>`,
	"places":   `<path d="M8 14.5s4.5-4.2 4.5-8a4.5 4.5 0 0 0-9 0c0 3.8 4.5 8 4.5 8z"/><circle cx="8" cy="6.5" r="1.6"/>`,
	"files":    `<path d="M4 1.5h5l3 3v10H4z"/><path d="M9 1.5v3h3"/>`,
	"metrics":  `<path d="M1.5 9h3l2-5 3 9 2-4h3"/>`,
	"search":   `<circle cx="7" cy="7" r="4.5"/><path d="M10.5 10.5 14 14"/>`,
	"new":      `<path d="M8 3v10M3 8h10"/>`,
	"more":     `<circle cx="3.5" cy="8" r=".9"/><circle cx="8" cy="8" r=".9"/><circle cx="12.5" cy="8" r=".9"/>`,
	// the kinds of the media list, on a blank tile: a picture, a video, a recording
	"image": `<rect x="2" y="3" width="12" height="10" rx="1.5"/><circle cx="5.5" cy="6.5" r="1.3"/><path d="M2.5 11.5l3.5-3.5 2.5 2.5 2-2 3 3"/>`,
	"video": `<rect x="1.5" y="4" width="9" height="8" rx="1.5"/><path d="M10.5 7l4-2v6l-4-2z"/>`,
	"audio": `<rect x="6" y="1.5" width="4" height="8" rx="2"/><path d="M3.5 7.5a4.5 4.5 0 0 0 9 0M8 12v2.5M5.5 14.5h5"/>`,
}

// icon is an icon of the sidebar as inline SVG, hidden from a reader: an icon is always beside its word.
func icon(name string) template.HTML {
	p, ok := icons[name]
	if !ok {
		return ""
	}
	return template.HTML(`<svg class="icon" viewBox="0 0 16 16" aria-hidden="true">` + p + `</svg>`)
}
