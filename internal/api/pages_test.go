package api_test

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
	"lifelog/internal/api/siren"
)

// TestSearchListsTitlesFirst: the search answers the pages whose title starts with the words before the pages whose
// words match, so a page is one search away by its title. The JSON lists those names in the order of their keys:
// "garden" before "garden log" (a space is before a letter) before "gardening draft", an old name that a rename kept,
// with the title of its page.
func TestSearchListsTitlesFirst(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	log := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Garden log", "body": "The roses."}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Roses", "body": "In the garden."}))
	garden := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Garden"}))
	draft := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Gardening draft"}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, draft, "rename"), map[string]string{"title": "Allotment"}))
	body := browse(t, h, "/search?q=garden")
	// The hits are in groups by kind (docs/plans/130-the-explore-pass.md, part 6): these are all pages.
	titles, words := strings.Index(body, `aria-label="Pages by title"`), strings.Index(body, `<section class="result-group" aria-label="Pages">`)
	if titles < 0 || words < 0 || titles > words {
		t.Fatalf("the search does not list the pages by title first:\n%.3000s", body)
	}
	if !strings.Contains(body[titles:words], `>Garden log</a>`) || strings.Contains(body[titles:words], `>Roses</a>`) {
		t.Errorf("the pages by title: %.600s", body[titles:words])
	}
	if !strings.Contains(body[words:], `>Roses</a>`) {
		t.Errorf("the pages by their words lack Roses: %.600s", body[words:])
	}
	// A page is in one group only: the words group leaves out the pages of the titles group.
	for _, in := range []string{`>Garden log</a>`, `>Garden</a>`, `>Allotment</a>`} {
		if strings.Contains(body[words:], in) {
			t.Errorf("the pages by their words list %s, which the titles list too: %.800s", in, body[words:])
		}
	}
	e := apitest.Must(t)(c.Get(t.Context(), "/search?q=garden"))
	var got []string
	for _, x := range props(e)["titles"].([]any) {
		m := x.(map[string]any)
		got = append(got, fmt.Sprintf("%v=%v %v %v", m["name"], m["title"], m["entity_type"], m["page_id"]))
	}
	for _, x := range props(e)["hits"].([]any) {
		for _, m := range props(e)["titles"].([]any) {
			if x.(map[string]any)["id"] == m.(map[string]any)["page_id"] {
				t.Errorf("the page %v is in the JSON hits and in the JSON titles", x.(map[string]any)["id"])
			}
		}
	}
	id := func(p *siren.Entity) any { return props(p)["id"] }
	want := []string{
		fmt.Sprintf("Garden=Garden page %v", id(garden)),
		fmt.Sprintf("Garden log=Garden log page %v", id(log)),
		fmt.Sprintf("Gardening draft=Allotment page %v", id(draft)),
	}
	if !slices.Equal(got, want) {
		t.Errorf("the JSON search carries the titles %q, want %q", got, want)
	}
}

// TestASearchOfOnlyBlanksIsTheEmptySearch: a search of spaces, a tab, a no-break space (U+00A0), an em space (U+2003)
// or an ideographic space (U+3000) has no words. It answers as the search with no q does: 200, the action search and
// no properties, so no hits and no titles, in JSON and in HTML.
func TestASearchOfOnlyBlanksIsTheEmptySearch(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "create-page"), map[string]string{"title": "Garden", "body": "The roses."}))
	for _, q := range []string{"", " ", "  \t ", " ", " ", "　  "} {
		path := "/search?q=" + url.QueryEscape(q)
		e, err := c.Get(t.Context(), path)
		if err != nil {
			t.Errorf("GET %s: %v", path, err)
			continue
		}
		if e.Properties != nil || len(e.Entities) != 0 || len(e.Actions) != 1 || e.Actions[0].Name != "search" {
			t.Errorf("GET %s: properties %v, %d entities, actions %v; want no properties, no entities and the action search",
				path, e.Properties, len(e.Entities), e.Actions)
		}
		browse(t, h, path)
	}
}

// TestSearchOrderNewestFirst: the field order of the action search is best (the default) or newest. Newest goes by the
// day of a page, a day page by its title, and keeps its order across the pages of the answer. The browser links the
// other order beside the heading of the words group, with the same words. An unknown order is a 422, in JSON and in HTML.
func TestSearchOrderNewestFirst(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	for _, day := range []string{"2021-05-05", "2098-01-01", "2020-03-01"} {
		apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/days/"+day)), "capture"), map[string]string{"text": "A lantern."}))
	}
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Shed", "body": "A lantern."}))
	titlesOf := func(e *siren.Entity) []string {
		var out []string
		for _, x := range props(e)["hits"].([]any) {
			out = append(out, x.(map[string]any)["title"].(string))
		}
		return out
	}
	best := apitest.Must(t)(c.Get(t.Context(), "/search?q=lantern"))
	if props(best)["order"] != "best" || len(titlesOf(best)) != 4 {
		t.Errorf("the default search: order %v, hits %q; want best and 4 hits", props(best)["order"], titlesOf(best))
	}
	newest := apitest.Must(t)(c.Get(t.Context(), "/search?q=lantern&order=newest"))
	want := []string{"2098-01-01", "Shed", "2021-05-05", "2020-03-01"} // Shed was made now, between the days
	if got := titlesOf(newest); props(newest)["order"] != "newest" || !slices.Equal(got, want) {
		t.Errorf("newest first: order %v, hits %q, want %q", props(newest)["order"], got, want)
	}
	first := apitest.Must(t)(c.Get(t.Context(), "/search?q=lantern&order=newest&limit=2"))
	if next := apitest.Href(first, "next"); !strings.Contains(next, "order=newest") {
		t.Errorf("the next link %q lost the order", next)
	}
	second := apitest.Must(t)(c.Get(t.Context(), apitest.Href(first, "next")))
	if got := append(titlesOf(first), titlesOf(second)...); !slices.Equal(got, want) {
		t.Errorf("the two pages of newest = %q, want %q", got, want)
	}
	if body := browse(t, h, "/search?q=lantern"); !strings.Contains(body, `href="/search?order=newest&amp;q=lantern"`) || !strings.Contains(body, ">Newest first</a>") {
		t.Errorf("the best-match page lacks the link Newest first:\n%.2500s", body)
	}
	body := browse(t, h, "/search?q=lantern&order=newest")
	if !strings.Contains(body, `href="/search?q=lantern"`) || !strings.Contains(body, ">Best match</a>") || strings.Contains(body, ">Newest first</a>") {
		t.Errorf("the newest page lacks the link Best match:\n%.2500s", body)
	}
	if strings.Contains(body, `name="order"`) {
		t.Errorf("the search form draws the field order, which is a link here:\n%.2500s", body)
	}
	for _, bad := range []string{"oldest", "NEWEST", "newest,best"} {
		path := "/search?q=lantern&order=" + url.QueryEscape(bad)
		_, err := c.Get(t.Context(), path)
		if apitest.Status(err) != 422 || !strings.Contains(err.Error(), "order must be one of best, newest") {
			t.Errorf("GET %s: %v, want a 422 that names the orders", path, err)
		}
		browseStatus(t, h, path, 422)
	}
}
