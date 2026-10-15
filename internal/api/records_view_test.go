package api_test

import (
	"strings"
	"testing"
)

// TestTheGhostPagesSayWhatTheyAre: the ghost pages say what a ghost page is under their title, and an empty list says
// why it is empty, not how to make a page.
func TestTheGhostPagesSayWhatTheyAre(t *testing.T) {
	_, h := freshOn(t, fixedToday)
	body := browse(t, h, "/ghosts")
	for _, want := range []string{`<p class="sub">Empty pages, older than 30 days, that nothing links to or from.</p>`,
		`<p class="empty">No ghost page: every page has text or a link.</p>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the ghost pages lack %s", want)
		}
	}
}

// TestAReadingsFactsAreOnACard: a reading's page puts its facts on a card, as an occurrence's does.
func TestAReadingsFactsAreOnACard(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	r := must(c.Do(t.Context(), find(root, "record"), map[string]string{"metric": "weight", "day": fixedToday, "value": "71.4"}))
	if body := browse(t, h, hrefOf(r, "self")); !strings.Contains(body, `<div class="sheet list"><dl class="facts">`) {
		t.Error("a reading's facts are not on a card")
	}
}
