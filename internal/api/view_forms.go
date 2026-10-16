package api

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Action is the entity's action of that name as this page draws it, nil when it is not offered.
func (v *view) Action(name string) *shownAction {
	for _, a := range v.Actions {
		if a.Name == name {
			return v.shown(a)
		}
	}
	return nil
}

// shownAction is an action as a page draws it: a copy of the entity's action, so the entity stays as it is and the
// JSON with it, and the marks of the browser, which the wire does not carry.
type shownAction struct {
	Action
	refused bool // the form a refusal came from: filled, open and described by the alert
	noFocus bool // the page shows a refusal: no field takes the focus from the alert
	asked   bool // the page's link asked for this form (?new=<name>): open, filled from the query
}

// shown is the action as this page draws it. On a page that shows a refusal, every action takes no focus, so the
// alert keeps it. The refused action is also marked and filled: each of its fields holds what the form sent. A sent
// value that is not a field of the action is dropped, and a file is never sent back.
//
// With no refusal, the page's link can ask for one form (?new=<name>, docs/plans/114-one-way-to-do-a-thing.md): it
// is open, and each field that the query names, but a path field or a file, holds the query's value.
func (v *view) shown(a Action) *shownAction {
	c := &shownAction{Action: a}
	if v.Refused == nil {
		if a.Name == "" || v.Asked("new") != a.Name {
			return c
		}
		c.asked = true
		c.Fields = slices.Clone(a.Fields)
		for i, f := range c.Fields {
			if _, named := v.query[f.Name]; named && f.In != "path" && f.Type != "file" && f.Name != "new" {
				c.Fields[i].Value = v.query.Get(f.Name)
			}
		}
		return c
	}
	c.noFocus = true
	if a.Href == v.Refused.Href {
		c.refused = true
		c.Fields = slices.Clone(a.Fields)
		for i, f := range c.Fields {
			if _, sent := v.Sent[f.Name]; sent && f.Type != "file" {
				c.Fields[i].Value = v.Sent.Get(f.Name)
			}
		}
	}
	return c
}

// formView is an action as the "form" template draws it. formOf takes name/value pairs: "button" labels the
// button, "class" styles the form, "hide" lists fields kept with their value but not shown, "autofocus" names
// the field the browser starts in, "more" lists optional fields folded under "More fields"
// (docs/plans/095-the-leaf-and-its-margin.md), "back" is the page the browser returns to after the write (no field
// of the action: the server reads it, see backOf), "placeholder" ("field=words") and "key" ("field=letter") give a
// field its prompt and its access key, and any other name fixes a field to a value (hidden).
type formView struct {
	*Action
	Button, Class string
	Focus         string
	Back          string
	Refused       bool    // the form a refusal came from: open, filled, described by the alert, with no focus
	Asked         bool    // the form the page's link asked for (?new=<name>): open
	Primary       bool    // the button is the primary one: a form with fields to fill
	Shown         []Field // the fields most writes need
	More          []Field // optional fields, folded; still in the form, so a browser sends them with no script
	Hidden        []Field
	prompts       map[string]string // field name to placeholder words
	keys          map[string]string // field name to its access key
}

// Preview reports a form with a text area: it offers Preview, a second button that sends the form to POST /render
// in a new tab, where the body shows as its page will show it, and nothing is saved.
func (f *formView) Preview() bool {
	for _, fd := range append(f.Shown[:len(f.Shown):len(f.Shown)], f.More...) {
		if fd.Type == "textarea" {
			return true
		}
	}
	return false
}

// Multipart reports a form with a file field, shown or folded: a browser sends it as multipart/form-data.
func (f *formView) Multipart() bool {
	for _, fd := range append(f.Shown[:len(f.Shown):len(f.Shown)], f.More...) {
		if fd.Type == "file" {
			return true
		}
	}
	return false
}

// fieldView is a field as the "field" template draws it: the field, and whether the browser starts in it.
type fieldView struct {
	Field
	Focus       bool
	Placeholder string // a quiet prompt in an empty box; it is an attribute, never sent
	Key         string // the field's access key, which the browser's own modifier reaches
}

// Fields are the fields the form shows (folded false) or folds under "More fields" (folded true).
func (f *formView) Fields(folded bool) []fieldView {
	list := f.Shown
	if folded {
		list = f.More
	}
	out := make([]fieldView, len(list))
	for i, fd := range list {
		out[i] = fieldView{fd, fd.Name == f.Focus, f.prompts[fd.Name], f.keys[fd.Name]}
	}
	return out
}

// MoreOpen reports folded fields of which one has a value: they are drawn open, so no value is out of sight.
func (f *formView) MoreOpen() bool {
	for _, fd := range f.More {
		if num(fd.Value) != "" {
			return true
		}
	}
	return false
}

func formOf(x any, opts ...string) (*formView, error) {
	var c shownAction // an action of the entity's list (range .Actions) has no marks
	switch x := x.(type) {
	case *shownAction:
		if x == nil { // the page does not offer the action
			return nil, nil
		}
		c = *x
	case Action:
		c.Action = x
	default:
		return nil, nil
	}
	a := &c.Action
	if len(opts)%2 != 0 {
		return nil, fmt.Errorf("form %s: options come in name/value pairs", a.Name)
	}
	f := &formView{Action: a, Button: a.Title}
	if f.Button == "" {
		f.Button = a.Name
	}
	fixed, hide, more := map[string]string{}, map[string]bool{}, map[string]bool{}
	for i := 0; i < len(opts); i += 2 {
		k, v := opts[i], opts[i+1]
		switch {
		case k == "button":
			f.Button = v
		case k == "class":
			f.Class = v
		case k == "hide":
			for _, n := range strings.Split(v, ",") {
				hide[n] = true
			}
		case k == "more":
			for _, n := range strings.Split(v, ",") {
				more[n] = true
			}
		case k == "autofocus":
			f.Focus = v
		case k == "back":
			f.Back = v
		case k == "placeholder": // "field=words"
			if name, words, ok := strings.Cut(v, "="); ok {
				if f.prompts == nil {
					f.prompts = map[string]string{}
				}
				f.prompts[name] = words
			}
		case k == "key": // "field=letter"
			if name, letter, ok := strings.Cut(v, "="); ok {
				if f.keys == nil {
					f.keys = map[string]string{}
				}
				f.keys[name] = letter
			}
		default:
			fixed[k] = v
		}
	}
	for _, fd := range a.Fields {
		if v, ok := fixed[fd.Name]; ok {
			fd.Value, fd.Type = v, "hidden"
		}
		switch {
		case fd.Type == "hidden" || hide[fd.Name]:
			f.Hidden = append(f.Hidden, fd)
		case more[fd.Name] && !fd.Required:
			f.More = append(f.More, fd)
		default:
			f.Shown = append(f.Shown, fd)
		}
	}
	f.Primary = len(f.Shown)+len(f.More) > 0
	f.Refused, f.Asked = c.refused, c.asked
	if c.asked && f.Focus == "" && len(f.Shown) > 0 { // a form the reader asked for starts in its first field
		f.Focus = f.Shown[0].Name
	}
	if c.noFocus { // a page that shows a refusal puts the focus on the alert, and on no field
		f.Focus = ""
	}
	return f, nil
}

// segmented reports a field the browser draws as a Segmented choice of the design system, a row of radio buttons:
// 2 to 5 options, none empty and each of 3 characters or fewer (a mood 1 to 5, a 1 or a 0). Any other field with
// options is a select.
func segmented(f Field) bool {
	if len(f.Options) < 2 || len(f.Options) > 5 {
		return false
	}
	for _, o := range f.Options {
		if o == "" || utf8.RuneCountInString(o) > 3 {
			return false
		}
	}
	return true
}
