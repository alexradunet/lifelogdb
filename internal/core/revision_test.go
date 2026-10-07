package core

import (
	"context"
	"errors"
	"testing"
)

func TestConditionalSaveRejectsEqualClockStaleToken(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Clock witness", "first")
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	var clock string
	if err := s.DB.R.QueryRow(`SELECT updated_at FROM entities WHERE id=?`, id).Scan(&clock); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(ctx, "cli", id, "committed [[Kept]]", before.Version); err != nil {
		t.Fatal(err)
	}
	// Deterministic equal-clock witness, not a timing-dependent collision search.
	// Only audit time is restored; committed editable state/version is untouched.
	if _, err := s.DB.W.Exec(`UPDATE entities SET updated_at=? WHERE id=?`, clock, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(ctx, "cli", id, "stale [[Forbidden]]", before.Version); status(err) != 409 {
		t.Fatalf("stale token: %v; want 409", err)
	}
	after, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.Body != "committed [[Kept]]" || after.Version == before.Version || titles(after.Out, "wikilink") != "Kept" {
		t.Fatalf("stale save changed committed state: %+v", after)
	}
	target, err := s.PageID(ctx, "Forbidden")
	if err != nil {
		t.Fatal(err)
	}
	if target != 0 {
		t.Fatal("stale save created a target")
	}
}

func TestEditRevisionNoopRollbackAndTypedMutation(t *testing.T) {
	s := fresh(t)
	id, err := s.CreatePerson(ctx, "cli", "Person witness", "Name", "", "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(ctx, "cli", id, before.Body, before.Version); err != nil {
		t.Fatal(err)
	}
	same, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if same.Version != before.Version {
		t.Fatal("body no-op consumed revision")
	}
	if _, err := s.DB.W.Exec(`UPDATE people SET name=name WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	same, err = s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if same.Version != before.Version {
		t.Fatal("typed no-op consumed revision")
	}
	rollback := errors.New("rollback edit")
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		if _, err := tx.SetBody(id, "rollback [[Missing]]"); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	same, err = s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if same.Version != before.Version || same.Body != before.Body {
		t.Fatal("rollback consumed revision or body")
	}
	if _, err := s.DB.W.Exec(`UPDATE people SET name='New name' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	changed, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Version == before.Version {
		t.Fatal("typed mutation retained stale revision")
	}
	if _, err := s.SaveBody(ctx, "cli", id, "stale", before.Version); status(err) != 409 {
		t.Fatalf("typed stale save: %v", err)
	}
}

func TestEditRevisionExhaustionAndCancellation(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Exhaustion witness", "kept")
	if err != nil {
		t.Fatal(err)
	}
	// Explicit accelerated counter fixture; no clock or production connection bypass.
	if _, err := s.DB.W.Exec(`UPDATE entities SET revision=9223372036854775807 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	before, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if before.Version != "9223372036854775807" {
		t.Fatalf("lossy token %q", before.Version)
	}
	if _, err := s.SaveBody(ctx, "cli", id, before.Body, before.Version); err != nil {
		t.Fatalf("exhausted no-op: %v", err)
	}
	if _, err := s.SaveBody(ctx, "cli", id, "changed [[Forbidden]]", before.Version); err == nil {
		t.Fatal("exhausted edit accepted")
	}
	if err := s.Tombstone(ctx, "cli", id); err == nil {
		t.Fatal("exhausted lifecycle change accepted")
	}
	if _, err := s.DB.W.Exec(`UPDATE entities SET revision=1 WHERE id=?`, id); err == nil {
		t.Fatal("counter decrease accepted")
	}
	after, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != before.Version || after.Body != before.Body || after.DeletedAt != "" {
		t.Fatalf("exhausted write changed state: %+v", after)
	}
	if target, err := s.PageID(ctx, "Forbidden"); err != nil || target != 0 {
		t.Fatalf("target after exhaustion: %d %v", target, err)
	}
	normal, _, err := s.CreatePage(ctx, "cli", "Canceled witness", "before")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.PageByID(ctx, normal)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	defer cancel()
	err = s.Do(canceled, "cli", func(tx *Tx) error {
		if _, err := tx.SetBody(normal, "changed [[Canceled target]]"); err != nil {
			return err
		}
		cancel()
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	current, err := s.PageByID(ctx, normal)
	if err != nil {
		t.Fatal(err)
	}
	if current.Body != p.Body || current.Version != p.Version {
		t.Fatalf("cancellation changed state: %+v", current)
	}
	if target, err := s.PageID(ctx, "Canceled target"); err != nil || target != 0 {
		t.Fatalf("target after cancellation: %d %v", target, err)
	}
	var clock string
	if err := s.DB.R.QueryRow(`SELECT updated_at FROM entities WHERE id=?`, normal).Scan(&clock); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(ctx, "cli", normal, "timestamp bypass", clock); status(err) != 409 {
		t.Fatalf("timestamp token accepted: %v", err)
	}
}
