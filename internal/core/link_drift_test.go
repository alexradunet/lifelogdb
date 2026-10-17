package core

import (
	"reflect"
	"slices"
	"testing"
)

// The wikilinks of a body are derived data (docs/contract/integrity-checks.md): they have a check and a repair. Each
// test damages a disposable file, reads the finding, repairs, and reads again.

func wikilinkTargets(t *testing.T, s *Store, from int64) []int64 {
	t.Helper()
	rows, err := s.DB.R.QueryContext(ctx, `SELECT to_id FROM links WHERE from_id = ? AND kind = 'wikilink' ORDER BY to_id`, from)
	targets, err := list(rows, err, func(id *int64) []any { return []any{id} })
	if err != nil {
		t.Fatal(err)
	}
	return targets
}

func TestIntegrityFindsLinksThatDifferFromTheirBodies(t *testing.T) {
	for _, damage := range []struct {
		name string
		run  func(s *Store, a, b, other int64) error
	}{
		{"a link row is missing", func(s *Store, a, b, _ int64) error {
			_, err := s.DB.W.ExecContext(ctx, `DELETE FROM links WHERE from_id = ? AND to_id = ?`, a, b)
			return err
		}},
		{"a link row has no word in the body", func(s *Store, a, _, other int64) error {
			_, err := s.DB.W.ExecContext(ctx, `INSERT INTO links(from_id, to_id, kind, created_at, source) VALUES (?, ?, 'wikilink', `+nowSQL+`, 'cli')`, a, other)
			return err
		}},
		{"the body names a page that does not exist", func(s *Store, a, _, _ int64) error {
			_, err := s.DB.W.ExecContext(ctx, `UPDATE entities SET body = 'now [[Nowhere Yet]]' WHERE id = ?`, a)
			return err
		}},
	} {
		t.Run(damage.name, func(t *testing.T) {
			s, a, b, _ := linkedFixture(t)
			other, _, err := s.CreatePage(ctx, "cli", "Other", "", "")
			if err != nil {
				t.Fatal(err)
			}
			clean, err := s.Integrity(ctx)
			if err != nil || !clean.OK || clean.LinkDrift == nil || len(clean.LinkDrift) != 0 {
				t.Fatalf("pages saved through the writer have the links of their bodies: %+v %v", clean, err)
			}
			if err := damage.run(s, a, b, other); err != nil {
				t.Fatal(err)
			}
			got, err := s.Integrity(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if got.OK || !reflect.DeepEqual(got.LinkDrift, []int64{a}) {
				t.Fatalf("want only the page %d in LinkDrift (the page Clear has the right links): %+v", a, got)
			}
		})
	}
}

func TestResyncLinksRewritesTheLinksOfTheDriftedPagesOnly(t *testing.T) {
	s, a, b, c := linkedFixture(t)
	var cVersion string
	if err := s.DB.R.QueryRowContext(ctx, `SELECT revision FROM entities WHERE id = ?`, c).Scan(&cVersion); err != nil {
		t.Fatal(err)
	}
	other, _, err := s.CreatePage(ctx, "cli", "Other", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM links WHERE from_id = ? AND to_id = ?`, []any{a, b}},
		{`INSERT INTO links(from_id, to_id, kind, created_at, source) VALUES (?, ?, 'wikilink', ` + nowSQL + `, 'cli')`, []any{a, other}},
		{`UPDATE entities SET body = 'see [[Bridge]] and [[Nowhere Yet]]' WHERE id = ?`, []any{a}},
	} {
		if _, err := s.DB.W.ExecContext(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	r, err := s.ResyncLinks(ctx, "cli")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Pages, []int64{a}) || !reflect.DeepEqual(r.Created, []string{"Nowhere Yet"}) {
		t.Fatalf("resync = %+v; want the page %d and one page made", r, a)
	}
	var nowhere int64
	if err := s.DB.R.QueryRowContext(ctx, `SELECT entity_id FROM entity_names WHERE name_key = 'nowhere yet'`).Scan(&nowhere); err != nil {
		t.Fatal(err)
	}
	if got, want := wikilinkTargets(t, s, a), []int64{b, nowhere}; !slices.Equal(got, want) {
		t.Errorf("links of the page after the resync = %v; want %v", got, want)
	}
	var after string
	if err := s.DB.R.QueryRowContext(ctx, `SELECT revision FROM entities WHERE id = ?`, c).Scan(&after); err != nil || after != cVersion {
		t.Errorf("a page with the right links was written: revision %q, was %q (%v)", after, cVersion, err)
	}
	checked, err := s.Integrity(ctx)
	if err != nil || !checked.OK || len(checked.LinkDrift) != 0 {
		t.Fatalf("after the resync: %+v %v", checked, err)
	}
	again, err := s.ResyncLinks(ctx, "cli")
	if err != nil || len(again.Pages) != 0 || again.Pages == nil {
		t.Fatalf("a second resync has nothing to do: %+v %v", again, err)
	}
}
