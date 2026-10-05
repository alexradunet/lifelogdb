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
	"time"

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
	return sqliteFileURI(path, q)
}

func sqliteFileURI(path string, q url.Values) string {
	return (&url.URL{Scheme: "file", Opaque: sqliteURIPath(path), RawQuery: q.Encode()}).String()
}

func sqliteURIPath(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

// DB is one life.db: a single writing connection and a pool of read-only ones.
type DB struct {
	W, R *sql.DB
	path string
	keep bool // a snapshot under its restore check: Close leaves the file as it was
}

// Open opens an existing life.db.
func Open(path string) (*DB, error) { return open(path, false) }

func open(path string, keep bool) (*DB, error) {
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
	d := &DB{W: w, R: r, path: path, keep: keep}
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
	return initWith(path, func(w *sql.DB) error {
		_, err := w.Exec(Schema)
		return err
	})
}

func initWith(path string, apply func(*sql.DB) error) error {
	reserved, err := reserveDatabasePath(path)
	if err != nil {
		return err
	}
	w, err := sql.Open("lifelog", dsn(path, false))
	if err != nil {
		return errors.Join(err, removeReservedDatabasePath(path, reserved))
	}
	if err := apply(w); err != nil {
		closeErr := w.Close()
		cleanupErr := removeReservedDatabasePath(path, reserved)
		return errors.Join(fmt.Errorf("applying schema.sql: %w", err), closeErr, cleanupErr)
	}
	if _, err := w.Exec("PRAGMA optimize"); err != nil {
		return errors.Join(err, w.Close())
	}
	return w.Close()
}

func reserveDatabasePath(path string) (os.FileInfo, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("%s already exists", path)
		}
		return nil, fmt.Errorf("reserving %s: %w", path, err)
	}
	info, statErr := f.Stat()
	closeErr := f.Close()
	if statErr != nil {
		return nil, errors.Join(fmt.Errorf("stat reserved database %s: %w", path, statErr), closeErr)
	}
	if closeErr != nil {
		return nil, errors.Join(fmt.Errorf("closing reserved database %s: %w", path, closeErr), removeReservedDatabasePath(path, info))
	}
	return info, nil
}

func removeReservedDatabasePath(path string, reserved os.FileInfo) error {
	if reserved == nil {
		return nil
	}
	current, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("checking reserved database %s for cleanup: %w", path, err)
	}
	if !os.SameFile(reserved, current) {
		return nil
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("removing failed database %s: %w", path, err)
	}
	return nil
}

func (d *DB) Close() error {
	if !d.keep {
		d.W.Exec("PRAGMA optimize")
	}
	return errors.Join(d.R.Close(), d.W.Close())
}

// AdHocReader opens a short-lived read-only handle for one ad-hoc query. It uses the same literal SQLite URI helper
// and connection hook as the ordinary reader pool, but is closed instead of pooled after the query finishes.
func (d *DB) AdHocReader() (*sql.DB, error) {
	r, err := sql.Open("lifelog", dsn(d.path, true))
	if err != nil {
		return nil, err
	}
	r.SetMaxOpenConns(1)
	return r, nil
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
	q := url.Values{}
	q.Set("mode", "ro")
	q.Add("_pragma", "trusted_schema(0)")
	r, err := sql.Open("lifelog", sqliteFileURI(from, q))
	if err != nil {
		return err
	}
	defer r.Close()
	_, err = r.Exec(`VACUUM INTO ?`, to)
	return err
}

// Snapshot takes a snapshot of a life.db into dir (docs/cookbook/take-a-snapshot.md, D25): a Copy named by the local
// day of now, life-YYYY-MM-DD.db, or life-YYYY-MM-DDTHHMMSS.db when the day has one already. It never overwrites a
// file and refuses a folder inside a git work tree. It returns the snapshot's path.
func Snapshot(from, dir string, now time.Time) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("no folder %s for the snapshot", dir)
	}
	if repo := gitWorkTree(dir); repo != "" {
		return "", fmt.Errorf("%s is inside the git work tree %s: a snapshot holds health data and private notes, "+
			"which git history cannot forget; choose a folder outside it", dir, repo)
	}
	to := filepath.Join(dir, "life-"+now.Format("2006-01-02")+".db")
	if _, err := os.Stat(to); err == nil {
		to = filepath.Join(dir, "life-"+now.Format("2006-01-02T150405")+".db")
	}
	return to, Copy(from, to)
}

// gitWorkTree is the folder at or above dir that holds a .git entry, or "".
func gitWorkTree(dir string) string {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		up := filepath.Dir(d)
		if up == d {
			return ""
		}
		d = up
	}
}

// OpenSnapshot opens a snapshot for its restore check (docs/cookbook/take-a-snapshot.md): as Open opens life.db —
// the FTS5 check is an INSERT, refused on a read-only connection — but Close runs no PRAGMA optimize, which can
// write to the file, so the checks leave the snapshot as it was.
func OpenSnapshot(path string) (*DB, error) { return open(path, true) }
