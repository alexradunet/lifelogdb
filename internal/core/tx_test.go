package core

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// TestARefusedWriteReturnsNoResult: a Store method returns the zero result with its error. The write rolled back, so a
// Sync of the links it made would name pages that do not exist. Capture fills its Sync before the mood reading is
// refused, and CreatePage and TryAddFile fill theirs before a day as the category is refused.
func TestARefusedWriteReturnsNoResult(t *testing.T) {
	s := fresh(t)
	mood := 9.0 // the range of Mood is 1-5 (measurements_scale_range)
	id, r, err := s.Capture(ctx, "cli", "2026-10-01", "Met [[Sam]]", &mood)
	if err == nil || id != 0 || !reflect.DeepEqual(r, Sync{}) {
		t.Errorf("Capture = %d, %+v, %v; want 0, no Sync and the refusal", id, r, err)
	}
	id, r, err = s.CreatePage(ctx, "cli", "Notes", "See [[Sam]]", "2026-10-01")
	if err == nil || id != 0 || !reflect.DeepEqual(r, Sync{}) {
		t.Errorf("CreatePage = %d, %+v, %v; want 0, no Sync and the refusal", id, r, err)
	}
	k, err := s.TryAddFile(ctx, "cli", FileIn{Title: "Scan.pdf", SHA256: sha('a'), MIME: "application/pdf", Body: "See [[Sam]]", Category: "2026-10-01"})
	if err == nil || !reflect.DeepEqual(k, Kept{}) {
		t.Errorf("TryAddFile = %+v, %v; want no result and the refusal", k, err)
	}
	if sam, err := s.PageID(ctx, "Sam"); err != nil || sam != 0 {
		t.Errorf("Sam = %d, %v; want no page", sam, err)
	}
}

// TestAFailedRollbackIsAFault: when fn fails and the rollback fails too, the transaction can be open or kept in part.
// Do then returns a fault, not the refusal of fn (a 409 or a 422 on every surface), and dryRun does not report a clean
// dry run. fn commits by itself, so the rollback finds no open transaction, as in db's TestWriteReportsAFailedRollback.
func TestAFailedRollbackIsAFault(t *testing.T) {
	s := fresh(t)
	commit := func(tx *Tx) error {
		_, err := tx.tx.ExecContext(tx.ctx, "COMMIT")
		return err
	}
	after := func(fail func(tx *Tx) error) func(tx *Tx) error {
		return func(tx *Tx) error {
			if err := commit(tx); err != nil {
				return err
			}
			return fail(tx)
		}
	}
	for _, c := range []struct {
		name string
		run  func() error
	}{
		{"a refusal of core", func() error {
			return s.Do(ctx, "cli", after(func(*Tx) error { return Conflict("synthetic refusal") }))
		}},
		{"a title in use", func() error {
			return s.Do(ctx, "cli", after(func(*Tx) error { return &ExistsError{ID: 1, Title: "Mood"} }))
		}},
		{"a refusal of the DDL", func() error {
			return s.Do(ctx, "cli", after(func(tx *Tx) error {
				_, err := tx.tx.ExecContext(tx.ctx, `INSERT INTO entities(entity_type) VALUES ('no such type')`)
				return err
			}))
		}},
		{"a dry run", func() error { return s.dryRun(ctx, "cli", commit) }},
	} {
		err := c.run()
		var refusal *Error
		var exists *ExistsError
		var rollback *sqlite.Error
		if err == nil || errors.As(err, &refusal) || errors.As(err, &exists) {
			t.Errorf("%s: %v; want a fault, not a refusal or no error", c.name, err)
		}
		if !errors.As(err, &rollback) || rollback.Code()&0xff == sqlite3.SQLITE_CONSTRAINT {
			t.Errorf("%s: %v; want the error of the rollback in it", c.name, err)
		}
	}
	if _, _, err := s.CreatePage(ctx, "cli", "After", "", ""); err != nil {
		t.Errorf("the writer cannot write after the failed rollbacks: %v", err)
	}
}

// TestAFailedSavepointRollbackKeepsBothErrors: fn fails, and then the savepoint cannot roll back to its start. The
// error of savepoint holds the error of fn and the error of the rollback, and Do reports it as a fault, not as the
// refusal of fn. Here fn releases the savepoint itself, so ROLLBACK TO finds no savepoint.
func TestAFailedSavepointRollbackKeepsBothErrors(t *testing.T) {
	s := fresh(t)
	refusal := Conflict("synthetic refusal")
	var inner error
	err := s.Do(ctx, "cli", func(tx *Tx) error {
		inner = tx.savepoint(func() error {
			if _, err := tx.tx.ExecContext(tx.ctx, `RELEASE step`); err != nil {
				return err
			}
			return refusal
		})
		return inner
	})
	var rollback *sqlite.Error
	if !errors.Is(inner, refusal) || !errors.As(inner, &rollback) {
		t.Errorf("savepoint: %v; want the error of fn and the error of ROLLBACK TO in it", inner)
	}
	var e *Error
	if err == nil || errors.As(err, &e) {
		t.Errorf("Do: %v; want a fault, not a refusal or no error", err)
	}
}

func TestTransactionOperationsStopAfterCancellation(t *testing.T) {
	for _, operation := range []struct {
		name string
		run  func(*Tx) error
	}{
		{"lookup", func(tx *Tx) error { _, err := tx.lookup("Before cancellation"); return err }},
		{"create", func(tx *Tx) error {
			_, _, err := tx.CreatePage("After cancellation", "", "2026-10-07")
			return err
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			s := fresh(t)
			opCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			err := s.Do(opCtx, "cli", func(tx *Tx) error {
				if _, _, err := tx.CreatePage("Before cancellation", "", "2026-10-07"); err != nil {
					return err
				}
				cancel()
				err := operation.run(tx)
				if !errors.Is(err, context.Canceled) && !errors.Is(err, sql.ErrTxDone) {
					t.Errorf("operation after cancellation: got %v, want cancellation or rolled-back transaction", err)
				}
				return err
			})
			if !errors.Is(err, context.Canceled) && !errors.Is(err, sql.ErrTxDone) {
				t.Errorf("transaction: got %v, want cancellation or rolled-back transaction", err)
			}
			var count int
			if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM entities WHERE source='cli'`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Errorf("canceled transaction persisted %d entities", count)
			}
		})
	}
}
