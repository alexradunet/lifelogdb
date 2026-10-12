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
		"/habits":          "/habits",
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
		"…ran [5k] with…":           "…ran <mark>5k</mark> with…",
		"<b>[x]</b>":                "&lt;b&gt;<mark>x</mark>&lt;/b&gt;",
		"<script>alert(1)</script>": "&lt;script&gt;alert(1)&lt;/script&gt;",
		"no match here":             "no match here",
		"[]":                        "[]",
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

// TestTheScriptReadsTitlesRenderAndTheRootOnly keeps the script's promise in its first comment: it reads /titles,
// /render and / (the root's actions, for the command list of docs/plans/094-the-command-list.md) and nothing else,
// and it keeps nothing in the browser.
func TestTheScriptReadsTitlesRenderAndTheRootOnly(t *testing.T) {
	js := string(appJS)
	calls := regexp.MustCompile(`fetch\(\s*"([^"?]*)`).FindAllStringSubmatch(js, -1)
	read := map[string]bool{}
	for _, c := range calls {
		read[c[1]] = true
	}
	if len(calls) != strings.Count(js, "fetch(") || len(read) != 3 || !read["/titles"] || !read["/render"] || !read["/"] {
		t.Errorf("the script fetches %v (%d calls), want /titles, /render and / only", read, strings.Count(js, "fetch("))
	}
	for _, never := range []string{"XMLHttpRequest", "WebSocket", "sendBeacon", "localStorage", "sessionStorage", "indexedDB",
		"document.cookie", "import(", "eval(", "new Function", "http:", "https:"} {
		if strings.Contains(js, never) {
			t.Errorf("the script uses %s", never)
		}
	}
}

// TestTheScriptDeclaresEachFunctionOnce: the script is one function scope, so a second function of the same name
// silently replaces the first (the command list's rows once replaced the suggestions after [[).
func TestTheScriptDeclaresEachFunctionOnce(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`function ([A-Za-z_$][\w$]*)\s*\(`).FindAllStringSubmatch(string(appJS), -1) {
		if seen[m[1]] {
			t.Errorf("the script declares %s twice", m[1])
		}
		seen[m[1]] = true
	}
}
