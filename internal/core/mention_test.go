package core

import "testing"

// TestBacklinksShowTheSentenceOfEachMention: a page's wikilink backlinks carry the words of the block that names the
// page, by any name that the page has kept. The day pages come first, newest first, then the other pages by title.
// Another kind of edge has no sentence. A source that names the page only in a code span has none.
func TestBacklinksShowTheSentenceOfEachMention(t *testing.T) {
	s := fresh(t)
	ana, err := s.CreatePerson(ctx, "cli", "Ana", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for title, body := range map[string]string{
		"2026-10-01": "Ran with [[Ana]] today.\n\nNothing else.",
		"2026-10-03": "Dinner.\n\n- walked\n- lunch with **[[Ana]]** and Bo",
		"Notes":      "Ask [[Ana]] about the lake.",
	} {
		if _, _, err := s.CreatePage(ctx, "cli", title, body, ""); err != nil {
			t.Fatal(err)
		}
	}
	// A rename keeps the old name: the 2026-10-02 page names the person by the new name, the others by the old one.
	if _, err := s.Rename(ctx, "cli", ana, "Ana Popescu"); err != nil {
		t.Fatal(err)
	}
	for title, body := range map[string]string{
		"2026-10-02": "Met [[Ana Popescu|Ana P.]] at the lake.",
		"Code":       "Typed `[[Ana]]` in the shell.",
	} {
		if _, _, err := s.CreatePage(ctx, "cli", title, body, ""); err != nil {
			t.Fatal(err)
		}
	}

	p, err := s.PageByID(ctx, ana)
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ title, context string }
	var got []row
	for _, e := range p.In {
		if e.Kind == "wikilink" {
			got = append(got, row{e.Title, e.Context})
		}
	}
	want := []row{
		{"2026-10-03", "lunch with Ana and Bo"},
		{"2026-10-02", "Met Ana P. at the lake."},
		{"2026-10-01", "Ran with Ana today."},
		{"Notes", "Ask Ana about the lake."},
	}
	if len(got) < len(want) {
		t.Fatalf("wikilink backlinks = %+v, want %+v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("backlink %d = %+v, want %+v (all: %+v)", i, got[i], w, got)
		}
	}
	for _, r := range got[len(want):] {
		if r.title != "Code" || r.context != "" {
			t.Errorf("backlink %+v: want only Code, with no sentence", r)
		}
	}
}
