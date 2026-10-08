package core

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
)

func TestCanonicalNamesSearchConflictPathsAndRebuild(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Blue", "needle original")
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
	other, _, err := s.CreatePage(ctx, "cli", "Other owner", "")
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
		id, _, err := s.CreatePage(ctx, "cli", name, "needle")
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
		if h.Snippet != "[needle]" {
			t.Fatalf("body field snippet %s: %q", fmt.Sprint(h.ID), h.Snippet)
		}
	}
}
