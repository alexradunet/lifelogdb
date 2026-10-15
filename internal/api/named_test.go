package api_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestPeopleListCarriesItsMentions: the rows of the people list of Browse say how many pages link to each person and
// the newest day page among them, in the order of the item links. The sheet says it in words.
func TestPeopleListCarriesItsMentions(t *testing.T) {
	c, h := freshOn(t, "2026-10-12") // the day the words are read against: a day of this year has no year
	root := must(c.Get(t.Context(), "/"))
	catalog := must(c.Get(t.Context(), "/actions"))
	for _, name := range []string{"Ana", "Sam", "Zed"} {
		must(c.Do(t.Context(), find(root, "create-person"), map[string]string{"title": name}))
	}
	must(c.Do(t.Context(), find(catalog, "capture"), map[string]string{"day": "2026-10-05", "text": "Lunch with [[Ana]]"}))
	must(c.Do(t.Context(), find(catalog, "capture"), map[string]string{"day": "2026-10-09", "text": "Walk with [[Ana]] and [[Sam]]"}))
	must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Trip", "body": "[[Ana]] came"}))

	list := must(c.Get(t.Context(), "/browse?kind=person"))
	rows, _ := list.Properties.(map[string]any)["rows"].([]any)
	type want struct {
		title    string
		mentions float64
		lastDay  string
	}
	wants := []want{{"Ana", 3, "2026-10-09"}, {"Sam", 1, "2026-10-09"}, {"Zed", 0, ""}}
	if len(rows) != len(wants) || len(list.Entities) != len(wants) {
		t.Fatalf("rows = %v, items = %d, want %d of each", rows, len(list.Entities), len(wants))
	}
	for i, w := range wants {
		row, _ := rows[i].(map[string]any)
		lastDay, _ := row["last_day"].(string)
		// The client may give a Go integer or a decoded JSON number: compare the printed value.
		if row["title"] != w.title || fmt.Sprint(row["mentions"]) != fmt.Sprint(w.mentions) || lastDay != w.lastDay {
			t.Errorf("row %d = %v, want %+v", i, row, w)
		}
		if list.Entities[i].Title != w.title {
			t.Errorf("item %d is %q, want %q (the order of rows)", i, list.Entities[i].Title, w.title)
		}
	}
	if _, has := rows[2].(map[string]any)["last_day"]; has {
		t.Errorf("a person with no day page has last_day: %v", rows[2])
	}

	body := page(t, h, "/browse?kind=person")
	for _, s := range []string{
		"mentioned on 3 pages, last Fri 9 Oct",
		"mentioned on 1 page, last Fri 9 Oct",
		"not mentioned yet",
		`<a class="entry named face" href="` + list.Entities[0].Href + `">`,
	} {
		if !strings.Contains(body, s) {
			t.Errorf("the people sheet lacks %q", s)
		}
	}
}

// TestPeopleListShowsThePictureAPersonEmbeds: in the people list of Browse, a person whose page embeds a picture has
// that picture's preview in its row, and the sheet draws it round before the name; a person with none has the icon of a person. A place has
// neither, even when its page embeds a picture.
func TestPeopleListShowsThePictureAPersonEmbeds(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	ctx := t.Context()
	root := must(c.Get(ctx, "/"))
	jpg, _ := jpegFile(t, "ana.jpg", 40, 30)
	photo := must(c.DoFiles(ctx, find(root, "add-file"), map[string]string{"title": "Ana face.jpg"}, map[string]string{"original": jpg}))
	ana := must(c.Do(ctx, find(root, "create-person"), map[string]string{"title": "Ana"}))
	must(c.Do(ctx, find(ana, "save-body"), map[string]string{"body": "![[Ana face.jpg]]\n\nMet at school."}))
	must(c.Do(ctx, find(root, "create-person"), map[string]string{"title": "Sam"}))
	lake := must(c.Do(ctx, find(root, "create-place"), map[string]string{"title": "Lake"}))
	must(c.Do(ctx, find(lake, "save-body"), map[string]string{"body": "![[Ana face.jpg]]"}))

	rows, _ := props(must(c.Get(ctx, "/browse?kind=person")))["rows"].([]any)
	if len(rows) != 2 {
		t.Fatalf("rows = %v, want Ana and Sam", rows)
	}
	want := hrefOf(photo, "preview")
	if got := str(rows[0].(map[string]any)["picture"]); want == "" || got != want {
		t.Errorf("Ana's picture = %q, want %q", got, want)
	}
	if _, has := rows[1].(map[string]any)["picture"]; has {
		t.Errorf("Sam has a picture: %v", rows[1])
	}
	body := page(t, h, "/browse?kind=person")
	for _, s := range []string{`<img class="face-picture" src="` + want + `" alt="" loading="lazy">`, `<span class="face-picture" aria-hidden="true">`} {
		if !strings.Contains(body, s) {
			t.Errorf("the people sheet lacks %q", s)
		}
	}
	places, _ := props(must(c.Get(ctx, "/browse?kind=place")))["rows"].([]any)
	if len(places) != 1 || places[0].(map[string]any)["picture"] != nil {
		t.Errorf("the places rows = %v, want Lake with no picture", places)
	}
	if body := page(t, h, "/browse?kind=place"); strings.Contains(body, `class="face-picture"`) {
		t.Errorf("the places sheet draws a picture of a face")
	}
}

// TestMentionsHaveNoDayWhenNoDayPageLinks: a page that is not a day page gives a count and no last day.
func TestMentionsHaveNoDayWhenNoDayPageLinks(t *testing.T) {
	c, h := freshOn(t, "2026-10-12") // the day the words are read against: a day of this year has no year
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "create-place"), map[string]string{"title": "Lake"}))
	must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Trip", "body": "At [[Lake]]"}))
	must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Swim", "body": "In [[Lake]]"}))
	if body := page(t, h, "/browse?kind=place"); !strings.Contains(body, "mentioned on 2 pages</span>") {
		t.Errorf("the places sheet lacks the count without a day:\n%.2000s", body)
	}
}

// TestDaysHaveNoRows: the list of the days is its item links alone; the lists of Browse carry rows.
func TestDaysHaveNoRows(t *testing.T) {
	c, _ := fresh(t)
	if props, _ := must(c.Get(t.Context(), "/days")).Properties.(map[string]any); props["rows"] != nil {
		t.Errorf("GET /days has rows: %v", props["rows"])
	}
}
