package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// fakeConn answers the connection check's reads from a table; a PRAGMA read fails when pragmasFail is set.
type fakeConn struct {
	rows        map[string]driver.Value
	pragmasFail bool
}

func (f fakeConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, errors.New("the check does not write")
}

func (f fakeConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if f.pragmasFail && strings.HasPrefix(q, "PRAGMA") {
		return nil, errors.New("synthetic read failure")
	}
	v, ok := f.rows[q]
	if !ok {
		return nil, errors.New("unexpected read " + q)
	}
	return &oneRow{v: v}, nil
}

type oneRow struct {
	v    driver.Value
	done bool
}

func (r *oneRow) Columns() []string { return []string{"v"} }
func (r *oneRow) Close() error      { return nil }
func (r *oneRow) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = r.v
	return nil
}

// The connection check refuses what it cannot read: a failed PRAGMA read is not a 0 that happens to be the value
// trusted_schema wants (docs/plans/098-the-codebase-made-cohesive.md).
func TestTheConnectionCheckRefusesWhatItCannotRead(t *testing.T) {
	c := fakeConn{rows: map[string]driver.Value{"SELECT sqlite_version()": "3.51.3"}, pragmasFail: true}
	if err := checkConnection(c, dsn(filepath.Join(t.TempDir(), "life.db"), true)); err == nil {
		t.Fatal("a reader whose trusted_schema could not be read was accepted")
	}
}

// A reader is known by its DSN's query, not by the text "mode=ro" anywhere in the DSN: a database under a folder of
// that name is still a writer, and its pragmas are checked.
func TestAReaderIsKnownByItsQueryNotByItsPath(t *testing.T) {
	rows := map[string]driver.Value{"SELECT sqlite_version()": "3.51.3", "PRAGMA trusted_schema": int64(0)}
	for _, p := range pragmas {
		rows["PRAGMA "+p.name] = p.want
	}
	rows["PRAGMA foreign_keys"] = int64(0) // a writer with this value must be refused
	path := filepath.Join(t.TempDir(), "mode=ro", "life.db")
	if err := checkConnection(fakeConn{rows: rows}, dsn(path, false)); err == nil || !strings.Contains(err.Error(), "foreign_keys") {
		t.Fatalf("a writer under a folder named mode=ro skipped its checks: %v", err)
	}
	if err := checkConnection(fakeConn{rows: rows}, dsn(path, true)); err != nil {
		t.Fatalf("a reader with trusted_schema 0 is accepted: %v", err)
	}
}
