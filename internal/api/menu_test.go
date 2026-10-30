package api_test

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// TestTheMenuHoldsTheActionsNoTemplatePlaces: a page draws the actions that it does not place itself in the ⋯ menu:
// the edit group with its label first, then the danger group, last and apart. Each item is a button that opens a dialog
// with the form in it (no script), and the page has no More section.
func TestTheMenuHoldsTheActionsNoTemplatePlaces(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	essay := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Essay"}))
	body := browse(t, h, apitest.Href(essay, "self"))
	for _, want := range []string{
		`<button type="button" class="button menu-button" popovertarget="menu" aria-label="More actions">`,
		`<div class="menu-group" role="group" aria-label="Edit"><span class="menu-label">Edit</span>`,
		`<button type="button" class="menu-item" commandfor="do-promote" command="show-modal">Make it a person</button>`,
		`<dialog class="menu-dialog" id="do-promote" closedby="any" aria-label="Make it a person">`,
		`<button type="button" class="menu-item" commandfor="do-tombstone-pages" command="show-modal">Delete</button>`,
		`commandfor="do-promote" command="close">Close</button>`,
		`commandfor="do-tombstone-pages" command="close" autofocus>Cancel</button>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the menu of a page lacks %s", want)
		}
	}
	if strings.Contains(body, `aria-label="More"><h2>More</h2>`) || strings.Contains(body, `<details class="action" name="more">`) {
		t.Error("the page still draws the More section")
	}
	// The danger group is last and apart: after every other group, and its own group in the danger colour class.
	list := body[strings.Index(body, `<div class="menu-list"`):]
	list = list[:strings.Index(list, `<dialog`)]
	if danger := strings.Index(list, `class="menu-group danger"`); danger < strings.Index(list, `aria-label="Edit"`) ||
		strings.Count(list, `class="menu-group`) != 2 || strings.Index(list, "Make it a person") > danger || strings.Index(list, ">Delete<") < danger {
		t.Errorf("Delete is not alone in the last group of the menu:\n%s", list)
	}
	// The delete dialog is the confirm step: its button is the danger one, and the page returns to itself.
	del := regexp.MustCompile(`(?s)<dialog class="menu-dialog" id="do-tombstone-pages".*?</dialog>`).FindString(body)
	if !strings.Contains(del, `<button class="danger">Delete</button>`) || !strings.Contains(del, `name="back" value="`+apitest.Href(essay, "self")+`"`) {
		t.Errorf("the dialog of Delete is not the danger form that returns to the page:\n%s", del)
	}
}

// TestAnOwnerActionOfTheMenuCarriesTheOwnerMark: the integrity page offers two repairs that are the owner's alone, and
// no other action; its menu marks both, on the button and in the dialog.
func TestAnOwnerActionOfTheMenuCarriesTheOwnerMark(t *testing.T) {
	_, h, _ := apitest.Fresh(t, "cli")
	body := mainOf(browse(t, h, "/integrity"))
	for _, name := range []string{"rebuild-search", "resync-links"} {
		btn := regexp.MustCompile(`<button type="button" class="menu-item" commandfor="do-` + name + `" command="show-modal">[^<]*<span class="owner">owner</span></button>`)
		if !btn.MatchString(body) {
			t.Errorf("the button of %s has no owner mark:\n%s", name, body)
		}
		if !strings.Contains(body, `id="do-`+name+`"`) {
			t.Errorf("no dialog for %s", name)
		}
	}
}

// TestAnEmptyMenuDrawsNoButton: a list whose every action has a place of its own (Browse, Metrics) draws no ⋯ button.
func TestAnEmptyMenuDrawsNoButton(t *testing.T) {
	_, h, _ := apitest.Fresh(t, "cli")
	for _, path := range []string{"/browse", "/metrics", "/days"} {
		if body := mainOf(browse(t, h, path)); strings.Contains(body, `menu-button`) || strings.Contains(body, `class="menu"`) {
			t.Errorf("%s draws a menu with no item", path)
		}
	}
}

// TestAnAskedFormOfTheMenuIsAnOpenDialog: ?new=rename opens the form of rename as an open dialog, not a modal one, with
// the fields that the query names filled in.
func TestAnAskedFormOfTheMenuIsAnOpenDialog(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	essay := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Essay"}))
	self := apitest.Href(essay, "self")
	body := browse(t, h, self+"?new=rename&title="+url.QueryEscape("A better title"))
	dialog := regexp.MustCompile(`(?s)<dialog class="menu-dialog" id="pencil"[^>]*>.*?</dialog>`).FindString(body)
	if !strings.Contains(dialog, ` aria-label="Rename" open>`) || !strings.Contains(dialog, `name="title" value="A better title"`) ||
		!strings.Contains(dialog, `command="close">Close</button>`) {
		t.Errorf("the asked dialog is not open and filled:\n%s", dialog)
	}
	if other := browse(t, h, self); strings.Contains(other, `id="pencil" closedby="any" aria-label="Rename" open`) {
		t.Error("the dialog of Rename is open without a link that asked for it")
	}
}
