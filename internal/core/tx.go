package core

import (
	"context"
	"database/sql"
	"errors"
)

// Tx is one BEGIN IMMEDIATE write transaction and the writer whose rows it writes (lifelog_meta.source).
// Every write operation is a method on it, so an import composes several into one transaction; each Store
// method is the same operation alone. Its SQL uses the context of the owning Do call.
type Tx struct {
	tx     *sql.Tx
	ctx    context.Context
	today  func() string // the local day of the store's clock (Store.Today)
	Source string
}

// Do runs fn in one write transaction, committed when fn returns nil.
func (s *Store) Do(ctx context.Context, source string, fn func(*Tx) error) error {
	if err := CheckSource(source); err != nil {
		return err
	}
	return refused(s.DB.Write(ctx, func(tx *sql.Tx) error { return fn(&Tx{tx: tx, ctx: ctx, today: s.Today, Source: source}) }))
}

// alone runs fn as the one operation of its own write transaction (Do): the Store form of a Tx method. s stands for
// the receiver, because a method cannot have a type parameter. On an error the write rolled back, so the result is
// the zero value: no surface reports what the write did not keep.
func alone[T any](s *Store, ctx context.Context, source string, fn func(*Tx) (T, error)) (T, error) {
	var v T
	if err := s.Do(ctx, source, func(t *Tx) (err error) { v, err = fn(t); return err }); err != nil {
		var zero T
		return zero, err
	}
	return v, nil
}

// errDryRun rolls a DryRun back after fn succeeded.
var errDryRun = errors.New("dry run")

// dryRun runs fn in a write transaction and always rolls it back: what a write would do, without doing it. Only
// errDryRun itself is a clean dry run: a rollback that failed after it is a fault (refused).
func (s *Store) dryRun(ctx context.Context, source string, fn func(*Tx) error) error {
	err := s.Do(ctx, source, func(t *Tx) error {
		if err := fn(t); err != nil {
			return err
		}
		return errDryRun
	})
	if err == errDryRun {
		return nil
	}
	return err
}

// savepoint runs fn inside a savepoint of the transaction: when fn fails, what it wrote is rolled back and the
// transaction goes on; fn's error is returned, a constraint or trigger of the DDL as the 422 that Do would return.
// A wikilink target that cannot be made, a facts write the writer refuses: the rest of the work is unaffected.
func (t *Tx) savepoint(fn func() error) error {
	if _, err := t.tx.ExecContext(t.ctx, `SAVEPOINT step`); err != nil {
		return err
	}
	if err := fn(); err != nil {
		if _, e := t.tx.ExecContext(t.ctx, `ROLLBACK TO step`); e != nil {
			return e
		}
		if _, e := t.tx.ExecContext(t.ctx, `RELEASE step`); e != nil {
			return e
		}
		return refused(err)
	}
	_, err := t.tx.ExecContext(t.ctx, `RELEASE step`)
	return err
}
