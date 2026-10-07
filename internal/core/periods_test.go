package core

import (
	"errors"
	"testing"
)

func TestLifePeriodWriterQueryAndRollback(t *testing.T) {
	s := fresh(t)
	start, end := "2018-09", ".."
	var id int64
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		var err error
		id, err = tx.CreatePeriod("Study at North College", "With [[Sam]].", &start, &end)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	p, err := s.PageByID(ctx, id)
	if err != nil || p.Type != "period" || p.Period == nil || p.Body != "With [[Sam]]." || titles(p.Out, "wikilink") != "Sam" {
		t.Fatalf("persisted page %+v %v", p, err)
	}
	for _, horizon := range []string{"2018-09-15", "2020-12-31"} {
		ps, err := s.LifePeriods(ctx, "2018-09-15", horizon, false)
		if err != nil || len(ps) != 1 || ps[0].Membership != "possible" {
			t.Fatalf("horizon%s %+v %v", horizon, ps, err)
		}
	}
	version := p.Version
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.EditPeriod(id, version, &start, &end) }); err != nil {
		t.Fatal(err)
	}
	p, err = s.PageByID(ctx, id)
	if err != nil || p.Version != version {
		t.Fatalf("no-op %+v %v", p, err)
	}
	observed := "2018-09-15"
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.EditPeriod(id, version, &start, &observed) }); err != nil {
		t.Fatal(err)
	}
	p, err = s.PageByID(ctx, id)
	if err != nil || p.Version == version {
		t.Fatalf("edit %+v %v", p, err)
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.EditPeriod(id, version, nil, nil) }); status(err) != 409 {
		t.Fatalf("stale error %v", err)
	}
	after := p.Version
	rollback := errors.New("deliberate rollback")
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		if err := tx.EditPeriod(id, after, nil, nil); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	p, err = s.PageByID(ctx, id)
	if err != nil || p.Version != after || p.Period.End == nil || *p.Period.End != observed {
		t.Fatalf("rollback %+v %v", p, err)
	}
	// Creation writes the entity with its whole body, so the failure to inject after the extension row is its own
	// insert; a failure after CreatePeriod returned (the wikilink target it made) must undo the target as well.
	if _, err := s.DB.W.Exec(`CREATE TRIGGER period_extension_failure AFTER INSERT ON periods BEGIN SELECT RAISE(ABORT,'injected post-extension failure'); END`); err != nil {
		t.Fatal(err)
	}
	err = s.Do(ctx, "cli", func(tx *Tx) error {
		_, err := tx.CreatePeriod("Failed period", "[[Atomic new name]]", nil, nil)
		return err
	})
	if err == nil {
		t.Fatal("accepted injected failure")
	}
	if _, err := s.DB.W.Exec(`DROP TRIGGER period_extension_failure`); err != nil {
		t.Fatal(err)
	}
	err = s.Do(ctx, "cli", func(tx *Tx) error {
		if _, err := tx.CreatePeriod("Failed period", "[[Atomic new name]]", nil, nil); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("rolled back creation: %v", err)
	}
	var count int
	if err := s.DB.R.QueryRow(`SELECT count(*) FROM entity_names WHERE name_key IN ('failed period','atomic new name')`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial names: %d %v", count, err)
	}
	if err := s.DB.R.QueryRow(`SELECT count(*) FROM entities e JOIN periods p ON p.id=e.id WHERE e.body LIKE '%Atomic new name%'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial period: %d %v", count, err)
	}
}

func TestLifePeriodStorageGuardsAndPromotion(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Coastal trip", "Original body")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	start, end := "2019~", "2019%"
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.PromotePeriod(id, p.Version, &start, &end) }); err != nil {
		t.Fatal(err)
	}
	p, err = s.PageByID(ctx, id)
	if err != nil || p.Body != "Original body" || p.Period == nil {
		t.Fatalf("promotion %+v %v", p, err)
	}
	for _, alias := range []string{"id", "rowid", "_rowid_", "oid"} {
		if _, err := s.DB.W.Exec(`UPDATE periods SET `+alias+`=?,start_boundary=NULL WHERE id=?`, id+1000, id); err == nil {
			t.Fatalf("accepted %s edit", alias)
		}
	}
	for _, bad := range []string{"2019-02-29?", "2019-13", "2019~~", "2019-2", ".."} {
		if _, err := s.DB.W.Exec(`UPDATE periods SET start_boundary=? WHERE id=?`, bad, id); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	if _, err := s.DB.W.Exec(`UPDATE periods SET start_boundary='2019-10',end_boundary='2019-09' WHERE id=?`, id); err == nil {
		t.Fatal("accepted reversed span")
	}
	if _, err := s.DB.W.Exec(`DELETE FROM periods WHERE id=?`, id); err == nil {
		t.Fatal("deleted extension")
	}
	for _, v := range []string{"0000-02-29", "9999-12", "2019-02?"} {
		if _, err := s.DB.W.Exec(`UPDATE periods SET start_boundary=?,end_boundary=? WHERE id=?`, v, v, id); err != nil {
			t.Fatalf("valid %s: %v", v, err)
		}
	}
	day, _, err := s.CreatePage(ctx, "cli", "2020-01-01", "")
	if err != nil {
		t.Fatal(err)
	}
	dp, err := s.PageByID(ctx, day)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.PromotePeriod(day, dp.Version, nil, nil) }); err == nil {
		t.Fatal("journal promotion accepted")
	}
	if err := s.Tombstone(ctx, "cli", id); err != nil {
		t.Fatal(err)
	}
	ps, err := s.LifePeriods(ctx, "", "", false)
	if err != nil || len(ps) != 0 {
		t.Fatalf("active %+v %v", ps, err)
	}
	ps, err = s.LifePeriods(ctx, "", "", true)
	if err != nil || len(ps) != 1 || ps[0].DeletedAt == "" {
		t.Fatalf("historical %+v %v", ps, err)
	}
	integrity, err := s.Integrity(ctx)
	if err != nil || !integrity.OK {
		t.Fatalf("integrity %+v %v", integrity, err)
	}
}
