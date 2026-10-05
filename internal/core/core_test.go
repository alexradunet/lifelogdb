package core

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifelog/internal/db"
)

var ctx = context.Background()

func fresh(t *testing.T) *Store {
	t.Helper()
	p := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(p); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return &Store{d}
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
	id, _, err := s.CreatePage(ctx, "cli", "Diet", "See [[Plan]] and [[Health/Diet]] and [[Diet]]")
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.PageByID(ctx, id)
	if titles(p.Out, "wikilink") != "Plan" {
		t.Fatalf("links %v", p.Out)
	}
	r, err := s.SaveBody(ctx, "cli", id, "Now [[Other]] only", p.Version)
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
	old, _, _ := s.CreatePage(ctx, "cli", "Old", "")
	if err := s.Tombstone(ctx, "cli", old); err != nil {
		t.Fatal(err)
	}
	_, r, err := s.CreatePage(ctx, "cli", "New", "[[old]]")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.Revived, ",") != "old" || len(r.Created) != 0 {
		t.Errorf("sync %+v", r)
	}
}

func TestTitlesAreUniqueByKey(t *testing.T) {
	s := fresh(t)
	s.CreatePage(ctx, "cli", "Café notes", "")
	_, _, err := s.CreatePage(ctx, "cli", "CAFÉ NOTES", "")
	var ex *ExistsError
	if !errors.As(err, &ex) {
		t.Errorf("a second page with the same key: %v", err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "CON", ""); status(err) != 422 {
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
	bob, err := s.CreatePerson(ctx, "cli", "Bob", "Bob Sample", "1990-02-30", "")
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
	m := Reading{Metric: "mood", Day: "2026-09-29", Value: 3, Key: "k1"}
	id, err := s.Record(ctx, "agent:x", m)
	if err != nil || id == 0 {
		t.Fatal(id, err)
	}
	if again, err := s.Record(ctx, "agent:x", m); err != nil || again != 0 {
		t.Errorf("a re-send with the same key: %d, %v", again, err)
	}
	fix, _, err := s.Correct(ctx, "cli", id, ptr(4))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Correct(ctx, "cli", id, ptr(5)); status(err) != 422 {
		t.Errorf("a second correction of one row: %v", err)
	}
	if _, _, err := s.Correct(ctx, "cli", fix, nil); err != nil {
		t.Fatal(err)
	}
	d, _ := s.Day(ctx, "2026-09-29")
	if len(d.Readings) != 0 {
		t.Errorf("a retracted reading is still current: %+v", d.Readings)
	}
}

func TestCorrectionRootIdentity(t *testing.T) {
	s := fresh(t)
	root, err := s.Record(ctx, "import:notebook", Reading{Metric: "mood", Day: "2026-10-07", Value: 3, Key: "Medical/Mood.md|reading|mood|2026-10-07|1"})
	if err != nil {
		t.Fatal(err)
	}
	assertRoot := func(name string, id int64) int64 {
		t.Helper()
		fix, key, err := s.Correct(ctx, "cli", id, ptr(4))
		if err != nil {
			t.Fatalf("%s correction: %v", name, err)
		}
		if key.Source != "import:notebook" || key.Key != "Medical/Mood.md|reading|mood|2026-10-07|1" || key.Metric != "Mood" || key.Value == nil || *key.Value != 4 {
			t.Fatalf("%s key = %+v, want imported root", name, key)
		}
		return fix
	}
	first := assertRoot("root", root)

	newest, _, err := s.Correct(ctx, "cli", first, ptr(5))
	if err != nil {
		t.Fatal(err)
	}
	retracted, key, err := s.Correct(ctx, "agent:owner", newest, nil)
	if err != nil {
		t.Fatalf("retraction of newest row: %v", err)
	}
	if key.Source != "import:notebook" || key.Key != "Medical/Mood.md|reading|mood|2026-10-07|1" || key.Metric != "Mood" || key.Value != nil {
		t.Fatalf("retraction key = %+v, want imported root with nil value", key)
	}
	var source string
	var rowKey sql.NullString
	if err := s.DB.R.QueryRowContext(ctx, `SELECT source, import_key FROM measurements WHERE id = ?`, retracted).Scan(&source, &rowKey); err != nil {
		t.Fatal(err)
	}
	if source != "agent:owner" || rowKey.Valid {
		t.Fatalf("corrected row provenance = source %q key %q, want writer source and no import key", source, rowKey.String)
	}

	plain, err := s.Record(ctx, "cli", Reading{Metric: "mood", Day: "2026-10-08", Value: 3})
	if err != nil {
		t.Fatal(err)
	}
	if _, key, err := s.Correct(ctx, "cli", plain, ptr(4)); err != nil {
		t.Fatalf("unkeyed correction: %v", err)
	} else if key.Source != "cli" || key.Key != "" || key.Metric != "Mood" {
		t.Fatalf("unkeyed root key = %+v, want root without import key", key)
	}

	legacyRoot, err := s.Record(ctx, "cli", Reading{Metric: "mood", Day: "2026-10-09", Value: 3})
	if err != nil {
		t.Fatal(err)
	}
	var intermediate int64
	if err := s.DB.W.QueryRowContext(ctx, `INSERT INTO measurements(metric_id, day, value, source, import_key, supersedes_id, created_at)
		SELECT metric_id, day, 4, 'import:legacy', 'legacy-intermediate', id, `+Now+` FROM measurements WHERE id = ? RETURNING id`, legacyRoot).Scan(&intermediate); err != nil {
		t.Fatal(err)
	}
	if _, key, err := s.Correct(ctx, "cli", intermediate, ptr(5)); err != nil {
		t.Fatalf("keyed intermediate correction: %v", err)
	} else if key.Source != "cli" || key.Key != "" || key.Metric != "Mood" {
		t.Fatalf("keyed intermediate resolved to %+v, want oldest unkeyed root", key)
	}

	if _, _, err := s.Correct(ctx, "cli", 987654321, ptr(4)); status(err) != 404 {
		t.Fatalf("unknown measurement: %v, want 404", err)
	}
}

func TestCorrectionDomains(t *testing.T) {
	s := fresh(t)
	captured, _, err := s.Capture(ctx, "cli", "2026-10-01", "domain test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterMetric(ctx, "cli", "focus", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterMetric(ctx, "cli", "evening_walk", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.StartHabit(ctx, "cli", "evening_walk", "2026-10-01", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterMetric(ctx, "cli", "winter_vitamin", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.StartHabit(ctx, "cli", "winter_vitamin", "2026-01-01", "2026-01-31"); err != nil {
		t.Fatal(err)
	}

	assertRejectedCorrection := func(name string, r Reading, value float64) {
		t.Helper()
		id, err := s.Record(ctx, "cli", r)
		if err != nil {
			t.Fatalf("%s record: %v", name, err)
		}
		before := measurementRows(t, s)
		if _, _, err := s.Correct(ctx, "cli", id, ptr(value)); status(err) != 422 {
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
	assertRejectedCorrection("current habit", Reading{Metric: "evening_walk", Day: "2026-10-01", Value: 1}, 2)
	assertRejectedCorrection("ended habit", Reading{Metric: "winter_vitamin", Day: "2026-02-05", Value: 0}, 2)

	unknownRows := measurementRows(t, s)
	if _, _, err := s.Correct(ctx, "cli", 987654321, ptr(1)); status(err) != 404 {
		t.Fatalf("unknown measurement: %v, want 404", err)
	}
	if got := measurementRows(t, s); got != unknownRows {
		t.Fatalf("unknown correction wrote %d rows, want %d", got, unknownRows)
	}

	lo, err := s.Record(ctx, "cli", Reading{Metric: "mood", Day: "2026-10-03", TakenAt: "2026-10-03T07:00:00.000Z", TZ: "Europe/Bucharest", Value: 3, CapturedWith: captured})
	if err != nil {
		t.Fatal(err)
	}
	loFix, _, err := s.Correct(ctx, "cli", lo, ptr(1))
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
	hiFix, _, err := s.Correct(ctx, "cli", hi, ptr(5))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Correct(ctx, "cli", hiFix, nil); err != nil {
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
	middle, _, err := s.Correct(ctx, "cli", start, ptr(4))
	if err != nil {
		t.Fatal(err)
	}
	end, _, err := s.Correct(ctx, "cli", middle, ptr(5))
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
	if _, _, err := s.Correct(ctx, "cli", focus, ptr(2.5)); err != nil {
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

func TestQueryStatementBoundary(t *testing.T) {
	s := fresh(t)
	allowed := []string{
		"SELECT ';' AS semi -- ; in a comment\n",
		"SELECT \"semi;colon\" FROM (SELECT 1 AS \"semi;colon\") /* ; inside a block comment */",
		"WITH x(v) AS (VALUES ('literal ;')) SELECT v FROM x; -- final terminator and comment",
		"WITH backlinks(id, title) AS (SELECT p.id, p.title FROM pages p WHERE p.title = 'Nowhere') SELECT count(*) FROM backlinks",
		"SELECT name FROM pragma_table_info('pages') WHERE name = 'title'",
		"VALUES (1)",
		"EXPLAIN QUERY PLAN SELECT * FROM pages",
		"PRAGMA trusted_schema",
		"PRAGMA table_info(pages)",
	}
	for _, q := range allowed {
		if _, err := s.Query(ctx, q, 10); err != nil {
			t.Errorf("allowed query %q: %v", q, err)
		}
	}

	rejected := []string{
		"",
		" -- only a comment\n /* and another */ ",
		"SELECT 1; SELECT 2",
		"DELETE FROM link_kinds WHERE kind = 'related'",
		"BEGIN; SELECT count(*) FROM pages",
		"COMMIT",
		"ATTACH DATABASE ':memory:' AS aux",
		"VACUUM",
		"PRAGMA trusted_schema = ON",
		"PRAGMA query_only(0)",
		"PRAGMA writable_schema",
		"EXPLAIN DELETE FROM link_kinds WHERE kind = 'related'",
	}
	for _, q := range rejected {
		if _, err := s.Query(ctx, q, 10); status(err) != 422 {
			t.Errorf("query %q status = %d (%v), want 422", q, status(err), err)
		}
	}

	r, err := s.Query(ctx, "SELECT kind FROM link_kinds ORDER BY kind", 3)
	if err != nil || len(r.Rows) != 3 || !r.Truncated {
		t.Errorf("rows %v truncated %v err %v", r, r != nil && r.Truncated, err)
	}
}

func TestQueryDoesNotLeakReaderState(t *testing.T) {
	s := fresh(t)
	s.DB.R.SetMaxOpenConns(1) // force ordinary reads to reuse one pooled reader if Query touched it
	assertReaderPragmas(t, s)

	queries := []struct {
		sql       string
		wantErr   bool
		truncated bool
	}{
		{sql: "SELECT kind FROM link_kinds ORDER BY kind LIMIT 1"},
		{sql: "SELECT kind FROM link_kinds ORDER BY kind LIMIT 1; -- final terminator"},
		{sql: "SELECT kind FROM link_kinds ORDER BY kind", truncated: true},
		{sql: "SELECT FROM pages", wantErr: true},
		{sql: "PRAGMA trusted_schema = ON", wantErr: true},
		{sql: "BEGIN; SELECT count(*) FROM pages", wantErr: true},
	}
	for _, q := range queries {
		r, err := s.Query(ctx, q.sql, 1) // includes success, truncation, SQLite syntax error and prevalidation refusal
		if q.wantErr {
			if status(err) != 422 {
				t.Fatalf("%q status = %d (%v), want 422", q.sql, status(err), err)
			}
		} else if err != nil {
			t.Fatalf("%q: %v", q.sql, err)
		} else if r.Truncated != q.truncated {
			t.Fatalf("%q truncated = %v, want %v", q.sql, r.Truncated, q.truncated)
		}
		assertReaderPragmas(t, s)
	}

	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, err := s.Query(short, `WITH RECURSIVE cnt(x) AS (VALUES(0) UNION ALL SELECT x+1 FROM cnt WHERE x < 1000000000) SELECT sum(x) FROM cnt`, 1)
	if err == nil || (status(err) != 422 && !errors.Is(err, context.DeadlineExceeded)) {
		t.Fatalf("deadline-bound query error = %v (status %d), want cancellation", err, status(err))
	}
	if !errors.Is(short.Err(), context.DeadlineExceeded) {
		t.Fatalf("expensive query failed without reaching its deadline: %v", err)
	}
	assertReaderPragmas(t, s)

	if _, _, err := s.CreatePage(ctx, "cli", "Fresh after query", ""); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM pages WHERE title = 'Fresh after query'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("ordinary reader after ad-hoc query sees %d fresh pages, %v; want 1", n, err)
	}
}

func assertReaderPragmas(t *testing.T, s *Store) {
	t.Helper()
	for p, want := range map[string]int64{"PRAGMA trusted_schema": 0, "PRAGMA query_only": 1} {
		var got int64
		if err := s.DB.R.QueryRowContext(ctx, p).Scan(&got); err != nil || got != want {
			t.Fatalf("ordinary reader %s = %d, %v; want %d", p, got, err, want)
		}
	}
}

func TestQueryKeepsOpenedRelativeDatabaseAfterChdir(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "original")
	alternate := filepath.Join(root, "alternate")
	for _, dir := range []string{original, alternate} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := db.Init(filepath.Join(dir, "life.db")); err != nil {
			t.Fatal(err)
		}
	}
	writeMetaValue(t, filepath.Join(original, "life.db"), "cwd-marker", "original")
	writeMetaValue(t, filepath.Join(alternate, "life.db"), "cwd-marker", "alternate")

	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldwd)
	if err := os.Chdir(original); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open("life.db")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := os.Chdir(alternate); err != nil {
		t.Fatal(err)
	}

	s := &Store{d}
	r, err := s.Query(ctx, `SELECT value FROM lifelog_meta WHERE key = 'cwd-marker'`, 1)
	if err != nil || len(r.Rows) != 1 || r.Rows[0][0] != "original" {
		t.Fatalf("ad-hoc query after chdir = %+v, %v; want original database", r, err)
	}
}

func writeMetaValue(t *testing.T, p, key, value string) {
	t.Helper()
	d, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.W.Exec(`INSERT INTO lifelog_meta(key, value) VALUES (?, ?)`, key, value); err != nil {
		t.Fatal(err)
	}
}

func TestQueryUsesLiteralReadOnlyPath(t *testing.T) {
	root := t.TempDir()
	requested := filepath.Join(root, "query literal # %23.db")
	sentinel := filepath.Join(root, "query literal ")
	if err := os.WriteFile(sentinel, []byte("sentinel"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := db.Init(requested); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(requested)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := &Store{d}
	r, err := s.Query(ctx, `SELECT file FROM pragma_database_list WHERE name = 'main'`, 1)
	if err != nil || len(r.Rows) != 1 {
		t.Fatalf("database_list query = %+v, %v", r, err)
	}
	got, ok := r.Rows[0][0].(string)
	if !ok {
		t.Fatalf("database_list file has type %T, want string", r.Rows[0][0])
	}
	wantInfo, err := os.Stat(requested)
	if err != nil {
		t.Fatal(err)
	}
	gotInfo, err := os.Stat(got)
	if err != nil {
		t.Fatalf("reported query path %q cannot be statted: %v", got, err)
	}
	if !os.SameFile(wantInfo, gotInfo) {
		t.Fatalf("query opened %q, not requested literal path %q", got, requested)
	}
	if b, err := os.ReadFile(sentinel); err != nil || string(b) != "sentinel" {
		t.Fatalf("alternate sentinel changed to %q, %v", string(b), err)
	}
}

func TestRedirectStubWrites(t *testing.T) {
	s := fresh(t)
	old, _, err := s.CreatePage(ctx, "cli", "Typo", "prose")
	if err != nil {
		t.Fatal(err)
	}
	target, err := s.Rename(ctx, "cli", old, "Correct")
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := s.CreatePage(ctx, "cli", "Other", "[[Typo]]")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.PageByID(ctx, old)
	dest, _ := s.PageByID(ctx, target)
	cases := []struct {
		name string
		run  func() error
	}{
		{"save", func() error { _, e := s.SaveBody(ctx, "cli", old, "bad", before.Version); return e }},
		{"import", func() error {
			return s.Do(ctx, "import:synthetic", func(tx *Tx) error { _, e := tx.SetBody(old, "bad"); return e })
		}},
		{"source", func() error { return s.Link(ctx, "cli", old, other, "related", "") }},
		{"target", func() error { return s.Link(ctx, "cli", other, old, "related", "") }},
		{"unlink source", func() error { return s.Unlink(ctx, "cli", old, other, "related") }},
		{"unlink target", func() error { return s.Unlink(ctx, "cli", other, old, "related") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if e := tc.run(); status(e) != 409 || !strings.Contains(e.Error(), "target") {
				t.Errorf("refusal: %v", e)
			}
		})
	}
	if e := s.Unlink(ctx, "cli", old, target, "redirect"); e == nil {
		t.Error("removed redirect")
	}
	after, _ := s.PageByID(ctx, old)
	destination, _ := s.PageByID(ctx, target)
	if after.Body != before.Body || titles(after.Out, "redirect") != "Correct" || len(after.Out) != 1 || destination.Body != dest.Body || destination.Version != dest.Version {
		t.Fatal("mutation changed stub or target")
	}
	mention, _ := s.PageByID(ctx, other)
	if titles(mention.Out, "wikilink") != "Typo" {
		t.Fatal("old wikilink changed")
	}
}
