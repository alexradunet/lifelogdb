package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestReadersPage(t *testing.T) {
	s := fresh(t)
	ctx := context.Background()
	for i := 1; i <= 5; i++ {
		if _, err := s.CreatePlace(ctx, "cli", fmt.Sprintf("Place %d", i)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Capture(ctx, "cli", fmt.Sprintf("2031-04-%02d", i), fmt.Sprintf("pageword [[Missing %d]]", i), nil); err != nil {
			t.Fatal(err)
		}
	}
	titles := func(es []Edge) string {
		var out []string
		for _, e := range es {
			out = append(out, e.Title)
		}
		return strings.Join(out, ",")
	}
	// Hand-counted: five places by title, five days newest first, five ghosts oldest first, five hits.
	for _, tc := range []struct {
		name          string
		limit, offset int
		want          string
	}{
		{"places first two", 2, 0, "Place 1,Place 2"}, {"places middle", 2, 2, "Place 3,Place 4"}, {"places last", 2, 4, "Place 5"}, {"places past", 2, 5, ""}, {"places all", 10, 0, "Place 1,Place 2,Place 3,Place 4,Place 5"},
	} {
		got, err := s.Named(ctx, "place", tc.limit, tc.offset)
		if err != nil || titles(got) != tc.want {
			t.Errorf("%s: %q %v", tc.name, titles(got), err)
		}
	}
	if got, err := s.RecentDays(ctx, 2, 1); err != nil || titles(got) != "2031-04-04,2031-04-03" {
		t.Errorf("days: %q %v", titles(got), err)
	}
	// The linked targets are not ghosts (a ghost has no link). Five empty pages of their own, inserted by direct SQL
	// with an old created_at (bulk setup: ghost_pages wants them older than 30 days, and created_at never changes),
	// are.
	err := s.Do(ctx, "cli", func(tx *Tx) error {
		for i := 1; i <= 5; i++ {
			var id int64
			at := fmt.Sprintf("2000-01-01T00:00:0%d.000Z", i)
			if err := tx.tx.QueryRowContext(ctx, `INSERT INTO entities(entity_type, preferred_name_key, body, created_at, updated_at, source)
			    VALUES ('page', ?, '', ?, ?, 'cli') RETURNING id`, fmt.Sprintf("ghost %d", i), at, at).Scan(&id); err != nil {
				return err
			}
			if _, err := tx.tx.ExecContext(ctx, `INSERT INTO entity_names(entity_id, title, name_key) VALUES (?, ?, ?)`, id, fmt.Sprintf("Ghost %d", i), fmt.Sprintf("ghost %d", i)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if none, err := s.Ghosts(ctx, 10, 0); err != nil || len(none) != 5 {
		t.Fatalf("ghosts: %+v %v", none, err)
	}
	ghosts, err := s.Ghosts(ctx, 2, 3)
	if err != nil || len(ghosts) != 2 || ghosts[0].Title != "Ghost 4" || ghosts[1].Title != "Ghost 5" {
		t.Errorf("ghosts page: %+v %v", ghosts, err)
	}
	hits, err := s.Search(ctx, "pageword", 2, 4)
	if err != nil || len(hits) != 1 {
		t.Errorf("search: %+v %v", hits, err)
	}
	all, err := s.Search(ctx, "pageword", 10, 0)
	if err != nil || len(all) != 5 {
		t.Errorf("search all: %+v %v", all, err)
	}
}

func TestLinkKindsFromMatchesTheRegistry(t *testing.T) {
	s := fresh(t)
	ctx := context.Background()
	rows, err := s.DB.R.QueryContext(ctx, `SELECT kind, from_types FROM link_kinds WHERE kind <> 'wikilink' ORDER BY kind`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := map[string][]string{}
	for rows.Next() {
		var kind string
		var from *string
		if err := rows.Scan(&kind, &from); err != nil {
			t.Fatal(err)
		}
		for _, typ := range []string{"page", "person", "place", "file", "metric", "category", "no-such-type"} {
			if from == nil || strings.Contains(","+*from+",", ","+typ+",") {
				want[typ] = append(want[typ], kind)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(want["page"]) == 0 {
		t.Fatal("the registry offers a page no kind")
	}
	for typ, kinds := range want {
		got, err := s.LinkKindsFrom(ctx, typ)
		if err != nil || strings.Join(got, ",") != strings.Join(kinds, ",") {
			t.Errorf("%s: got %v want %v (%v)", typ, got, kinds, err)
		}
	}
	// A type written as an injection is a value: it matches only the kinds open to any type, like an unknown type.
	if got, err := s.LinkKindsFrom(ctx, "page' OR '1'='1"); err != nil || strings.Join(got, ",") != strings.Join(want["no-such-type"], ",") {
		t.Errorf("injection: %v %v want %v", got, err, want["no-such-type"])
	}
}
