package core

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// A new person, place or file is filed where it is made (docs/plans/117-one-browse-view.md, rule 6): the thing and
// its part-of link in one write, a missing category made, and a day as the category refused with nothing written.

// makers are the three writes that take a category, each making a thing titled title.
var makers = []struct {
	kind string
	make func(s *Store, title, category string) (int64, error)
}{
	{"person", func(s *Store, title, category string) (int64, error) {
		return s.CreatePerson(ctx, "cli", title, "", "", "", category)
	}},
	{"place", func(s *Store, title, category string) (int64, error) {
		return s.CreatePlace(ctx, "cli", title, category)
	}},
	{"file", func(s *Store, title, category string) (int64, error) {
		k, err := s.AddFile(ctx, "cli", FileIn{Title: title, SHA256: sha('b'), MIME: "application/pdf", Day: "2031-05-01", Category: category})
		return k.ID, err
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

// The same original again finds its page; with a category, that page is filed there. A tombstoned page is not filed.
func TestAddFileFilesTheFileThatExists(t *testing.T) {
	s := fresh(t)
	n := pageCount(t, s)
	in := FileIn{Title: "Scan.pdf", SHA256: sha('c'), MIME: "application/pdf", Day: "2031-05-01"}
	first, err := s.AddFile(ctx, "cli", in)
	if err != nil {
		t.Fatal(err)
	}
	if first.Filed != nil {
		t.Errorf("no category sent, but filed: %+v", first.Filed)
	}

	in.Title, in.Category = "Another title.pdf", "Papers"
	again, err := s.AddFile(ctx, "cli", in)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Existing || again.ID != first.ID {
		t.Fatalf("the same original again = %+v, want the page %d", again, first.ID)
	}
	if f := again.Filed; f == nil || !f.Created || f.Added != 1 || f.Category.Title != "Papers" {
		t.Errorf("filed = %+v, want Papers made and 1 link added", again.Filed)
	}
	if got := partOf(t, s, first.ID); !slices.Equal(got, []string{"Papers"}) {
		t.Errorf("Scan.pdf is part-of %v, want [Papers]", got)
	}
	if got := pageCount(t, s); got != n+2 { // the file and Papers: a file with no picture is shown on no day page
		t.Errorf("%d entities, want %d: the same original made a second page", got, n+2)
	}

	// Sent a third time: it is in Papers already, and nothing more is written.
	third, err := s.AddFile(ctx, "cli", in)
	if err != nil {
		t.Fatal(err)
	}
	if f := third.Filed; f == nil || f.Created || f.Added != 0 {
		t.Errorf("filed again = %+v, want no category made and no link added", third.Filed)
	}

	// A tombstoned file is not filed: nothing is written for it, not even the category.
	if err := s.Tombstone(ctx, "cli", first.ID); err != nil {
		t.Fatal(err)
	}
	in.Category = "Old papers"
	gone, err := s.AddFile(ctx, "cli", in)
	if err != nil {
		t.Fatal(err)
	}
	if !gone.Deleted || gone.Filed != nil {
		t.Errorf("a tombstoned original = %+v, want deleted and not filed", gone)
	}
	if id, err := s.PageID(ctx, "Old papers"); err != nil || id != 0 {
		t.Errorf("Old papers = %d, %v: the category of a tombstoned file was made", id, err)
	}
}

// A dry run says it would file the file, and writes nothing: no page, no category, no link.
func TestTryAddFileWithACategoryWritesNothing(t *testing.T) {
	s := fresh(t)
	before, err := countRows(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	n := pageCount(t, s)
	k, err := s.TryAddFile(ctx, "cli", FileIn{Title: "Scan.pdf", SHA256: sha('d'), MIME: "application/pdf", Day: "2031-05-01", Category: "Papers"})
	if err != nil {
		t.Fatal(err)
	}
	if k.ID != 0 || k.Filed == nil || !k.Filed.Created || k.Filed.Added != 1 || k.Filed.Category.Title != "Papers" || k.Filed.Category.ID != 0 {
		t.Errorf("dry run = %+v, filed %+v: want no id, and Papers that it would make and file in", k, k.Filed)
	}
	after, err := countRows(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || pageCount(t, s) != n {
		t.Errorf("rows before %+v, after %+v: the dry run wrote", before, after)
	}

	// The dry run of the same original: it would file the page that exists.
	kept, err := s.AddFile(ctx, "cli", FileIn{Title: "Scan.pdf", SHA256: sha('d'), MIME: "application/pdf", Day: "2031-05-01"})
	if err != nil {
		t.Fatal(err)
	}
	k, err = s.TryAddFile(ctx, "cli", FileIn{Title: "Scan.pdf", SHA256: sha('d'), MIME: "application/pdf", Day: "2031-05-01", Category: "Papers"})
	if err != nil {
		t.Fatal(err)
	}
	if !k.Existing || k.ID != kept.ID || k.Filed == nil || k.Filed.Added != 1 {
		t.Errorf("dry run of the same original = %+v, filed %+v: want its page, filed", k, k.Filed)
	}
	if got := partOf(t, s, kept.ID); len(got) != 0 {
		t.Errorf("the dry run filed the page in %v", got)
	}

	// A dry run with a day as the category is refused, as the write is.
	if _, err := s.TryAddFile(ctx, "cli", FileIn{Title: "Other.pdf", SHA256: sha('e'), MIME: "application/pdf", Category: "2031-05-02"}); err == nil {
		t.Error("a dry run with a day as the category was accepted")
	}
}
