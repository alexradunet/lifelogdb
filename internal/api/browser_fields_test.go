package api

import (
	"bytes"
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
