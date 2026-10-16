package api_test

import (
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// TestSearchListsTitlesFirst: the search answers the pages whose title starts with the words before the pages whose
// words match, so a page is one search away by its title.
func TestSearchListsTitlesFirst(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Garden log", "body": "The roses."}))
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Roses", "body": "In the garden."}))
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
	if ts, _ := props(e)["titles"].([]any); len(ts) != 1 {
		t.Errorf("the JSON search carries %d titles, want 1", len(ts))
	}
}
