package view

import (
	"html/template"
	"slices"
	"strings"
	"time"

	"lifelog/internal/pixelart"
	"lifelog/internal/text"
	"lifelog/siren"
)

// The shell is the frame of every page a browser gets (docs/plans/092-the-app-shell.md): the sidebar, with today's
// date. It reads the clock and the Today strip. This month is on Calendar, so the sidebar does not draw it a second time.

// Shell is what the sidebar knows: today, and its zone, and the Today strip.
type Shell struct {
	Today string
	// Ground is the floor the owner chose (settings: boards, oak, firelight or embers). The shell is built with the
	// empty setting already read as boards, so the layout and the sheet never see an empty value.
	Ground string
	Zone   *time.Location // the zone of today: the words for a time use it
	Strip  *Strip         // what today has: nil draws no strip (docs/plans/137-pixel-wood-and-the-focus-pass.md, part 4)
	Chat   *Chat          // today's conversation, for the chat column: nil draws no column (docs/plans/146-settings-and-the-chat-column.md)
	Apps   []AppLink      // the connected apps that run (docs/plans/154-connected-apps-through-the-catalog.md): a link each in the rail
	Lock   bool           // the lock is on for this session (docs/plans/160-the-lock.md): the rail has the Lock button
	Tasks  bool           // a task service runs (docs/plans/158-one-task-service-behind-tasks.md): the rail has Tasks
}

// AppLink is a connected app in the rail: its name, which is its address /with/NAME, and the title of its home.
type AppLink struct{ Name, Title string }

// Chat is what the chat column shows: today's turns, the version of the conversation page (empty when the day has no
// page, so the script can tell that the page changed), and the action say, to the address of today.
type Chat struct {
	Turns   []Turn
	Version string
	Say     siren.Action
}

// Turn is one turn of a conversation as a page draws it. Who is owner, lifelog or text. A tool line is as the page
// writes it, with its "- " mark: the thread draws each as a badge, with the thing its address names (cards.go). Via is
// the writer that relayed the turn, "" with none.
type Turn struct {
	Who, At, Via, Text string
	Tools              []string
}

// Chat is the chat column of the shell. The conversation view is the column at full size, so it has none.
func (v *view) Chat() *Chat {
	if v.Shell == nil || v.IsConversation() {
		return nil
	}
	return v.Shell.Chat
}

// IsConversation reports the view of a conversation (class conversation): the turns of a day and the page they are
// written on.
func (v *view) IsConversation() bool { return slices.Contains(v.Class, "conversation") }

// Turns are the turns of the conversation view, read from the property turns of the entity.
func (v *view) Turns() []Turn {
	props, _ := v.Props.(map[string]any)
	var out []Turn
	for _, x := range anyList(props["turns"]) {
		m, _ := x.(map[string]any)
		t := Turn{Who: Num(m["who"]), At: Num(m["at"]), Via: Num(m["via"]), Text: Num(m["text"])}
		for _, tool := range anyList(m["tools"]) {
			t.Tools = append(t.Tools, Num(tool))
		}
		out = append(out, t)
	}
	return out
}

// dayChip is a day of the day bar of the chat screen: the conversation of that day, marked when it is the one drawn.
type dayChip struct {
	Href, Text string
	Current    bool
}

// ChatDays are the chips of the day bar: the days before today that have a conversation page (Facts.ChatDays), the
// day of the view when it is another day, and Today last. The chip of the day of the view is marked.
func (v *view) ChatDays() []dayChip {
	props, _ := v.Props.(map[string]any)
	shown, today := Num(props["day"]), v.Today()
	days := slices.Clone(v.chatDays)
	if shown != today && !slices.Contains(days, shown) && text.IsDay(shown) {
		days = append(days, shown)
		slices.Sort(days)
	}
	var out []dayChip
	for _, d := range days {
		t, _ := text.ParseDay(d)
		out = append(out, dayChip{Href: siren.DayHref(d) + "/conversation", Text: t.Format("Mon 2"), Current: d == shown})
	}
	if today != "" {
		out = append(out, dayChip{Href: siren.DayHref("today") + "/conversation", Text: "Today", Current: today == shown})
	}
	return out
}

// Tool is a tool line as a badge says it: the name of the tool, the words after it and, when the words are an
// address of this server, that address.
type Tool struct{ Name, Words, Href string }

// ToolChip reads a tool line as the page writes it (API.md, conversation): “- `name` “, “- `name` → [href](href)“,
// “- `name` → href“ or “- `name` failed: code“. The words are what follows the name, without the arrow and the mark
// of a link; Href is the address after the arrow when it starts with a slash, else "". The API's refs of a turn and
// the badges of the thread read the line through this one function. A line that is no such line is a name alone.
func ToolChip(line string) Tool {
	line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
	rest, ok := strings.CutPrefix(line, "`")
	if !ok {
		return Tool{Name: line}
	}
	name, words, ok := strings.Cut(rest, "`")
	if !ok {
		return Tool{Name: line}
	}
	words = strings.TrimSpace(words)
	after, arrow := strings.CutPrefix(words, "→")
	words = strings.TrimSpace(after)
	t := Tool{Name: name, Words: words}
	if !arrow {
		return t
	}
	if inner, ok := strings.CutPrefix(words, "["); ok {
		if label, href, ok := strings.Cut(inner, "]("); ok {
			t.Words = label
			t.Href, _, _ = strings.Cut(href, ")")
		}
	} else if f := strings.Fields(words); len(f) > 0 {
		t.Href = f[0]
	}
	if !strings.HasPrefix(t.Href, "/") {
		t.Href = ""
	}
	return t
}

// waiting reports a conversation whose last turn is the owner's: the agent has not answered yet.
func waiting(turns []Turn) bool { return len(turns) > 0 && turns[len(turns)-1].Who == "owner" }

// Strip is the Today strip of the rail: what is done today, and how many days in a row the owner wrote. The server
// fills it once a page, so the rail and the head of the day read the same streak.
type Strip struct {
	Written bool    // the day page of today has words
	Mood    bool    // a mood reading of today
	Habits  []Habit // the chips (daily metrics with no unit), by name
	Streak  int     // days written in a row, ending today or yesterday
}

// Habit is a chip: a daily metric with no unit (D31, D24). Done is nil when it has no reading today, else whether the reading is not 0.
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
// phone, the bottom bar with More), "chat" Chat (a phone has the button of the column in its place, except on the
// conversation), "owner" the links at its foot. Each stands for the pages under its paths, so the
// one the browser is on is marked (Nav). A page's own neighbours need no entry here, since every template places them
// from the entity's links. Chat is the conversation of any day. Browse is the one list of the record (docs/plans/117-one-browse-view.md, rule 7). Key is
// the access key of an item, "" for none: t is Today.
var navItems = []navEntry{
	{"Lifelog", "/", "", []string{"/"}, "home", ""},
	{"Today", "/days/today", "today", []string{"/days/today"}, "day", "t"},
	{"This week", "/weeks/today", "week", []string{"/weeks"}, "day", ""},
	{"Calendar", "/days", "calendar", []string{"/months", "/years", "/days"}, "day", ""},
	{"Metrics", "/metrics", "metrics", []string{"/metrics"}, "day", ""},
	{"Browse", "/browse", "browse", []string{"/browse"}, "day", ""},
	{"Chat", "/days/today/conversation", "chat", []string{"/days/today/conversation"}, "chat", ""},
	{"Every action", "/actions", "", []string{"/actions"}, "owner", ""},
	{"Integrity checks", "/integrity", "", []string{"/integrity"}, "owner", ""},
	{"Settings", "/settings", "", []string{"/settings"}, "owner", ""},
}

// navEntry is a row of the table of the sidebar.
type navEntry struct {
	Title, Href, Icon string
	Paths             []string // the pages it stands for: these paths and the paths under them
	Group, Key        string
}

// tasksEntry is the sidebar link of Tasks. It is in the sidebar only while a task service runs (Shell.Tasks), after
// Browse (docs/plans/158-one-task-service-behind-tasks.md).
var tasksEntry = navEntry{"Tasks", "/tasks", "tasks", []string{"/tasks"}, "day", ""}

// navTable is navItems, with Tasks after Browse while a task service runs.
func (v *view) navTable() []navEntry {
	if v.Shell == nil || !v.Shell.Tasks {
		return navItems
	}
	at := slices.IndexFunc(navItems, func(it navEntry) bool { return it.Href == "/browse" })
	return slices.Insert(slices.Clone(navItems), at+1, tasksEntry)
}

// navItem is one sidebar link, marked when the browser is on a page it stands for.
type navItem struct {
	Title, Href, Icon, Group, Key string
	Current                       bool
}

// newKinds are the items of the rail's New menu (docs/plans/114-one-way-to-do-a-thing.md): each kind is made on its
// list, and the link opens the list with its New form open. A page, a person and a file are made on their
// Browse list (docs/plans/117-one-browse-view.md, rule 7); a file on the media list. A metric is made on /metrics,
// and a reading is added on the page of today, Home (docs/plans/131-the-pareto-pass.md, part 2).
var newKinds = []struct{ Title, Href, Icon string }{
	{"Page", siren.BrowseNewHref("page"), "new"},
	{"Person", siren.BrowseNewHref("person"), "people"},
	{"Metric", siren.NewMetricHref(""), "metrics"},
	{"Reading", "/?new=record#record", "today"},
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
	if strings.HasPrefix(path, "/days/") && strings.HasSuffix(path, "/conversation") { // the conversation of any day is Chat
		path = "/days/today/conversation"
	}
	table := v.navTable()
	out := make([]navItem, len(table))
	best, at, length := -1, 0, 0
	for i, it := range table {
		out[i] = navItem{Title: it.Title, Href: it.Href, Icon: it.Icon, Group: it.Group, Key: it.Key}
		for _, p := range it.Paths {
			if n := navMatch(path, p); n > at || n == at && n > 0 && len(p) > length {
				best, at, length = i, n, len(p)
			}
		}
	}
	for _, app := range v.apps() { // after the table: the apps change while the server runs
		href := "/with/" + app.Name
		out = append(out, navItem{Title: app.Title, Href: href, Icon: "browse", Group: "apps"})
		if n := navMatch(path, href); n > at || n == at && n > 0 && len(href) > length {
			best, at, length = len(out)-1, n, len(href)
		}
	}
	if best >= 0 {
		out[best].Current = true
	}
	return out
}

// apps are the connected apps that run; none on a page with no shell.
func (v *view) apps() []AppLink {
	if v.Shell == nil {
		return nil
	}
	return v.Shell.Apps
}

// Apps are the nav items of the connected apps, for the sheet that More opens on a phone.
func (v *view) Apps() []navItem {
	var out []navItem
	for _, it := range v.Nav() {
		if it.Group == "apps" {
			out = append(out, it)
		}
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
