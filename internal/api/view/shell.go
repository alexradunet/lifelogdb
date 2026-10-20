package view

import (
	"html/template"
	"strings"
	"time"

	"lifelog/internal/api/siren"
	"lifelog/internal/pixelart"
)

// The shell is the frame of every page a browser gets (docs/plans/092-the-app-shell.md): the sidebar, with today's
// date. It reads the clock and the Today strip. This month is on Calendar, so the sidebar does not draw it a second time.

// Shell is what the sidebar knows: today, and its zone, and the Today strip.
type Shell struct {
	Today string
	Zone  *time.Location // the zone of today: the words for a time use it
	Strip *Strip         // what today has: nil draws no strip (docs/plans/137-pixel-wood-and-the-focus-pass.md, part 4)
}

// Strip is the Today strip of the rail: what is done today, and how many days in a row the owner wrote. The server
// fills it once a page, so the rail and the head of the day read the same streak.
type Strip struct {
	Written bool    // the day page of today has words
	Mood    bool    // a mood reading of today
	Habits  []Habit // the habits in use, by name
	Streak  int     // days written in a row, ending today or yesterday
}

// Habit is a habit in use (D24). Done is nil when it has no reading today, else whether the reading is not 0.
type Habit struct {
	Name string
	Done *bool
}

// State is "open" with no reading today, "done" or "no" by the reading.
func (h Habit) State() string {
	switch {
	case h.Done == nil:
		return "open"
	case *h.Done:
		return "done"
	}
	return "no"
}

// Href is the page that records the habit as done: the form of the reading on Home, with the habit and the value 1
// filled in from the query (shown). A chip leads to the one form; it is not a second way to record. The write then
// returns to here, the page the chip was on.
func (h Habit) Href(here string) string {
	return siren.NewReadingHref(h.Name, "", 1, here)
}

// Total is how many things the strip counts: the entry, the mood and each habit.
func (s *Strip) Total() int { return 2 + len(s.Habits) }

// DoneCount is how many of them are done today.
func (s *Strip) DoneCount() int {
	n := 0
	for _, done := range []bool{s.Written, s.Mood} {
		if done {
			n++
		}
	}
	for _, h := range s.Habits {
		if h.Done != nil && *h.Done {
			n++
		}
	}
	return n
}

// Strip is the strip of the rail; nil on a page with no shell or no state.
func (v *view) Strip() *Strip {
	if v.Shell == nil {
		return nil
	}
	return v.Shell.Strip
}

// Streak is the days written in a row, 0 when the page has no strip. The head of a day shows it when it is 2 or more.
func (v *view) Streak() int {
	if s := v.Strip(); s != nil {
		return s.Streak
	}
	return 0
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
	{"This week", "/weeks/today", "week", []string{"/weeks"}, "day", ""},
	{"Plans", "/plans", "plans", []string{"/plans"}, "day", ""},
	{"Calendar", "/days", "calendar", []string{"/months", "/years", "/days"}, "day", ""},
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
// and a reading is added on the page of today, Home (docs/plans/131-the-pareto-pass.md, part 2).
var newKinds = []struct{ Title, Href, Icon string }{
	{"Page", siren.BrowseNewHref("page"), "new"},
	{"Person", siren.BrowseNewHref("person"), "people"},
	{"Place", siren.BrowseNewHref("place"), "places"},
	{"Metric", siren.NewMetricHref(""), "metrics"},
	{"Reading", "/?new=record#record", "metrics"},
	{"File", siren.BrowseNewHref("media"), "files"},
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

// icon is an icon of the interface: an empty span that the sheet draws by its class, hidden from a reader because an
// icon is always beside its word. The name must be a glyph of the icon sheet (pixelart.IconNames); any other name draws
// nothing.
func icon(name string) template.HTML {
	if _, ok := pixelart.IconCell(name); !ok {
		return ""
	}
	return template.HTML(`<span class="icon icon-` + name + `" aria-hidden="true"></span>`)
}
