package api_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestPeopleListCarriesItsMentions: the rows of /people say how many pages link to each person and the newest day
// page among them, in the order of the item links. The sheet says it in words.
func TestPeopleListCarriesItsMentions(t *testing.T) {
	c, h := fresh(t)
	root := must(c.Get(t.Context(), "/"))
	catalog := must(c.Get(t.Context(), "/actions"))
	for _, name := range []string{"Ana", "Sam", "Zed"} {
		must(c.Do(t.Context(), find(root, "create-person"), map[string]string{"title": name}))
	}
	must(c.Do(t.Context(), find(catalog, "capture"), map[string]string{"day": "2026-10-05", "text": "Lunch with [[Ana]]"}))
	must(c.Do(t.Context(), find(catalog, "capture"), map[string]string{"day": "2026-10-09", "text": "Walk with [[Ana]] and [[Sam]]"}))
	must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Trip", "body": "[[Ana]] came"}))

	list := must(c.Get(t.Context(), "/people"))
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

	body := page(t, h, "/people")
	for _, s := range []string{
		"mentioned on 3 pages, last Fri 9 Oct 2026",
		"mentioned on 1 page, last Fri 9 Oct 2026",
		"not mentioned yet",
		`<a class="entry named" href="` + list.Entities[0].Href + `">`,
	} {
		if !strings.Contains(body, s) {
			t.Errorf("the people sheet lacks %q", s)
		}
	}
}

// TestMentionsHaveNoDayWhenNoDayPageLinks: a page that is not a day page gives a count and no last day.
func TestMentionsHaveNoDayWhenNoDayPageLinks(t *testing.T) {
	c, h := fresh(t)
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "create-place"), map[string]string{"title": "Lake"}))
	must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Trip", "body": "At [[Lake]]"}))
	must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Swim", "body": "In [[Lake]]"}))
	if body := page(t, h, "/places"); !strings.Contains(body, "mentioned on 2 pages</span>") {
		t.Errorf("the places sheet lacks the count without a day:\n%.2000s", body)
	}
}

// TestFilesListHasNoRows: only people and places carry rows.
func TestFilesListHasNoRows(t *testing.T) {
	c, _ := fresh(t)
	for _, path := range []string{"/files", "/days", "/ghosts"} {
		if props, _ := must(c.Get(t.Context(), path)).Properties.(map[string]any); props["rows"] != nil {
			t.Errorf("GET %s has rows: %v", path, props["rows"])
		}
	}
}
