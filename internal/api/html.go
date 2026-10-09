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
	"sort"
	"strings"
	"time"

	"lifelog/internal/text"
)

// The browser view is a template per class of entity (html/*.html), and the generic one for any other class:
// properties, links, and every action as a form. A template is a view of the entity the JSON client gets: it
// reads the properties by their JSON names and places the entity's own actions, so a form is shown only where
// the API offers the action. There is no JavaScript: a form posts, and the server answers with a redirect.
//
// The look is two stylesheets of this application's own; no framework is vendored and nothing is fetched.
// base.css styles ordinary HTML by element (its forms, tables, buttons, print sheet and dark palette); app.css
// holds only what this app's own vocabulary needs (.wikilink, .badge, .chart, the dense rows), and a test keeps
// the two apart.
//
//go:embed html/*.html html/*.css
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
	"basecss":  func() template.CSS { return baseCSS },
	"appcss":   func() template.CSS { return appCSS },
	"value":    renderValue,
	"rel":      func(r []string) string { return strings.Join(r, " ") },
	"post":     func(a Action) bool { return a.Method == "POST" },
	"markdown": markdown,
	"mark":     mark,
	"rejected": rejected,
	"chart":    chart,
	"title":    titleHref,
	"weekday":  weekday,
	"form":     formOf,
	"group":    group,
	"num": func(v any) string {
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	},
	"pathesc": url.PathEscape,
}).ParseFS(views, "html/*.html"))

// viewOf names the template that shows an entity of a class; a class without one is shown by "generic".
var viewOf = map[string]string{
	"root": "root", "day": "day", "page": "page", "person": "page", "place": "page", "file": "page", "period": "page",
	"days": "list", "people": "list", "places": "list", "files": "list", "ghosts": "list", "search": "search",
	"metrics": "metrics", "series": "series", "habits": "habits", "measurement": "measurement", "error": "error", "login": "login",
}

// view is what a template gets: the entity, its properties as the JSON client reads them, the path the browser
// asked for (which navigation link is current) and, for an error answering a form, the text that form sent (so
// a refused save loses nothing) and the page it came from.
type view struct {
	*Entity
	Props     any
	Path      string
	Submitted string
	Back      string
}

func renderHTML(w http.ResponseWriter, r *http.Request, status int, e *Entity) {
	v := &view{Entity: e, Props: jsonShape(e.Properties), Path: r.URL.Path}
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

// navItems are the top navigation's links, in order. One of them is marked current per page (Nav); a page's own
// neighbours need no entry here, since every template places them from the entity's links.
var navItems = []struct{ Title, Href string }{
	{"Home", "/"}, {"Today", "/days/today"}, {"Days", "/days"}, {"People", "/people"}, {"Places", "/places"},
	{"Files", "/files"}, {"Metrics", "/metrics"}, {"Habits", "/habits"}, {"Tasks", "/tasks"}, {"New", "/#new"},
}

// navItem is one navigation link, marked when the browser is on it.
type navItem struct {
	Title, Href string
	Current     bool
}

// Nav is the top navigation with the link of this page marked, so a reader knows where they are.
func (v *view) Nav() []navItem {
	out := make([]navItem, len(navItems))
	best, at := -1, 0
	for i, it := range navItems {
		out[i] = navItem{Title: it.Title, Href: it.Href}
		switch n := navMatch(v.Path, it.Href); {
		case n > at: // a better match than the one held
			best, at = i, n
		case n == at && n > 0 && len(it.Href) > len(navItems[best].Href): // of two equal ones, the more specific
			best = i
		}
	}
	if best >= 0 {
		out[best].Current = true
	}
	return out
}

// navMatch is how well a navigation href fits the request path: 2 when it is the path, 1 when the path is under
// it, 0 when it is not. A fragment link never matches: it names a place on a page, not a page.
func navMatch(path, href string) int {
	if strings.Contains(href, "#") {
		return 0
	}
	switch {
	case path == href:
		return 2
	case href != "/" && strings.HasPrefix(path, href+"/"):
		return 1
	}
	return 0
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
// button, "class" styles the form, "hide" lists fields kept with their value but not shown, "autofocus" names
// the field the browser starts in, "choices:<field>" offers a field's values as a list, and any other name
// fixes a field to a value (hidden).
type formView struct {
	*Action
	Button, Class string
	Focus         string
	Shown         []Field
	Hidden        []Field
}

// Multipart reports a form with a file field: a browser sends it as multipart/form-data.
func (f *formView) Multipart() bool {
	for _, fd := range f.Shown {
		if fd.Type == "file" {
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
	fixed, hide, choices := map[string]string{}, map[string]bool{}, map[string][]string{}
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
		case k == "autofocus":
			f.Focus = v
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
		if fd.Type == "hidden" || hide[fd.Name] {
			f.Hidden = append(f.Hidden, fd)
		} else {
			f.Shown = append(f.Shown, fd)
		}
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

func weekday(day string) string {
	t, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return ""
	}
	return t.Weekday().String()
}

// group splits a list of objects by the value of key, keeping the order of first appearance.
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
		k := fmt.Sprint(m[key])
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

// renderValue shows any property value: objects as two-column tables, lists of objects as tables, long text as pre.
func renderValue(v any) template.HTML {
	var sb strings.Builder
	writeValue(&sb, jsonShape(v))
	return template.HTML(sb.String())
}

func writeValue(sb *strings.Builder, x any) {
	esc := template.HTMLEscapeString
	switch x := x.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sb.WriteString("<table>")
		for _, k := range keys {
			sb.WriteString(`<tr><th scope="row">` + esc(k) + "</th><td>")
			writeValue(sb, x[k])
			sb.WriteString("</td></tr>")
		}
		sb.WriteString("</table>")
	case []any:
		if len(x) == 0 {
			sb.WriteString(`<small>none</small>`)
			return
		}
		if _, ok := x[0].(map[string]any); ok {
			var cols []string
			seen := map[string]bool{}
			for _, row := range x {
				if m, ok := row.(map[string]any); ok {
					for k := range m {
						if !seen[k] {
							cols = append(cols, k)
							seen[k] = true
						}
					}
				}
			}
			sort.Strings(cols)
			sb.WriteString("<table><thead><tr>")
			for _, c := range cols {
				sb.WriteString(`<th scope="col">` + esc(c) + "</th>")
			}
			sb.WriteString("</tr></thead><tbody>")
			for _, row := range x {
				m, _ := row.(map[string]any)
				sb.WriteString("<tr>")
				for _, c := range cols {
					sb.WriteString("<td>")
					writeValue(sb, m[c])
					sb.WriteString("</td>")
				}
				sb.WriteString("</tr>")
			}
			sb.WriteString("</tbody></table>")
			return
		}
		sb.WriteString("<ul>")
		for _, it := range x {
			sb.WriteString("<li>")
			writeValue(sb, it)
			sb.WriteString("</li>")
		}
		sb.WriteString("</ul>")
	case string:
		if strings.Contains(x, "\n") || len(x) > 80 {
			sb.WriteString("<pre>" + esc(x) + "</pre>")
		} else {
			sb.WriteString(esc(x))
		}
	case nil:
		sb.WriteString(`<small>—</small>`)
	default:
		sb.WriteString(esc(fmt.Sprint(x)))
	}
}
