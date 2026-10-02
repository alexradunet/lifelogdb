// Package db opens life.db the way docs/contract/connections.md requires: the per-connection pragmas, read
// back and refused if wrong, the SQLite version floor, BEGIN IMMEDIATE for every write, and read-only readers.
package db

//go:generate go run ../../tools/copyschema ../../docs/schema/schema.sql schema.sql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"modernc.org/sqlite"
)

//go:embed schema.sql
var Schema string

// MinVersion is the writer's floor (connections.md: the WAL race fixed in 3.51.3).
const MinVersion = 3051003

const applicationID = 0x4C494645

// The pragmas every writing connection sets and reads back, with the value it must read back.
var pragmas = []struct {
	name string
	set  string
	want int64
}{
	{"foreign_keys", "1", 1},
	{"recursive_triggers", "1", 1},
	{"synchronous", "2", 2},
	{"trusted_schema", "0", 0},
	{"busy_timeout", "5000", 5000},
}

func init() {
	d := &sqlite.Driver{}
	d.RegisterConnectionHook(checkConnection)
	sql.Register("lifelog", d)
}

// checkConnection runs on every new connection: the version floor always, trusted_schema on readers, the pragmas
// on writers.
func checkConnection(c sqlite.ExecQuerierContext, dsn string) error {
	if v := version(c); v < MinVersion {
		return fmt.Errorf("SQLite %d is older than %d (connections.md)", v, MinVersion)
	}
	if strings.Contains(dsn, "mode=ro") {
		if got := intOf(c, "PRAGMA trusted_schema"); got != 0 {
			return fmt.Errorf("PRAGMA trusted_schema reads back %d on a reader, want 0; refusing to read", got)
		}
		return nil
	}
	for _, p := range pragmas {
		if got := intOf(c, "PRAGMA "+p.name); got != p.want {
			return fmt.Errorf("PRAGMA %s reads back %d, want %d; refusing to write", p.name, got, p.want)
		}
	}
	return nil
}

func intOf(c sqlite.ExecQuerierContext, q string) int64 {
	n, _ := valueOf(c, q).(int64)
	return n
}

func valueOf(c sqlite.ExecQuerierContext, q string) driver.Value {
	rows, err := c.QueryContext(context.Background(), q, nil)
	if err != nil {
		return nil
	}
	defer rows.Close()
	v := make([]driver.Value, 1)
	if rows.Next(v) != nil {
		return nil
	}
	return v[0]
}

// version is sqlite_version() as SQLITE_VERSION_NUMBER (X*1000000 + Y*1000 + Z).
func version(c sqlite.ExecQuerierContext) int64 {
	s, _ := valueOf(c, "SELECT sqlite_version()").(string)
	var x, y, z int64
	fmt.Sscanf(s, "%d.%d.%d", &x, &y, &z)
	return x*1000000 + y*1000 + z
}

func dsn(path string, readOnly bool) string {
	q := url.Values{}
	if readOnly {
		q.Set("mode", "ro")
		q.Add("_pragma", "query_only(1)")
		q.Add("_pragma", "trusted_schema(0)")
	} else {
		q.Set("_defensive", "1")
		q.Set("_txlock", "immediate") // database/sql's Begin sends BEGIN IMMEDIATE
		for _, p := range pragmas {
			q.Add("_pragma", p.name+"("+p.set+")")
		}
	}
	return "file:" + filepath.ToSlash(path) + "?" + q.Encode()
}

// DB is one life.db: a single writing connection and a pool of read-only ones.
type DB struct {
	W, R *sql.DB
}

// Open opens an existing life.db.
func Open(path string) (*DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("no database at %s (create one with `lifelog init`): %w", path, err)
	}
	w, err := sql.Open("lifelog", dsn(path, false))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1) // one writer; BEGIN IMMEDIATE serialises against other processes
	r, err := sql.Open("lifelog", dsn(path, true))
	if err != nil {
		w.Close()
		return nil, err
	}
	d := &DB{W: w, R: r}
	var id int64
	if err := w.QueryRow("PRAGMA application_id").Scan(&id); err != nil {
		d.Close()
		return nil, err
	}
	if id != applicationID {
		d.Close()
		return nil, fmt.Errorf("%s is not a Lifelog database (application_id %#x)", path, id)
	}
	return d, nil
}

// Init creates a new life.db from the canonical DDL. It refuses an existing file.
func Init(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	w, err := sql.Open("lifelog", dsn(path, false))
	if err != nil {
		return err
	}
	defer w.Close()
	if _, err := w.Exec(Schema); err != nil {
		w.Close()
		os.Remove(path)
		return fmt.Errorf("applying schema.sql: %w", err)
	}
	_, err = w.Exec("PRAGMA optimize")
	return err
}

func (d *DB) Close() error {
	d.W.Exec("PRAGMA optimize")
	return errors.Join(d.R.Close(), d.W.Close())
}

// Write runs fn in one BEGIN IMMEDIATE transaction, committed when fn returns nil.
func (d *DB) Write(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Copy writes a copy of a life.db to a new file through a read-only connection (docs/contract/imports.md step 1:
// VACUUM INTO). It refuses an existing target.
func Copy(from, to string) error {
	if _, err := os.Stat(to); err == nil {
		return fmt.Errorf("%s already exists", to)
	}
	if _, err := os.Stat(from); err != nil {
		return fmt.Errorf("no database at %s: %w", from, err)
	}
	r, err := sql.Open("lifelog", "file:"+filepath.ToSlash(from)+"?mode=ro&_pragma=trusted_schema(0)")
	if err != nil {
		return err
	}
	defer r.Close()
	_, err = r.Exec(`VACUUM INTO ?`, to)
	return err
}
