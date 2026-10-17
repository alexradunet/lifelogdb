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
	e := apitest.Must(t)(c.Get(t.Context(), "/search?q=garden"))
	var got []string
	for _, x := range props(e)["titles"].([]any) {
		m := x.(map[string]any)
		got = append(got, fmt.Sprintf("%v=%v %v %v", m["name"], m["title"], m["entity_type"], m["page_id"]))
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
