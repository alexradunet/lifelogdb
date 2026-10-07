package core

import (
	"database/sql"
	"errors"
	"testing"
)

func TestPromotionRefusesRetainedTypedEdges(t *testing.T) {
	for _, incoming := range []bool{true, false} {
		t.Run(map[bool]string{true: "incoming", false: "outgoing"}[incoming], func(t *testing.T) {
			s := fresh(t)
			category, _, err := s.CreatePage(ctx, "cli", "Category", "retained prose")
			if err != nil {
				t.Fatal(err)
			}
			other, _, err := s.CreatePage(ctx, "cli", "Other", "")
			if err != nil {
				t.Fatal(err)
			}
			kind := "part-of"
			from, to := other, category
			if !incoming {
				kind = "at"
				from, to = category, other
				if err := s.Promote(ctx, "cli", other, "place", ""); err != nil {
					t.Fatal(err)
				}
				// Deliberate DDL setup isolates endpoint enforcement from at's day-only writer rule.
				if _, err := s.DB.W.Exec(`INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES(?,?,'at',`+Now+`,'cli')`, from, to); err != nil {
					t.Fatal(err)
				}
			} else if err := s.Link(ctx, "cli", from, to, kind, "retained note"); err != nil {
				t.Fatal(err)
			}
			before, err := s.PageByID(ctx, category)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Promote(ctx, "cli", category, "person", "Changed"); err == nil {
				t.Fatal("promotion accepted invalid retained edge")
			}
			after, err := s.PageByID(ctx, category)
			if err != nil {
				t.Fatal(err)
			}
			if after.Type != "page" || after.Body != before.Body || after.Version != before.Version || after.Person != nil {
				t.Fatalf("refused promotion changed page: %+v", after)
			}
			var edges, details int
			if err := s.DB.R.QueryRow(`SELECT count(*) FROM links WHERE from_id=? AND to_id=? AND kind=?`, from, to, kind).Scan(&edges); err != nil {
				t.Fatal(err)
			}
			if err := s.DB.R.QueryRow(`SELECT count(*) FROM people WHERE id=?`, category).Scan(&details); err != nil {
				t.Fatal(err)
			}
			if edges != 1 || details != 0 {
				t.Fatalf("refusal retained %d edges / %d people; want 1/0", edges, details)
			}
		})
	}
}

func TestMoodHabitRegistrationRefusedAtomically(t *testing.T) {
	s := fresh(t)
	err := s.Do(ctx, "cli", func(tx *Tx) error {
		if _, _, _, err := tx.CreatePage("Rollback witness", "prose", "2026-10-06", ""); err != nil {
			return err
		}
		return tx.StartHabit("Mood", "2026-10-06", "")
	})
	if err == nil {
		t.Fatal("Mood habit accepted")
	}
	id, err := s.PageID(ctx, "Rollback witness")
	if err != nil {
		t.Fatal(err)
	}
	if id != 0 {
		t.Fatal("partial prose survived refusal")
	}
	var periods int
	if err := s.DB.R.QueryRow(`SELECT count(*) FROM habit_periods WHERE metric_id=1`).Scan(&periods); err != nil {
		t.Fatal(err)
	}
	if periods != 0 {
		t.Fatalf("Mood retained %d periods", periods)
	}
	if _, _, err := s.Capture(ctx, "cli", "2026-10-06", "ordinary mood", ptr(4)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.W.Exec(`INSERT INTO habit_periods(metric_id,start_day,source) VALUES(1,'2026-10-07','cli')`); err == nil {
		t.Fatal("DDL accepted Mood habit")
	}
}

func TestLinkFieldsImmutableAndSymmetricNoteShared(t *testing.T) {
	s := fresh(t)
	a, err := s.CreatePerson(ctx, "cli", "Person A", "A", "", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreatePerson(ctx, "cli", "Person B", "B", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Link(ctx, "cli", a, b, "friend", "old note"); err != nil {
		t.Fatal(err)
	}
	var id int64
	var created string
	if err := s.DB.R.QueryRow(`SELECT id,created_at FROM links WHERE from_id=? AND to_id=? AND kind='friend'`, a, b).Scan(&id, &created); err != nil {
		t.Fatal(err)
	}
	for _, set := range []string{`id=id+1000`, `created_at='2020-01-01T00:00:00.000Z'`, `from_id=to_id`, `to_id=from_id`, `kind='related'`, `source='api'`} {
		if _, err := s.DB.W.Exec(`UPDATE links SET `+set+` WHERE id=?`, id); err == nil {
			t.Fatalf("immutable update accepted: %s", set)
		}
	}
	var gotID int64
	var gotCreated string
	if err := s.DB.R.QueryRow(`SELECT id,created_at FROM links WHERE from_id=? AND to_id=? AND kind='friend'`, a, b).Scan(&gotID, &gotCreated); err != nil {
		t.Fatal(err)
	}
	if gotID != id || gotCreated != created {
		t.Fatal("immutable refusal changed persisted link")
	}
	assertNotes := func(want any) {
		t.Helper()
		var mismatches int
		if err := s.DB.R.QueryRow(`SELECT count(*) FROM links WHERE kind='friend' AND note IS NOT ?`, want).Scan(&mismatches); err != nil {
			t.Fatal(err)
		}
		if mismatches != 0 {
			t.Fatalf("%d mirrored notes differ from %v", mismatches, want)
		}
	}
	for _, tc := range []struct {
		from, to int64
		note     any
	}{{a, b, "forward"}, {b, a, "reverse"}, {b, a, "reverse"}, {a, b, nil}} {
		if _, err := s.DB.W.Exec(`UPDATE links SET note=? WHERE from_id=? AND to_id=? AND kind='friend'`, tc.note, tc.from, tc.to); err != nil {
			t.Fatal(err)
		}
		assertNotes(tc.note)
	}
	rollback := errors.New("rollback note")
	if err := s.DB.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE links SET note='rollback' WHERE from_id=? AND to_id=? AND kind='friend'`, a, b); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatalf("rollback: %v", err)
	}
	assertNotes(nil)
	if err := s.Link(ctx, "cli", a, b, "parent-of", "directional"); err != nil {
		t.Fatal(err)
	}
	if err := s.Link(ctx, "cli", b, a, "parent-of", "other direction"); err != nil {
		t.Fatal(err)
	}
	if err := s.Link(ctx, "cli", a, a, "related", "self"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.W.Exec(`UPDATE links SET note='updated self' WHERE from_id=? AND to_id=? AND kind='related'`, a, a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.W.Exec(`UPDATE links SET note='updated direction' WHERE from_id=? AND to_id=? AND kind='parent-of'`, a, b); err != nil {
		t.Fatal(err)
	}
	var reverse string
	if err := s.DB.R.QueryRow(`SELECT note FROM links WHERE from_id=? AND to_id=? AND kind='parent-of'`, b, a).Scan(&reverse); err != nil {
		t.Fatal(err)
	}
	if reverse != "other direction" {
		t.Fatalf("directional reverse changed: %q", reverse)
	}
	assertNotes(nil)
}

func TestIntegrityDetectsDeliberateTypedEdgeDamage(t *testing.T) {
	s := fresh(t)
	category, _, err := s.CreatePage(ctx, "cli", "Damaged category", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterMetric(ctx, "cli", "Weight", "kg", ""); err != nil {
		t.Fatal(err)
	}
	metric, err := s.PageID(ctx, "Weight")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Link(ctx, "cli", metric, category, "part-of", ""); err != nil {
		t.Fatal(err)
	}
	var edge int64
	if err := s.DB.R.QueryRow(`SELECT id FROM links WHERE from_id=? AND to_id=? AND kind='part-of'`, metric, category).Scan(&edge); err != nil {
		t.Fatal(err)
	}
	clean, err := s.Integrity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !clean.OK {
		t.Fatalf("clean setup: %+v", clean)
	}
	// Deliberate damage bypasses only the guard in this disposable fixture.
	if _, err := s.DB.W.Exec(`DROP TRIGGER entities_endpoint_types; UPDATE entities SET entity_type='place' WHERE id=?`, category); err != nil {
		t.Fatal(err)
	}
	damaged, err := s.Integrity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if damaged.OK || damaged.ForeignKeys != 0 || len(damaged.OrphanEntities) != 0 || !damaged.FullTextIndexOK || len(damaged.InvalidTypedLinks) != 1 || damaged.InvalidTypedLinks[0] != edge {
		t.Fatalf("damage diagnostics: %+v", damaged)
	}
}
