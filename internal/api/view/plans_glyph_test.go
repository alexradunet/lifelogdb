package view

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api/siren"
)

// TestAClosedLineShowsItsBoxAsAGlyph: a done, a dropped and a carried line each draw one box as an image with the
// class and the label of their state. None of them offers a tick (Done or Dropped); a carried line offers no form.
func TestAClosedLineShowsItsBoxAsAGlyph(t *testing.T) {
	line := func(n int, box, text string) map[string]any {
		return map[string]any{"page_id": 7, "line": n, "box": box, "text": text, "title": "Week"}
	}
	e := &siren.Entity{
		Class: []string{"page"}, Title: "Week",
		Properties: map[string]any{"id": 7, "period": "week", "entity_type": "page", "lines": []any{
			line(1, "x", "paid"), line(2, "-", "skipped"), line(3, ">", "moved"),
		}},
		Actions: []siren.Action{{Name: "tick", Method: "POST", Href: "/pages/{id}/tick", Fields: []siren.Field{
			{Name: "id", Type: "number", In: "path"}, {Name: "line", Type: "number"}, {Name: "text", Type: "text"}, {Name: "outcome", Type: "text"},
		}}},
	}
	u, _ := url.Parse("/pages/7")
	b, err := Draw(e, Facts{URL: u})
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	for _, c := range []struct{ word, glyph string }{{"done", "☑"}, {"dropped", "☒"}, {"carried", "▷"}} {
		want := `<span class="box ` + c.word + `" role="img" aria-label="` + c.word + `">` + c.glyph + `</span>`
		if n := strings.Count(html, want); n != 1 {
			t.Errorf("%d boxes %s, want 1", n, want)
		}
	}
	for _, no := range []string{`value="done"`, `value="dropped"`} {
		if strings.Contains(html, no) {
			t.Errorf("a closed line offers a tick: %s", no)
		}
	}
	// A done or dropped line has one quiet button, Reopen, and a carried line has no form at all.
	if n := len(regexp.MustCompile(`class="tick-line"`).FindAllString(html, -1)); n != 2 {
		t.Errorf("%d tick forms, want 2 (Reopen of done and of dropped)", n)
	}
}
