package core

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestCanonicalNamesSearchConflictPathsAndRebuild(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Blue", "needle original", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename(ctx, "cli", id, "Sky"); err != nil {
		t.Fatal(err)
	}
	check := func(query string, want int64) {
		t.Helper()
		hits, err := s.Search(ctx, query, 1, 0)
		if err != nil || len(hits) != 1 || hits[0].ID != want {
			t.Fatalf("search %q: %+v %v; want %d", query, hits, err, want)
		}
		r, err := s.Integrity(ctx)
		if err != nil || !r.OK {
			t.Fatalf("integrity: %+v %v", r, err)
		}
	}
	for _, q := range []string{"Blue original", "Sky original", `all_names:"blue sky"`, `all_names:Blue body:original`, `body:"needle original"`} {
		check(q, id)
	}
	empty, err := s.Search(ctx, `body:"Blue needle"`, 10, 0)
	if err != nil || len(empty) != 0 {
		t.Fatalf("alias must not duplicate body: %+v %v", empty, err)
	}
	other, _, err := s.CreatePage(ctx, "cli", "Other owner", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO entity_names(entity_id,title,name_key) VALUES (?,'Blue','blue') ON CONFLICT DO NOTHING`, other)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	check("Blue original", id)
	var nameID int64
	if err := s.DB.R.QueryRow(`SELECT id FROM entity_names WHERE name_key='blue'`).Scan(&nameID); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{
		"ON CONFLICT(name_key) DO NOTHING",
		"ON CONFLICT(entity_id,name_key) DO NOTHING",
		"ON CONFLICT(id) DO NOTHING",
		"ON CONFLICT DO NOTHING",
		"ON CONFLICT(name_key) DO UPDATE SET title=excluded.title",
		"ON CONFLICT(entity_id,name_key) DO UPDATE SET title=excluded.title",
		"ON CONFLICT(id) DO UPDATE SET title=excluded.title",
	} {
		for _, spelling := range []string{"Blue", "BLUE", "blue"} {
			if err := s.DB.Write(ctx, func(tx *sql.Tx) error {
				_, err := tx.Exec(`INSERT INTO entity_names(id,entity_id,title,name_key) VALUES(?,?,?,'blue') `+suffix, nameID, id, spelling)
				return err
			}); err != nil {
				t.Fatalf("%s: %v", suffix, err)
			}
			check("Blue original", id)
		}
	}
	before, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename(ctx, "cli", id, "SKY"); err != nil {
		t.Fatal(err)
	}
	check("Sky needle", id)
	fail := errors.New("rollback search")
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		if _, err := tx.Rename(id, "Rollback"); err != nil {
			return err
		}
		if _, err := tx.SetBody(id, "rolled back"); err != nil {
			return err
		}
		return fail
	}); !errors.Is(err, fail) {
		t.Fatal(err)
	}
	check("Blue original", id)
	hits, err := s.Search(ctx, "Rollback", 10, 0)
	if err != nil || len(hits) != 0 {
		t.Fatalf("rollback indexed: %+v %v", hits, err)
	}
	p, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if p.Body != before.Body {
		t.Fatal("rollback changed body")
	}
	if _, err := s.SaveBody(ctx, "cli", id, "replacement needle", p.Version); err != nil {
		t.Fatal(err)
	}
	check("Blue replacement", id)
	if _, err := s.DB.W.Exec(`INSERT INTO entities_fts(entities_fts) VALUES('rebuild')`); err != nil {
		t.Fatal(err)
	}
	check("Sky replacement", id)
	if err := s.Tombstone(ctx, "cli", id); err != nil {
		t.Fatal(err)
	}
	hits, err = s.Search(ctx, "replacement", 10, 0)
	if err != nil || len(hits) != 0 {
		t.Fatalf("tombstone visible: %+v %v", hits, err)
	}
	if err := s.Revive(ctx, "cli", id); err != nil {
		t.Fatal(err)
	}
	check("Blue replacement", id)
	if _, err := s.DB.W.Exec(`INSERT INTO entities_fts(entities_fts,rowid,preferred,all_names,body) SELECT 'delete',id,preferred,all_names,body FROM entity_search_content WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	r, err := s.Integrity(ctx)
	if err != nil || r.OK || r.FullTextIndexOK {
		t.Fatalf("missing document not detected: %+v %v", r, err)
	}
	if _, err := s.DB.W.Exec(`INSERT INTO entities_fts(entities_fts) VALUES('rebuild')`); err != nil {
		t.Fatal(err)
	}
	check("Blue replacement", id)
}

func TestCanonicalSearchRankLimitAndStableTie(t *testing.T) {
	s := fresh(t)
	var first int64
	for i, name := range []string{"Alpha", "Beta"} {
		id, _, err := s.CreatePage(ctx, "cli", name, "needle", "")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = id
		}
		if _, err := s.Rename(ctx, "cli", id, name+" old"); err != nil {
			t.Fatal(err)
		}
	}
	hits, err := s.Search(ctx, "needle", 1, 0)
	if err != nil || len(hits) != 1 || hits[0].ID != first {
		t.Fatalf("ranked limited tie: %+v %v; want %d", hits, err, first)
	}
	hits, err = s.Search(ctx, "needle", 10, 0)
	if err != nil || len(hits) != 2 || hits[0].ID != first || hits[1].ID <= first {
		t.Fatalf("one document per entity: %+v %v", hits, err)
	}
	for _, h := range hits {
		if h.Snippet != "⟦needle⟧" {
			t.Fatalf("body field snippet %s: %q", fmt.Sprint(h.ID), h.Snippet)
		}
	}
}

// TestSearchSnippetKeepsTheBracketsOfTheText says the marks of a match cannot be confused with the text: a body's
// [[wikilinks]] and check boxes keep their brackets, and only the matched word is marked.
func TestSearchSnippetKeepsTheBracketsOfTheText(t *testing.T) {
	s := fresh(t)
	if _, _, err := s.CreatePage(ctx, "cli", "Plans", "Coffee with [[Ana]]; we planned the [[Garden project]]. - [x] seeds", ""); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Search(ctx, "garden", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, h := range hits {
		if h.Title == "Plans" {
			got = append(got, h.Snippet)
		}
	}
	want := "Coffee with [[Ana]]; we planned the [[⟦Garden⟧ project]]. - [x] seeds"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("snippet of Plans = %q, want [%q]", got, want)
	}
}

// TestASaveSaysWhichLinksItAdded: a save's Linked is every page the body links after it; Added is the ones this save
// linked that the page did not link before, so a capture can report what it added and not the whole day again.
func TestASaveSaysWhichLinksItAdded(t *testing.T) {
	s := fresh(t)
	_, r, err := s.Capture(ctx, "cli", "2026-10-14", "Ran with [[Sam]] at [[Lakeside Park]].", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Added, []string{"Sam", "Lakeside Park"}) {
		t.Errorf("first capture added %v", r.Added)
	}
	_, r, err = s.Capture(ctx, "cli", "2026-10-14", "Lunch with [[Mihai]] and [[Sam]]. #food", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Linked, []string{"Sam", "Lakeside Park", "Mihai", "food"}) || !reflect.DeepEqual(r.Added, []string{"Mihai", "food"}) {
		t.Errorf("second capture linked %v, added %v; want every link, and Mihai and food added", r.Linked, r.Added)
	}
	_, r, err = s.Capture(ctx, "cli", "2026-10-14", "Home again.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Added) != 0 {
		t.Errorf("a capture with no link added %v", r.Added)
	}
}
