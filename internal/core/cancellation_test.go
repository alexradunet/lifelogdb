package core

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestTransactionOperationsStopAfterCancellation(t *testing.T) {
	for _, operation := range []struct {
		name string
		run  func(*Tx) error
	}{
		{"lookup", func(tx *Tx) error { _, err := tx.Lookup("Before cancellation"); return err }},
		{"create", func(tx *Tx) error {
			_, _, _, err := tx.CreatePage("After cancellation", "", "2026-10-07", "")
			return err
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			s := fresh(t)
			opCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			err := s.Do(opCtx, "cli", func(tx *Tx) error {
				if _, _, _, err := tx.CreatePage("Before cancellation", "", "2026-10-07", ""); err != nil {
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
