package api

import (
	"bytes"
	"net/url"
	"regexp"
	"slices"
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
// none empty and each of 3 characters or fewer; any other field with options is a select. A flag (the options 0 and
// 1) is one checkbox, unless the action requires it.
func TestSegmentedIsForAFewShortOptions(t *testing.T) {
	for _, tc := range []struct {
		options []string
		want    bool
	}{
		{[]string{"1", "2", "3", "4", "5"}, true},
		{[]string{"1", "0"}, true},
		{[]string{"0", "1"}, false},
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
	if segmented(Field{Name: "f", Type: "text", Pages: true, Options: []string{"Art", "Gym"}}) {
		t.Error("a field of two short page titles is segmented: it takes a new title too")
	}
	if !segmented(Field{Name: "f", Type: "number", Required: true, Options: []string{"0", "1"}}) {
		t.Error("a required flag is not segmented: a checkbox cannot send 0")
	}
}

// TestAFlagIsOneCheckbox: a field of the options 0 and 1 is one checkbox before the field's title. It sends 1 when it
// is checked and nothing when it is not, which the server reads as 0; a value of 1 draws it checked.
func TestAFlagIsOneCheckbox(t *testing.T) {
	for _, tc := range []struct {
		move string
		want string
	}{
		{"", `<label><input type="checkbox" name="move" value="1"><span>`},
		{"0", `<label><input type="checkbox" name="move" value="1"><span>`},
		{"1", `<label><input type="checkbox" name="move" value="1" checked><span>`},
	} {
		got := markup(drawForm(t, action("file-pages", nil, map[string]any{"move": tc.move})))
		if want := tc.want + "Move (remove the other categories)</span></label>"; !strings.Contains(got, want) || strings.Contains(got, `type="radio"`) {
			t.Errorf("move %q: the form lacks %s, or has radio buttons:\n%s", tc.move, want, got)
		}
	}
}

// TestAFieldOfPageTitlesOffersThemInADatalist: a field of page titles with options is a text input with a datalist
// of the titles, so the reader picks one or types a new one. The datalist's id is the action's name and the field's,
// or the form's id and the field's. With no options it is a plain input; a field with options that does not take a
// page's title is still a select. Titles that look like a flag (0 and 1) in a field that is not required are still
// titles: an input with a datalist, not a checkbox.
func TestAFieldOfPageTitlesOffersThemInADatalist(t *testing.T) {
	file := withOptions(action("file-pages", nil, nil), "category", []string{"Art", "Gym"})
	flagLike := withOptions(action("register-metric", nil, nil), "category", []string{"0", "1"})
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"named by its action", markup(drawForm(t, file)),
			`<input type="text" name="category" value="" required list="file-pages-category"><datalist id="file-pages-category"><option value="Art"><option value="Gym"></datalist></label>`},
		{"named by its form", markup(drawForm(t, file, "id", "pick")),
			`<input type="text" name="category" value="" required list="pick-category"><datalist id="pick-category"><option value="Art"><option value="Gym"></datalist></label>`},
		{"with no options", markup(drawForm(t, action("file-pages", nil, nil))),
			`<input type="text" name="category" value="" required></label>`},
		{"not a page's title", markup(drawForm(t, action("promote", map[string]string{"id": "7"}, nil))),
			`<select name="to"><option>person</option><option>place</option></select>`},
		{"the titles 0 and 1", markup(drawForm(t, flagLike)),
			`<input type="text" name="category" value="" list="register-metric-category"><datalist id="register-metric-category"><option value="0"><option value="1"></datalist></label>`},
	} {
		if !strings.Contains(tc.got, tc.want) {
			t.Errorf("%s: the form lacks %s:\n%s", tc.name, tc.want, tc.got)
		}
	}
	if got := markup(drawForm(t, action("file-pages", nil, nil))); strings.Contains(got, "datalist") || strings.Contains(got, " list=") {
		t.Errorf("a field with no options has a datalist: %s", got)
	}
}

// markup is the first value of drawForm: the markup, without the formView.
func markup(s string, _ *formView) string { return s }

// TestOrSendsTheFormToASecondAction: the pick bar's form is file-pages with an id, whose ids the boxes outside it
// send, and Delete pages is its second button: it sends the same form to tombstone-pages with no check of the
// category. When the delete is the refused form, so is the bar's. With no second action, the form has one button.
func TestOrSendsTheFormToASecondAction(t *testing.T) {
	draw := func(f *formView) string {
		t.Helper()
		var b bytes.Buffer
		if err := templates.ExecuteTemplate(&b, "form", f); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	form := func(a *shownAction, opts ...string) *formView {
		t.Helper()
		f, err := formOf(a, opts...)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	file := &shownAction{Action: withOptions(action("file-pages", nil, nil), "category", []string{"Art"})}
	del := &shownAction{Action: action("tombstone-pages", nil, nil)}

	got := draw(form(file, "id", "pick", "outside", "ids", "back", "/browse").Or(form(del)))
	for _, want := range []string{`<form id="pick" method="POST" action="/pages/file">`, `<input type="hidden" name="back" value="/browse">`,
		`<button class="primary">Put in a category</button><button class="danger" formaction="/pages/tombstone" formnovalidate>Delete pages</button></form>`} {
		if !strings.Contains(got, want) {
			t.Errorf("the form lacks %s:\n%s", want, got)
		}
	}
	if strings.Contains(got, `name="ids"`) || strings.Contains(got, "aria-describedby") {
		t.Errorf("the form draws the ids that the boxes send, or is described by an alert: %s", got)
	}

	refused := &shownAction{Action: del.Action, refused: true, noFocus: true}
	if got := draw(form(file, "id", "pick").Or(form(refused))); !strings.Contains(got, `<form id="pick" method="POST" action="/pages/file" aria-describedby="refused">`) {
		t.Errorf("a refused delete does not mark the bar's form: %s", got)
	}
	if got := draw(form(file).Or(nil)); strings.Count(got, "<button") != 1 || strings.Contains(got, "formaction") {
		t.Errorf("a form with no second action has another button: %s", got)
	}
}

// TestAnEmptyAutofocusKeepsTheFocusOut: a form the page's link asks for starts in its first field, unless the view
// names the field, or none ("autofocus" ""): the New metric fold draws one action in three forms, and only the first
// takes the focus.
func TestAnEmptyAutofocusKeepsTheFocusOut(t *testing.T) {
	asked := &shownAction{Action: action("register-metric", nil, nil), asked: true}
	for _, tc := range []struct {
		opts []string
		want string
	}{
		{nil, "name"},
		{[]string{"autofocus", "note"}, "note"},
		{[]string{"autofocus", ""}, ""},
	} {
		f, err := formOf(asked, tc.opts...)
		if err != nil {
			t.Fatal(err)
		}
		if f.Focus != tc.want {
			t.Errorf("options %q: the focus is in %q, want %q", tc.opts, f.Focus, tc.want)
		}
	}
}

// TestARefusalFillsTheFormThatWasSent: of the forms of a refused action, the form that was sent is refused: it holds
// what it sent and the alert describes it. A form with an id is that form only when the refusal sent its id (form); a
// form with no id, or a refusal that sent no id, is that form too. A field that the form fixes keeps its fixed value,
// not the value that was sent: a refusal that sent no id fills the habit's form too, and its unit stays empty. A sent
// file is never drawn back, and no form of a page that shows a refusal takes the focus.
func TestARefusalFillsTheFormThatWasSent(t *testing.T) {
	refusal := func(sent url.Values) *shownAction {
		return &shownAction{Action: action("register-metric", nil, nil), refused: true, sent: sent, noFocus: true}
	}
	byScore := refusal(url.Values{"form": {"new-score"}, "name": {"Mood"}, "unit": {"1-10"}})
	for _, tc := range []struct {
		what string
		c    *shownAction
		opts []string
		want bool
	}{
		{"the form that was sent", byScore, []string{"id", "new-score"}, true},
		{"another form of the action", byScore, []string{"id", "new-number"}, false},
		{"a form with no id", byScore, nil, true},
		{"a refusal that sent no id", refusal(url.Values{"name": {"Mood"}, "unit": {"1-10"}}), []string{"id", "new-number"}, true},
	} {
		f, err := formOf(tc.c, tc.opts...)
		if err != nil {
			t.Fatal(err)
		}
		filled := map[string]any{}
		for _, fd := range f.Shown {
			filled[fd.Name] = fd.Value
		}
		if f.Refused != tc.want || (filled["name"] == "Mood" && filled["unit"] == "1-10") != tc.want || f.Focus != "" {
			t.Errorf("%s: refused %v, fields %v, focus %q; want refused and filled %v, no focus", tc.what, f.Refused, filled, f.Focus, tc.want)
		}
	}
	habit, err := formOf(refusal(url.Values{"name": {"Mood"}, "unit": {"1-10"}}), "id", "new-habit", "unit", "")
	if err != nil {
		t.Fatal(err)
	}
	unit := slices.IndexFunc(habit.Hidden, func(fd Field) bool { return fd.Name == "unit" })
	if !habit.Refused || unit < 0 || habit.Hidden[unit].Value != "" {
		t.Errorf("a refusal that sent no id and the unit 1-10: the habit's form is refused %v, its hidden fields %v; want refused, and the unit hidden and empty", habit.Refused, habit.Hidden)
	}
	file := &shownAction{Action: action("add-file", nil, nil), refused: true, sent: url.Values{"title": {"Scan"}, "original": {"scan.jpg"}}}
	f, err := formOf(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range f.Shown {
		if (fd.Name == "title" && fd.Value != "Scan") || (fd.Name == "original" && fd.Value != nil) {
			t.Errorf("a refused add-file draws %s with %v: want the title it sent, and no file", fd.Name, fd.Value)
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
