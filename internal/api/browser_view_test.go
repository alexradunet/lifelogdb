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
	known := map[string]bool{}
	for _, s := range append(append([]spec{}, catalog...), importCatalog...) {
		known[s.Name] = true
	}
	for name := range dangerous {
		if !known[name] {
			t.Errorf("dangerous names %q, which is no catalog action", name)
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

// TestNavMarksThePageTheBrowserIsOn says one navigation link is current: the longest one that fits the path, and
// never a fragment (a place on a page is not a page).
func TestNavMarksThePageTheBrowserIsOn(t *testing.T) {
	for path, want := range map[string]string{
		"/":                "/",
		"/days":            "/days",
		"/days/2026-01-01": "/days",
		"/days/today":      "/days/today",
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
		".tiny", ".inline", ".danger", ".quiet", ".desc", ".group", ".done", ".notdone", ".scroll", ".brand"} {
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
