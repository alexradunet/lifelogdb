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
	if err := pages.ExecuteTemplate(&b, "form", f); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`name="hidden" value="0"`, `name="optional" value=""`, `name="default" value="5"`, `>0</textarea>`, `<option selected>0</option>`} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("missing %s", want)
		}
	}
}

// TestAFieldForAPageTitleCarriesDataPages: the catalog says which fields take a page's title (Pages), the form
// marks them with data-pages, and the script hangs the list of names after [[ on exactly those inputs.
func TestAFieldForAPageTitleCarriesDataPages(t *testing.T) {
	var b bytes.Buffer
	f, err := formOf(action("create-task", nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := pages.ExecuteTemplate(&b, "form", f); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); !strings.Contains(got, `name="project" value="" data-pages>`) || strings.Count(got, "data-pages") != 1 {
		t.Errorf("the project field of create-task is not the one field marked data-pages:\n%s", got)
	}
	for _, name := range []string{"link", "unlink", "find", "edit-task"} {
		marked := 0
		for _, fd := range action(name, nil, nil).Fields {
			if fd.Pages {
				marked++
			}
		}
		if marked != 1 {
			t.Errorf("%s marks %d fields as a page's title, want 1", name, marked)
		}
	}
}
