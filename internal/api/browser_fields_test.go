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
