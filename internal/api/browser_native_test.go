package api_test

import (
	"strings"
	"testing"
)

// The browser without a script (docs/plans/108-the-browser-made-current.md): what app.js once did, a page does by
// the browser's own means, and these tests hold the HTML that asks for them.

// TestTheSearchDialogIsInThePage: every page of the shell carries the search dialog once, a modal with the search
// form inside, which the top bar's Search opens by its command and Escape or a tap outside closes (closedby). The
// login page, which has no shell, carries none (the public listener's test says so).
func TestTheSearchDialogIsInThePage(t *testing.T) {
	_, h := fresh(t)
	body := page(t, h, "/")
	for _, want := range []string{
		`<dialog class="palette" id="palette" closedby="any" aria-label="Search">` + "\n" + `<form method="GET" action="/search"><input type="search" name="q" autocomplete="off" aria-label="Search" placeholder="A page's title, or words of a body"></form>`,
		`<button type="button" class="rail-search-link button" commandfor="palette" command="show-modal" aria-label="Search">`,
	} {
		if strings.Count(body, want) != 1 {
			t.Errorf("Home carries %s %d times, want once", want, strings.Count(body, want))
		}
	}
}

// TestTheKeysAreInThePage: the keys are the browser's access keys, on the search, New, Today, the capture and the
// previous and next page, and the footer says so; there is no script to take any other key.
func TestTheKeysAreInThePage(t *testing.T) {
	_, h := freshOn(t, fixedToday)
	day := page(t, h, "/days/"+fixedToday)
	for _, want := range []string{
		`<input type="search" name="q" placeholder="Search" aria-label="Search" accesskey="k">`,
		`<a class="button primary rail-new" href="/#new" accesskey="n">`,
		`<a href="/days/today" aria-current="page" accesskey="t">`,
		`placeholder="What happened? Who was there?" accesskey="c" autofocus>`,
		`<a class="button step" accesskey="[" href="/days/2031-05-13" rel="prev"`, `<a class="button step" accesskey="]" href="/days/2031-05-15" rel="next"`,
		`<p class="keys"><small>Access keys, with the browser's modifier: <kbd>k</kbd> search · <kbd>n</kbd> new · <kbd>c</kbd> capture · <kbd>t</kbd> today · <kbd>[</kbd> <kbd>]</kbd> previous, next</small></p>`,
	} {
		if !strings.Contains(day, want) {
			t.Errorf("the day lacks %s", want)
		}
	}
}

// TestSearchListsTitlesFirst: the search answers the pages whose title starts with the words before the pages whose
// words match, so a page is one search away by its title, as it once was by the command list.
func TestSearchListsTitlesFirst(t *testing.T) {
	c, h := fresh(t)
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Garden log", "body": "The roses."}))
	must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Roses", "body": "In the garden."}))
	body := page(t, h, "/search?q=garden")
	titles, words := strings.Index(body, `aria-label="Pages by title"`), strings.Index(body, `aria-label="Pages by their words"`)
	if titles < 0 || words < 0 || titles > words {
		t.Fatalf("the search does not list the pages by title first:\n%.3000s", body)
	}
	if !strings.Contains(body[titles:words], `>Garden log</a>`) || strings.Contains(body[titles:words], `>Roses</a>`) {
		t.Errorf("the pages by title: %.600s", body[titles:words])
	}
	if !strings.Contains(body[words:], `>Roses</a>`) {
		t.Errorf("the pages by their words lack Roses: %.600s", body[words:])
	}
	e := must(c.Get(t.Context(), "/search?q=garden"))
	if ts, _ := props(e)["titles"].([]any); len(ts) != 1 {
		t.Errorf("the JSON search carries %d titles, want 1", len(ts))
	}
}
