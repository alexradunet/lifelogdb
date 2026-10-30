package view

import (
	"slices"
	"sort"
	"strings"

	"lifelog/siren"
)

// The generic view reads the shape of an entity and never a class or the name of an app. The three rules are in
// extensions/README.md, "How the browser draws a page of an app". The code below holds them.

// QuickAdd is the first action of the entity that a browser draws as one line at the top of the page: it writes
// (it is not a GET), it has no path field, and exactly one of its fields is required and has the type text. Nil when
// no action has that shape. Only the page of a connected app has one: the catalog page of this server lists every
// action once, in its own order.
func (v *view) QuickAdd() *siren.Action {
	if !strings.HasPrefix(v.Path, "/with/") {
		return nil
	}
	for i, a := range v.Actions {
		if quickAdd(a) {
			return &v.Actions[i]
		}
	}
	return nil
}

func quickAdd(a siren.Action) bool {
	if a.Method == "GET" {
		return false
	}
	required := 0
	for _, f := range a.Fields {
		switch {
		case f.In == "path":
			return false // a browser cannot fill a path in one line: opener draws it
		case f.Type == "hidden" || !f.Required:
		case f.Type == "text":
			required++
		default:
			return false
		}
	}
	return required == 1
}

// OtherActions are the actions of the entity that the Actions section draws: all but the quick-add.
func (v *view) OtherActions() []siren.Action {
	q := v.QuickAdd()
	var out []siren.Action
	for i := range v.Actions {
		if q == nil || &v.Actions[i] != q {
			out = append(out, v.Actions[i])
		}
	}
	return out
}

// SideLinks are the links of the entity that a wide screen draws as a side column: all but self.
func (v *view) SideLinks() []siren.Link {
	var out []siren.Link
	for _, l := range v.Links {
		if !slices.Contains(l.Rel, "self") {
			out = append(out, l)
		}
	}
	return out
}

// IsHere reports a link to the page the browser is on (its query does not count).
func (v *view) IsHere(href string) bool {
	path, _, _ := strings.Cut(href, "?")
	return path == v.Path
}

// roundAction reports an action of a row that a browser draws as a small round button: it has no field to fill.
func roundAction(a siren.Action) bool { return len(a.Fields) == 0 }

// rowChips are the scalar properties of an embedded link as "name: value", in the order of the names: a string that
// is not empty, a number, a true boolean. A list, an object, a null, an empty string and false make no chip.
func rowChips(l siren.Link) []string {
	shape, err := jsonShape(l.Properties)
	props, _ := shape.(map[string]any)
	if err != nil || len(props) == 0 {
		return nil
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []string
	for _, name := range names {
		switch x := props[name].(type) {
		case string:
			if x != "" {
				out = append(out, name+": "+x)
			}
		case float64:
			out = append(out, name+": "+Num(x))
		case bool:
			if x {
				out = append(out, name+": yes")
			}
		}
	}
	return out
}
