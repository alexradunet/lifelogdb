package core

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestMetricNoteLinks(t *testing.T) {
	s := fresh(t)
	if _, err := s.CreatePerson(ctx, "cli", "Bob Sample", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Diet Plan", "owner note", ""); err != nil {
		t.Fatal(err)
	}
	note := "Discuss with [[Bob Sample]], [[Diet Plan]], [[diet plan|again]], #health #Health and [[Health/Diet]]."
	want := "Bob Sample,Diet Plan,health"
	if added, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "Fresh Link Metric", Unit: "count", Note: note}); err != nil || !added {
		t.Fatalf("fresh metric with a note: %v %v", added, err)
	}
	fresh := pageByTitle(t, s, "Fresh Link Metric")
	if fresh.Body != note || titles(fresh.Out, "wikilink") != want {
		t.Fatalf("fresh metric links: body %q links %v", fresh.Body, fresh.Out)
	}
	for _, target := range []string{"Bob Sample", "Diet Plan", "health"} {
		assertBacklinks(t, s, target, "Fresh Link Metric")
	}
	if id, err := s.PageID(ctx, "Health/Diet"); err != nil || id != 0 {
		t.Fatalf("invalid target became page %d: %v", id, err)
	}

	ghost, _, err := s.CreatePage(ctx, "cli", "Promoted Link Metric", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if added, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "promoted link metric", Unit: "count", Note: note}); err != nil || !added {
		t.Fatalf("promoted metric with a note: %v %v", added, err)
	}
	promoted, err := s.PageByID(ctx, ghost)
	if err != nil {
		t.Fatal(err)
	}
	if promoted.Type != "metric" || promoted.Body != note || titles(promoted.Out, "wikilink") != want {
		t.Fatalf("promoted metric links: %+v", promoted)
	}
	for _, target := range []string{"Bob Sample", "Diet Plan", "health"} {
		assertBacklinks(t, s, target, "Fresh Link Metric,Promoted Link Metric")
	}

	if added, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "Fresh Link Metric", Unit: "count", Note: "Replacement [[Changed Target]] #other"}); err != nil || added {
		t.Fatalf("re-registering a metric with a body: %v %v", added, err)
	}
	fresh = pageByTitle(t, s, "Fresh Link Metric")
	if fresh.Body != note || titles(fresh.Out, "wikilink") != want {
		t.Errorf("re-registration changed the body or links: body %q links %v", fresh.Body, fresh.Out)
	}
	if id, err := s.PageID(ctx, "Changed Target"); err != nil || id != 0 {
		t.Errorf("re-registration linked the replacement note to page %d: %v", id, err)
	}
}

func pageByTitle(t *testing.T, s *Store, title string) *Page {
	t.Helper()
	id, err := s.PageID(ctx, title)
	if err != nil || id == 0 {
		t.Fatalf("page %q: id %d, %v", title, id, err)
	}
	p, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func assertBacklinks(t *testing.T, s *Store, title, want string) {
	t.Helper()
	p := pageByTitle(t, s, title)
	if got := titles(p.In, "wikilink"); got != want {
		t.Fatalf("%s backlinks = %q, want %q (all backlinks: %s)", title, got, want, strings.Join(edgeTitles(p.In), ","))
	}
}

func edgeTitles(es []Edge) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Title)
	}
	return out
}

func TestMetricsArePagesFiledInCategories(t *testing.T) {
	s := fresh(t)
	// file files a metric in the category at path, as the owner does through the catalog: the category pages made
	// when missing, each part-of the one above it, then the metric's page part-of the last.
	file := func(metric, path string) (added bool, err error) {
		err = s.Do(ctx, "cli", func(tx *Tx) error {
			id, err := tx.metricID(metric)
			if err != nil {
				return err
			}
			var above int64
			for _, title := range strings.Split(path, "/") {
				p, err := tx.lookup(title)
				if err != nil {
					return err
				}
				var c int64
				if p == nil {
					if c, _, err = tx.CreatePage(title, "", ""); err != nil {
						return err
					}
				} else {
					c = p.ID
				}
				if above != 0 {
					if _, err := tx.link(c, above, "part-of", ""); err != nil {
						return err
					}
				}
				above = c
			}
			added, err = tx.link(id, above, "part-of", "")
			return err
		})
		return
	}
	if added, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "LDL cholesterol", Unit: "mg/dL", Note: "low-density lipoprotein"}); err != nil || !added {
		t.Fatalf("a metric with a title for a name: %v %v", added, err)
	}
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "ldl CHOLESTEROL", Unit: "mmol/L"}); status(err) != 409 {
		t.Errorf("the same name in another case is the same metric, and its unit never changes: %v", err)
	}
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "a/b"}); status(err) != 422 {
		t.Errorf("a name that is not a valid title: %v", err)
	}
	ghost, _, err := s.CreatePage(ctx, "cli", "Ferritin", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if added, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "ferritin", Unit: "ng/mL", Note: "iron stores"}); err != nil || !added {
		t.Fatalf("an empty page of that title becomes the metric: %v %v", added, err)
	}
	if p, _ := s.PageByID(ctx, ghost); p.Type != "metric" || p.Body != "iron stores" {
		t.Errorf("the promoted page: %+v", p)
	}
	written, _, err := s.CreatePage(ctx, "cli", "aPTT", "Clotting time; mine runs long.", "")
	if err != nil {
		t.Fatal(err)
	}
	if added, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "aptt", Unit: "seconds", Note: "aPTT"}); err != nil || !added {
		t.Fatalf("a page the owner wrote about it becomes the metric: %v %v", added, err)
	}
	if p, _ := s.PageByID(ctx, written); p.Type != "metric" || p.Title != "aPTT" || p.Body != "Clotting time; mine runs long." {
		t.Errorf("the owner's text is kept, the note is not written over it: %+v", p)
	}
	if added, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "aptt", Unit: "seconds", Note: "aPTT"}); err != nil || added {
		t.Errorf("registering it again is a no-op: %v %v", added, err)
	}
	if added, err := file("LDL cholesterol", "Biomarkers/Lipids"); err != nil || !added {
		t.Fatalf("filing makes the category pages and their part-of links: %v %v", added, err)
	}
	if added, err := file("ldl cholesterol", "Biomarkers/Lipids"); err != nil || added {
		t.Errorf("filing it again adds nothing: %v %v", added, err)
	}
	if _, err := file("Ferritin", "Biomarkers/Iron"); err != nil {
		t.Fatal(err)
	}
	if _, err := file("Ferritin", "Supplements"); err != nil {
		t.Errorf("a metric in a second category: %v", err)
	}
	if _, err := file("Ferritin", "Old shelf"); err != nil {
		t.Fatal(err)
	}
	shelf, err := s.PageID(ctx, "Old shelf")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", shelf); err != nil { // the paths below leave out a tombstoned category
		t.Fatal(err)
	}
	if _, err := file("Ferritin", "2026-10-03"); status(err) != 422 {
		t.Errorf("a day page is not a category: %v", err)
	}
	ms, err := s.Metrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, m := range ms {
		got[m.Name] = fmt.Sprint(m.Categories)
	}
	if got["LDL cholesterol"] != "[Biomarkers/Lipids]" || got["Ferritin"] != "[Biomarkers/Iron Supplements]" || got["Mood"] != "[]" {
		t.Errorf("metrics carry the paths of their categories: %v", got)
	}
	cs, err := s.Categories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, c := range cs {
		paths = append(paths, c.Path)
	}
	if want := "[Biomarkers Biomarkers/Iron Biomarkers/Lipids Supplements]"; fmt.Sprint(paths) != want {
		t.Errorf("categories by path: %v", paths)
	}
}

// TestCategoryPathsDrawATree: the paths of the categories from the part-of links, in the order of the categories'
// title keys. Each category is drawn under its first parent by title key, and a cycle ends where the walk up meets a
// category it has seen. The paths are checked by hand.
func TestCategoryPathsDrawATree(t *testing.T) {
	for _, tc := range []struct {
		name   string
		titles map[int64]string  // the categories
		filed  map[int64][]int64 // the categories of each page, in the order of their title keys
		want   map[int64]string
	}{
		{"no links", nil, nil, map[int64]string{}},
		{"no parent", map[int64]string{1: "Health"}, map[int64][]int64{10: {1}}, map[int64]string{1: "Health"}},
		{"a long chain", map[int64]string{1: "A", 2: "B", 3: "C", 4: "D"}, map[int64][]int64{2: {1}, 3: {2}, 4: {3}, 10: {4}},
			map[int64]string{1: "A", 2: "A/B", 3: "A/B/C", 4: "A/B/C/D"}},
		{"two parents: the first by title key", map[int64]string{1: "Beta", 2: "Alpha", 3: "Gamma"}, map[int64][]int64{3: {2, 1}, 10: {3}},
			map[int64]string{1: "Beta", 2: "Alpha", 3: "Alpha/Gamma"}},
		{"a cycle", map[int64]string{1: "A", 2: "B"}, map[int64][]int64{2: {1}, 10: {1}, 1: {2}},
			map[int64]string{1: "B/A", 2: "A/B"}},
		{"a cycle of three, one with a second parent", map[int64]string{1: "A", 2: "B", 3: "C", 4: "D"}, map[int64][]int64{3: {1, 4}, 1: {2}, 2: {3}},
			map[int64]string{1: "C/B/A", 2: "A/C/B", 3: "B/A/C", 4: "D"}},
	} {
		cats := map[int64]*Category{}
		for id, title := range tc.titles {
			cats[id] = &Category{ID: id, Title: title}
		}
		if got := categoryPaths(cats, tc.filed); !maps.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// partOf is the titles of the pages id is part-of, by title.
func partOf(t *testing.T, s *Store, id int64) []string {
	t.Helper()
	rows, err := s.DB.R.QueryContext(ctx, `SELECT n.title FROM links l JOIN entities p ON p.id = l.to_id
		JOIN entity_names n ON n.entity_id = p.id AND n.name_key = p.preferred_name_key
		 WHERE l.from_id = ? AND l.kind = 'part-of' ORDER BY n.title`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			t.Fatal(err)
		}
		out = append(out, title)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestFilePages(t *testing.T) {
	s := fresh(t)
	ferritin, ldl, weight := metricID(t, s, "Ferritin", "ug/L"), metricID(t, s, "LDL", "mg/dL"), metricID(t, s, "Weight", "kg")

	f, err := s.FilePages(ctx, "cli", []int64{ferritin, ldl}, "Bloodwork", false)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Created || f.Added != 2 || f.Moved != 0 || f.Category.Title != "Bloodwork" {
		t.Fatalf("first filing = %+v, want Bloodwork made, 2 added", f)
	}
	cat, err := s.PageByID(ctx, f.Category.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cat.Type != "page" || cat.Day != "" || cat.Body != "" {
		t.Errorf("the made category = type %q day %q body %q, want a plain empty page with no day", cat.Type, cat.Day, cat.Body)
	}

	again, err := s.FilePages(ctx, "cli", []int64{ferritin, ldl}, "bloodwork", false)
	if err != nil {
		t.Fatal(err)
	}
	if again.Created || again.Added != 0 || again.Category.ID != f.Category.ID {
		t.Errorf("second filing = %+v, want nothing new in the same page", again)
	}

	if _, err := s.FilePages(ctx, "cli", []int64{ferritin}, "Iron", false); err != nil {
		t.Fatal(err)
	}
	if got := partOf(t, s, ferritin); !slices.Equal(got, []string{"Bloodwork", "Iron"}) {
		t.Fatalf("after add, Ferritin is part-of %v", got)
	}

	if _, err := s.FilePages(ctx, "cli", []int64{weight}, "Sport", false); err != nil {
		t.Fatal(err)
	}
	moved, err := s.FilePages(ctx, "cli", []int64{ferritin, weight}, "Iron", true)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Moved != 2 || moved.Added != 1 {
		t.Errorf("move = %+v, want 2 links removed (Bloodwork, Sport) and 1 added", moved)
	}
	if got := partOf(t, s, ferritin); !slices.Equal(got, []string{"Iron"}) {
		t.Errorf("after move, Ferritin is part-of %v, want [Iron]", got)
	}
	if got := partOf(t, s, weight); !slices.Equal(got, []string{"Iron"}) {
		t.Errorf("after move, Weight is part-of %v, want [Iron]", got)
	}
	if got := partOf(t, s, ldl); !slices.Equal(got, []string{"Bloodwork"}) {
		t.Errorf("a move of other pages changed LDL: part-of %v", got)
	}
}

func TestFilePagesRefusals(t *testing.T) {
	s := fresh(t)
	ferritin := metricID(t, s, "Ferritin", "ug/L")
	_, err := s.CreatePerson(ctx, "cli", "Bob Sample", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	gone, _, err := s.CreatePage(ctx, "cli", "Old Group", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", gone); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name     string
		ids      []int64
		category string
		want     int
	}{
		{"a missing page", []int64{ferritin, 9999}, "Bloodwork", 404},
		{"a deleted category", []int64{ferritin}, "Old Group", 409},
		{"a deleted page", []int64{gone}, "Bloodwork", 409},
		{"a person as the category", []int64{ferritin}, "Bob Sample", 422},
		{"a day as the category", []int64{ferritin}, "2026-10-01", 422},
		{"a title the contract refuses", []int64{ferritin}, "Health/Diet", 422},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := s.FilePages(ctx, "cli", c.ids, c.category, false)
			if status(err) != c.want {
				t.Fatalf("status %d (%v), want %d", status(err), err, c.want)
			}
			if got := partOf(t, s, ferritin); len(got) != 0 {
				t.Errorf("a refused filing wrote part-of %v", got)
			}
			if id, err := s.PageID(ctx, "Bloodwork"); err != nil || id != 0 {
				t.Errorf("a refused filing made the category, page %d (%v)", id, err)
			}
		})
	}
}

// A day page is never filed (D26): links_endpoint_types refuses a part-of link from it, so file-pages that names a
// day page among other pages is a 422 that writes nothing (not the other pages' links, not the category it would
// make, no revision), and so is a part-of link from a day page to a category that exists. The refusal is SQLite's
// sentence: the writer holds no rule of its own here.
func TestADayPageIsNeverFiled(t *testing.T) {
	s := fresh(t)
	plain, _, err := s.CreatePage(ctx, "cli", "Plans", "", "")
	if err != nil {
		t.Fatal(err)
	}
	day, _, err := s.Capture(ctx, "cli", "2031-05-02", "a synthetic entry", nil)
	if err != nil {
		t.Fatal(err)
	}
	before, err := countRows(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	version := func(id int64) string {
		t.Helper()
		p, err := s.PageByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return p.Version
	}
	plainVersion, dayVersion := version(plain), version(day)
	const sentence = "a day page is never filed in a category (D26)"

	for _, ids := range [][]int64{{day}, {plain, day}, {day, plain}} {
		_, err := s.FilePages(ctx, "cli", ids, "Garden", false)
		if status(err) != 422 || !strings.Contains(err.Error(), sentence) {
			t.Errorf("file-pages %v: %d %v, want a 422 that says %q", ids, status(err), err, sentence)
		}
	}
	if _, err := s.FilePages(ctx, "cli", []int64{plain, day}, "Garden", true); status(err) != 422 {
		t.Errorf("file-pages with move: %d %v, want a 422", status(err), err)
	}
	after, err := countRows(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("rows before %+v, after %+v: a refused filing wrote", before, after)
	}
	if id, err := s.PageID(ctx, "Garden"); err != nil || id != 0 {
		t.Errorf("a refused filing made the category, page %d (%v)", id, err)
	}
	if got := partOf(t, s, plain); len(got) != 0 {
		t.Errorf("a refused filing filed the plain page in %v", got)
	}
	if version(plain) != plainVersion || version(day) != dayVersion {
		t.Errorf("a refused filing advanced a revision: plain %s->%s, day %s->%s", plainVersion, version(plain), dayVersion, version(day))
	}

	// The category exists: the part-of link from the day page is refused, the one from the plain page is not.
	if _, err := s.FilePages(ctx, "cli", []int64{plain}, "Garden", false); err != nil {
		t.Fatal(err)
	}
	garden := pageByTitle(t, s, "Garden").ID
	if err := s.Link(ctx, "cli", day, garden, "part-of", ""); status(err) != 422 || !strings.Contains(err.Error(), sentence) {
		t.Errorf("link part-of from a day page: %d %v, want a 422 that says %q", status(err), err, sentence)
	}
	if got := partOf(t, s, day); len(got) != 0 {
		t.Errorf("the day page is part-of %v", got)
	}
}

// A new page is filed where it is made (docs/plans/115-categories-to-browse.md): the page and its part-of link in one
// write, a missing category made, and a day as the category refused with nothing written. A person, a place, a file
// and a metric are filed by file-pages (docs/plans/131-the-pareto-pass.md, part 2).

// makers are the writes that take a category, each making a thing titled title.
var makers = []struct {
	kind string
	make func(s *Store, title, category string) (int64, error)
}{
	{"page", func(s *Store, title, category string) (int64, error) {
		id, _, err := s.CreatePage(ctx, "cli", title, "", category)
		return id, err
	}},
}

func TestANewThingIsFiledWhereItIsMade(t *testing.T) {
	for _, m := range makers {
		t.Run(m.kind, func(t *testing.T) {
			s := fresh(t)
			if _, _, err := s.CreatePage(ctx, "cli", "Family", "", ""); err != nil {
				t.Fatal(err)
			}
			if _, err := s.FilePages(ctx, "cli", []int64{pageByTitle(t, s, "Family").ID}, "Everyone", false); err != nil {
				t.Fatal(err) // Family is now a category under Everyone
			}

			id, err := m.make(s, "Sam", "Family")
			if err != nil {
				t.Fatal(err)
			}
			p, err := s.PageByID(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if p.Type != m.kind || p.Title != "Sam" {
				t.Errorf("made %s %q, want %s Sam", p.Type, p.Title, m.kind)
			}
			if got := partOf(t, s, id); !slices.Equal(got, []string{"Family"}) {
				t.Errorf("Sam is part-of %v, want [Family]", got)
			}
		})
	}
}

func TestANewThingMakesAMissingCategory(t *testing.T) {
	for _, m := range makers {
		t.Run(m.kind, func(t *testing.T) {
			s := fresh(t)
			id, err := m.make(s, "Ana", "Cousins")
			if err != nil {
				t.Fatal(err)
			}
			if got := partOf(t, s, id); !slices.Equal(got, []string{"Cousins"}) {
				t.Errorf("Ana is part-of %v, want [Cousins]", got)
			}
			cat := pageByTitle(t, s, "Cousins")
			if cat.Type != "page" || cat.Body != "" || cat.Day != "" {
				t.Errorf("the made category = type %q body %q day %q, want a plain empty page", cat.Type, cat.Body, cat.Day)
			}
		})
	}
}

// A day is never a category, and the write that names one is refused with nothing written: not the thing, not the
// link, not the day page. For a day with no page yet, entities_day_page refuses the plain page that filing would make
// of the day's title; for a day page that exists, links_endpoint_types refuses the part-of link to it (D26).
func TestADayAsTheCategoryWritesNothing(t *testing.T) {
	for _, dayPage := range []struct {
		name   string
		exists bool
		refuse string // a part of the refusal that names the rule reached
	}{
		{"no day page", false, "entities_day_page"},
		{"a day page", true, "never a day page (D26)"},
	} {
		for _, m := range makers {
			t.Run(dayPage.name+"/"+m.kind, func(t *testing.T) {
				s := fresh(t)
				if dayPage.exists {
					if _, _, err := s.Capture(ctx, "cli", "2031-05-02", "a synthetic entry", nil); err != nil {
						t.Fatal(err)
					}
				}
				before, err := countRows(ctx, s)
				if err != nil {
					t.Fatal(err)
				}
				n := pageCount(t, s)
				id, err := m.make(s, "Nobody", "2031-05-02")
				if err == nil {
					t.Fatalf("a day as the category was accepted (id %d)", id)
				}
				var refused *Error
				if !errors.As(err, &refused) || refused.Status != 422 {
					t.Errorf("a day as the category: %v, want a 422", err)
				}
				if !strings.Contains(err.Error(), dayPage.refuse) {
					t.Errorf("a day as the category: %v, want the refusal of %s", err, dayPage.refuse)
				}
				if id != 0 {
					t.Errorf("a refused write answered id %d", id)
				}
				after, err := countRows(ctx, s)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, after) {
					t.Errorf("rows before %+v, after %+v: the refused write wrote", before, after)
				}
				if got := pageCount(t, s); got != n {
					t.Errorf("%d entities after the refused write, want %d", got, n)
				}
			})
		}
	}
}

// pageCount is the number of entities, tombstones included (a fresh file has the metric the schema registers).
func pageCount(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM entities`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
