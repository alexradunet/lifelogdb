package api_test

import (
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// headOf is the head of a page: from its <header class="page-head"> to the end of the header.
func headOf(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, `<header class="page-head">`)
	if i < 0 {
		t.Fatalf("no head in the page:\n%.400s", body)
	}
	head := body[i:]
	return head[:strings.Index(head, "</header>")]
}

// TestATitleEditIsThePencilAndNotInTheMenu: rename is a pencil beside the title, named by the action's title, with its
// form in a popover under it, filled with the current title. It is drawn once, and the menu does not list it.
func TestATitleEditIsThePencilAndNotInTheMenu(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	essay := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Essay"}))
	body := browse(t, h, apitest.Href(essay, "self"))
	head := headOf(t, body)
	if !regexp.MustCompile(`<h1>Essay</h1><div class="pop"><button type="button" class="button pencil" aria-label="Rename" title="Rename" popovertarget="pencil">`).MatchString(head) {
		t.Errorf("the pencil is not beside the title:\n%s", head)
	}
	if !strings.Contains(head, `<div class="head-pop" id="pencil" popover`) || !strings.Contains(head, `<input type="text" name="title" value="Essay" required autofocus>`) {
		t.Errorf("the popover of the pencil lacks its form, filled with the title and focused:\n%s", head)
	}
	if n := strings.Count(body, `data-action="rename"`); n != 1 {
		t.Errorf("rename is drawn %d times, want once", n)
	}
	if strings.Contains(body, `do-rename`) {
		t.Error("the menu lists rename")
	}
}

// TestACreateActionIsAButtonWithAPopover: a create action of a list is a button of its head, and its form is in a
// popover under it. The first button is the primary one; the others are plain. The old lead option is refused.
func TestACreateActionIsAButtonWithAPopover(t *testing.T) {
	_, h, _ := apitest.Fresh(t, "cli")
	people := headOf(t, browse(t, h, "/browse?kind=person"))
	if !regexp.MustCompile(`<button type="button" class="button primary" popovertarget="new-create-person"><span class="icon icon-new" aria-hidden="true"></span>New person`).MatchString(people) ||
		!strings.Contains(people, `<div class="head-pop" id="new-create-person" popover`) || !strings.Contains(people, `data-action="create-person"`) {
		t.Errorf("New person is not a primary button with a popover and its form:\n%s", people)
	}
	if strings.Contains(people, `<details class="action"`) {
		t.Error("the head still draws a create action as a fold")
	}
	board := headOf(t, browse(t, h, "/metrics"))
	first := strings.Index(board, `class="button primary" popovertarget="new-register-metric"`)
	second := strings.Index(board, `class="button" popovertarget="new-record"`)
	if first < 0 || second < first || strings.Count(board, `button primary`) != 1 {
		t.Errorf("the board does not draw New metric as the one primary button, before Add a reading:\n%s", board)
	}
}

// TestTheMetaLineIsUnderTheTitleAndTheFactsSectionIsGone: a person's name and dates, a file's type and short SHA-256 and
// when the page was created are one line in the head; the margin has no Facts section.
func TestTheMetaLineIsUnderTheTitleAndTheFactsSectionIsGone(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))
	sam := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-person"), map[string]string{"title": "Sam", "name": "Samuel Ortiz", "birth_day": "1980-02-03", "death_day": "2020-01-02"}))
	apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "add-file"), map[string]string{"title": "Scan.pdf", "sha256": strings.Repeat("ab", 32), "mime": "application/pdf"}))
	scan := apitest.Must(t)(c.Get(ctx, "/pages?title=Scan.pdf"))
	for _, row := range []struct {
		name, path string
		want       []string
	}{
		{"a person", apitest.Href(sam, "self"), []string{`<p class="meta-line">Samuel Ortiz · born Sun 3 Feb 1980 · died Thu 2 Jan 2020 · created <time `}},
		{"a file", apitest.Href(scan, "self"), []string{`<p class="meta-line">application/pdf · <span title="SHA-256 of the original: ` + strings.Repeat("ab", 32) + `">` + strings.Repeat("ab", 6) + `…</span> · created <time `}},
	} {
		body := browse(t, h, row.path)
		head := headOf(t, body)
		for _, want := range row.want {
			if !strings.Contains(head, want) {
				t.Errorf("%s: the head lacks %s:\n%s", row.name, want, head)
			}
		}
		if strings.Contains(body, `aria-label="Facts"`) || strings.Contains(body, `<dt>SHA-256</dt>`) {
			t.Errorf("%s: the page still draws a Facts section", row.name)
		}
	}
}
