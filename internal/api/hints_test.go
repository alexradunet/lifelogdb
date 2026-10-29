package api_test

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// TestAFormDrawsTheHintAndTheActionsPageTheDescription: a form says what the action does in the words of its hint.
// /actions lists every action with the text of the API.
func TestAFormDrawsTheHintAndTheActionsPageTheDescription(t *testing.T) {
	_, h, _ := apitest.Fresh(t, "cli")
	hint := "Make a person. The title is the short name that links use. The name is the full name."
	if page := browse(t, h, "/browse?kind=person"); !strings.Contains(page, hint) {
		t.Errorf("/browse?kind=person does not draw the hint of create-person")
	}
	page := browse(t, h, "/actions")
	if strings.Contains(page, hint) || !strings.Contains(page, "Create a person: title is the permanent handle") {
		t.Errorf("/actions does not draw the description of create-person")
	}
}

// TestARefusedReadingOfAnUnknownMetricLinksToTheFormOfTheMetric: the answer says the metric does not exist, in the
// words of the buttons, and links to the form that adds it with the name filled.
func TestARefusedReadingOfAnUnknownMetricLinksToTheFormOfTheMetric(t *testing.T) {
	_, h, _ := apitest.Fresh(t, "cli")
	rec := submit(h, "/measurements", url.Values{"metric": {"Ghost"}, "day": {"2026-10-12"}, "value": {"3"}}, "")
	body := rec.Body.String()
	if rec.Code != 404 || !strings.Contains(body, `href="/metrics?name=Ghost&amp;new=register-metric"`) || !strings.Contains(body, "New metric") {
		t.Errorf("status %d, and the page has no link to the form of the metric:\n%.3000s", rec.Code, body)
	}
}

// TestFilingNamesFewPagesAndCountsMany: the sentence of file-pages names up to three pages, else counts them.
func TestFilingNamesFewPagesAndCountsMany(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	var ids []string
	for _, title := range []string{"One", "Two", "Three", "Four"} {
		p := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": title}))
		ids = append(ids, fmt.Sprint(p.Properties.(map[string]any)["id"]))
	}
	file := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/browse")), "file-pages")
	for _, tc := range []struct {
		ids  []string
		want string
	}{
		{ids[:1], "Put One in Box (a new category)."}, {ids[:3], "Put One, Two and Three in Box."}, {ids, "Put 4 pages in Box."},
	} {
		done := apitest.Must(t)(c.Do(t.Context(), file, map[string]string{"ids": strings.Join(tc.ids, ","), "category": "Box"}))
		if done.Result != tc.want {
			t.Errorf("%d pages: %q, want %q", len(tc.ids), done.Result, tc.want)
		}
	}
}
