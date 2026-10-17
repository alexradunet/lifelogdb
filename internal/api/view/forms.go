package view

import (
	"cmp"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"lifelog/internal/api/siren"
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
	siren.Action
	refused bool       // the action a refusal came from: formOf fills, opens and describes the form that was sent
	sent    url.Values // what the refused form sent
	noFocus bool       // the page shows a refusal: no field takes the focus from the alert
	asked   bool       // the page's link asked for this form (?new=<name>): open, filled from the query
}

// shown is the action as this page draws it. On a page that shows a refusal, every action takes no focus, so the
// alert keeps it. The refused action is also marked, with what the form sent (see formOf).
//
// With no refusal, the page's link can ask for one form (?new=<name>, docs/plans/114-one-way-to-do-a-thing.md): it
// is open, and each field that the query names, but a path field or a file, holds the query's value.
func (v *view) shown(a siren.Action) *shownAction {
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
		c.refused, c.sent = true, v.Sent
	}
	return c
}

// formView is an action as the "form" template draws it. formOf takes name/value pairs: "button" labels the
// button, "class" styles the form, "id" names it (the form sends its id as form), "hide" lists fields kept with their
// value but not shown, "outside" lists fields that controls outside the form send (the boxes of a list join the form
// by its id) and that the form does not draw, "autofocus" names the field the browser starts in ("" for none),
// "more" lists optional fields folded under "More fields" (docs/plans/095-the-leaf-and-its-margin.md), "back" is the
// page the browser returns to after the write (no field of the action: the server reads it, see backOf), "label"
// ("field=words") gives a field a title in this form only (the score form calls its unit Range), "placeholder"
// ("field=words") and "key" ("field=letter") give a field its prompt and its access key, "required" lists fields
// that the form requires though the action does not, "pattern" ("field=regexp") is what the browser accepts in a
// field (a score's range), and any other name fixes a field to a value (hidden).
type formView struct {
	*siren.Action
	Button, Class string
	ID            string // the form's id, which the controls outside it name (form="pick")
	Focus         string
	Back          string
	Refused       bool          // the form a refusal came from: open, filled, described by the alert, with no focus
	Asked         bool          // the form the page's link asked for (?new=<name>): open
	Primary       bool          // the button is the primary one: a form with fields to fill
	Also          *formView     // a second action that the form sends to, by a button of its own (Or)
	Shown         []siren.Field // the fields most writes need
	More          []siren.Field // optional fields, folded; still in the form, so a browser sends them with no script
	Hidden        []siren.Field
	prompts       map[string]string // field name to placeholder words
	keys          map[string]string // field name to its access key
	patterns      map[string]string // field name to the pattern of its value
}

// Or is the form f with a second button, after its own, that sends the same form to the action of g (formaction),
// with no check of the fields that f requires: the Delete of a list, beside its Put in a category. g's action takes
// the method of f's and no field that f does not send. When g is the form a refusal came from, so is f. With no g, f
// is as it was.
func (f *formView) Or(g *formView) *formView {
	f.Also = g
	if g != nil && g.Refused {
		f.Refused = true
	}
	return f
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
	siren.Field
	Focus       bool
	Placeholder string // a quiet prompt in an empty box; it is an attribute, never sent
	Key         string // the field's access key, which the browser's own modifier reaches
	List        string // the id of the datalist of a field of page titles with options (Pages): see Fields
	Pattern     string // the browser sends the form only when the value matches it
}

// Fields are the fields the form shows (folded false) or folds under "More fields" (folded true). A field of page
// titles with options offers them in a datalist, whose id is the form's id, or else its action's name, and the
// field's name: a page draws an action in one form, or gives each of its forms an id.
func (f *formView) Fields(folded bool) []fieldView {
	list := f.Shown
	if folded {
		list = f.More
	}
	out := make([]fieldView, len(list))
	for i, fd := range list {
		out[i] = fieldView{fd, fd.Name == f.Focus, f.prompts[fd.Name], f.keys[fd.Name], "", f.patterns[fd.Name]}
		if fd.Pages && len(fd.Options) > 0 {
			out[i].List = cmp.Or(f.ID, f.Name) + "-" + fd.Name
		}
	}
	return out
}

// MoreOpen reports folded fields of which one has a value: they are drawn open, so no value is out of sight.
func (f *formView) MoreOpen() bool {
	for _, fd := range f.More {
		if Num(fd.Value) != "" {
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
	case siren.Action:
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
	fixed, hide, outside, more, required := map[string]string{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	labels := map[string]string{}
	focused := false // the options name the field of the focus, or none
	for i := 0; i < len(opts); i += 2 {
		k, v := opts[i], opts[i+1]
		switch {
		case k == "button":
			f.Button = v
		case k == "class":
			f.Class = v
		case k == "id":
			f.ID = v
		case k == "hide":
			for _, n := range strings.Split(v, ",") {
				hide[n] = true
			}
		case k == "outside":
			for _, n := range strings.Split(v, ",") {
				outside[n] = true
			}
		case k == "more":
			for _, n := range strings.Split(v, ",") {
				more[n] = true
			}
		case k == "required":
			for _, n := range strings.Split(v, ",") {
				required[n] = true
			}
		case k == "autofocus":
			f.Focus, focused = v, true
		case k == "back":
			f.Back = v
		case k == "label": // "field=words"
			if name, words, ok := strings.Cut(v, "="); ok {
				labels[name] = words
			}
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
		case k == "pattern": // "field=regexp"
			if name, re, ok := strings.Cut(v, "="); ok {
				if f.patterns == nil {
					f.patterns = map[string]string{}
				}
				f.patterns[name] = re
			}
		default:
			fixed[k] = v
		}
	}
	// The form a refusal came from is open, described by the alert, and each of its fields holds what it sent: a sent
	// value that is not a field of the action is dropped, and a file is never sent back. A page that draws one action
	// in several forms gives each one an id, which the form sends as form (no field of any action): of those forms,
	// only the one with the id that was sent is the form a refusal came from.
	f.Refused = c.refused && (f.ID == "" || c.sent.Get("form") == "" || c.sent.Get("form") == f.ID)
	for _, fd := range a.Fields {
		if _, sent := c.sent[fd.Name]; f.Refused && sent && fd.Type != "file" {
			fd.Value = c.sent.Get(fd.Name)
		}
		if v, ok := fixed[fd.Name]; ok { // after the sent value: a fixed field keeps its value (the habit's empty unit)
			fd.Value, fd.Type = v, "hidden"
		}
		if required[fd.Name] {
			fd.Required = true
		}
		if l, ok := labels[fd.Name]; ok {
			fd.Title = l
		}
		switch {
		case outside[fd.Name]:
		case fd.Type == "hidden" || hide[fd.Name]:
			f.Hidden = append(f.Hidden, fd)
		case more[fd.Name] && !fd.Required:
			f.More = append(f.More, fd)
		default:
			f.Shown = append(f.Shown, fd)
		}
	}
	f.Primary = len(f.Shown)+len(f.More) > 0
	f.Asked = c.asked
	if c.asked && !focused && len(f.Shown) > 0 { // a form the reader asked for starts in its first field
		f.Focus = f.Shown[0].Name
	}
	if c.noFocus { // a page that shows a refusal puts the focus on the alert, and on no field
		f.Focus = ""
	}
	return f, nil
}

// segmented reports a field the browser draws as a Segmented choice of the design system, a row of radio buttons:
// 2 to 5 options, none empty and each of 3 characters or fewer (a mood 1 to 5). A field of page titles is never one:
// its options are the titles it offers, and it takes a new title too (a datalist). A flag is never one either: it is
// a checkbox. Any other field with options is a select.
func segmented(f siren.Field) bool {
	if f.Pages || checkbox(f) || len(f.Options) < 2 || len(f.Options) > 5 {
		return false
	}
	for _, o := range f.Options {
		if o == "" || utf8.RuneCountInString(o) > 3 {
			return false
		}
	}
	return true
}

// checkbox reports a flag that the browser draws as one checkbox before the field's title: the options 0 and 1, in
// a field that the action does not require. A checked box sends 1, and a box that is not checked sends nothing,
// which the server reads as 0 (flag). A required flag stays a row of radio buttons, as a checkbox cannot send 0. A
// field of page titles is never one: its options are titles (two categories titled 0 and 1), which a datalist offers.
func checkbox(f siren.Field) bool {
	return !f.Pages && !f.Required && slices.Equal(f.Options, []string{"0", "1"})
}
