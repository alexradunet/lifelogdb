package core

import "testing"

// The full-text index is derived data (docs/contract/integrity-checks.md): it has a check and a repair. The test
// damages a disposable file, reads the finding, repairs, and reads again.

// linkedFixture is a page A whose body links B, and a page C whose links are right.
func linkedFixture(t *testing.T) (s *Store, a, b, c int64) {
	t.Helper()
	s = fresh(t)
	create := func(title, body string) int64 {
		t.Helper()
		id, _, err := s.CreatePage(ctx, "cli", title, body, "")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	b = create("Bridge", "")
	a = create("Anchor", "see [[Bridge]]")
	c = create("Clear", "see [[Bridge]] and [[Anchor]]")
	return s, a, b, c
}

func TestRebuildSearchRepairsADriftedIndex(t *testing.T) {
	s, _, _, _ := linkedFixture(t)
	// An index document that no page holds: the drift that the integrity check of FTS5 finds.
	if _, err := s.DB.W.ExecContext(ctx, `INSERT INTO entities_fts(rowid, preferred, all_names, body) VALUES (999, 'ghost', 'ghost', 'drifted')`); err != nil {
		t.Fatal(err)
	}
	damaged, err := s.Integrity(ctx)
	if err != nil || damaged.FullTextIndexOK || damaged.OK {
		t.Fatalf("the drifted index must be found: %+v %v", damaged, err)
	}
	n, err := s.RebuildSearch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 { // the three pages and the metric Mood that a fresh file holds
		t.Errorf("documents = %d; want 4", n)
	}
	repaired, err := s.Integrity(ctx)
	if err != nil || !repaired.OK || !repaired.FullTextIndexOK {
		t.Fatalf("after the rebuild: %+v %v", repaired, err)
	}
	hits, err := s.SearchWith(ctx, "Bridge", SearchOptions{}, 10, 0)
	if err != nil || len(hits) == 0 {
		t.Errorf("the rebuilt index finds no page: %+v %v", hits, err)
	}
}
