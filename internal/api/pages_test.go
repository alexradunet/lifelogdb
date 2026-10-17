package api_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
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
	id := func(p *api.Entity) any { return props(p)["id"] }
	want := []string{
		fmt.Sprintf("Garden=Garden page %v", id(garden)),
		fmt.Sprintf("Garden log=Garden log page %v", id(log)),
		fmt.Sprintf("Gardening draft=Allotment page %v", id(draft)),
	}
	if !slices.Equal(got, want) {
		t.Errorf("the JSON search carries the titles %q, want %q", got, want)
	}
}
