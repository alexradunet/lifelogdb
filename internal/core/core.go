// Package core is the one place that writes life.db: every write is a named operation that runs the SQL of
// docs/cookbook/ inside one BEGIN IMMEDIATE transaction. The rules are the docs'; this package cites them.
package core

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"lifelog/internal/db"
	"lifelog/internal/text"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// nowSQL is the instant format of lifelog_meta.instants, written by SQLite so every row of a statement agrees.
const nowSQL = "strftime('%Y-%m-%dT%H:%M:%fZ','now')"

// nullIfEmpty is the SQL value of an optional text: NULL when the text is empty.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Store is one open life.db. SnapshotDir is the folder the snapshot action writes to; empty means beside the file.
// Clock and Zone are the clock of the writer. Every surface reads from them today (Today), the zone of the words for a
// time and the name of a snapshot (Local), and the life of a feedback receipt (Now). A nil Clock is time.Now and a nil
// Zone is time.Local, the clock of this device; a test gives a fixed clock. The instants that a write stores in a row
// (created_at, updated_at, deleted_at and the others of lifelog_meta.instants) do not come from this clock: SQLite
// writes them from its own clock (nowSQL and the triggers of the schema).
type Store struct {
	DB          *db.DB
	SnapshotDir string
	Clock       func() time.Time
	Zone        *time.Location
}

// Now is the instant of the store's clock. The clock of this device (a nil Clock) gives time.Now with its monotonic
// reading, so a span between two instants (the life of a feedback receipt) does not change when the wall clock is set.
func (s *Store) Now() time.Time {
	if s.Clock == nil {
		return time.Now()
	}
	return s.Clock()
}

// Local is the instant of the store's clock in the store's zone: its day is today, and the words for a time and the
// name of a snapshot are in its zone. Time.In removes the monotonic reading, so a span is measured with Now, not Local.
func (s *Store) Local() time.Time {
	zone := s.Zone
	if zone == nil {
		zone = time.Local
	}
	return s.Now().In(zone)
}

// Today is the local calendar day of the store's clock (lifelog_meta.days).
func (s *Store) Today() string { return s.Local().Format(time.DateOnly) }

// Error carries an HTTP-shaped status so every surface reports the same failure the same way. Code, when set,
// names the failure for a client that branches on it (docs/plans/079-client-contract-hygiene.md); the API fills
// a general one from the status otherwise.
type Error struct {
	Status int
	Msg    string
	Code   string
	Err    error // the cause, when one is kept for errors.Is: a context that ran out, a driver's refusal
}

func (e *Error) Error() string { return e.Msg }
func (e *Error) Unwrap() error { return e.Err }

// invalidCause is a 422 that keeps its cause: SQLite's refusal of a query, with the message as the client sees it.
func invalidCause(err error, format string, a ...any) error {
	return &Error{Status: 422, Msg: fmt.Sprintf(format, a...), Err: err}
}

// NotFound is a 404: the row the request names does not exist.
func NotFound(format string, a ...any) error {
	return &Error{Status: 404, Msg: fmt.Sprintf(format, a...)}
}

// Invalid is a 422: the request is well-formed, and a rule refuses it.
func Invalid(format string, a ...any) error {
	return &Error{Status: 422, Msg: fmt.Sprintf(format, a...)}
}

// Conflict is a 409 with no code: the row is not in the state the request needs.
func Conflict(format string, a ...any) error {
	return &Error{Status: 409, Msg: fmt.Sprintf(format, a...)}
}

// conflictCode is a 409 with a code a client branches on (e.g. stale_version).
func conflictCode(code, format string, a ...any) error {
	return &Error{Status: 409, Msg: fmt.Sprintf(format, a...), Code: code}
}

// refused turns a constraint or trigger failure of the DDL into a 422 with SQLite's own message. A write whose
// rollback failed (db.RollbackError) is a fault, whatever its fn returned: the transaction can be open or kept in part.
// Its text keeps the error of fn, and its chain only the error of the rollback, so no surface reads it as a refusal.
func refused(err error) error {
	if err == nil {
		return nil
	}
	var rb *db.RollbackError
	if errors.As(err, &rb) {
		return fmt.Errorf("%v\nrollback: %w", rb.Err, rb.Rollback)
	}
	var e *Error
	if errors.As(err, &e) {
		return err
	}
	var se *sqlite.Error
	if errors.As(err, &se) && se.Code()&0xff == sqlite3.SQLITE_CONSTRAINT { // CHECKs, FKs, UNIQUE and RAISE(ABORT)
		return &Error{Status: 422, Msg: se.Error()}
	}
	return err
}

var sourceRE = regexp.MustCompile(`^[a-z0-9_:.-]{1,64}$`)

// CheckSource validates a writer name (lifelog_meta.source).
func CheckSource(s string) error {
	if !sourceRE.MatchString(s) {
		return Invalid("source %q: 1-64 characters of a-z 0-9 _ : . -", s)
	}
	return nil
}

// ParseDay parses a YYYY-MM-DD local day that round-trips (date(x) IS x). For anything else ok is false and the
// time is zero.
func ParseDay(s string) (time.Time, bool) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil || t.Format(time.DateOnly) != s {
		return time.Time{}, false
	}
	return t, true
}

// IsDay reports a YYYY-MM-DD local day that round-trips (date(x) IS x).
func IsDay(s string) bool {
	_, ok := ParseDay(s)
	return ok
}

// instantLayout is the layout of a lifelog_meta.instants instant: UTC, milliseconds, Z.
const instantLayout = "2006-01-02T15:04:05.000Z"

// IsInstant reports a lifelog_meta.instants instant.
func IsInstant(s string) bool {
	t, err := time.Parse(instantLayout, s)
	return err == nil && t.Format(instantLayout) == s
}

// The checks of a surface's input that every write and read shares, so one refusal has one message.

// checkDay refuses a value that is not a local day (lifelog_meta.days); what names the field.
func checkDay(what, s string) error {
	if !IsDay(s) {
		return Invalid("%s %q is not a day (YYYY-MM-DD)", what, s)
	}
	return nil
}

// checkWindow refuses a window that is not two local days in order, both inclusive.
func checkWindow(from, through string) error {
	if !IsDay(from) || !IsDay(through) || from > through {
		return Invalid("from %q and through %q must be days (YYYY-MM-DD), from first", from, through)
	}
	return nil
}

// checkTitle refuses a title the contract does not admit (docs/contract/titles-and-wikilinks.md); what names the field.
func checkTitle(what, title string) error {
	if !text.ValidTitle(title) {
		return Invalid("%s %q is not a valid title (docs/contract/titles-and-wikilinks.md)", what, title)
	}
	return nil
}

// The one loop of core over the rows of a query. rows and err are what QueryContext returned, so a caller passes its
// call straight in. Every Scan, Err and Close branch is here.

// each scans every row into dst and then runs row. A Scan error ends the read. The rows are closed in every case, and
// the error of the query, of a Scan, of the rows (Err) or of their Close is returned.
func each(rows *sql.Rows, err error, dst []any, row func()) (e error) {
	if err != nil {
		return err
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			e = errors.Join(e, cerr)
		}
	}()
	for rows.Next() {
		if err := rows.Scan(dst...); err != nil {
			return err
		}
		row()
	}
	return rows.Err()
}

// list reads every row into a slice: fields gives the destinations of one row's columns in v. The slice is never nil
// when the read succeeds, so an empty list is [] in JSON.
func list[T any](rows *sql.Rows, err error, fields func(v *T) []any) ([]T, error) {
	var v T
	out := []T{}
	if err := each(rows, err, fields(&v), func() { out = append(out, v) }); err != nil {
		return nil, err
	}
	return out, nil
}

// groupCounts reads the rows of a GROUP BY count, a key and its count, into a map. A key with no row is absent.
func groupCounts(rows *sql.Rows, err error) (map[string]int, error) {
	out := map[string]int{}
	var k string
	var n int
	if err := each(rows, err, []any{&k, &n}, func() { out[k] = n }); err != nil {
		return nil, err
	}
	return out, nil
}
