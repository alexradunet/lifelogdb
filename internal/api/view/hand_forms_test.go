package view

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"lifelog/siren"
)

func tickAction(href string) siren.Action {
	fields := []siren.Field{{Name: "line", Type: "number", Required: true}, {Name: "text", Type: "text", Required: true},
		{Name: "outcome", Type: "text", Required: true, Options: []string{"done", "dropped", "reopen"}}, {Name: "done", Type: "date"}}
	if strings.Contains(href, "{id}") {
		fields = append([]siren.Field{{Name: "id", Type: "number", In: "path"}}, fields...)
	}
	return siren.Action{Name: "tick", Method: "POST", Href: href, Fields: fields}
}

// TestALineActionPostsToTheHrefOfTheEntity: a view that lists the lines of many pages gets the action templated, and
// the line's form posts to that href with the line's page filled. A filled action is the address of its own page.
func TestALineActionPostsToTheHrefOfTheEntity(t *testing.T) {
	u, _ := url.Parse("/pages/7")
	open := map[string]any{"page_id": 9, "line": 2, "box": " ", "text": "Pay", "title": "Bank"}
	e := &siren.Entity{Class: []string{"page"}, Title: "Week",
		Properties: map[string]any{"id": 7, "period": "week", "entity_type": "page", "lines": []any{open}},
		Actions:    []siren.Action{tickAction("/pages/{id}/tick")}}
	b, err := Draw(e, Facts{URL: u})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `action="/pages/9/tick"`) {
		t.Errorf("the line of page 9 does not post to the filled href of the entity")
	}
	v := &view{Entity: e}
	if got := v.TickHref(9.0); got != "/pages/9/tick" {
		t.Errorf("TickHref %q", got)
	}
	if got := v.TickHref(nil); got != "" {
		t.Errorf("TickHref with no page %q, want empty", got)
	}
	e.Actions = []siren.Action{tickAction("/pages/7/tick")}
	if got := (&view{Entity: e}).TickHref(7.0); got != "/pages/7/tick" {
		t.Errorf("TickHref of a filled action %q", got)
	}
	if got := (&view{Entity: &siren.Entity{}}).TickHref(7.0); got != "" {
		t.Errorf("a view with no tick %q, want empty", got)
	}
}

// TestTheTickFormHoldsOnlyTheLineAndItsButtons: the hidden names are back, line and text; the buttons are the
// options of outcome; the field outcome and the optional done are not drawn.
func TestTheTickFormHoldsOnlyTheLineAndItsButtons(t *testing.T) {
	a := tickAction("/pages/9/tick")
	a.Fields[2].Value, a.Fields[1].Value = nil, "Pay"
	a.Fields[0].Value = 2
	html, _ := drawForm(t, a, "class", "tick-line", "buttons", "outcome=done,dropped", "hide", "line,text", "outside", "done", "back", "/x#row")
	if !strings.Contains(html, `action="/pages/9/tick"`) {
		t.Errorf("action: %s", html)
	}
	var names []string
	for _, m := range regexp.MustCompile(`<input type="hidden" name="(\w+)"`).FindAllStringSubmatch(html, -1) {
		names = append(names, m[1])
	}
	if strings.Join(names, ",") != "line,text,back" {
		t.Errorf("hidden names %v, want line,text,back", names)
	}
	for _, want := range []string{`<button name="outcome" value="done" class="small">Done</button>`, `<button name="outcome" value="dropped" class="small quiet">Dropped</button>`} {
		if !strings.Contains(html, want) {
			t.Errorf("lacks %s in %s", want, html)
		}
	}
	if strings.Contains(html, `type="text" name="outcome"`) || strings.Contains(html, `name="done"`) || strings.Count(html, "<button") != 2 {
		t.Errorf("the form draws more than its buttons: %s", html)
	}
	html, _ = drawForm(t, a, "buttons", "outcome=reopen")
	if !strings.Contains(html, `<button name="outcome" value="reopen" class="small quiet">Reopen</button>`) {
		t.Errorf("a lone button is quiet: %s", html)
	}
}

// TestAFixedValueIsAHiddenInput: a form whose fields are all fixed or hidden is hidden inputs and one button, the
// glyph with its aria-label, and nothing else (the × of a category).
func TestAFixedValueIsAHiddenInput(t *testing.T) {
	file := filePages(0, "Sport")
	file.Fields = append(file.Fields, siren.Field{Name: "from", Type: "text", Pages: true})
	html, _ := drawForm(t, file, "ids", "5", "from", "Sport", "outside", "category,move", "class", "tiny", "button", "×", "aria", "Remove from Sport")
	if n := regexp.MustCompile(`name="(\w+)"`).FindAllStringSubmatch(html, -1); len(n) != 2 || n[0][1] != "ids" || n[1][1] != "from" {
		t.Errorf("file-pages sends %v, want ids and from: %s", n, html)
	}
	for _, want := range []string{`class="tiny"`, `<input type="hidden" name="ids" value="5">`, `<input type="hidden" name="from" value="Sport">`,
		`<button class="remove" aria-label="Remove from Sport">×</button>`} {
		if !strings.Contains(html, want) {
			t.Errorf("file-pages lacks %s in %s", want, html)
		}
	}
	if strings.Contains(html, "<label") || strings.Contains(html, "<details") || strings.Contains(html, "<datalist") {
		t.Errorf("file-pages draws a field: %s", html)
	}
}

// TestMoreStarFoldsEveryOptionalField: "*" folds every field that the action does not require, and keeps the
// required ones and the fixed ones out of the fold.
func TestMoreStarFoldsEveryOptionalField(t *testing.T) {
	plan := siren.Action{Name: "plan", Method: "POST", Href: "/plans", Fields: []siren.Field{
		{Name: "page", Type: "text", Required: true}, {Name: "text", Type: "text", Required: true},
		{Name: "on", Type: "date"}, {Name: "due", Type: "date"}, {Name: "every", Type: "text"}, {Name: "from_done", Type: "number", Options: []string{"0", "1"}}}}
	_, f := drawForm(t, plan, "page", "Bank", "more", "*")
	var shown, more []string
	for _, fd := range f.Shown {
		shown = append(shown, fd.Name)
	}
	for _, fd := range f.More {
		more = append(more, fd.Name)
	}
	if strings.Join(shown, ",") != "text" || strings.Join(more, ",") != "on,due,every,from_done" {
		t.Errorf("shown %v, folded %v", shown, more)
	}
}
