package core

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"lifelog/internal/db"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var ctx = context.Background()

func fresh(t *testing.T) *Store {
	t.Helper()
	p := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return &Store{DB: d}
}

func status(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}

func titles(es []Edge, kind string) string {
	var out []string
	for _, e := range es {
		if e.Kind == kind {
			out = append(out, e.Title)
		}
	}
	return strings.Join(out, ",")
}

func TestCaptureCreatesTheDayAndItsLinks(t *testing.T) {
	s := fresh(t)
	mood := 4.0
	id, r, err := s.Capture(ctx, "cli", "2026-09-29", "Met [[Sam]] #health", &mood)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.Created, ",") != "Sam,health" {
		t.Errorf("created %v", r.Created)
	}
	id2, _, err := s.Capture(ctx, "cli", "2026-09-29", "Later.", nil)
	if err != nil || id2 != id {
		t.Fatalf("second capture: id %d (first %d), %v", id2, id, err)
	}
	p, _ := s.PageByID(ctx, id)
	if p.Body != "Met [[Sam]] #health\n\nLater." || !p.IsDayPage {
		t.Errorf("day page %+v", p)
	}
	if got := titles(p.Out, "wikilink"); got != "Sam,health" {
		t.Errorf("wikilinks after a second capture: %s", got)
	}
	d, _ := s.Day(ctx, "2026-09-29")
	if len(d.Readings) != 1 || d.Readings[0].Metric != "Mood" || d.Readings[0].CapturedWith != id {
		t.Errorf("mood reading %+v", d.Readings)
	}
	if _, _, err := s.Capture(ctx, "cli", "2026-09-29", "", ptr(9)); status(err) != 422 {
		t.Errorf("mood 9: %v", err)
	}
}

func ptr(f float64) *float64 { return &f }

func TestSaveBodyAddsAndDeletesLinks(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Diet", "See [[Plan]] and [[Health/Diet]] and [[Diet]]", "")
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.PageByID(ctx, id)
	if titles(p.Out, "wikilink") != "Plan" {
		t.Fatalf("links %v", p.Out)
	}
	r, err := s.SaveBody(ctx, "cli", id, "nowSQL [[Other]] only", p.Version)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Skipped) != 0 {
		t.Errorf("skipped %v", r.Skipped)
	}
	p2, _ := s.PageByID(ctx, id)
	if titles(p2.Out, "wikilink") != "Other" {
		t.Errorf("after the re-save: %v", p2.Out)
	}
	if _, err := s.SaveBody(ctx, "cli", id, "stale", p.Version); status(err) != 409 {
		t.Errorf("a save with an old version: %v", err)
	}
}

func TestSaveRevivesATombstonedTarget(t *testing.T) {
	s := fresh(t)
	old, _, _ := s.CreatePage(ctx, "cli", "Old", "", "")
	if err := s.Tombstone(ctx, "cli", old); err != nil {
		t.Fatal(err)
	}
	_, r, err := s.CreatePage(ctx, "cli", "New", "[[old]]", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.Revived, ",") != "old" || len(r.Created) != 0 {
		t.Errorf("sync %+v", r)
	}
}

func TestTitlesAreUniqueByKey(t *testing.T) {
	s := fresh(t)
	s.CreatePage(ctx, "cli", "Café notes", "", "")
	_, _, err := s.CreatePage(ctx, "cli", "CAFÉ NOTES", "", "")
	var ex *ExistsError
	if !errors.As(err, &ex) {
		t.Errorf("a second page with the same key: %v", err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "CON", "", ""); status(err) != 422 {
		t.Errorf("a device name: %v", err)
	}
}

func TestPeoplePlacesAndPromotion(t *testing.T) {
	s := fresh(t)
	day, _, _ := s.Capture(ctx, "cli", "2026-07-31", "At [[Lakeside]] with [[Ana]]", nil)
	ana, _ := s.PageID(ctx, "Ana")
	lake, _ := s.PageID(ctx, "lakeside")
	if err := s.Promote(ctx, "cli", ana, "person", "Ana Example"); err != nil {
		t.Fatal(err)
	}
	if err := s.Promote(ctx, "cli", lake, "place", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Promote(ctx, "cli", day, "place", ""); status(err) != 422 {
		t.Errorf("promoting a day page: %v", err)
	}
	if err := s.Link(ctx, "cli", day, lake, "at", "evening"); err != nil {
		t.Fatal(err)
	}
	if err := s.Link(ctx, "cli", ana, lake, "at", ""); status(err) != 422 {
		t.Errorf("an at link from a person: %v", err)
	}
	d, _ := s.Day(ctx, "2026-07-31")
	found := false
	for _, r := range d.Rows {
		found = found || (r.What == "at" && r.Detail == "Lakeside")
	}
	if !found {
		t.Errorf("day view %+v", d.Rows)
	}
	bob, err := s.CreatePerson(ctx, "cli", "Bob", "Bob Sample", "1990-02-30", "", "")
	if status(err) != 422 || bob != 0 {
		t.Errorf("an impossible birth day: %v", err)
	}
	if err := s.Link(ctx, "cli", ana, lake, "friend", ""); status(err) != 422 {
		t.Errorf("friend to a place: %v", err)
	}
	p, _ := s.PageByID(ctx, ana)
	if p.Person == nil || p.Person.Name != "Ana Example" {
		t.Errorf("person %+v", p.Person)
	}
}

func TestMeasurementsAreAppendOnly(t *testing.T) {
	s := fresh(t)
	if _, err := s.Record(ctx, "cli", Reading{Metric: "weight", Day: "2026-09-29", Value: 70}); status(err) != 404 {
		t.Errorf("an unregistered metric: %v", err)
	}
	m := Reading{Metric: "mood", Day: "2026-09-29", Value: 3, ImportKey: "k1"}
	id, err := s.Record(ctx, "agent:x", m)
	if err != nil || id == 0 {
		t.Fatal(id, err)
	}
	if again, err := s.Record(ctx, "agent:x", m); err != nil || again != 0 {
		t.Errorf("a re-send with the same key: %d, %v", again, err)
	}
	fix, err := s.Correct(ctx, "cli", id, ptr(4))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Correct(ctx, "cli", id, ptr(5)); status(err) != 422 {
		t.Errorf("a second correction of one row: %v", err)
	}
	if _, err := s.Correct(ctx, "cli", fix, nil); err != nil {
		t.Fatal(err)
	}
	d, _ := s.Day(ctx, "2026-09-29")
	if len(d.Readings) != 0 {
		t.Errorf("a retracted reading is still current: %+v", d.Readings)
	}
}

// A correction of an imported, keyed reading is the corrector's row: its writer's source and no import key.
func TestACorrectionKeepsItsWritersProvenance(t *testing.T) {
	s := fresh(t)
	root, err := s.Record(ctx, "import:notebook", Reading{Metric: "mood", Day: "2026-10-07", Value: 3, ImportKey: "Medical/Mood.md|reading|mood|2026-10-07|1"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Correct(ctx, "cli", root, ptr(4))
	if err != nil {
		t.Fatal(err)
	}
	newest, err := s.Correct(ctx, "cli", first, ptr(5))
	if err != nil {
		t.Fatal(err)
	}
	retracted, err := s.Correct(ctx, "agent:owner", newest, nil)
	if err != nil {
		t.Fatalf("retraction of newest row: %v", err)
	}
	var source string
	var rowKey sql.NullString
	var value sql.NullFloat64
	if err := s.DB.R.QueryRowContext(ctx, `SELECT source, import_key, value FROM measurements WHERE id = ?`, retracted).Scan(&source, &rowKey, &value); err != nil {
		t.Fatal(err)
	}
	if source != "agent:owner" || rowKey.Valid || value.Valid {
		t.Fatalf("retraction provenance = source %q key %q value %v, want the writer's source, no key and no value", source, rowKey.String, value)
	}
	if _, err := s.Correct(ctx, "cli", 987654321, ptr(4)); status(err) != 404 {
		t.Fatalf("unknown measurement: %v, want 404", err)
	}
}

func TestCorrectionDomains(t *testing.T) {
	s := fresh(t)
	captured, _, err := s.Capture(ctx, "cli", "2026-10-01", "domain test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "focus"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "evening_walk"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "winter_vitamin"}); err != nil {
		t.Fatal(err)
	}

	assertRejectedCorrection := func(name string, r Reading, value float64) {
		t.Helper()
		id, err := s.Record(ctx, "cli", r)
		if err != nil {
			t.Fatalf("%s record: %v", name, err)
		}
		before := measurementRows(t, s)
		if _, err := s.Correct(ctx, "cli", id, ptr(value)); status(err) != 422 {
			t.Fatalf("%s correct to %g: %v, want 422", name, value, err)
		}
		if got := measurementRows(t, s); got != before {
			t.Fatalf("%s wrote %d rows, want %d", name, got, before)
		}
		m, err := s.Measurement(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if !m.Current || m.Value != r.Value {
			t.Fatalf("%s current reading after rejection: current=%v value=%g, want %g", name, m.Current, m.Value, r.Value)
		}
	}

	assertRejectedCorrection("mood above range", Reading{Metric: "mood", Day: "2026-10-01", Value: 3}, 6)
	assertRejectedCorrection("fractional mood", Reading{Metric: "mood", Day: "2026-10-02", Value: 3}, 2.5)

	unknownRows := measurementRows(t, s)
	if _, err := s.Correct(ctx, "cli", 987654321, ptr(1)); status(err) != 404 {
		t.Fatalf("unknown measurement: %v, want 404", err)
	}
	if got := measurementRows(t, s); got != unknownRows {
		t.Fatalf("unknown correction wrote %d rows, want %d", got, unknownRows)
	}

	lo, err := s.Record(ctx, "cli", Reading{Metric: "mood", Day: "2026-10-03", TakenAt: "2026-10-03T07:00:00.000Z", TZ: "Europe/Bucharest", Value: 3, CapturedWith: captured})
	if err != nil {
		t.Fatal(err)
	}
	loFix, err := s.Correct(ctx, "cli", lo, ptr(1))
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.Measurement(ctx, loFix)
	if err != nil {
		t.Fatal(err)
	}
	if m.Metric != "Mood" || m.Day != "2026-10-03" || m.TakenAt != "2026-10-03T07:00:00.000Z" || m.TZ != "Europe/Bucharest" || m.CapturedWith != captured || m.Supersedes != lo || !m.Current || m.Value != 1 {
		t.Errorf("corrected row did not copy identity/provenance: %+v", m)
	}

	hi, err := s.Record(ctx, "cli", Reading{Metric: "mood", Day: "2026-10-04", Value: 4})
	if err != nil {
		t.Fatal(err)
	}
	hiFix, err := s.Correct(ctx, "cli", hi, ptr(5))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Correct(ctx, "cli", hiFix, nil); err != nil {
		t.Fatalf("nil retraction: %v", err)
	}
	d, err := s.Day(ctx, "2026-10-04")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Readings) != 0 {
		t.Fatalf("retracted boundary correction remains current: %+v", d.Readings)
	}

	start, err := s.Record(ctx, "cli", Reading{Metric: "mood", Day: "2026-10-05", Value: 3})
	if err != nil {
		t.Fatal(err)
	}
	middle, err := s.Correct(ctx, "cli", start, ptr(4))
	if err != nil {
		t.Fatal(err)
	}
	end, err := s.Correct(ctx, "cli", middle, ptr(5))
	if err != nil {
		t.Fatalf("correction of a correction: %v", err)
	}
	if m, err := s.Measurement(ctx, end); err != nil || !m.Current || m.Supersedes != middle || m.Value != 5 {
		t.Fatalf("second correction: %+v, %v", m, err)
	}

	focus, err := s.Record(ctx, "cli", Reading{Metric: "focus", Day: "2026-10-06", Value: 7})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Correct(ctx, "cli", focus, ptr(2.5)); err != nil {
		t.Fatalf("finite non-habit unitless correction: %v", err)
	}
}

func measurementRows(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM measurements`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRenamedIdentityWritesThroughRetainedHandle(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Typo", "prose", "")
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := s.Rename(ctx, "cli", id, "Correct")
	if err != nil || renamed != id {
		t.Fatalf("rename: %d %v", renamed, err)
	}
	other, _, err := s.CreatePage(ctx, "cli", "Other", "[[Typo]]", "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBody(ctx, "cli", id, "#REDIRECT [[Other]]", before.Version); err != nil {
		t.Fatal(err)
	}
	after, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.Body != "#REDIRECT [[Other]]" || titles(after.Out, "wikilink") != "Other,REDIRECT" || len(after.Out) != 2 {
		t.Fatalf("ordinary prose: %+v", after)
	}
	if err := s.Do(ctx, "import:synthetic", func(tx *Tx) error { _, err := tx.SetBody(id, "imported [[Other]]"); return err }); err != nil {
		t.Fatal(err)
	}
	if err := s.Link(ctx, "cli", id, other, "part-of", "note"); err != nil {
		t.Fatal(err)
	}
	if err := s.Unlink(ctx, "cli", id, other, "part-of"); err != nil {
		t.Fatal(err)
	}
	if err := s.Link(ctx, "cli", other, id, "part-of", "reverse"); err != nil {
		t.Fatal(err)
	}
	if err := s.Unlink(ctx, "cli", other, id, "part-of"); err != nil {
		t.Fatal(err)
	}
	if err := s.Link(ctx, "cli", other, id, "redirect", ""); status(err) != 422 {
		t.Fatalf("removed kind: %v", err)
	}
	mention, err := s.PageByID(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if titles(mention.Out, "wikilink") != "Correct" || len(mention.Out) != 1 {
		t.Fatalf("old handle endpoint: %+v", mention)
	}
	old, err := s.PageID(ctx, "Typo")
	if err != nil || old != id {
		t.Fatalf("retained handle: %d %v", old, err)
	}
}

// withoutTrigger runs one statement with a trigger dropped and restores it: deliberate damage, or a row a file
// written before the rule may hold. It returns the statement's result.
func withoutTrigger(t *testing.T, s *Store, trigger, statement string, args ...any) sql.Result {
	t.Helper()
	var definition string
	if err := s.DB.W.QueryRow(`SELECT sql FROM sqlite_schema WHERE type='trigger' AND name=?`, trigger).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.W.Exec("DROP TRIGGER " + trigger); err != nil {
		t.Fatal(err)
	}
	res, err := s.DB.W.Exec(statement, args...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.W.Exec(definition); err != nil {
		t.Fatal(err)
	}
	return res
}

// TestRowReadsReportEveryFault: each and list, the one loop of core over rows, return the error of the query, of a
// Scan, of the rows (Err) and of their Close, and they close the rows in every case. The fault driver of
// integrity_test.go gives each fault: its rows hold one integer, and with the fault scan a second row that is text.
func TestRowReadsReportEveryFault(t *testing.T) {
	for _, c := range []struct {
		fault    string
		rows     int  // the rows that each gives to its row function
		closed   int  // the rows the driver closed
		ofDriver bool // the driver's fault is in the error
		ofScan   bool // the Scan error of the second row is in the error
	}{
		{fault: "", rows: 0, closed: 1},
		{fault: "query", rows: 0, closed: 0, ofDriver: true},
		{fault: "scan", rows: 1, closed: 1, ofScan: true},
		{fault: "late", rows: 1, closed: 1, ofDriver: true},
		{fault: "close", rows: 1, closed: 1, ofDriver: true},
		{fault: "scan-close", rows: 1, closed: 1, ofDriver: true, ofScan: true},
	} {
		t.Run("fault="+c.fault, func(t *testing.T) {
			check := func(what string, err error, closed int) {
				t.Helper()
				if (err != nil) != (c.ofDriver || c.ofScan) {
					t.Errorf("%s: error %v, want an error: %v", what, err, c.ofDriver || c.ofScan)
				}
				if errors.Is(err, errIntegrityFault) != c.ofDriver {
					t.Errorf("%s: error %v, want the driver's fault in it: %v", what, err, c.ofDriver)
				}
				if ofScan := err != nil && strings.Contains(err.Error(), "Scan error on column index 0"); ofScan != c.ofScan {
					t.Errorf("%s: error %v, want the Scan error in it: %v", what, err, c.ofScan)
				}
				if closed != c.closed {
					t.Errorf("%s: the driver closed %d rows, want %d", what, closed, c.closed)
				}
			}

			driver := &integrityConnector{check: "orphan entities", fault: c.fault}
			s := faultIntegrityStore(t, driver)
			var v int64
			var seen []int64
			rows, err := s.DB.R.QueryContext(ctx, "SELECT value")
			err = each(rows, err, []any{&v}, func() { seen = append(seen, v) })
			check("each", err, driver.closed)
			if len(seen) != c.rows || c.rows == 1 && seen[0] != 42 {
				t.Errorf("each: the row function saw %v, want %d rows of 42", seen, c.rows)
			}

			driver = &integrityConnector{check: "orphan entities", fault: c.fault}
			s = faultIntegrityStore(t, driver)
			rows, err = s.DB.R.QueryContext(ctx, "SELECT value")
			got, err := list(rows, err, func(v *int64) []any { return []any{v} })
			check("list", err, driver.closed)
			switch {
			case err != nil && got != nil:
				t.Errorf("list: %v with the error %v, want no list", got, err)
			case err == nil && (got == nil || len(got) != 0):
				t.Errorf("list: %#v, want an empty list that is not nil", got)
			}
		})
	}
}

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
	commit := func(t *Tx) error {
		_, err := t.tx.ExecContext(t.ctx, "COMMIT")
		return err
	}
	after := func(fail func(t *Tx) error) func(t *Tx) error {
		return func(t *Tx) error {
			if err := commit(t); err != nil {
				return err
			}
			return fail(t)
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
			return s.Do(ctx, "cli", after(func(t *Tx) error {
				_, err := t.tx.ExecContext(t.ctx, `INSERT INTO entities(entity_type) VALUES ('no such type')`)
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

// TestParseDayReadsOnlyADayThatRoundTrips: ParseDay gives the time of a YYYY-MM-DD day (lifelog_meta.days), and a
// zero time and false for anything else. A string that time.Parse reads but that does not round-trip is not a day:
// "+026-10-05" is read as the year 26. IsDay agrees with ParseDay.
func TestParseDayReadsOnlyADayThatRoundTrips(t *testing.T) {
	for _, tc := range []struct {
		s    string
		ok   bool
		want time.Time
	}{
		{"2026-10-05", true, time.Date(2026, time.October, 5, 0, 0, 0, 0, time.UTC)},
		{"0000-01-01", true, time.Date(0, time.January, 1, 0, 0, 0, 0, time.UTC)},
		{"9999-12-31", true, time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC)},
		{"2028-02-29", true, time.Date(2028, time.February, 29, 0, 0, 0, 0, time.UTC)},
		{"2026-02-29", false, time.Time{}},
		{"2026-10-5", false, time.Time{}},
		{"+026-10-05", false, time.Time{}},
		{"-001-12-26", false, time.Time{}},
		{"-0001-12-26", false, time.Time{}},
		{"2026-10-05T00:00:00Z", false, time.Time{}},
		{" 2026-10-05", false, time.Time{}},
		{"", false, time.Time{}},
	} {
		got, ok := ParseDay(tc.s)
		if !got.Equal(tc.want) || ok != tc.ok || IsDay(tc.s) != tc.ok {
			t.Errorf("ParseDay(%q) = %v, %v (IsDay %v), want %v, %v", tc.s, got, ok, IsDay(tc.s), tc.want, tc.ok)
		}
	}
}

// TestTheClockOfThisDeviceKeepsItsMonotonicReading: with no Clock, the instant of Now is time.Now with its monotonic
// reading, with a Zone and with none. Thus a span between two instants (the life of a feedback receipt) does not change
// when the wall clock is set. A time that keeps the monotonic reading is not equal to its Round(0). With a Clock, Now
// is the instant of that clock, Local is the same instant in the Zone, and Today is the day of Local.
func TestTheClockOfThisDeviceKeepsItsMonotonicReading(t *testing.T) {
	zone := time.FixedZone("+14", 14*60*60)
	for name, s := range map[string]*Store{"no zone": {}, "a zone": {Zone: zone}} {
		if now := s.Now(); now == now.Round(0) {
			t.Errorf("%s: Now gives %v, with no monotonic reading", name, now)
		}
	}
	at := time.Date(2026, 3, 10, 11, 30, 0, 0, time.UTC) // 2026-03-11 01:30 in the zone
	s := &Store{Clock: func() time.Time { return at }, Zone: zone}
	if now, local := s.Now(), s.Local(); now != at || !local.Equal(at) || local.Location() != zone || s.Today() != "2026-03-11" {
		t.Errorf("a fixed clock: Now %v, Local %v, Today %s; want %v, the same instant in +14, and 2026-03-11", now, local, s.Today(), at)
	}
}
