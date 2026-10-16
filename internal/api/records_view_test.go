package api_test

import (
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// TestTheGhostPagesSayWhatTheyAre: the ghost list of Browse says what a ghost page is under its title, and an empty
// list says why it is empty, not how to make a page.
func TestTheGhostPagesSayWhatTheyAre(t *testing.T) {
	_, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	body := browse(t, h, "/browse?ghosts=1")
	for _, want := range []string{`<p class="sub"><span>Ghost pages</span><span>Empty pages, older than 30 days, that nothing links to or from.</span></p>`,
		`<p class="empty">No ghost page: every page has text or a link.</p>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the ghost pages lack %s", want)
		}
	}
}

// TestAReadingsFactsAreOnACard: a reading's page puts its facts on a card.
func TestAReadingsFactsAreOnACard(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "register-metric"), map[string]string{"name": "weight", "unit": "kg"}))
	r := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "record"), map[string]string{"metric": "weight", "day": fixedToday, "value": "71.4"}))
	if body := browse(t, h, apitest.Href(r, "self")); !strings.Contains(body, `<div class="sheet list"><dl class="facts">`) {
		t.Error("a reading's facts are not on a card")
	}
}
