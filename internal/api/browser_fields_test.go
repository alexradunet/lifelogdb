package api

import (
	"bytes"
	"strings"
	"testing"
)

func TestBrowserFieldPresence(t *testing.T) {
	a := Action{Name: "fields", Method: "POST", Href: "/fields", Fields: []Field{
		{Name: "hidden", Type: "hidden", Value: 0},
		{Name: "optional", Type: "number"},
		{Name: "default", Type: "number", Value: 5},
		{Name: "textarea", Type: "textarea", Value: 0},
		{Name: "choice", Type: "number", Value: 0, Options: []string{"", "0", "1"}},
	}}
	f, err := formOf(a)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := templates.ExecuteTemplate(&b, "form", f); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`name="hidden" value="0"`, `name="optional" value=""`, `name="default" value="5"`, `>0</textarea>`, `<option selected>0</option>`} {
		if !strings.Contains(b.String(), want) {
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
