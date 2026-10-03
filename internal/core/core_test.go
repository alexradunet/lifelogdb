package core

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

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

func TestQueryCannotWrite(t *testing.T) {
	s := fresh(t)
	if _, err := s.Query(ctx, "DELETE FROM link_kinds WHERE kind = 'related'", 10); status(err) != 422 {
		t.Errorf("a write through Query: %v", err)
	}
	r, err := s.Query(ctx, "SELECT kind FROM link_kinds ORDER BY kind", 3)
	if err != nil || len(r.Rows) != 3 || !r.Truncated {
		t.Errorf("rows %v truncated %v err %v", r, r != nil && r.Truncated, err)
	}
}

// A PRAGMA sent through Query never reaches the next reader: the connection gets its pragmas back first.
func TestQueryRestoresTheReaderPragmas(t *testing.T) {
	s := fresh(t)
	s.DB.R.SetMaxOpenConns(1) // the next query reuses the same connection
	for _, p := range []string{"PRAGMA trusted_schema = ON", "PRAGMA query_only = OFF"} {
		if _, err := s.Query(ctx, p, 10); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}
	for p, want := range map[string]int64{"PRAGMA trusted_schema": 0, "PRAGMA query_only": 1} {
		r, err := s.Query(ctx, p, 10)
		if err != nil || len(r.Rows) != 1 || r.Rows[0][0] != want {
			t.Errorf("%s after a reset sent through Query: %v %v, want %d", p, r, err, want)
		}
	}
}
