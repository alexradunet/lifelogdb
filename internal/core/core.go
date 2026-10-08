// Package core is the one place that writes life.db: every write is a named operation that runs the SQL of
// docs/cookbook/ inside one BEGIN IMMEDIATE transaction. The rules are the docs'; this package cites them.
package core

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"lifelog/internal/db"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Now is the instant format of lifelog_meta.instants, written by SQLite so every row of a statement agrees.
const Now = "strftime('%Y-%m-%dT%H:%M:%fZ','now')"

// Store is one open life.db. SnapshotDir is the folder the snapshot action writes to; empty means beside the file.
type Store struct {
	DB          *db.DB
	SnapshotDir string
}

// Error carries an HTTP-shaped status so every surface reports the same failure the same way. Code, when set,
// names the failure for a client that branches on it (docs/plans/079-client-contract-hygiene.md); the API fills
// a general one from the status otherwise.
type Error struct {
	Status int
	Msg    string
	Code   string
}

func (e *Error) Error() string { return e.Msg }

func notFound(format string, a ...any) error {
	return &Error{Status: 404, Msg: fmt.Sprintf(format, a...)}
}
func invalid(format string, a ...any) error {
	return &Error{Status: 422, Msg: fmt.Sprintf(format, a...)}
}
func conflict(format string, a ...any) error {
	return &Error{Status: 409, Msg: fmt.Sprintf(format, a...)}
}

// conflictCode is a 409 with a code a client branches on (e.g. stale_version).
func conflictCode(code, format string, a ...any) error {
	return &Error{Status: 409, Msg: fmt.Sprintf(format, a...), Code: code}
}

// refused turns a constraint or trigger failure of the DDL into a 422 with SQLite's own message.
func refused(err error) error {
	if err == nil {
		return nil
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
		return invalid("source %q: 1-64 characters of a-z 0-9 _ : . -", s)
	}
	return nil
}

// Today is the local calendar day of this device (lifelog_meta.days).
func Today() string { return time.Now().Format(time.DateOnly) }

// IsDay reports a YYYY-MM-DD local day that round-trips (date(x) IS x).
func IsDay(s string) bool {
	t, err := time.Parse(time.DateOnly, s)
	return err == nil && t.Format(time.DateOnly) == s
}

// IsInstant reports a lifelog_meta.instants instant: UTC, milliseconds, Z.
func IsInstant(s string) bool {
	t, err := time.Parse("2006-01-02T15:04:05.000Z", s)
	return err == nil && t.Format("2006-01-02T15:04:05.000Z") == s
}
