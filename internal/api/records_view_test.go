package api_test

import (
	"strings"
	"testing"
)

// TestThePeriodsAreAList: the recorded periods are drawn as the people and the places are, a row each with its
// boundaries in words, and not as the generic view's properties; an empty list names the way forward.
func TestThePeriodsAreAList(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	if body := browse(t, h, "/periods"); !strings.Contains(body, `<p class="empty">No period yet. Record period makes one.</p>`) {
		t.Error("an empty list of periods does not say how to make one")
	}
	root := must(c.Get(t.Context(), "/"))
	cluj := must(c.Do(t.Context(), find(root, "create-period"), map[string]string{"title": "Living in Cluj", "start_boundary": "2019-09", "end_boundary": "2023-06"}))
	must(c.Do(t.Context(), find(root, "create-period"), map[string]string{"title": "Childhood"}))
	must(c.Do(t.Context(), find(root, "create-period"), map[string]string{"title": "Since the move", "start_boundary": "2024-03"}))
	body := browse(t, h, "/periods")
	for _, want := range []string{
		`<a class="entry named" href="` + hrefOf(cluj, "self") + `"><span class="first">Living in Cluj</span><span class="meta">2019-09 to 2023-06</span></a>`,
		`<span class="first">Childhood</span><span class="meta">no boundaries</span>`,
		`<span class="first">Since the move</span><span class="meta">2024-03 to unknown</span>`,
		`<p class="sub">3 on this page</p>`, `>Record period</summary>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the periods lack %s", want)
		}
	}
	if strings.Contains(body, `aria-label="Properties"`) {
		t.Error("the periods are drawn by the generic view")
	}
}
