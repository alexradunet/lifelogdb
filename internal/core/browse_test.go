package core

import (
	"fmt"
	"maps"
	"strings"
	"testing"
)

// browseFixture writes, through the writer, one of each kind in and out of the categories Family, Cousins (under
// Family) and Work, and returns the ids by title. Hand-checked layout:
//
//	Family:  Beta, Cousins, Ana, photo.jpg, scan.pdf, the day page 2031-05-01, Old (tombstoned)
//	Cousins: Gamma
//	Work:    Delta, Harbor, Weight
//	nowhere: Alpha, Bo, Lakeside, memo.m4a, notes.txt, Walk, Mood (the metric every file has)
func browseFixture(t *testing.T) (*Store, map[string]int64) {
	t.Helper()
	s := fresh(t)
	ids := map[string]int64{}
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, title := range []string{"Alpha", "Beta", "Gamma", "Delta", "Old"} {
		id, _, err := s.CreatePage(ctx, "cli", title, "", "")
		check(err)
		ids[title] = id
	}
	for _, title := range []string{"Ana", "Bo"} {
		id, err := s.CreatePerson(ctx, "cli", title, "", "", "")
		check(err)
		ids[title] = id
	}
	for _, title := range []string{"Lakeside", "Harbor"} {
		id, err := s.CreatePlace(ctx, "cli", title)
		check(err)
		ids[title] = id
	}
	for i, f := range []FileIn{{Title: "photo.jpg", MIME: "image/jpeg", Day: "2031-05-02"}, {Title: "memo.m4a", MIME: "audio/mp4", Day: "2031-05-01"},
		{Title: "scan.pdf", MIME: "application/pdf", Day: "2031-04-01"}, {Title: "notes.txt", MIME: "text/plain", Day: "2031-03-01"}} {
		f.SHA256 = strings.Repeat(fmt.Sprintf("%02x", i+1), 32)
		k, err := s.AddFile(ctx, "cli", f)
		check(err)
		ids[f.Title] = k.ID
	}
	for _, m := range [][2]string{{"Weight", "kg"}, {"Walk", ""}} {
		_, err := s.RegisterMetric(ctx, "cli", m[0], m[1], "")
		check(err)
		id, err := s.PageID(ctx, m[0])
		check(err)
		ids[m[0]] = id
	}
	day, _, err := s.Capture(ctx, "cli", "2031-05-01", "Lunch with [[Ana]] at [[Lakeside]]", nil)
	check(err)
	ids["2031-05-01"] = day
	file := func(category string, titles ...string) {
		t.Helper()
		var list []int64
		for _, title := range titles {
			list = append(list, ids[title])
		}
		f, err := s.FilePages(ctx, "cli", list, category, false)
		check(err)
		ids[category] = f.Category.ID
	}
	file("Family", "Beta", "Ana", "photo.jpg", "scan.pdf", "2031-05-01", "Old")
	file("Cousins", "Gamma")
	file("Family", "Cousins")
	file("Work", "Delta", "Harbor", "Weight")
	check(s.Tombstone(ctx, "cli", ids["Old"]))
	return s, ids
}

func pageTitles(rows []PageRow) string {
	var out []string
	for _, r := range rows {
		title := r.Title
		if r.Category {
			title += "*"
		}
		out = append(out, title)
	}
	return strings.Join(out, ",")
}

// TestPlainPagesInEachScope: the plain pages by title, a category marked; in a category only what is filed directly
// in it; not filed, the pages in no category that are not categories, the rows of Unfiled. A day page and a
// tombstone are in no list.
func TestPlainPagesInEachScope(t *testing.T) {
	s, ids := browseFixture(t)
	for _, tc := range []struct {
		name string
		sc   Scope
		want string
	}{
		{"everything", Scope{}, "Alpha,Beta,Cousins*,Delta,Family*,Gamma,Work*"},
		{"in Family", Scope{In: ids["Family"]}, "Beta,Cousins*"},
		{"in Cousins", Scope{In: ids["Cousins"]}, "Gamma"},
		{"not filed", Scope{Unfiled: true}, "Alpha"},
	} {
		got, err := s.PlainPages(ctx, tc.sc, 50, 0)
		if err != nil || pageTitles(got) != tc.want {
			t.Errorf("%s: %q %v, want %q", tc.name, pageTitles(got), err, tc.want)
		}
	}
	if got, err := s.PlainPages(ctx, Scope{}, 2, 2); err != nil || pageTitles(got) != "Cousins*,Delta" {
		t.Errorf("a page of two after two: %q %v", pageTitles(got), err)
	}
	unfiled, err := s.Unfiled(ctx, 50, 0)
	if err != nil || len(unfiled) != 1 || unfiled[0].Title != "Alpha" || unfiled[0].ID != ids["Alpha"] {
		t.Errorf("Unfiled: %+v %v", unfiled, err)
	}
}

// TestTheKindsInEachScope: people and places with their mentions, media and documents newest day first, metrics by
// title with their unit, each in the three scopes.
func TestTheKindsInEachScope(t *testing.T) {
	s, ids := browseFixture(t)
	named := func(typ string, sc Scope) string {
		t.Helper()
		rows, err := s.NamedMentions(ctx, typ, sc, 50, 0)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range rows {
			out = append(out, fmt.Sprintf("%s:%d:%s", r.Title, r.Mentions, r.LastDay))
		}
		return strings.Join(out, ",")
	}
	files := func(media bool, kind string, sc Scope) string {
		t.Helper()
		var rows []FileRow
		var err error
		if media {
			rows, err = s.Media(ctx, kind, sc, 50, 0)
		} else {
			rows, err = s.OtherFiles(ctx, sc, 50, 0)
		}
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range rows {
			out = append(out, r.Title+":"+r.Day)
		}
		return strings.Join(out, ",")
	}
	metrics := func(sc Scope) string {
		t.Helper()
		rows, err := s.MetricRows(ctx, sc, 50, 0)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range rows {
			out = append(out, r.Title+":"+r.Unit)
		}
		return strings.Join(out, ",")
	}
	family, unfiled := Scope{In: ids["Family"]}, Scope{Unfiled: true}
	for _, tc := range []struct{ name, got, want string }{
		{"people", named("person", Scope{}), "Ana:1:2031-05-01,Bo:0:"},
		{"people in Family", named("person", family), "Ana:1:2031-05-01"},
		{"people not filed", named("person", unfiled), "Bo:0:"},
		{"places", named("place", Scope{}), "Harbor:0:,Lakeside:1:2031-05-01"},
		{"places in Family", named("place", family), ""},
		{"places in Work", named("place", Scope{In: ids["Work"]}), "Harbor:0:"},
		{"places not filed", named("place", unfiled), "Lakeside:1:2031-05-01"},
		{"media", files(true, "", Scope{}), "photo.jpg:2031-05-02,memo.m4a:2031-05-01"},
		{"recordings", files(true, "audio", Scope{}), "memo.m4a:2031-05-01"},
		{"media in Family", files(true, "", family), "photo.jpg:2031-05-02"},
		{"media not filed", files(true, "", unfiled), "memo.m4a:2031-05-01"},
		{"pictures not filed", files(true, "image", unfiled), ""},
		{"documents", files(false, "", Scope{}), "scan.pdf:2031-04-01,notes.txt:2031-03-01"},
		{"documents in Family", files(false, "", family), "scan.pdf:2031-04-01"},
		{"documents not filed", files(false, "", unfiled), "notes.txt:2031-03-01"},
		{"metrics", metrics(Scope{}), "Mood:1-5,Walk:,Weight:kg"},
		{"metrics in Work", metrics(Scope{In: ids["Work"]}), "Weight:kg"},
		{"metrics not filed", metrics(unfiled), "Mood:1-5,Walk:"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// TestFiledInListsTheCategoriesFirst: All in a category is every live thing filed directly in it, the categories
// first, then by title, each with its kind; a day page and a tombstone are not in it, nor what a sub-category holds.
func TestFiledInListsTheCategoriesFirst(t *testing.T) {
	s, ids := browseFixture(t)
	rows, err := s.FiledIn(ctx, ids["Family"], 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rows {
		got = append(got, fmt.Sprintf("%s:%s:%s:%v:%s", r.Title, r.Type, r.Kind, r.Category, r.MIME))
	}
	want := "Cousins:page:page:true:,Ana:person:person:false:,Beta:page:page:false:,photo.jpg:file:media:false:image/jpeg,scan.pdf:file:document:false:application/pdf"
	if strings.Join(got, ",") != want {
		t.Errorf("FiledIn(Family):\n got %s\nwant %s", strings.Join(got, ","), want)
	}
	if rows[0].ID != ids["Cousins"] || rows[1].ID != ids["Ana"] {
		t.Errorf("ids: %+v", rows[:2])
	}
	// The tree counts the same things: the day page and the tombstone filed in Family are not counted.
	cats, err := s.Categories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cats {
		if c.ID == ids["Family"] && c.Count != len(rows) {
			t.Errorf("Family counts %d, All lists %d", c.Count, len(rows))
		}
	}
	if page, err := s.FiledIn(ctx, ids["Family"], 2, 3); err != nil || len(page) != 2 || page[0].Title != "photo.jpg" || page[1].Title != "scan.pdf" {
		t.Errorf("the last page of two: %+v %v", page, err)
	}
}

// TestBrowseCountsEachKindInEachScope: one count a kind in the scope, all their sum, and the ghost pages.
func TestBrowseCountsEachKindInEachScope(t *testing.T) {
	s, ids := browseFixture(t)
	for _, tc := range []struct {
		name string
		sc   Scope
		want map[string]int
	}{
		{"everything", Scope{}, map[string]int{"page": 7, "person": 2, "place": 2, "media": 2, "document": 2, "metric": 3, "all": 18, "ghosts": 0}},
		{"in Family", Scope{In: ids["Family"]}, map[string]int{"page": 2, "person": 1, "place": 0, "media": 1, "document": 1, "metric": 0, "all": 5, "ghosts": 0}},
		{"in Work", Scope{In: ids["Work"]}, map[string]int{"page": 1, "person": 0, "place": 1, "media": 0, "document": 0, "metric": 1, "all": 3, "ghosts": 0}},
		{"not filed", Scope{Unfiled: true}, map[string]int{"page": 1, "person": 1, "place": 1, "media": 1, "document": 1, "metric": 2, "all": 7, "ghosts": 0}},
	} {
		got, err := s.BrowseCounts(ctx, tc.sc)
		if err != nil || !maps.Equal(got, tc.want) {
			t.Errorf("%s: %v %v, want %v", tc.name, got, err, tc.want)
		}
	}
}

// TestBrowseCountsTheGhostPages: the count of the ghost_pages view, a day page left out (a day page is never in
// Browse). The ghosts are inserted by direct SQL with an old created_at (bulk setup, as the hygiene test does:
// ghost_pages wants them older than 30 days, and created_at never changes); a page written now is not a ghost. The
// writer makes an empty day page with no links too (CreatePage with a date as the title); the old one here has that
// shape.
func TestBrowseCountsTheGhostPages(t *testing.T) {
	s := fresh(t)
	if _, _, err := s.CreatePage(ctx, "cli", "Fresh", "", ""); err != nil {
		t.Fatal(err)
	}
	err := s.Do(ctx, "cli", func(tx *Tx) error {
		for i, title := range []string{"Ghost A", "Ghost B", "2000-02-01"} {
			var id int64
			at := fmt.Sprintf("2000-01-01T00:00:0%d.000Z", i+1)
			var day any
			if title == "2000-02-01" {
				day = title
			}
			if err := tx.tx.QueryRowContext(ctx, `INSERT INTO entities(entity_type, preferred_name_key, day, body, created_at, updated_at, source)
			    VALUES ('page', ?, ?, '', ?, ?, 'cli') RETURNING id`, strings.ToLower(title), day, at, at).Scan(&id); err != nil {
				return err
			}
			if _, err := tx.tx.ExecContext(ctx, `INSERT INTO entity_names(entity_id, title, name_key) VALUES (?, ?, ?)`, id, title, strings.ToLower(title)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var inView int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM ghost_pages`).Scan(&inView); err != nil || inView != 3 {
		t.Fatalf("the view has %d rows, %v: the setup wants the old day page in it", inView, err)
	}
	got, err := s.BrowseCounts(ctx, Scope{})
	if err != nil || got["ghosts"] != 2 || got["page"] != 3 {
		t.Errorf("counts: %v %v, want 2 ghosts among 3 pages", got, err)
	}
	ghosts, err := s.Ghosts(ctx, 10, 0)
	if err != nil || len(ghosts) != 2 || ghosts[0].Title != "Ghost A" || ghosts[1].Title != "Ghost B" {
		t.Errorf("Ghosts: %+v %v, want Ghost A, Ghost B", ghosts, err)
	}
}

// TestNotFiledCountsOnlyALiveCategory: a tombstoned category is not in the tree, so a thing filed only in it is in no
// category. It is in Not filed, and the chip counts it.
func TestNotFiledCountsOnlyALiveCategory(t *testing.T) {
	s, ids := browseFixture(t)
	cy, err := s.CreatePerson(ctx, "cli", "Cy", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	eps, _, err := s.CreatePage(ctx, "cli", "Eps", "", "")
	if err != nil {
		t.Fatal(err)
	}
	gone, err := s.FilePages(ctx, "cli", []int64{cy, eps, ids["Bo"]}, "Gone", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.FilePages(ctx, "cli", []int64{ids["Bo"]}, "Work", false); err != nil { // Bo stays in a live one
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", gone.Category.ID); err != nil {
		t.Fatal(err)
	}
	cats, err := s.Categories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cats {
		if c.ID == gone.Category.ID {
			t.Errorf("the tree draws the tombstoned category: %+v", c)
		}
	}
	unfiled := Scope{Unfiled: true}
	people, err := s.NamedMentions(ctx, "person", unfiled, 50, 0)
	if err != nil || len(people) != 1 || people[0].ID != cy {
		t.Errorf("people not filed: %+v %v, want Cy", people, err)
	}
	if pages, err := s.PlainPages(ctx, unfiled, 50, 0); err != nil || pageTitles(pages) != "Alpha,Eps" {
		t.Errorf("pages not filed: %q %v, want Alpha,Eps", pageTitles(pages), err)
	}
	if list, err := s.Unfiled(ctx, 50, 0); err != nil || len(list) != 2 || list[1].ID != eps {
		t.Errorf("Unfiled: %+v %v, want Alpha, Eps", list, err)
	}
	if got, err := s.BrowseCounts(ctx, unfiled); err != nil || got["person"] != 1 || got["page"] != 2 {
		t.Errorf("counts not filed: %v %v, want 1 person and 2 pages", got, err)
	}
}

// TestBrowseOrdersTitlesWithoutCase: a list by title is in the order of the folded title, as the tree and the
// metrics board are: a lowercase title is not put after every capital.
func TestBrowseOrdersTitlesWithoutCase(t *testing.T) {
	s := fresh(t)
	var pages []int64
	for _, title := range []string{"reading", "Alpha", "Zeta"} {
		id, _, err := s.CreatePage(ctx, "cli", title, "", "")
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, id)
	}
	for _, title := range []string{"bo", "Ana", "Cy"} {
		if _, err := s.CreatePerson(ctx, "cli", title, "", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []string{"commits", "Weight"} {
		if _, err := s.RegisterMetric(ctx, "cli", m, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.FilePages(ctx, "cli", pages, "Shelf", false); err != nil {
		t.Fatal(err)
	}
	shelf, err := s.PageID(ctx, "Shelf")
	if err != nil {
		t.Fatal(err)
	}
	titles := func(rows any, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		switch rs := rows.(type) {
		case []PageRow:
			for _, r := range rs {
				out = append(out, r.Title)
			}
		case []NamedRow:
			for _, r := range rs {
				out = append(out, r.Title)
			}
		case []MetricRow:
			for _, r := range rs {
				out = append(out, r.Title)
			}
		case []FiledRow:
			for _, r := range rs {
				out = append(out, r.Title)
			}
		case []Edge:
			for _, r := range rs {
				out = append(out, r.Title)
			}
		}
		return strings.Join(out, ",")
	}
	for _, tc := range []struct{ name, got, want string }{
		{"pages", titles(s.PlainPages(ctx, Scope{}, 50, 0)), "Alpha,reading,Shelf,Zeta"},
		{"people", titles(s.NamedMentions(ctx, "person", Scope{}, 50, 0)), "Ana,bo,Cy"},
		{"people, Named", titles(s.Named(ctx, "person", 50, 0)), "Ana,bo,Cy"},
		{"metrics", titles(s.MetricRows(ctx, Scope{}, 50, 0)), "commits,Mood,Weight"},
		{"in Shelf", titles(s.FiledIn(ctx, shelf, 50, 0)), "Alpha,reading,Zeta"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, tc.got, tc.want)
		}
	}
	for _, title := range []string{"Delta", "beta"} {
		if _, _, err := s.CreatePage(ctx, "cli", title, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if got := titles(s.Unfiled(ctx, 50, 0)); got != "beta,Delta" {
		t.Errorf("Unfiled: %q", got)
	}
}
