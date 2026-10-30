package view

import (
	"fmt"
	"slices"

	"lifelog/siren"
)

// The ⋯ menu and the create buttons are the two places where a page draws the actions that no template places by name
// (docs/plans/159-the-class-of-an-action.md). An action that a template places (placing.places) is drawn there; a
// `create` action is drawn by the head (Creates); any other action of the entity is in the menu. A new action of the
// catalog is on a page with no change of a template.

// placing is the way this view's entity is drawn. A page of a connected app or of the task service (drawnByShape) is
// always drawn by generic: the class is the app's word, and it may be the name of a class of this server.
func (v *view) placing() placing {
	if drawnByShape(v.Path) {
		return placing{template: "generic"}
	}
	return placingOf(v.Entity)
}

// placed reports an action that the template of this view draws in a place of its own.
func (v *view) placed(name string) bool { return slices.Contains(v.placing().places, name) }

// menuItem is one action of the ⋯ menu: its button in the list, and its dialog with the form.
type menuItem struct {
	ID    string // the id of the dialog, which the button names (commandfor)
	Title string
	Owner bool
	Note  string // what the action does, above its form: its hint, else its description
	Open  bool   // the page's link asked for this form (?new=<name>): its dialog is open
	Close string // the words of the button that closes the dialog
	Form  *formView
}

// menu is the ⋯ menu in three groups: the actions that edit the resource, the others, and the danger actions, last
// and apart. The order of the entity is kept inside a group.
type menu struct {
	Edit, Other, Danger []menuItem
}

// Empty reports a menu with no item: the page draws no ⋯ button.
func (m menu) Empty() bool { return len(m.Edit)+len(m.Other)+len(m.Danger) == 0 }

// Items are the items of the menu in the order it draws them.
func (m menu) Items() []menuItem {
	return slices.Concat(m.Edit, m.Other, m.Danger)
}

// Menu is the actions of the entity that the ⋯ menu draws: those that no template places and that are not `create`
// (Creates draws these). A `toggle` that no template places is in it too.
func (v *view) Menu() (menu, error) {
	var m menu
	for _, a := range v.Actions {
		if v.placed(a.Name) || slices.Contains(a.Class, "create") {
			continue
		}
		f, err := formOf(v.shown(a), v.menuOptions(a)...)
		if err != nil {
			return menu{}, err
		}
		it := menuItem{ID: "do-" + a.Name, Title: a.Title, Owner: a.Owner, Note: a.Hint, Open: f.Asked, Close: "Close", Form: f}
		if it.Note == "" {
			it.Note = a.Description
		}
		if it.Title == "" {
			it.Title = a.Name
		}
		switch {
		case a.Danger:
			it.Close = "Cancel"
			m.Danger = append(m.Danger, it)
		case slices.Contains(a.Class, "edit"):
			m.Edit = append(m.Edit, it)
		default:
			m.Other = append(m.Other, it)
		}
	}
	return m, nil
}

// menuOptions are the form options of an action in the menu. The ids of a delete are filled in already, so the form
// hides them, and a delete returns to the page: a deleted page, and a deleted metric, stay as pages.
func (v *view) menuOptions(a siren.Action) []string {
	switch a.Name {
	case "tombstone-pages":
		return []string{"hide", "ids", "back", v.pageHere()}
	case "edit-metric":
		return []string{"button", "Save"}
	}
	return nil
}

// pageHere is the address of the page of this view's entity: the view of a metric (a series) returns to its page,
// which a deleted metric keeps, and any other page returns to itself.
func (v *view) pageHere() string {
	if props, ok := v.Props.(map[string]any); ok && v.IsSeries() {
		return fmt.Sprintf("/pages/%s", Num(props["id"]))
	}
	return v.Here
}

// Creates are the forms of the `create` actions that no template places: a button of the head opens each. The
// quick-add line of a page drawn by shape is not among them.
func (v *view) Creates() ([]*formView, error) {
	quick := v.QuickAdd()
	var out []*formView
	for _, a := range v.Actions {
		if !slices.Contains(a.Class, "create") || v.placed(a.Name) || (quick != nil && quick.Name == a.Name) {
			continue
		}
		f, err := formOf(v.shown(a), createOptions(a)...)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// createOptions are the form options of a `create` action in the head. register-metric is the main write of the
// board, so its button is the primary one; the form of a file keeps the fields that a browser cannot fill folded, and
// the form of a reading folds its time and sends a key that makes a form sent twice one reading.
func createOptions(a siren.Action) []string {
	switch a.Name {
	case "register-metric":
		return []string{"lead", "1"}
	case "add-file":
		return []string{"hide", "sha256,mime", "more", "preview,dry_run"}
	case "record":
		return []string{"more", "taken_at,tz", "import_key", newKey()}
	}
	return nil
}
