package api

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"lifelog/internal/core"
	"lifelog/internal/text"
)

// The browser view is a template per class of entity (html/*.html), and the generic one for any other class:
// properties, links, and every action as a form. A template is a view of the entity the JSON client gets: it
// reads the properties by their JSON names and places the entity's own actions, so a form is shown only where
// the API offers the action. A form posts, and the server answers with a redirect.
//
// The look is the Almanac design system (docs/plans/092-the-app-shell.md) bound in Hearthwood, the materials of the
// owner's Balaur design system (docs/plans/096-bound-in-hearthwood.md), in two stylesheets of this application's
// own; no framework is vendored and nothing is fetched from another host. base.css holds the tokens and styles
// ordinary HTML by element (its forms, tables, buttons and print sheet, light and dark); app.css holds only what
// this app's own vocabulary needs (the shell, its components, .wikilink, .chart), and a test keeps the two apart.
// The two are one cascade in four layers (tokens, base, components, utilities), declared once in base.css, and
// every rule is in logical properties (docs/plans/108-the-browser-made-current.md).
// The three faces are embedded files that /assets/fonts/ serves. There is no script: what a page does beyond
// its links and forms, the browser does by itself (docs/plans/108-the-browser-made-current.md).
//
//go:embed html/*.html html/*.css html/*.svg html/*.webmanifest html/fonts/*.woff2 html/fonts/*.txt
var views embed.FS

// baseCSS and appCSS are the two stylesheets, in that order: the look of ordinary HTML, then this app's own.
var (
	baseCSS = template.CSS(mustView("base.css"))
	appCSS  = template.CSS(mustView("app.css"))
)

func mustView(name string) []byte {
	b, err := views.ReadFile("html/" + name)
	if err != nil {
		panic(err) // the sheets are embedded: one missing is a build defect, not a runtime condition
	}
	return b
}

var pages = template.Must(template.New("").Funcs(template.FuncMap{
	"basecss":    func() template.CSS { return baseCSS },
	"appcss":     func() template.CSS { return appCSS },
	"rel":        func(r []string) string { return strings.Join(r, " ") },
	"markdown":   markdown,
	"mark":       mark,
	"rejected":   rejected,
	"chart":      chart,
	"titleHref":  titleHref,
	"weekday":    weekday,
	"unitWords":  unitWords,
	"form":       formOf,
	"group":      group,
	"weeks":      weeks,
	"calItem":    calItem,
	"dayOfMonth": dayOfMonth,
	"dict":       dict,
	"monthBand":  monthBand,
	"num":        num,
	"pathesc":    url.PathEscape,
	"icon":       icon,
	"shortDay":   shortDay,
	"addDays":    addDays,
	"week":       isoWeek,
	"isoWeekKey": isoWeekKey,
	"yearOf":     func(month string) string { return month[:min(4, len(month))] },
	"yearPage":   yearPage,
	"sinceLast":  sinceLastWeek,
	"nameWords":  nameWords,
	"monthName": func(month string) string { // "2026-10" to "October"
		if t, ok := monthOf(month); ok {
			return t.Format("January")
		}
		return month
	},
	"crumbs":    func(path string) string { return strings.ReplaceAll(path, "/", " › ") }, // "Family/Cousins" to "Family › Cousins"
	"add":       func(a, b int) int { return a + b },
	"segmented": segmented,
	"alert":     alertOf,
	"upTo":      upTo,
	"beyond":    beyond,
}).ParseFS(views, "html/*.html"))

// dict is the arguments of a sub-template that needs more than one value: name/value pairs, each name a string.
func dict(kv ...any) (map[string]any, error) {
	if len(kv)%2 != 0 {
		return nil, fmt.Errorf("dict: %d arguments, want name/value pairs", len(kv))
	}
	m := make(map[string]any, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: name %v is not a string", kv[i])
		}
		m[k] = kv[i+1]
	}
	return m, nil
}

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
	"root": "root", "day": "day", "page": "page", "person": "page", "place": "page", "file": "page",
	"days": "list", "search": "search", "browse": "browse",

	"metrics": "metrics", "series": "metric", "metric": "page", "measurement": "measurement", "error": "error", "login": "login",
	"month": "month", "week": "week", "year": "year", "styleguide": "styleguide",
	"preview": "preview",
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
	Shell     *shellView   // the sidebar's read; nil when the page is drawn without it
	Refused   *refusedView // a form of this page failed: the alert, and the form as it was sent
	Sent      url.Values
	query     url.Values // the page's own query: ?new=<action> and the fields it fills
}

// refusedView is the error of a refused form, as the page it came from draws it: the alert at the top of the page.
type refusedView struct {
	Message string
	Words   string // the alert's first words (alertOf)
	Danger  bool
	Href    string // the refused action's address
}

// Asked is a parameter of the page's own query (docs/plans/114-one-way-to-do-a-thing.md): ?new=<action> opens that
// action's form, and ?category= fills a field.
func (v *view) Asked(name string) string { return v.query.Get(name) }

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

// drawFailed is the page a browser gets when its view cannot be drawn: a 500 that names no internal cause. The
// server's log has the cause.
const drawFailed = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Error · Lifelog</title></head>
<body><p role="alert">This page cannot be drawn. The server's log has the cause.</p><p><a href="/">Home</a></p></body></html>
`

// facts are what a page shows beyond its entity. The one who draws the page reads them from the request and the
// store: the address the browser asked for, the shell's read and, for an error that answers a form, the text that
// form sent and the page it came from.
type facts struct {
	url       *url.URL   // the address the browser asked for
	shell     *shellView // the sidebar's read; nil when the page is drawn without it
	submitted string     // the body (or the text) that a refused form sent
	back      string     // the page that a refused form came from, when it is a page of this server
}

// pageView is the view of a page: the entity, the facts of the page and the refusal of a form that the page shows,
// nil for none. It reads nothing, so the same arguments give the same view. Properties that JSON cannot carry (a
// NaN, an infinity) are an error, so a page is never drawn with its properties left out.
func pageView(e *Entity, f facts, rf *refusal) (*view, error) {
	shape, err := jsonShape(e.Properties)
	if err != nil {
		return nil, err
	}
	v := &view{Entity: e, Props: shape, Path: f.url.Path, Here: hereOf(f.url), Submitted: f.submitted, Back: f.back,
		Shell: f.shell, query: f.url.Query()}
	if rf != nil {
		props, _ := rf.entity.Properties.(map[string]any)
		code, _ := props["code"].(string)
		message, _ := props["message"].(string)
		words := alertOf(code)
		v.Refused = &refusedView{Message: message, Words: words.Words, Danger: words.Danger, Href: rf.href}
		v.Sent = rf.sent
	}
	return v, nil
}

// templateOf names the template that draws an entity: the first of its classes that has one, else "generic".
func templateOf(e *Entity) string {
	for _, c := range e.Class {
		if n, ok := viewOf[c]; ok {
			return n
		}
	}
	return "generic"
}

// htmlOf is the page of a view, drawn by the template of its entity's class.
func htmlOf(v *view) ([]byte, error) {
	var b bytes.Buffer
	if err := pages.ExecuteTemplate(&b, templateOf(v.Entity), v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// navItems are the sidebar's links, in order, by group: "home" is the brand, "day" the sections of every day (on a
// phone, the bottom bar with More), "owner" the links at its foot. Each stands for the pages under its paths, so the
// one the browser is on is marked (Nav). A page's own neighbours need no entry here, since every template places them
// from the entity's links. Browse is the one list of the record (docs/plans/117-one-browse-view.md, rule 7).
var navItems = []struct {
	Title, Href, Icon string
	Paths             []string // the pages it stands for: these paths and the paths under them
	Group             string
}{
	{"Lifelog", "/", "", []string{"/"}, "home"},
	{"Today", "/days/today", "today", []string{"/days/today"}, "day"},
	{"Calendar", "/months/current", "calendar", []string{"/months", "/weeks", "/years", "/days"}, "day"},
	{"Metrics", "/metrics", "metrics", []string{"/metrics"}, "day"},
	{"Browse", "/browse", "browse", []string{"/browse"}, "day"},
	{"Every action", "/actions", "", []string{"/actions"}, "owner"},
	{"Integrity checks", "/integrity", "", []string{"/integrity"}, "owner"},
}

// navItem is one sidebar link, marked when the browser is on a page it stands for.
type navItem struct {
	Title, Href, Icon, Group string
	Current                  bool
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
	if v.Shell != nil && v.Shell.Today != "" && path == dayHref(v.Shell.Today) {
		path = "/days/today"
	}
	out := make([]navItem, len(navItems))
	best, at, length := -1, 0, 0
	for i, it := range navItems {
		out[i] = navItem{Title: it.Title, Href: it.Href, Icon: it.Icon, Group: it.Group}
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
	if l := v.linkByRel(rel); l != nil {
		return l.Title
	}
	return ""
}

// Action is the entity's action of that name as this page draws it, nil when it is not offered.
func (v *view) Action(name string) *shownAction {
	for _, a := range v.Actions {
		if a.Name == name {
			return v.shown(a)
		}
	}
	return nil
}

// shownAction is an action as a page draws it: a copy of the entity's action, so the entity stays as it is and the
// JSON with it, and the marks of the browser, which the wire does not carry.
type shownAction struct {
	Action
	refused bool // the form a refusal came from: filled, open and described by the alert
	noFocus bool // the page shows a refusal: no field takes the focus from the alert
	asked   bool // the page's link asked for this form (?new=<name>): open, filled from the query
}

// shown is the action as this page draws it. On a page that shows a refusal, every action takes no focus, so the
// alert keeps it. The refused action is also marked and filled: each of its fields holds what the form sent. A sent
// value that is not a field of the action is dropped, and a file is never sent back.
//
// With no refusal, the page's link can ask for one form (?new=<name>, docs/plans/114-one-way-to-do-a-thing.md): it
// is open, and each field that the query names, but a path field or a file, holds the query's value.
func (v *view) shown(a Action) *shownAction {
	c := &shownAction{Action: a}
	if v.Refused == nil {
		if a.Name == "" || v.Asked("new") != a.Name {
			return c
		}
		c.asked = true
		c.Fields = slices.Clone(a.Fields)
		for i, f := range c.Fields {
			if _, named := v.query[f.Name]; named && f.In != "path" && f.Type != "file" && f.Name != "new" {
				c.Fields[i].Value = v.query.Get(f.Name)
			}
		}
		return c
	}
	c.noFocus = true
	if a.Href == v.Refused.Href {
		c.refused = true
		c.Fields = slices.Clone(a.Fields)
		for i, f := range c.Fields {
			if _, sent := v.Sent[f.Name]; sent && f.Type != "file" {
				c.Fields[i].Value = v.Sent.Get(f.Name)
			}
		}
	}
	return c
}

// Link is the href of the entity's link with that rel, "" when it has none.
func (v *view) Link(rel string) string {
	if l := v.linkByRel(rel); l != nil {
		return l.Href
	}
	return ""
}

// LinksOf are the entity's links with that rel, in order.
func (v *view) LinksOf(rel string) []Link {
	var out []Link
	for _, l := range v.Links {
		if slices.Contains(l.Rel, rel) {
			out = append(out, l)
		}
	}
	return out
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
// button, "class" styles the form, "hide" lists fields kept with their value but not shown, "autofocus" names
// the field the browser starts in, "more" lists optional fields folded under "More fields"
// (docs/plans/095-the-leaf-and-its-margin.md), "back" is the page the browser returns to after the write (no field
// of the action: the server reads it, see backOf), "placeholder" ("field=words") and "key" ("field=letter") give a
// field its prompt and its access key, and any other name fixes a field to a value (hidden).
type formView struct {
	*Action
	Button, Class string
	Focus         string
	Back          string
	Refused       bool    // the form a refusal came from: open, filled, described by the alert, with no focus
	Asked         bool    // the form the page's link asked for (?new=<name>): open
	Primary       bool    // the button is the primary one: a form with fields to fill
	Shown         []Field // the fields most writes need
	More          []Field // optional fields, folded; still in the form, so a browser sends them with no script
	Hidden        []Field
	prompts       map[string]string // field name to placeholder words
	keys          map[string]string // field name to its access key
}

// Preview reports a form with a body to write: it offers Preview, a second button that sends the form to
// POST /render in a new tab, where the body shows as its page will show it, and nothing is saved.
func (f *formView) Preview() bool {
	for _, fd := range append(f.Shown[:len(f.Shown):len(f.Shown)], f.More...) {
		if fd.Type == "textarea" && (fd.Name == "body" || fd.Name == "text") {
			return true
		}
	}
	return false
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
	Focus       bool
	Placeholder string // a quiet prompt in an empty box; it is an attribute, never sent
	Key         string // the field's access key, which the browser's own modifier reaches
}

// Fields are the fields the form shows (folded false) or folds under "More fields" (folded true).
func (f *formView) Fields(folded bool) []fieldView {
	list := f.Shown
	if folded {
		list = f.More
	}
	out := make([]fieldView, len(list))
	for i, fd := range list {
		out[i] = fieldView{fd, fd.Name == f.Focus, f.prompts[fd.Name], f.keys[fd.Name]}
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
	var c shownAction // an action of the entity's list (range .Actions) has no marks
	switch x := x.(type) {
	case *shownAction:
		if x == nil { // the page does not offer the action
			return nil, nil
		}
		c = *x
	case Action:
		c.Action = x
	default:
		return nil, nil
	}
	a := &c.Action
	if len(opts)%2 != 0 {
		return nil, fmt.Errorf("form %s: options come in name/value pairs", a.Name)
	}
	f := &formView{Action: a, Button: a.Title}
	if f.Button == "" {
		f.Button = a.Name
	}
	fixed, hide, more := map[string]string{}, map[string]bool{}, map[string]bool{}
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
		case k == "placeholder": // "field=words"
			if name, words, ok := strings.Cut(v, "="); ok {
				if f.prompts == nil {
					f.prompts = map[string]string{}
				}
				f.prompts[name] = words
			}
		case k == "key": // "field=letter"
			if name, letter, ok := strings.Cut(v, "="); ok {
				if f.keys == nil {
					f.keys = map[string]string{}
				}
				f.keys[name] = letter
			}
		default:
			fixed[k] = v
		}
	}
	for _, fd := range a.Fields {
		if v, ok := fixed[fd.Name]; ok {
			fd.Value, fd.Type = v, "hidden"
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
	f.Primary = len(f.Shown)+len(f.More) > 0
	f.Refused, f.Asked = c.refused, c.asked
	if c.asked && f.Focus == "" && len(f.Shown) > 0 { // a form the reader asked for starts in its first field
		f.Focus = f.Shown[0].Name
	}
	if c.noFocus { // a page that shows a refusal puts the focus on the alert, and on no field
		f.Focus = ""
	}
	return f, nil
}

// jsonShape is v as a JSON client reads it: maps by JSON name, lists, strings, float64s. A value that JSON cannot
// carry (a NaN, an infinity) is an error, so a page is never drawn with its properties left out.
func jsonShape(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var x any
	if err := json.Unmarshal(b, &x); err != nil {
		return nil, err
	}
	return x, nil
}

// mark is a search snippet as plain text with its matched words marked: the search puts core.SnippetOpen and
// core.SnippetClose around them. The text is escaped first, so a snippet can never carry HTML of its own.
func mark(snippet string) template.HTML {
	return template.HTML(markRE.ReplaceAllString(template.HTMLEscapeString(plainSnippet(snippet)), "<mark>$1</mark>"))
}

// plainSnippet is the words of a snippet without its CommonMark, on one line: the marker of a heading, a quote, a
// list item or a task box at the start of a line is cut, and the brackets of a wikilink or an embed. It is for a
// list of hits, not a rendering; the JSON keeps the text as it is.
func plainSnippet(s string) string {
	var words []string
	for line := range strings.SplitSeq(s, "\n") {
		line = core.BlockMark.ReplaceAllString(strings.TrimSpace(line), "")
		if line != "" {
			words = append(words, line)
		}
	}
	return strings.NewReplacer("![[", "", "[[", "", "]]", "").Replace(strings.Join(words, " "))
}

var markRE = regexp.MustCompile(core.SnippetOpen + `([^` + core.SnippetOpen + core.SnippetClose + `]+)` + core.SnippetClose)

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

// chartPoints is how many readings the chart draws a point for: more points than this overlap on its 640 units and
// only add weight, so the line alone draws a longer series (docs/plans/100-one-page-a-metric.md).
const chartPoints = 200

// chart draws a series as an inline SVG: a line through its readings, each placed by its day between from and to,
// and a point a reading while there are at most chartPoints.
func chart(from, to string, readings any) template.HTML {
	list, _ := readings.([]any)
	// An absent lower bound is explicit all-history mode. Plot from the earliest
	// returned observation, without giving an ordinary calendar date sentinel meaning.
	if from == "" {
		for _, it := range list {
			m, _ := it.(map[string]any)
			day, _ := m["day"].(string)
			if core.IsDay(day) && (from == "" || day < from) {
				from = day
			}
		}
	}
	f, ok1 := core.ParseDay(from)
	t, ok2 := core.ParseDay(to)
	if len(list) == 0 || !ok1 || !ok2 || t.Before(f) {
		return ""
	}
	type pt struct {
		day   time.Time
		x     float64
		value float64
	}
	var pts []pt
	lo, hi := 0.0, 0.0
	for i, it := range list {
		m, _ := it.(map[string]any)
		day, _ := m["day"].(string)
		v, _ := m["value"].(float64)
		d, ok := core.ParseDay(day)
		if !ok {
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
		pts = append(pts, pt{d, x, v})
	}
	const w, h, pad, left, bottom = 640.0, 200.0, 14.0, 48.0, 22.0
	span := hi - lo
	if span == 0 {
		span = 1
	}
	x := func(p pt) float64 { return left + p.x*(w-left-pad) }
	y := func(v float64) float64 { return pad + (1-(v-lo)/span)*(h-2*pad-bottom) }
	var sb strings.Builder
	first, last := f.Format(layoutDateYear), t.Format(layoutDateYear) // the ends in words, with the year: a range can span years
	fmt.Fprintf(&sb, `<svg class="chart" viewBox="0 0 %g %g" role="img" aria-label="%d readings from %s to %s">`,
		w, h, len(pts), first, last)
	fmt.Fprintf(&sb, `<line class="axis" x1="%g" y1="%g" x2="%g" y2="%g"/><line class="axis" x1="%g" y1="%g" x2="%g" y2="%g"/>`,
		left, y(hi), w-pad, y(hi), left, y(lo), w-pad, y(lo))
	fmt.Fprintf(&sb, `<text x="%g" y="%g">%g</text><text x="%g" y="%g">%g</text>`, left-6, y(hi)+4, hi, left-6, y(lo)+4, lo)
	fmt.Fprintf(&sb, `<text class="x" x="%g" y="%g">%s</text><text class="x" x="%g" y="%g">%s</text>`,
		left+40, h-4, first, w-pad-40, h-4, last)
	sb.WriteString(`<polyline points="`)
	for _, p := range pts {
		fmt.Fprintf(&sb, "%.1f,%.1f ", x(p), y(p.value))
	}
	sb.WriteString(`"/>`)
	if len(pts) <= chartPoints {
		for _, p := range pts {
			fmt.Fprintf(&sb, `<circle cx="%.1f" cy="%.1f" r="3"><title>%s: %g</title></circle>`, x(p), y(p.value),
				p.day.Format(layoutDateYear), p.value)
		}
	}
	sb.WriteString(`</svg>`)
	return template.HTML(sb.String())
}
