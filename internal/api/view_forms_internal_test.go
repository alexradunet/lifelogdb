package api

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

// drawForm draws the action with the template form, as a page draws it, with the options of formOf.
func drawForm(t *testing.T, a Action, opts ...string) (string, *formView) {
	t.Helper()
	f, err := formOf(a, opts...)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := templates.ExecuteTemplate(&b, "form", f); err != nil {
		t.Fatal(err)
	}
	return b.String(), f
}

func TestBrowserFieldPresence(t *testing.T) {
	a := Action{Name: "fields", Method: "POST", Href: "/fields", Fields: []Field{
		{Name: "hidden", Type: "hidden", Value: 0},
		{Name: "optional", Type: "number"},
		{Name: "default", Type: "number", Value: 5},
		{Name: "textarea", Type: "textarea", Value: 0},
		{Name: "choice", Type: "number", Value: 0, Options: []string{"", "0", "1"}},
	}}
	got, _ := drawForm(t, a)
	for _, want := range []string{`name="hidden" value="0"`, `name="optional" value=""`, `name="default" value="5"`, `>0</textarea>`, `<option selected>0</option>`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s", want)
		}
	}
}

// TestTheCatalogMarksTheFieldOfAPageTitle: the catalog says which field takes a page's title (Pages), so a client
// can offer the names of the pages: one field of each action that names a page.
func TestTheCatalogMarksTheFieldOfAPageTitle(t *testing.T) {
	for _, tc := range []struct{ action, field string }{
		{"create-page", "category"}, {"link", "to"}, {"unlink", "to"}, {"find", "title"}, {"file-pages", "category"},
	} {
		var marked []string
		for _, fd := range action(tc.action, nil, nil).Fields {
			if fd.Pages {
				marked = append(marked, fd.Name)
			}
		}
		if len(marked) != 1 || marked[0] != tc.field {
			t.Errorf("%s marks %v as a page's title, want [%s]", tc.action, marked, tc.field)
		}
	}
}

// TestPreviewKeysOnATextArea: a form offers Preview when it has a text area, shown or folded, whatever the area's
// name; a field of another type named body does not make it offer Preview.
func TestPreviewKeysOnATextArea(t *testing.T) {
	for _, c := range []struct {
		name   string
		fields []Field
		opts   []string
		want   bool
	}{
		{"a text area named note", []Field{{Name: "note", Type: "textarea"}}, nil, true},
		{"a folded text area", []Field{{Name: "title", Type: "text", Required: true}, {Name: "note", Type: "textarea"}}, []string{"more", "note"}, true},
		{"an input named body", []Field{{Name: "body", Type: "text"}}, nil, false},
		{"no field", nil, nil, false},
	} {
		f, err := formOf(Action{Name: "x", Method: "POST", Href: "/x", Fields: c.fields}, c.opts...)
		if err != nil {
			t.Fatal(err)
		}
		if got := f.Preview(); got != c.want {
			t.Errorf("%s: Preview %v, want %v", c.name, got, c.want)
		}
	}
}

// TestEveryTextAreaOfTheCatalogOffersPreview: a form of the catalog offers Preview when it has a text area, and only
// then. POST /render reads the field body or text, so each text area of the catalog has one of these names.
func TestEveryTextAreaOfTheCatalogOffersPreview(t *testing.T) {
	areas := 0
	for _, s := range catalog {
		f, err := formOf(templated(s))
		if err != nil {
			t.Fatal(err)
		}
		area := false
		for _, fd := range s.Fields {
			if fd.Type == "textarea" {
				area = true
				areas++
				if fd.Name != "body" && fd.Name != "text" {
					t.Errorf("%s: the text area %s is not body or text, which POST /render reads", s.Name, fd.Name)
				}
			}
		}
		if f.Preview() != area {
			t.Errorf("%s: Preview %v, want %v", s.Name, f.Preview(), area)
		}
	}
	if areas == 0 {
		t.Error("the catalog has no text area: the test checks nothing")
	}
}

// TestSegmentedIsForAFewShortOptions: a field is drawn as a row of radio buttons only when it has 2 to 5 options,
// none empty and each of 3 characters or fewer; any other field with options is a select.
func TestSegmentedIsForAFewShortOptions(t *testing.T) {
	for _, tc := range []struct {
		options []string
		want    bool
	}{
		{[]string{"1", "2", "3", "4", "5"}, true},
		{[]string{"1", "0"}, true},
		{[]string{"yes", "no"}, true},
		{[]string{"1"}, false},
		{[]string{"1", "2", "3", "4", "5", "6"}, false},
		{[]string{"", "0", "1"}, false},
		{[]string{"open", "done", "skipped"}, false},
		{nil, false},
	} {
		if got := segmented(Field{Name: "f", Type: "text", Options: tc.options}); got != tc.want {
			t.Errorf("segmented(%q) = %v, want %v", tc.options, got, tc.want)
		}
	}
}

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
	if got, _ := drawForm(t, action("tombstone", map[string]string{"id": "7"}, nil)); !strings.Contains(got, `<button class="danger">`) {
		t.Errorf("a tombstone draws no danger button: %s", got)
	}
	if got, _ := drawForm(t, action("revive", map[string]string{"id": "7"}, nil)); strings.Contains(got, "danger") {
		t.Errorf("a revive draws a danger button: %s", got)
	}
	if got, _ := drawForm(t, action("save-body", map[string]string{"id": "7"}, map[string]any{"body": "words", "version": "3"})); !strings.Contains(got, `name="body" rows="18"`) {
		t.Errorf("a body is not drawn as tall as the catalog says: %s", got)
	}
	if got, _ := drawForm(t, action("capture", map[string]string{"day": "2026-01-01"}, nil), "autofocus", "text"); strings.Count(got, "autofocus") != 1 ||
		!strings.Contains(got, `name="text" rows="4" autofocus`) {
		t.Errorf("the browser does not start in the field the view named: %s", got)
	}
}

// TestOptionalFieldsFoldUnderMoreFields: the optional fields a view names are drawn inside the form under "More
// fields", with their values, so a browser sends them with no script; a required field is never folded; folded
// fields with a value are drawn open; a folded file field still makes the form multipart
// (docs/plans/095-the-leaf-and-its-margin.md).
func TestOptionalFieldsFoldUnderMoreFields(t *testing.T) {
	more := regexp.MustCompile(`(?s)<details class="more-fields"( open)?><summary>([^<]*)</summary>(.*?)</details>`)
	got, f := drawForm(t, action("record", nil, map[string]any{"day": "2026-10-12"}), "more", "metric,taken_at,tz,import_key")
	m := more.FindStringSubmatch(got)
	if m == nil {
		t.Fatalf("no More fields in the form: %s", got)
	}
	if m[1] != "" || m[2] != "3 more fields" {
		t.Errorf("More fields: open %q, summary %q; want closed, %q", m[1], m[2], "3 more fields")
	}
	for _, name := range []string{"taken_at", "tz", "import_key"} {
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
	got, _ = drawForm(t, action("record", nil, map[string]any{"tz": "Europe/Bucharest"}), "more", "taken_at,tz")
	if m := more.FindStringSubmatch(got); m == nil || m[1] != " open" || m[2] != "2 more fields" ||
		!strings.Contains(m[3], `name="tz" value="Europe/Bucharest"`) {
		t.Errorf("a folded field with a value is out of sight: %s", got)
	}
	if got, _ = drawForm(t, action("add-file", nil, nil), "more", "preview"); !strings.Contains(got, `enctype="multipart/form-data"`) ||
		!more.MatchString(got) || !strings.Contains(more.FindStringSubmatch(got)[3], `type="file" name="preview"`) {
		t.Errorf("a form with a folded file is not multipart: %s", got)
	}
	if got, _ = drawForm(t, action("record", nil, nil)); strings.Contains(got, "more-fields") {
		t.Errorf("a form whose view folds nothing has More fields: %s", got)
	}
}
