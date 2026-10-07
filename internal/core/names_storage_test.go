package core

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
)

func TestNameOwnershipAndJournalReservations(t *testing.T) {
	s := fresh(t)
	ordinary, _, err := s.CreatePage(ctx, "cli", "Dated note", "prose")
	if err != nil {
		t.Fatal(err)
	}
	day, _, err := s.Capture(ctx, "cli", "2026-02-28", "day prose", nil)
	if err != nil {
		t.Fatal(err)
	}
	if again, _, err := s.Capture(ctx, "cli", "2026-02-28", "", nil); err == nil || again != 0 {
		t.Fatalf("empty capture = %d %v", again, err)
	}
	if again, _, err := s.Capture(ctx, "cli", "2026-02-28", "more", nil); err != nil || again != day {
		t.Fatalf("existing day = %d %v", again, err)
	}
	execRefused := func(id int64, q string, args ...any) {
		t.Helper()
		before, err := s.PageByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.DB.Write(ctx, func(tx *sql.Tx) error { _, err := tx.Exec(q, args...); return err }); err == nil {
			t.Fatalf("accepted %s", q)
		}
		after, err := s.PageByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("refusal changed identity: before=%+v after=%+v", before, after)
		}
	}
	execRefused(day, `INSERT INTO entity_names(entity_id,title,name_key) VALUES(?,'Heading','heading')`, day)
	execRefused(day, `UPDATE entity_names SET title='Other' WHERE entity_id=?`, day)
	execRefused(day, `UPDATE entities SET preferred_name_key='other' WHERE id=?`, day)
	execRefused(day, `UPDATE entities SET day='2026-03-01' WHERE id=?`, day)
	execRefused(day, `UPDATE entities SET entity_type='place' WHERE id=?`, day)
	execRefused(ordinary, `INSERT INTO entity_names(entity_id,title,name_key) VALUES(?,'2026-03-01','2026-03-01')`, ordinary)
	execRefused(ordinary, `UPDATE entities SET preferred_name_key='2026-03-01',day='2026-03-01' WHERE id=?`, ordinary)
	execRefused(ordinary, `UPDATE entities SET preferred_name_key='2026-02-28',day='2026-02-28' WHERE id=?`, ordinary)
	execRefused(ordinary, `UPDATE entity_names SET entity_id=? WHERE name_key='dated note'`, day)
	execRefused(ordinary, `UPDATE entity_names SET name_key='reassigned' WHERE entity_id=?`, ordinary)
	execRefused(ordinary, `DELETE FROM entity_names WHERE entity_id=?`, ordinary)
	if _, err := s.Rename(ctx, "cli", day, "Heading"); status(err) != 409 {
		t.Fatalf("day rename: %v", err)
	}
	if err := s.Promote(ctx, "cli", day, "place", ""); err == nil {
		t.Fatal("day promoted")
	}
	if _, err := s.Rename(ctx, "cli", ordinary, "New dated note"); err != nil {
		t.Fatalf("dated ordinary alias control: %v", err)
	}
	if err := s.Tombstone(ctx, "cli", day); err != nil {
		t.Fatal(err)
	}
	if again, _, err := s.Capture(ctx, "cli", "2026-02-28", "revived", nil); err != nil || again != day {
		t.Fatalf("revival=%d %v", again, err)
	}
	for _, q := range []string{
		`INSERT INTO entities(entity_type,preferred_name_key,created_at,updated_at,source) VALUES('page','missing',` + Now + `,` + Now + `,'cli')`,
		`INSERT INTO entities(entity_type,preferred_name_key,created_at,updated_at,source) VALUES('page','mood',` + Now + `,` + Now + `,'cli')`,
	} {
		if err := s.DB.Write(ctx, func(tx *sql.Tx) error { _, err := tx.Exec(q); return err }); err == nil {
			t.Fatalf("deferred owner commit accepted: %s", q)
		}
	}
	r, err := s.Integrity(ctx)
	if err != nil || !r.OK {
		t.Fatalf("integrity after refusals: %+v %v", r, err)
	}
}

func TestStableNamesRevivalSelfAndRollback(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Old", "kept")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename(ctx, "cli", id, "New"); err != nil {
		t.Fatal(err)
	}
	before, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename(ctx, "cli", id, "New"); err != nil {
		t.Fatal(err)
	}
	same, err := s.PageByID(ctx, id)
	if err != nil || same.Version != before.Version {
		t.Fatalf("noop token: %+v %v", same, err)
	}
	fail := errors.New("rollback")
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		if _, err := tx.Rename(id, "Rolled back"); err != nil {
			return err
		}
		return fail
	}); !errors.Is(err, fail) {
		t.Fatal(err)
	}
	same, err = s.PageByID(ctx, id)
	if err != nil || !reflect.DeepEqual(before, same) {
		t.Fatalf("rename rollback: %+v %v", same, err)
	}
	if got, err := s.PageID(ctx, "Rolled back"); err != nil || got != 0 {
		t.Fatalf("rolled back alias retained: %d %v", got, err)
	}
	r, err := s.SaveBody(ctx, "cli", id, "[[Old]] [[New]]", before.Version)
	if err != nil || len(r.Linked) != 0 {
		t.Fatalf("self aliases: %+v %v", r, err)
	}
	if err := s.Tombstone(ctx, "cli", id); err != nil {
		t.Fatal(err)
	}
	source, _, err := s.CreatePage(ctx, "cli", "Source", "[[Old]] [[New]]")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.PageByID(ctx, source)
	if err != nil || len(p.Out) != 1 || p.Out[0].ID != id {
		t.Fatalf("revived alias edge: %+v %v", p, err)
	}
	if err := s.Tombstone(ctx, "cli", id); err != nil {
		t.Fatal(err)
	}
	r, err = s.SaveBody(ctx, "cli", source, "[[Old]] [[New]]", p.Version)
	if err != nil || len(r.Revived) != 1 || len(r.Linked) != 1 {
		t.Fatalf("revival feedback: %+v %v", r, err)
	}
}

func TestMoodIdentitySurvivesRename(t *testing.T) {
	s := fresh(t)
	if _, err := s.Rename(ctx, "cli", 1, "Wellbeing"); err != nil {
		t.Fatal(err)
	}
	if err := s.StartHabit(ctx, "cli", "Wellbeing", "2026-02-28", ""); err == nil {
		t.Fatal("renamed Mood became habit")
	}
	for _, name := range []string{"Mood", "Wellbeing"} {
		if _, err := s.Record(context.Background(), "cli", Reading{Metric: name, Day: "2026-02-28", Value: 0}); status(err) != 422 {
			t.Fatalf("invalid Mood via %q: %v", name, err)
		}
	}
	v := float64(4)
	_, _, err := s.Capture(ctx, "cli", "2026-02-28", "normal", &v)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.Series(ctx, "Mood", "2026-02-27", "2026-02-28")
	if err != nil || len(rows) != 1 || rows[0].Metric != "Wellbeing" {
		t.Fatalf("alias series: %+v %v", rows, err)
	}
	bad := float64(0)
	if _, _, err := s.Correct(ctx, "cli", rows[0].ID, &bad); status(err) != 422 {
		t.Fatalf("invalid renamed Mood correction: %v", err)
	}
}

func TestNameRevisionExhaustionRefusesAtomically(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Old", "kept [[Target]]")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename(ctx, "cli", id, "New"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename(ctx, "cli", id, "Old"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.W.Exec(`UPDATE entities SET revision=9223372036854775807 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	before, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename(ctx, "cli", id, "Old"); err != nil {
		t.Fatalf("exhausted no-op: %v", err)
	}
	for _, name := range []string{"OLD", "New", "Unowned"} {
		if _, err := s.Rename(ctx, "cli", id, name); err == nil {
			t.Fatalf("exhausted rename %q accepted", name)
		}
		after, err := s.PageByID(ctx, id)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("exhaustion changed state: %+v %v", after, err)
		}
	}
	if id, err := s.PageID(ctx, "Unowned"); err != nil || id != 0 {
		t.Fatalf("failed alias persisted=%d %v", id, err)
	}
	r, err := s.Integrity(ctx)
	if err != nil || !r.OK {
		t.Fatalf("exhaustion damaged search: %+v %v", r, err)
	}
}

func TestIntegrityDetectsDeliberatePreferredOwnerDamage(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Owner", "prose")
	if err != nil {
		t.Fatal(err)
	}
	// Deliberate external damage only: same pooled connection, FK disabled for this write.
	if _, err := s.DB.W.Exec(`PRAGMA foreign_keys=OFF; UPDATE entities SET preferred_name_key='mood' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.W.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	r, err := s.Integrity(ctx)
	if err != nil || r.OK || !reflect.DeepEqual(r.OrphanEntities, []int64{id}) {
		t.Fatalf("independent preferred-owner diagnostic: %+v %v", r, err)
	}
}
