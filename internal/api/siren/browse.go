package siren

import "net/url"

// newActions are the actions that make a thing of a Browse kind on the list of that kind (docs/plans/117-one-browse-view.md,
// rule 7). A file of any kind is made by add-file. A kind that is not here (metric, all) is made elsewhere or is not
// made on its list.
var newActions = map[string]string{
	"page": "create-page", "person": "create-person", "place": "create-place", "media": "add-file", "document": "add-file",
}

// NewActionOf is the name of the action that makes a thing of the Browse kind on its list, "" for none. The list
// offers the action, and the rail of the browser links to the list with its form open (BrowseNewHref), so both read
// the name here.
func NewActionOf(kind string) string { return newActions[kind] }

// BrowseNewHref is the address of the Browse list of a kind with the form of its new action open.
func BrowseNewHref(kind string) string {
	return BrowseHref(url.Values{"kind": {kind}, "new": {NewActionOf(kind)}})
}

// BrowseKindHref is the address of the Browse list of a kind.
func BrowseKindHref(kind string) string { return BrowseHref(url.Values{"kind": {kind}}) }

// GhostsHref is the address of the Browse list of the ghost pages.
func GhostsHref() string { return BrowseHref(url.Values{"kind": {"page"}, "ghosts": {"1"}}) }
