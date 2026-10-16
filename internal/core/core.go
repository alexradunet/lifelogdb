// Package core is the one place that writes life.db: every write is a named operation that runs the SQL of
// docs/cookbook/ inside one BEGIN IMMEDIATE transaction. The rules are the docs'; this package cites them.
package core

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"lifelog/internal/db"
	"lifelog/internal/text"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// nowSQL is the instant format of lifelog_meta.instants, written by SQLite so every row of a statement agrees.
const nowSQL = "strftime('%Y-%m-%dT%H:%M:%fZ','now')"

// Store is one open life.db. SnapshotDir is the folder the snapshot action writes to; empty means beside the file.
// Clock and Zone are the one clock of the writer: every surface reads today and now from them (Now, Today). A nil
// Clock is time.Now and a nil Zone is time.Local, the clock of this device; a test gives a fixed clock.
type Store struct {
	DB          *db.DB
	SnapshotDir string
	Clock       func() time.Time
	Zone        *time.Location
}

// Now is the instant of the store's clock, in the store's zone.
func (s *Store) Now() time.Time {
	now, zone := s.Clock, s.Zone
	if now == nil {
		now = time.Now
	}
	if zone == nil {
		zone = time.Local
	}
	return now().In(zone)
}

// Today is the local calendar day of the store's clock (lifelog_meta.days).
func (s *Store) Today() string { return s.Now().Format(time.DateOnly) }

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
		return Invalid("source %q: 1-64 characters of a-z 0-9 _ : . -", s)
	}
	return nil
}

// IsDay reports a YYYY-MM-DD local day that round-trips (date(x) IS x).
func IsDay(s string) bool {
	t, err := time.Parse(time.DateOnly, s)
	return err == nil && t.Format(time.DateOnly) == s
}

// instantLayout is the layout of a lifelog_meta.instants instant: UTC, milliseconds, Z.
const instantLayout = "2006-01-02T15:04:05.000Z"

// IsInstant reports a lifelog_meta.instants instant.
func IsInstant(s string) bool {
	t, err := time.Parse(instantLayout, s)
	return err == nil && t.Format(instantLayout) == s
}

// IsOwner reports a writer that is the owner: not an agent (agent:*) and not an import (import:*). The owner-only
// actions (register-metric, snapshot) are refused to any other writer, on every surface.
func IsOwner(source string) bool {
	return !strings.HasPrefix(source, "agent:") && !strings.HasPrefix(source, "import:")
}

// Forbidden is a 403: the writer may not run this action.
func Forbidden(format string, a ...any) error {
	return &Error{Status: 403, Msg: fmt.Sprintf(format, a...)}
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

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
