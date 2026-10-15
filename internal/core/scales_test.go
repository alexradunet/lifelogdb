package core

import (
	"strings"
	"testing"
)

// A scale names its range as its unit, and the writer holds a '1-5' scale to whole numbers from 1 to 5 whichever
// metric it is (D6, issue 0015).
func TestScalesAreRangeChecked(t *testing.T) {
	s := fresh(t)
	for _, m := range []struct{ name, unit string }{{"Energy", "1-5"}, {"Pain", "0-10"}} {
		if _, err := s.RegisterMetric(ctx, "cli", m.name, m.unit, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []float64{0, 6, 2.5} {
		if _, err := s.Record(ctx, "cli", Reading{Metric: "Energy", Day: "2026-09-01", Value: v}); status(err) != 422 {
			t.Errorf("Energy 1-5 reading %g: %v, want 422", v, err)
		}
	}
	if _, err := s.Record(ctx, "cli", Reading{Metric: "Energy", Day: "2026-09-01", Value: 4}); err != nil {
		t.Errorf("Energy 1-5 reading 4: %v", err)
	}
}

func TestRename(t *testing.T) {
	s := fresh(t)
	old, _, _ := s.CreatePage(ctx, "cli", "Sourdogh", "Feed the starter. [[Baking]]", "")
	day, _, _ := s.Capture(ctx, "cli", "2026-09-29", "baked, see [[Sourdogh]]", nil)
	baking, _ := s.PageID(ctx, "Baking")
	s.Link(ctx, "cli", old, baking, "part-of", "")
	to, err := s.Rename(ctx, "cli", old, "Sourdough")
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.PageByID(ctx, to)
	if p.Body != "Feed the starter. [[Baking]]" || titles(p.Out, "wikilink") != "Baking" || titles(p.Out, "part-of") != "Baking" {
		t.Errorf("the new page: %+v", p)
	}
	if to != old {
		t.Fatalf("rename changed id: %d to %d", old, to)
	}
	if got, err := s.PageID(ctx, "Sourdogh"); err != nil || got != old {
		t.Fatalf("old alias = %d %v", got, err)
	}
	// Backlinks retain their endpoint; no redirect traversal is needed.
	if !strings.Contains(titles(p.In, "wikilink"), "2026-09-29") {
		t.Errorf("backlinks of the new page: %+v", p.In)
	}
	for _, c := range []struct {
		id    int64
		title string
		want  int
	}{
		{day, "Day one", 409},    // a day page keeps its title
		{to, "Baking", 409},      // into an existing page with text of its own
		{to, "Health/Diet", 422}, // not a valid title
	} {
		if _, err := s.Rename(ctx, "cli", c.id, c.title); status(err) != c.want && !(c.want == 409 && isExists(err)) {
			t.Errorf("rename %d to %q: %v, want %d", c.id, c.title, err, c.want)
		}
	}
	// a typo ghost, empty, into an existing person
	s.Capture(ctx, "cli", "2026-09-30", "met [[Sm]]", nil)
	ghost, _ := s.PageID(ctx, "Sm")
	sam, _ := s.CreatePerson(ctx, "cli", "Sam", "Sam Example", "", "")
	if to, err := s.Rename(ctx, "cli", ghost, "Sam"); !isExists(err) || to != 0 {
		t.Errorf("ghost conflict must refuse without merging: %d, %v", to, err)
	}
	pe, err := s.PageByID(ctx, sam)
	if err != nil || len(pe.In) != 0 {
		t.Fatalf("conflict changed person: %+v %v", pe, err)
	}
	g, err := s.PageByID(ctx, ghost)
	if err != nil || !strings.Contains(titles(g.In, "wikilink"), "2026-09-30") {
		t.Fatalf("conflict lost ghost backlink: %+v %v", g, err)
	}
}

func isExists(err error) bool {
	_, ok := err.(*ExistsError)
	return ok || status(err) == 409
}

func TestCaptureRefusesATombstonedMood(t *testing.T) {
	s := fresh(t)
	one := float64(1)
	mood, err := s.PageID(ctx, "Mood")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", mood); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Capture(ctx, "cli", "2026-10-01", "refused", &one); status(err) != 404 {
		t.Errorf("tombstoned mood: %v", err)
	}
	var n int
	if err := s.DB.R.QueryRow(`SELECT count(*) FROM entity_names WHERE name_key = '2026-10-01'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("missing mood capture persisted")
	}
	if _, _, err := s.Capture(ctx, "cli", "2026-10-01", "text only", nil); err != nil {
		t.Fatal(err)
	}
}
