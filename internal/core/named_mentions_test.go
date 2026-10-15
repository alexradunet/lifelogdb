package core

import "testing"

// TestNamedMentionsCountLivePagesAndTheNewestDay: a person is mentioned once by each live page that links to it.
// A tombstoned page does not count, and only a day page gives the last day. The order is the order of Named.
func TestNamedMentionsCountLivePagesAndTheNewestDay(t *testing.T) {
	s := fresh(t)
	for _, name := range []string{"Sam", "Ana", "Zed"} {
		if _, err := s.CreatePerson(ctx, "cli", name, "", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.Capture(ctx, "cli", "2026-10-05", "Lunch with [[Ana]]", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Capture(ctx, "cli", "2026-10-09", "Walk with [[Ana]] and [[Sam]]", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Trip", "[[Ana]] came"); err != nil {
		t.Fatal(err)
	}
	gone, _, err := s.CreatePage(ctx, "cli", "Old note", "[[Sam]] was there")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", gone); err != nil {
		t.Fatal(err)
	}

	got, err := s.NamedMentions(ctx, "person", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Ana: two day pages and Trip. Sam: one day page (the tombstoned page does not count). Zed: none.
	want := []struct {
		title    string
		mentions int
		lastDay  string
	}{{"Ana", 3, "2026-10-09"}, {"Sam", 1, "2026-10-09"}, {"Zed", 0, ""}}
	if len(got) != len(want) {
		t.Fatalf("NamedMentions = %+v, want %d rows", got, len(want))
	}
	for i, w := range want {
		if got[i].Title != w.title || got[i].Mentions != w.mentions || got[i].LastDay != w.lastDay {
			t.Errorf("row %d = %+v, want %+v", i, got[i], w)
		}
	}

	named, err := s.Named(ctx, "person", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(named) != len(got) {
		t.Fatalf("Named has %d rows, NamedMentions %d", len(named), len(got))
	}
	for i, e := range named {
		if got[i].ID != e.ID {
			t.Errorf("row %d is page %d, Named has page %d", i, got[i].ID, e.ID)
		}
	}
	second, err := s.NamedMentions(ctx, "person", 1, 1)
	if err != nil || len(second) != 1 || second[0].Title != "Sam" {
		t.Errorf("NamedMentions(1, 1) = %+v, %v, want Sam", second, err)
	}
}

// TestNamedMentionsGiveAPersonTheFirstPictureItsPageEmbeds: a person's picture is the first file its body embeds
// that keeps a preview and is live. A file with no preview, a tombstoned file and a plain [[link]] are passed over.
func TestNamedMentionsGiveAPersonTheFirstPictureItsPageEmbeds(t *testing.T) {
	s := fresh(t)
	pic := smallJPEG(t)
	keep := func(title, mime string, c byte, preview []byte) int64 {
		t.Helper()
		k, err := s.AddFile(ctx, "cli", FileIn{Title: title, SHA256: sha(c), MIME: mime, Day: "2026-10-01", Preview: preview})
		if err != nil {
			t.Fatal(err)
		}
		return k.ID
	}
	keep("Notes.pdf", "application/pdf", 'a', nil)
	face := keep("Ana face.jpg", "image/jpeg", 'b', pic)
	keep("Beach.jpg", "image/jpeg", 'c', pic)
	gone := keep("Old face.jpg", "image/jpeg", 'd', pic)
	bodies := map[string]string{
		"Ana": "![[Notes.pdf]] Met at school.\n\n![[Ana face.jpg]]\n\n![[Beach.jpg]]",
		"Sam": "![[Old face.jpg]] and [[Beach.jpg]], a link and not an embed",
		"Zed": "",
	}
	for _, name := range []string{"Ana", "Sam", "Zed"} {
		id, err := s.CreatePerson(ctx, "cli", name, "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if bodies[name] != "" {
			if _, err := s.SaveBody(ctx, "cli", id, bodies[name], "1"); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := s.Tombstone(ctx, "cli", gone); err != nil { // after the save, which revives a page it links
		t.Fatal(err)
	}

	got, err := s.NamedMentions(ctx, "person", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{"Ana": face, "Sam": 0, "Zed": 0}
	if len(got) != len(want) {
		t.Fatalf("NamedMentions = %+v, want %d rows", got, len(want))
	}
	for _, r := range got {
		if r.Picture != want[r.Title] {
			t.Errorf("%s: picture %d, want %d", r.Title, r.Picture, want[r.Title])
		}
	}
}

// TestNamedMentionsGiveAPlaceItsPoint: a place with a row in places has its point in its row, and a place without
// one has none. A person never has a point.
func TestNamedMentionsGiveAPlaceItsPoint(t *testing.T) {
	s := fresh(t)
	placeID(t, s, "Lake", Point{Lat: 45.5, Lon: -0.25, RadiusM: 800, LinkDays: false})
	if _, err := s.CreatePlace(ctx, "cli", "Somewhere"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePerson(ctx, "cli", "Ana", "", "", ""); err != nil {
		t.Fatal(err)
	}

	got, err := s.NamedMentions(ctx, "place", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Title != "Lake" || got[1].Title != "Somewhere" {
		t.Fatalf("NamedMentions = %+v, want Lake and Somewhere", got)
	}
	if p := got[0].Point; p == nil || *p != (Point{Lat: 45.5, Lon: -0.25, RadiusM: 800, LinkDays: false}) {
		t.Errorf("Lake's point = %+v, want 45.5, -0.25, 800 m, not linked", p)
	}
	if got[1].Point != nil {
		t.Errorf("Somewhere has a point: %+v", got[1].Point)
	}
	people, err := s.NamedMentions(ctx, "person", 10, 0)
	if err != nil || len(people) != 1 || people[0].Point != nil {
		t.Errorf("people = %+v, %v, want Ana with no point", people, err)
	}
}
