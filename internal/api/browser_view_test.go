package api

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

// TestDangerIsACatalogAction keeps the destructive-action set honest: it names nothing the catalog does not
// have, and it is what a browser draws from — never an action's name read by a template.
func TestDangerIsACatalogAction(t *testing.T) {
	dangerous := map[string]bool{}
	for _, s := range catalog {
		if s.Danger {
			dangerous[s.Name] = true
		}
	}
	if len(dangerous) == 0 {
		t.Error("no action is destructive: the set lost its contents")
	}
	for _, name := range []string{"tombstone", "retract"} {
		if !dangerous[name] {
			t.Errorf("%s hides or retracts what is there: it is not marked dangerous", name)
		}
	}
	for _, name := range []string{"revive", "capture", "save-body"} {
		if dangerous[name] {
			t.Errorf("%s writes or restores: it must not be marked dangerous", name)
		}
	}
}

// TestDangerAndRowsReachTheForm says the button of a destructive action is drawn as one, and that a textarea is
// as tall as the catalog says — both from the catalog, so no template knows an action or a field by name.
func TestDangerAndRowsReachTheForm(t *testing.T) {
	draw := func(t *testing.T, a Action, opts ...string) string {
		t.Helper()
		f, err := formOf(a, opts...)
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		if err := pages.ExecuteTemplate(&b, "form", f); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	if got := draw(t, action("tombstone", map[string]string{"id": "7"}, nil)); !strings.Contains(got, `<button class="danger">`) {
		t.Errorf("a tombstone draws no danger button: %s", got)
	}
	if got := draw(t, action("revive", map[string]string{"id": "7"}, nil)); strings.Contains(got, "danger") {
		t.Errorf("a revive draws a danger button: %s", got)
	}
	if got := draw(t, action("save-body", map[string]string{"id": "7"}, map[string]any{"body": "words", "version": "3"})); !strings.Contains(got, `name="body" rows="18"`) {
		t.Errorf("a body is not drawn as tall as the catalog says: %s", got)
	}
	if got := draw(t, action("capture", map[string]string{"day": "2026-01-01"}, nil), "autofocus", "text"); strings.Count(got, "autofocus") != 1 ||
		!strings.Contains(got, `name="text" rows="4" autofocus`) {
		t.Errorf("the browser does not start in the field the view named: %s", got)
	}
}

// TestOptionalFieldsFoldUnderMoreFields: the optional fields a view names are drawn inside the form under "More
// fields", with their values, so a browser sends them with no script; a required field is never folded; folded
// fields with a value are drawn open; a folded file field still makes the form multipart
// (docs/plans/095-the-leaf-and-its-margin.md).
func TestOptionalFieldsFoldUnderMoreFields(t *testing.T) {
	draw := func(t *testing.T, a Action, opts ...string) (string, *formView) {
		t.Helper()
		f, err := formOf(a, opts...)
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		if err := pages.ExecuteTemplate(&b, "form", f); err != nil {
			t.Fatal(err)
		}
		return b.String(), f
	}
	more := regexp.MustCompile(`(?s)<details class="more-fields"( open)?><summary>([^<]*)</summary>(.*?)</details>`)
	got, f := draw(t, action("record", nil, map[string]any{"day": "2026-10-12"}), "more", "metric,session_id,taken_at,tz,import_key")
	m := more.FindStringSubmatch(got)
	if m == nil {
		t.Fatalf("no More fields in the form: %s", got)
	}
	if m[1] != "" || m[2] != "4 more fields" {
		t.Errorf("More fields: open %q, summary %q; want closed, %q", m[1], m[2], "4 more fields")
	}
	for _, name := range []string{"session_id", "taken_at", "tz", "import_key"} {
		if !strings.Contains(m[3], `name="`+name+`"`) {
			t.Errorf("%s is not folded: %s", name, m[3])
		}
	}
	if strings.Contains(m[3], `name="metric"`) || !strings.Contains(got, `name="metric"`) {
		t.Errorf("the required metric is folded or gone: %s", got)
	}
	if !strings.Contains(got, `<input type="date" name="day" value="2026-10-12" required>`) || !f.Primary ||
		!strings.Contains(got, `</details>
<button class="primary">`) || strings.Index(got, "</details>") > strings.Index(got, "<button") {
		t.Errorf("the shown fields, or the button after the folded ones, are not drawn: %s", got)
	}
	got, _ = draw(t, action("record", nil, map[string]any{"tz": "Europe/Bucharest"}), "more", "taken_at,tz")
	if m := more.FindStringSubmatch(got); m == nil || m[1] != " open" || m[2] != "2 more fields" ||
		!strings.Contains(m[3], `name="tz" value="Europe/Bucharest"`) {
		t.Errorf("a folded field with a value is out of sight: %s", got)
	}
	if got, _ = draw(t, action("add-file", nil, nil), "more", "preview"); !strings.Contains(got, `enctype="multipart/form-data"`) ||
		!more.MatchString(got) || !strings.Contains(more.FindStringSubmatch(got)[3], `type="file" name="preview"`) {
		t.Errorf("a form with a folded file is not multipart: %s", got)
	}
	if got, _ = draw(t, action("record", nil, nil)); strings.Contains(got, "more-fields") {
		t.Errorf("a form whose view folds nothing has More fields: %s", got)
	}
}

// TestTheChartSaysItsEndsInWords: the ends of a series' chart are dates in words with their year, on the axis and
// for a reader, not ISO days (docs/plans/095-the-leaf-and-its-margin.md). 14 July 2026 is a Tuesday: 12 October 2026
// is a Monday, 90 days later.
func TestTheChartSaysItsEndsInWords(t *testing.T) {
	got := string(chart("2026-07-14", "2026-10-12", []any{
		map[string]any{"day": "2026-09-29", "value": 78.4}, map[string]any{"day": "2026-10-12", "value": 77.1}}))
	for _, want := range []string{`aria-label="2 readings from Tue 14 Jul 2026 to Mon 12 Oct 2026"`, `>Tue 14 Jul 2026</text>`, `>Mon 12 Oct 2026</text>`} {
		if !strings.Contains(got, want) {
			t.Errorf("the chart lacks %s:\n%s", want, got)
		}
	}
	if strings.Contains(got, "2026-07-14") || strings.Contains(got, "2026-10-12") {
		t.Errorf("the chart shows an ISO day:\n%s", got)
	}
}

// TestNavMarksThePageTheBrowserIsOn says one navigation link is current: the one whose path is the page's, else the
// one with the longest path the page is under, and never a fragment (a place on a page is not a page).
func TestNavMarksThePageTheBrowserIsOn(t *testing.T) {
	for path, want := range map[string]string{
		"/":                "/",
		"/days":            "/months/current",
		"/days/2026-01-01": "/months/current",
		"/days/today":      "/days/today",
		"/months/2026-10":  "/months/current",
		"/weeks/2026-W41":  "/months/current",
		"/years/2026":      "/months/current",
		"/deadlines":       "/deadlines",
		"/integrity":       "/integrity",
		"/people":          "/people",
		"/metrics/TSH":     "/metrics",
		"/tasks/7":         "/tasks",
		"/pages/7":         "",
		"/search":          "",
		"/import/prepared": "",
		"/measurements/1":  "",
	} {
		var got string
		for _, it := range (&view{Path: path}).Nav() {
			if it.Current {
				if got != "" {
					t.Errorf("%s marks both %s and %s current", path, got, it.Href)
				}
				got = it.Href
			}
		}
		if got != want {
			t.Errorf("%s marks %q current, want %q", path, got, want)
		}
	}
	for _, it := range (&view{Path: "/"}).Nav() {
		if it.Href == "/#new" && it.Current {
			t.Error("a fragment link is marked current")
		}
	}
}

// TestMarkShowsTheMatchedWords says a search snippet marks what matched, and that the snippet's own text can
// never become markup.
func TestMarkShowsTheMatchedWords(t *testing.T) {
	for src, want := range map[string]string{
		"…ran ⟦5k⟧ with…":                  "…ran <mark>5k</mark> with…",
		"<b>⟦x⟧</b>":                       "&lt;b&gt;<mark>x</mark>&lt;/b&gt;",
		"<script>alert(1)</script>":        "&lt;script&gt;alert(1)&lt;/script&gt;",
		"no match here":                    "no match here",
		"⟦⟧":                               "⟦⟧",
		"the [[⟦Garden⟧ project]] - [x] a": "the <mark>Garden</mark> project - [x] a",
	} {
		if got := string(mark(src)); got != want {
			t.Errorf("mark(%q) = %q, want %q", src, got, want)
		}
	}
}

// TestASnippetIsPlainText says a search snippet shows the words of a body without its CommonMark: the markers of a
// heading, a quote, a list item and a task box at the start of a line, the brackets of a wikilink and of an embed are
// cut, and the lines are joined with a space; a #tag keeps its sign, and a mark keeps its place.
func TestASnippetIsPlainText(t *testing.T) {
	for src, want := range map[string]string{
		"Raised beds.\n\n- [x] Order cedar boards\n- [ ] Fill with ⟦soil⟧": "Raised beds. Order cedar boards Fill with <mark>soil</mark>",
		"Coffee with [[Ana]]; the [[⟦Garden⟧ project]].":                   "Coffee with Ana; the <mark>Garden</mark> project.",
		"## A ⟦heading⟧\n> a quote\n* one\n+ two":                          "A <mark>heading</mark> a quote one two",
		"#running at the lake\n![[sunset.png]]":                            "#running at the lake sunset.png",
		"a [link](x) and [x] in a line":                                    "a [link](x) and [x] in a line",
	} {
		if got := string(mark(src)); got != want {
			t.Errorf("mark(%q) = %q, want %q", src, got, want)
		}
	}
}

// TestTheBaseSheetCarriesNoAppVocabulary keeps the two sheets apart: base.css styles ordinary HTML, and this
// app's own names live in app.css alone, so a page reads as HTML with either file alone. Comments are cut first:
// the rule is about selectors, not about prose that names one.
func TestTheBaseSheetCarriesNoAppVocabulary(t *testing.T) {
	base := cssComments.ReplaceAllString(string(baseCSS), "")
	for _, name := range []string{".wikilink", ".badge", ".chart", ".pager", ".plain", ".prose", ".sub", ".skip",
		".tiny", ".inline", ".danger", ".quiet", ".desc", ".group", ".done", ".notdone", ".scroll", ".brand",
		".occ", ".outcomes", ".settled", ".calendar", ".mood", ".more", ".here"} {
		if strings.Contains(base, name) {
			t.Errorf("base.css styles %s, which belongs to app.css: the base sheet is ordinary HTML", name)
		}
	}
	app := cssComments.ReplaceAllString(string(appCSS), "")
	for _, want := range []string{"a.wikilink", "svg.chart", ".badge", "nav.pager", "form.tiny"} {
		if !strings.Contains(app, want) {
			t.Errorf("app.css lost %s: the app's own vocabulary has no home", want)
		}
	}
	for _, want := range []string{"table", "label", "button", "details", "article", "aside", "@media print"} {
		if !strings.Contains(base, want) {
			t.Errorf("base.css does not style %s: ordinary HTML would be bare", want)
		}
	}
}

// cssComments cuts a stylesheet's comments, so a test reads its rules and not its prose.
var cssComments = regexp.MustCompile(`(?s)/\*.*?\*/`)

// TestTheSheetsAreLayered: the two sheets are one cascade in four layers (docs/plans/108-the-browser-made-current.md),
// declared once at the top of base.css before anything but the fonts, which stay outside the layers; app.css holds
// the two last layers and never repeats the declaration, which would be a second home for the order.
func TestTheSheetsAreLayered(t *testing.T) {
	base := strings.TrimSpace(cssComments.ReplaceAllString(string(baseCSS), ""))
	app := cssComments.ReplaceAllString(string(appCSS), "")
	if !strings.HasPrefix(base, "@layer tokens, base, components, utilities;") {
		t.Errorf("base.css does not start with the layer order: %.80q", base)
	}
	before := base[:strings.Index(base, "@layer tokens{")]
	if rest := regexp.MustCompile(`(?s)@layer [^;]+;|@font-face\{[^}]*\}|\s`).ReplaceAllString(before, ""); rest != "" {
		t.Errorf("base.css has a rule outside the layers before the tokens: %.80q", rest)
	}
	for _, want := range []string{"@layer tokens{", "@layer base{"} {
		if strings.Count(base, want) != 1 {
			t.Errorf("base.css opens %s %d times, want once", want, strings.Count(base, want))
		}
	}
	for _, want := range []string{"@layer components{", "@layer utilities{"} {
		if strings.Count(app, want) != 1 {
			t.Errorf("app.css opens %s %d times, want once", want, strings.Count(app, want))
		}
	}
	if strings.Contains(app, "@layer tokens") || strings.Contains(app, "@layer base") || regexp.MustCompile(`@layer [^{]*;`).MatchString(app) {
		t.Error("app.css declares a layer order or a layer of base.css")
	}
}

// TestTheSheetsUseLogicalProperties: a rule says inline and block, start and end, never left, right, top and
// bottom, so the sheets read the same in any writing direction. A value stays physical where the property is a
// position of a background or a shadow; only a property name is checked here.
func TestTheSheetsUseLogicalProperties(t *testing.T) {
	physical := []*regexp.Regexp{
		regexp.MustCompile(`(^|[{;\s])(margin|padding|border)-(left|right|top|bottom)(-[a-z]+)?\s*:`),
		regexp.MustCompile(`(^|[{;\s])(left|right|top|bottom|width|height|min-width|max-width|min-height|max-height)\s*:`),
		regexp.MustCompile(`(^|[{;\s])(text-align|float)\s*:\s*(left|right)\b`),
	}
	for name, sheet := range map[string]string{"base.css": string(baseCSS), "app.css": string(appCSS)} {
		rules := cssComments.ReplaceAllString(sheet, "")
		for _, re := range physical {
			for _, m := range re.FindAllString(rules, -1) {
				t.Errorf("%s uses a physical property: %q", name, strings.TrimSpace(m))
			}
		}
	}
}

// TestTheSheetsQueryTheColumnNotTheScreen: a layout inside the page asks the width of its column (@container
// column), not of the screen, so it is right beside an open sheet and in a narrow window alike; a width media query
// stays only where the screen is the subject: the phone's bars and the touch size of a control (899 px), the shell's
// own columns and the top layer. The title and the sheet scale with the column by clamp(), not by a step.
func TestTheSheetsQueryTheColumnNotTheScreen(t *testing.T) {
	widths := func(sheet string) string {
		seen := map[string]bool{}
		var out []string
		for _, m := range regexp.MustCompile(`@media \(max-width:(\d+px)\)`).FindAllStringSubmatch(cssComments.ReplaceAllString(sheet, ""), -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				out = append(out, m[1])
			}
		}
		return strings.Join(out, " ")
	}
	if got := widths(string(baseCSS)); got != "899px" {
		t.Errorf("base.css asks the screen's width at %s, want 899px alone", got)
	}
	if got := widths(string(appCSS)); got != "899px" {
		t.Errorf("app.css asks the screen's width at %s, want 899px alone", got)
	}
	app := cssComments.ReplaceAllString(string(appCSS), "")
	if n := strings.Count(app, "@container column ("); n < 8 {
		t.Errorf("app.css asks its column %d times, want at least 8", n)
	}
	if !strings.Contains(app, "container:column / inline-size") {
		t.Error("app.css does not make the column a container")
	}
	base := cssComments.ReplaceAllString(string(baseCSS), "")
	for _, want := range []string{"h1{font:400 clamp(", "h2{font-size:clamp(", "@container (max-width:608px)"} {
		if !strings.Contains(base, want) {
			t.Errorf("base.css lacks %s", want)
		}
	}
	for _, want := range []string{".leaf{flex:none; display:flex; flex-direction:column; inline-size:clamp(", "--sheet-gap:clamp("} {
		if !strings.Contains(app, want) {
			t.Errorf("app.css lacks %s", want)
		}
	}
}

// TestTheMotionsAreOffUnderReducedMotion: every entry, exit and scroll-driven motion of the sheets is inside a block
// that asks for motion (prefers-reduced-motion:no-preference), and the scroll-driven one inside @supports as well,
// so a browser that does not want motion, or cannot drive it by the scroll, gets none.
func TestTheMotionsAreOffUnderReducedMotion(t *testing.T) {
	for name, sheet := range map[string]string{"base.css": string(baseCSS), "app.css": string(appCSS)} {
		t.Run(name, func(t *testing.T) { motionsAskFirst(t, cssComments.ReplaceAllString(sheet, ""), name == "app.css") })
	}
}

// motionsAskFirst checks one sheet; scroll says the sheet must hold the scroll-driven animation.
func motionsAskFirst(t *testing.T, app string, scroll bool) {
	// enclosing is the preludes of the blocks around the index, outermost first.
	enclosing := func(at int) []string {
		var stack []string
		start := 0
		for i := 0; i < at; i++ {
			switch app[i] {
			case '{':
				stack = append(stack, strings.TrimSpace(app[start:i]))
				start = i + 1
			case '}':
				stack = stack[:len(stack)-1]
				start = i + 1
			case ';':
				start = i + 1
			}
		}
		return stack
	}
	within := func(stack []string, want string) bool {
		for _, p := range stack {
			if strings.Contains(p, want) {
				return true
			}
		}
		return false
	}
	for _, token := range []string{"@starting-style", "allow-discrete", "animation-timeline:scroll(root)"} {
		if !strings.Contains(app, token) && (scroll || token != "animation-timeline:scroll(root)") {
			t.Errorf("the sheet has no %s", token)
		}
		for at := strings.Index(app, token); at >= 0; at = strings.Index(app, token) {
			stack := enclosing(at)
			if !within(stack, "prefers-reduced-motion:no-preference") {
				t.Errorf("%s at %q is outside a block that asks for motion", token, strings.Join(stack, " > "))
			}
			if strings.HasPrefix(token, "animation-timeline:") && !within(stack, "@supports (animation-timeline:scroll())") {
				t.Errorf("%s at %q is outside @supports", token, strings.Join(stack, " > "))
			}
			app = app[:at] + strings.Repeat(" ", len(token)) + app[at+len(token):]
		}
	}
}

// TestTheFieldsSayWhatTheBrowserRefuses: ordinary HTML, so base.css: a field the browser refuses (:user-invalid)
// and one the server refused (aria-invalid) share one rule, and the select is the app's own where a sheet may draw
// it (appearance:base-select), behind @supports so the native one stays elsewhere.
func TestTheFieldsSayWhatTheBrowserRefuses(t *testing.T) {
	base := cssComments.ReplaceAllString(string(baseCSS), "")
	app := cssComments.ReplaceAllString(string(appCSS), "")
	for _, want := range []string{":is(input,textarea,select):is([aria-invalid=true],:user-invalid){border-color:var(--danger-line)}",
		"@supports (appearance:base-select){", "::picker(select){", "select::picker-icon{"} {
		if !strings.Contains(base, want) {
			t.Errorf("base.css lacks %s", want)
		}
	}
	if strings.Contains(app, "aria-invalid") || strings.Contains(app, "base-select") {
		t.Error("app.css styles a field's refusal or the select: ordinary HTML belongs to base.css")
	}
	if strings.Count(base, "base-select") != 2 {
		t.Errorf("base.css names base-select %d times, want the @supports and the one declaration", strings.Count(base, "base-select"))
	}
}

// TestAClosedToastIsNotShown: the toast is a dialog, and a closed dialog is the browser's display:none, which a
// display on the element itself would beat; so the sheet sets the toast's display only while it is open (the
// owner's Close once closed the dialog and left the box in view).
func TestAClosedToastIsNotShown(t *testing.T) {
	app := cssComments.ReplaceAllString(string(appCSS), "")
	if !strings.Contains(app, ".toast[open]{display:flex}") {
		t.Error("app.css does not tie the toast's display to its open state")
	}
	for _, m := range regexp.MustCompile(`\.toast\{[^{}]*`).FindAllString(app, -1) {
		if strings.Contains(m, "display:") {
			t.Errorf("app.css sets the toast's display outside [open]: %.80s", m)
		}
	}
}
