package core

import (
	"context"
	"errors"
	"fmt"
	"lifelog/internal/db"
	"path/filepath"
	"testing"
)

func TestSessionCaptureRetryEditLifecycleAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(path); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	s := &Store{DB: d}
	kind, _, err := s.CreatePage(ctx, "cli", "Sleep", "")
	if err != nil {
		t.Fatal(err)
	}
	in := SessionInput{Kind: "Sleep", Day: "2020-01-02", Key: "18446744073709551615", StartLocal: "2020-01-01T23:00:00.000", EndLocal: "2020-01-02T07:00:00.000", StartZoneUnverified: "Claimed/Zone"}
	var id int64
	if err := s.Do(ctx, "import:synthetic", func(tx *Tx) error { var e error; id, _, e = tx.CaptureSession(in); return e }); err != nil {
		t.Fatal(err)
	}
	have, err := s.Session(ctx, id)
	if err != nil || have.Day != "2020-01-02" || have.StartAt != "" || have.ElapsedMilliseconds != nil || have.Key != in.Key || have.StartZoneUnverified != "Claimed/Zone" {
		t.Fatalf("stored %+v %v", have, err)
	}
	if _, err := s.Rename(ctx, "cli", kind, "Rest"); err != nil {
		t.Fatal(err)
	}
	in.Kind = "Rest"
	if err := s.Do(ctx, "import:synthetic", func(tx *Tx) error {
		got, existing, err := tx.CaptureSession(in)
		if err == nil && (!existing || got != id) {
			return fmt.Errorf("retry %d %v", got, existing)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := s.Session(ctx, id)
	if err != nil || updated.Version != have.Version || updated.KindID != kind || updated.Kind != "Rest" {
		t.Fatalf("rename/retry %+v %v", updated, err)
	}
	changed := in
	changed.Day = "2020-01-03"
	if err := s.Do(ctx, "import:synthetic", func(tx *Tx) error { _, _, err := tx.CaptureSession(changed); return err }); status(err) != 409 {
		t.Fatalf("changed retry %v", err)
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.EditSession(id, have.Version, in) }); err != nil {
		t.Fatal(err)
	}
	updated, err = s.Session(ctx, id)
	if err != nil || updated.Version != have.Version {
		t.Fatalf("no-op %+v %v", updated, err)
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.EditSession(id, have.Version, changed) }); err != nil {
		t.Fatal(err)
	}
	updated, err = s.Session(ctx, id)
	if err != nil || updated.Version == have.Version || updated.Day != changed.Day {
		t.Fatalf("edit %+v %v", updated, err)
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.EditSession(id, have.Version, in) }); status(err) != 409 {
		t.Fatalf("stale %v", err)
	}
	rollback := errors.New("deliberate rollback")
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		if err := tx.EditSession(id, updated.Version, in); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	kept, err := s.Session(ctx, id)
	if err != nil || kept.Version != updated.Version || kept.Day != changed.Day {
		t.Fatalf("rollback %+v %v", kept, err)
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.SessionLifecycle(id, kept.Version, true) }); err != nil {
		t.Fatal(err)
	}
	tomb, err := s.Session(ctx, id)
	if err != nil || tomb.DeletedAt == "" || tomb.Version == kept.Version {
		t.Fatalf("tombstone %+v %v", tomb, err)
	}
	if err := s.Do(ctx, "import:synthetic", func(tx *Tx) error { _, _, err := tx.CaptureSession(changed); return err }); status(err) != 409 {
		t.Fatalf("retry tombstone %v", err)
	}
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.SessionLifecycle(id, tomb.Version, false) }); err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", kind); err != nil {
		t.Fatal(err)
	}
	revived, err := s.Session(ctx, id)
	if err != nil || revived.KindDeletedAt == "" || revived.DeletedAt != "" {
		t.Fatalf("kind lifecycle %+v %v", revived, err)
	}
	in.Key = "9007199254740993"
	if err := s.Do(ctx, "cli", func(tx *Tx) error { _, _, err := tx.CaptureSession(in); return err }); err == nil {
		t.Fatal("selected tombstoned kind")
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d, err = db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.DB = d
	reopened, err := s.Session(ctx, id)
	if err != nil || reopened.Key != "18446744073709551615" || reopened.KindID != kind || reopened.StartLocal != in.StartLocal {
		t.Fatalf("reopen %+v %v", reopened, err)
	}
}

func TestSessionStorageIdentityAndTimeRefusals(t *testing.T) {
	s := fresh(t)
	kind, _, err := s.CreatePage(ctx, "cli", "Workout", "")
	if err != nil {
		t.Fatal(err)
	}
	in := SessionInput{Kind: "Workout", Day: "2020-01-02", StartAt: "2020-01-01T23:00:00.000Z", EndAt: "2020-01-02T07:00:00.000Z", StartOffset: "+23:59"}
	var id int64
	if err := s.Do(ctx, "cli", func(tx *Tx) error { var e error; id, _, e = tx.CaptureSession(in); return e }); err != nil {
		t.Fatal(err)
	}
	have, err := s.Session(ctx, id)
	if err != nil || have.ElapsedMilliseconds == nil || *have.ElapsedMilliseconds != 28800000 {
		t.Fatalf("elapsed %+v %v", have, err)
	}
	for _, alias := range []string{"id", "rowid", "_rowid_", "oid"} {
		if _, err := s.DB.W.Exec(`UPDATE sessions SET `+alias+`=?,day='2020-01-03' WHERE id=?`, id+1000, id); err == nil {
			t.Fatalf("accepted %s", alias)
		}
	}
	for _, stmt := range []string{`UPDATE sessions SET start_at=NULL WHERE id=?`, `UPDATE sessions SET start_local='2020-01-01T23:00:00.000' WHERE id=?`, `UPDATE sessions SET end_at='2020-01-01T22:59:59.999Z' WHERE id=?`, `UPDATE sessions SET start_offset='-00:00' WHERE id=?`, `UPDATE sessions SET start_offset='+24:00' WHERE id=?`, `UPDATE sessions SET source='api' WHERE id=?`, `DELETE FROM sessions WHERE id=?`} {
		if _, err := s.DB.W.Exec(stmt, id); err == nil {
			t.Fatalf("accepted %s", stmt)
		}
	}
	if err := s.Promote(ctx, "cli", kind, "person", ""); err == nil {
		t.Fatal("promoted retained session kind")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.Do(canceled, "cli", func(tx *Tx) error { _, _, err := tx.CaptureSession(in); return err }); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled capture %v", err)
	}
	after, err := s.Session(ctx, id)
	if err != nil || after.Version != have.Version || after.Day != have.Day {
		t.Fatalf("forbidden side effects %+v %v", after, err)
	}
}
