package api

import "testing"

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

// TestEveryTextAreaOfTheCatalogIsBodyOrText: a form with a text area offers Preview (TestPreviewKeysOnATextArea, in
// the tests of the view), which sends the form to POST /render, and POST /render reads the field body or text. So each
// text area of the catalog has one of these names.
func TestEveryTextAreaOfTheCatalogIsBodyOrText(t *testing.T) {
	areas := 0
	for _, s := range catalog {
		for _, fd := range s.Fields {
			if fd.Type == "textarea" {
				areas++
				if fd.Name != "body" && fd.Name != "text" {
					t.Errorf("%s: the text area %s is not body or text, which POST /render reads", s.Name, fd.Name)
				}
			}
		}
	}
	if areas == 0 {
		t.Error("the catalog has no text area: the test checks nothing")
	}
}

// TestNoActionHasAFieldNamedFormOrBack: a browser form sends its id in form and the page to return to in back, and the
// handlers ignore both, so no action of the catalog has a field of either name.
func TestNoActionHasAFieldNamedFormOrBack(t *testing.T) {
	for _, s := range catalog {
		for _, fd := range s.Fields {
			if fd.Name == "form" || fd.Name == "back" {
				t.Errorf("%s has a field named %s, which the browser sends for itself", s.Name, fd.Name)
			}
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
	for _, name := range []string{"tombstone-pages", "retract"} {
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
