package api

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"lifelog/internal/text"
)

// The browser view is a template per class of entity (html/*.html), and the generic one for any other class:
// properties, links, and every action as a form. A template is a view of the entity the JSON client gets: it
// reads the properties by their JSON names and places the entity's own actions, so a form is shown only where
// the API offers the action. A form posts, and the server answers with a redirect.
//
// The look is the Almanac design system (docs/plans/092-the-app-shell.md), in two stylesheets of this
// application's own; no framework is vendored and nothing is fetched from another host. base.css holds the tokens
// and styles ordinary HTML by element (its forms, tables, buttons and print sheet, light and dark); app.css holds
// only what this app's own vocabulary needs (the shell, its components, .wikilink, .chart), and a test keeps the
// two apart. The two faces are embedded files that /assets/fonts/ serves. app.js is the one script, written into
// each page like the sheets: it makes a page better and no page needs it
// (docs/plans/091-the-browser-for-daily-use.md).
//
//go:embed html/*.html html/*.css html/*.js html/fonts/*.woff2 html/fonts/*.txt
var views embed.FS

// baseCSS and appCSS are the two stylesheets, in that order: the look of ordinary HTML, then this app's own.
// appJS is the script.
var (
	baseCSS = template.CSS(mustView("base.css"))
	appCSS  = template.CSS(mustView("app.css"))
	appJS   = template.JS(mustView("app.js"))
)

func mustView(name string) []byte {
	b, err := views.ReadFile("html/" + name)
	if err != nil {
		panic(err) // the sheets are embedded: one missing is a build defect, not a runtime condition
	}
	return b
}

var pages = template.Must(template.New("").Funcs(template.FuncMap{
	"basecss":  func() template.CSS { return baseCSS },
	"appcss":   func() template.CSS { return appCSS },
	"appjs":    func() template.JS { return appJS },
	"rel":      func(r []string) string { return strings.Join(r, " ") },
	"post":     func(a Action) bool { return a.Method == "POST" },
	"markdown": markdown,
	"mark":     mark,
	"rejected": rejected,
	"chart":    chart,
	"title":    titleHref,
	"weekday":  weekday,
	"form":     formOf,
	"outcomes": outcomes,
	"group":    group,
	"due":      dueGroups,
	"dueWords": dueWords,
	"cadence":  cadence,
	"weeks":    weeks,
	"dayOfMonth": func(day string) string {
		if t, err := time.Parse(time.DateOnly, day); err == nil {
			return strconv.Itoa(t.Day())
		}
		return day
	},
	"num":       num,
	"pathesc":   url.PathEscape,
	"icon":      icon,
	"shortDay":  shortDay,
	"addDays":   addDays,
	"week":      isoWeek,
	"segmented": segmented,
	"alert":     alertOf,
	"capLater":  capLater,
}).ParseFS(views, "html/*.html"))

// num shows a property's value. A number is written in full, never with an exponent: the JSON client reads every
// number as a float64, and an id of a million must still make the link /pages/1000000, not /pages/1e+06.
func num(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

// viewOf names the template that shows an entity of a class; a class without one is shown by "generic".
var viewOf = map[string]string{
	"root": "root", "day": "day", "page": "page", "person": "page", "place": "page", "file": "page", "period": "page",
	"days": "list", "people": "list", "places": "list", "files": "list", "ghosts": "list", "search": "search",
	"metrics": "metrics", "series": "series", "habits": "habits", "measurement": "measurement", "error": "error", "login": "login",
	"tasks": "tasks", "task": "task", "occurrence": "occurrence", "deadlines": "deadlines", "month": "month",
}

// view is what a template gets: the entity, its properties as the JSON client reads them, the path the browser
// asked for (which navigation link is current), the address of this page (where a form on it asks to return,
// docs/plans/091-the-browser-for-daily-use.md) and, for an error answering a form, the text that form sent (so a
// refused save loses nothing) and the page it came from.
type view struct {
	*Entity
	Props     any
	Path      string
	Here      string
	Submitted string
	Back      string
	Shell     *shellView // the sidebar's read; nil when the page is drawn without it
}

// hereOf is the address of the page the browser is on, without the one-use feedback receipt: a form that returns
// here must not carry a receipt that was already taken.
func hereOf(u *url.URL) string {
	q := u.Query()
	q.Del("feedback")
	if len(q) == 0 {
		return u.EscapedPath()
	}
	return u.EscapedPath() + "?" + q.Encode()
}

func renderHTML(w http.ResponseWriter, r *http.Request, status int, e *Entity, sv *shellView) {
	v := &view{Entity: e, Props: jsonShape(e.Properties), Path: r.URL.Path, Here: hereOf(r.URL), Shell: sv}
	if status >= 400 && r.Method == "POST" {
		v.Submitted = r.PostForm.Get("body")
		if v.Submitted == "" {
			v.Submitted = r.PostForm.Get("text")
		}
		if ref, err := url.Parse(r.Referer()); err == nil && ref.Host == r.Host && strings.HasPrefix(ref.Path, "/") {
			v.Back = ref.RequestURI()
		}
	}
	name := "generic"
	for _, c := range e.Class {
		if n, ok := viewOf[c]; ok {
			name = n
			break
		}
	}
	var b bytes.Buffer
	if err := pages.ExecuteTemplate(&b, name, v); err != nil {
		status = http.StatusInternalServerError
		b.Reset()
		fmt.Fprintf(&b, "<!doctype html><title>Error</title><pre>%s</pre>", template.HTMLEscapeString(err.Error()))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	io.Copy(w, &b)
}

// navItems are the sidebar's links, in order, by group: "home" is the brand, "day" the sections of every day,
// "record" the sections of the record, "owner" the links at its foot. Each stands for the pages under its paths, so
// the one the browser is on is marked (Nav). A page's own neighbours need no entry here, since every template
// places them from the entity's links.
var navItems = []struct {
	Title, Href, Icon string
	Paths             []string // the pages it stands for: these paths and the paths under them
	Group             string
}{
	{"Lifelog", "/", "", []string{"/"}, "home"},
	{"Today", "/days/today", "today", []string{"/days/today"}, "day"},
	{"Calendar", "/months/current", "calendar", []string{"/months", "/days"}, "day"},
	{"Due", "/deadlines", "due", []string{"/deadlines"}, "day"},
	{"Tasks", "/tasks", "tasks", []string{"/tasks"}, "day"},
	{"People", "/people", "people", []string{"/people"}, "record"},
	{"Places", "/places", "places", []string{"/places"}, "record"},
	{"Files", "/files", "files", []string{"/files"}, "record"},
	{"Metrics", "/metrics", "metrics", []string{"/metrics"}, "record"},
	{"Habits", "/habits", "habits", []string{"/habits"}, "record"},
	{"Sessions", "/sessions", "sessions", []string{"/sessions"}, "record"},
	{"Periods", "/periods", "periods", []string{"/periods"}, "record"},
	{"Ghost pages", "/ghosts", "ghosts", []string{"/ghosts"}, "record"},
	{"Every action", "/actions", "", []string{"/actions"}, "owner"},
	{"Integrity checks", "/integrity", "", []string{"/integrity"}, "owner"},
}

// navItem is one sidebar link, marked when the browser is on a page it stands for. Count is the number beside it:
// the open occurrences overdue or due today, beside Due.
type navItem struct {
	Title, Href, Icon, Group string
	Current                  bool
	Count                    int
}

// Nav is the sidebar with the link of this page marked, so a reader knows where they are: the item whose path is
// the page's path, else the one with the longest path the page is under. Today's day is Today, not Calendar.
func (v *view) Nav() []navItem {
	path := v.Path
	if v.Shell != nil && v.Shell.Today != "" && path == dayHref(v.Shell.Today) {
		path = "/days/today"
	}
	out := make([]navItem, len(navItems))
	best, at, length := -1, 0, 0
	for i, it := range navItems {
		out[i] = navItem{Title: it.Title, Href: it.Href, Icon: it.Icon, Group: it.Group}
		if it.Href == "/deadlines" && v.Shell != nil {
			out[i].Count = v.Shell.Due
		}
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

// LinkTitle is the title of the entity's link with that rel, "" when it has none.
func (v *view) LinkTitle(rel string) string {
	for _, l := range v.Links {
		for _, r := range l.Rel {
			if r == rel {
				return l.Title
			}
		}
	}
	return ""
}

// Action is the entity's action of that name, nil when it is not offered.
func (v *view) Action(name string) *Action {
	for i := range v.Actions {
		if v.Actions[i].Name == name {
			return &v.Actions[i]
		}
	}
	return nil
}

// ActionFor is the action of that name on the resource href (a view offering one per item, e.g. a check-in
// per habit).
func (v *view) ActionFor(name, href string) *Action {
	for i := range v.Actions {
		if a := &v.Actions[i]; a.Name == name && strings.HasPrefix(a.Href, href+"/") {
			return a
		}
	}
	return nil
}

// Link is the href of the entity's link with that rel, "" when it has none.
func (v *view) Link(rel string) string {
	for _, l := range v.Links {
		for _, r := range l.Rel {
			if r == rel {
				return l.Href
			}
		}
	}
	return ""
}

// Embedded are the entity's embedded links with that rel.
func (v *view) Embedded(rel string) []Link {
	var out []Link
	for _, l := range v.Entities {
		for _, r := range l.Rel {
			if r == rel {
				out = append(out, l)
				break
			}
		}
	}
	return out
}

// formView is an action as the "form" template draws it. formOf takes name/value pairs: "button" labels the
// button, "class" styles the form, "primary" ("1") draws its button as the primary one, "hide" lists fields kept with their value but not shown, "autofocus" names
// the field the browser starts in, "choices:<field>" offers a field's values as a list, "more" lists optional
// fields folded under "More fields" (docs/plans/095-the-leaf-and-its-margin.md), "back" is the page the browser
// returns to after the write (no field of the action: the server reads it, see backOf), and any other name fixes a
// field to a value (hidden).
type formView struct {
	*Action
	Button, Class string
	Focus         string
	Back          string
	Primary       bool    // the button is the primary one: a form with fields to fill
	Shown         []Field // the fields most writes need
	More          []Field // optional fields, folded; still in the form, so a browser sends them with no script
	Hidden        []Field
}

// Multipart reports a form with a file field, shown or folded: a browser sends it as multipart/form-data.
func (f *formView) Multipart() bool {
	for _, fd := range append(f.Shown[:len(f.Shown):len(f.Shown)], f.More...) {
		if fd.Type == "file" {
			return true
		}
	}
	return false
}

// fieldView is a field as the "field" template draws it: the field, and whether the browser starts in it.
type fieldView struct {
	Field
	Focus bool
}

// Fields are the fields the form shows (folded false) or folds under "More fields" (folded true).
func (f *formView) Fields(folded bool) []fieldView {
	list := f.Shown
	if folded {
		list = f.More
	}
	out := make([]fieldView, len(list))
	for i, fd := range list {
		out[i] = fieldView{fd, fd.Name == f.Focus}
	}
	return out
}

// MoreOpen reports folded fields of which one has a value: they are drawn open, so no value is out of sight.
func (f *formView) MoreOpen() bool {
	for _, fd := range f.More {
		if num(fd.Value) != "" {
			return true
		}
	}
	return false
}

func formOf(x any, opts ...string) (*formView, error) {
	var a *Action
	switch x := x.(type) {
	case *Action:
		a = x
	case Action:
		a = &x
	}
	if a == nil {
		return nil, nil
	}
	if len(opts)%2 != 0 {
		return nil, fmt.Errorf("form %s: options come in name/value pairs", a.Name)
	}
	f := &formView{Action: a, Button: a.Title}
	if f.Button == "" {
		f.Button = a.Name
	}
	fixed, hide, more, choices := map[string]string{}, map[string]bool{}, map[string]bool{}, map[string][]string{}
	primary := false // the view named the button primary or not; else a form with fields to fill is primary
	for i := 0; i < len(opts); i += 2 {
		k, v := opts[i], opts[i+1]
		switch {
		case k == "button":
			f.Button = v
		case k == "class":
			f.Class = v
		case k == "hide":
			for _, n := range strings.Split(v, ",") {
				hide[n] = true
			}
		case k == "more":
			for _, n := range strings.Split(v, ",") {
				more[n] = true
			}
		case k == "autofocus":
			f.Focus = v
		case k == "back":
			f.Back = v
		case k == "primary":
			f.Primary, primary = v == "1", true
		case strings.HasPrefix(k, "choices:"):
			choices[strings.TrimPrefix(k, "choices:")] = strings.Fields(v)
		default:
			fixed[k] = v
		}
	}
	for _, fd := range a.Fields {
		if v, ok := fixed[fd.Name]; ok {
			fd.Value, fd.Type = v, "hidden"
		}
		if c, ok := choices[fd.Name]; ok {
			fd.Options = c
			if !fd.Required {
				fd.Options = append([]string{""}, c...)
			}
		}
		switch {
		case fd.Type == "hidden" || hide[fd.Name]:
			f.Hidden = append(f.Hidden, fd)
		case more[fd.Name] && !fd.Required:
			f.More = append(f.More, fd)
		default:
			f.Shown = append(f.Shown, fd)
		}
	}
	if !primary {
		f.Primary = len(f.Shown)+len(f.More) > 0
	}
	return f, nil
}

// jsonShape is v as a JSON client reads it: maps by JSON name, lists, strings, float64s.
func jsonShape(v any) any {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	var x any
	json.Unmarshal(b, &x)
	return x
}

// mark is a search snippet with its matched words marked: snippet() puts [ and ] around them. The text is
// escaped first, so a snippet can never carry HTML of its own.
func mark(snippet string) template.HTML {
	return template.HTML(markRE.ReplaceAllString(template.HTMLEscapeString(snippet), "<mark>$1</mark>"))
}

var markRE = regexp.MustCompile(`\[([^[\]]+)\]`)

// rejected are the [[…]] names in a body that make no link, because they are not valid titles.
func rejected(body string) []string {
	_, _, out := text.Targets(body, "")
	return out
}

// segmented reports a field the browser draws as a Segmented choice of the design system, a row of radio buttons:
// 2 to 5 options, none empty and each of 3 characters or fewer (a mood 1 to 5, a 1 or a 0). Any other field with
// options is a select.
func segmented(f Field) bool {
	if len(f.Options) < 2 || len(f.Options) > 5 {
		return false
	}
	for _, o := range f.Options {
		if o == "" || utf8.RuneCountInString(o) > 3 {
			return false
		}
	}
	return true
}

// alertView is how an error entity is drawn (docs/plans/092-the-app-shell.md): the first words, picked by the
// error's code, and the tone: danger when the request was refused or failed, warn when the owner can fix it here.
type alertView struct {
	Words  string
	Danger bool
}

// alertOf picks an error's first words and tone by its code (API.md, Errors); the message follows them.
func alertOf(code any) alertView {
	switch code {
	case "forbidden", "cross_origin", "token_required", "unauthorized":
		return alertView{"Refused.", true}
	case "too_large", "timeout", "internal":
		return alertView{"Not done.", true}
	case "stale_version":
		return alertView{"Not saved. The page changed since you opened it.", false}
	case "exists":
		return alertView{"Not created.", false}
	}
	return alertView{}
}

// addDays is the day n days after day ("" for a day that is not one).
func addDays(day string, n int) string {
	t, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, n).Format(time.DateOnly)
}

// isoWeek is the ISO 8601 week of a day, 0 for a day that is not one.
func isoWeek(day string) int {
	t, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return 0
	}
	_, w := t.ISOWeek()
	return w
}

func weekday(day string) string {
	t, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return ""
	}
	return t.Weekday().String()
}

// group splits a list of objects by the value of key, keeping the order of first appearance. The objects without
// the key are in the bucket whose key is "".
type bucket struct {
	Key   string
	Items []any
}

func group(list any, key string) []bucket {
	var out []bucket
	at := map[string]int{}
	items, _ := list.([]any)
	for _, it := range items {
		m, _ := it.(map[string]any)
		k := num(m[key])
		i, ok := at[k]
		if !ok {
			i = len(out)
			at[k] = i
			out = append(out, bucket{Key: k})
		}
		out[i].Items = append(out[i].Items, it)
	}
	return out
}

// chart draws a series as an inline SVG: one point per reading, placed by its day between from and to.
func chart(from, to string, readings any) template.HTML {
	list, _ := readings.([]any)
	// An absent lower bound is explicit all-history mode. Plot from the earliest
	// returned observation, without giving an ordinary calendar date sentinel meaning.
	if from == "" {
		for _, it := range list {
			m, _ := it.(map[string]any)
			day, _ := m["day"].(string)
			if _, err := time.Parse(time.DateOnly, day); err == nil && (from == "" || day < from) {
				from = day
			}
		}
	}
	f, err1 := time.Parse(time.DateOnly, from)
	t, err2 := time.Parse(time.DateOnly, to)
	if len(list) == 0 || err1 != nil || err2 != nil || t.Before(f) {
		return ""
	}
	type pt struct {
		day   string
		x     float64
		value float64
	}
	var pts []pt
	lo, hi := 0.0, 0.0
	for i, it := range list {
		m, _ := it.(map[string]any)
		day, _ := m["day"].(string)
		v, _ := m["value"].(float64)
		d, err := time.Parse(time.DateOnly, day)
		if err != nil {
			continue
		}
		if i == 0 || v < lo {
			lo = v
		}
		if i == 0 || v > hi {
			hi = v
		}
		// Unix seconds cover the full supported date range without duration saturation.
		x := 0.5
		if t.After(f) {
			x = float64(d.Unix()-f.Unix()) / float64(t.Unix()-f.Unix())
		}
		pts = append(pts, pt{day, x, v})
	}
	const w, h, pad, left, bottom = 640.0, 200.0, 14.0, 48.0, 22.0
	span := hi - lo
	if span == 0 {
		span = 1
	}
	x := func(p pt) float64 { return left + p.x*(w-left-pad) }
	y := func(v float64) float64 { return pad + (1-(v-lo)/span)*(h-2*pad-bottom) }
	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg class="chart" viewBox="0 0 %g %g" role="img" aria-label="%d readings from %s to %s">`,
		w, h, len(pts), from, to)
	fmt.Fprintf(&sb, `<line class="axis" x1="%g" y1="%g" x2="%g" y2="%g"/><line class="axis" x1="%g" y1="%g" x2="%g" y2="%g"/>`,
		left, y(hi), w-pad, y(hi), left, y(lo), w-pad, y(lo))
	fmt.Fprintf(&sb, `<text x="%g" y="%g">%g</text><text x="%g" y="%g">%g</text>`, left-6, y(hi)+4, hi, left-6, y(lo)+4, lo)
	fmt.Fprintf(&sb, `<text class="x" x="%g" y="%g">%s</text><text class="x" x="%g" y="%g">%s</text>`,
		left+30, h-4, from, w-pad-30, h-4, to)
	sb.WriteString(`<polyline points="`)
	for _, p := range pts {
		fmt.Fprintf(&sb, "%.1f,%.1f ", x(p), y(p.value))
	}
	sb.WriteString(`"/>`)
	for _, p := range pts {
		fmt.Fprintf(&sb, `<circle cx="%.1f" cy="%.1f" r="3"><title>%s: %g</title></circle>`, x(p), y(p.value),
			template.HTMLEscapeString(p.day), p.value)
	}
	sb.WriteString(`</svg>`)
	return template.HTML(sb.String())
}
