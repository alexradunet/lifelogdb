package core

import (
	"strings"
	"testing"
)

func TestHabits(t *testing.T) {
	s := fresh(t)
	if _, err := s.RegisterMetric(ctx, "cli", "evening_walk", "", "1 = done that day"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterMetric(ctx, "cli", "weight", "kg", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterMetric(ctx, "cli", "weight", "lb", ""); status(err) != 409 {
		t.Errorf("a metric re-registered with another unit: %v", err)
	}
	if err := s.StartHabit(ctx, "cli", "weight", "2026-09-01", ""); status(err) != 422 {
		t.Errorf("a habit on a metric with a unit: %v", err)
	}
	if err := s.StartHabit(ctx, "cli", "evening_walk", "2026-09-01", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.StartHabit(ctx, "cli", "evening_walk", "2026-09-01", ""); err != nil {
		t.Errorf("a re-sent period: %v", err)
	}
	if err := s.StartHabit(ctx, "cli", "evening_walk", "2026-09-10", ""); status(err) != 422 {
		t.Errorf("an overlapping period: %v", err)
	}
	if err := s.StopHabit(ctx, "cli", "evening_walk", "2026-09-05"); err != nil {
		t.Fatal(err)
	}
	if err := s.StopHabit(ctx, "cli", "evening_walk", "2026-09-06"); status(err) != 404 {
		t.Errorf("stopping a habit with no open period: %v", err)
	}
	// a re-send of the closed period carries its end; without it, it would reopen and overlap a later one
	if err := s.StartHabit(ctx, "cli", "evening_walk", "2026-09-01", "2026-09-05"); err != nil {
		t.Fatal(err)
	}
	for _, d := range []struct {
		day string
		v   float64
	}{{"2026-09-01", 1}, {"2026-09-02", 0}, {"2026-09-04", 1}} {
		if _, err := s.Record(ctx, "cli", Reading{Metric: "evening_walk", Day: d.day, Value: d.v}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Record(ctx, "cli", Reading{Metric: "evening_walk", Day: "2026-09-03", Value: 2}); status(err) != 422 {
		t.Errorf("a check-in of 2: %v", err)
	}
	c, err := s.Completion(ctx, "2026-08-30", "2026-09-07")
	if err != nil {
		t.Fatal(err)
	}
	if len(c) != 1 || c[0].ActiveDays != 5 || c[0].Done != 2 || c[0].NotDone != 1 || c[0].NotRecorded != 2 {
		t.Errorf("completion %+v", c)
	}
	h, _ := s.Habits(ctx, "2026-09-03")
	if len(h) != 1 || h[0].State != "not recorded" {
		t.Errorf("habits of a day with no check-in: %+v", h)
	}
	h, _ = s.Habits(ctx, "2026-09-06")
	if len(h) != 0 {
		t.Errorf("a day after the period: %+v", h)
	}
	if _, err := s.RegisterMetric(ctx, "cli", "steps_0_1", "", ""); err != nil {
		t.Fatal(err)
	}
	s.Record(ctx, "cli", Reading{Metric: "steps_0_1", Day: "2026-09-01", Value: 7})
	if err := s.StartHabit(ctx, "cli", "steps_0_1", "2026-09-01", ""); err == nil || !strings.Contains(err.Error(), "other than 0 and 1") {
		t.Errorf("a habit on a metric with other readings: %v", err)
	}
}

func TestRename(t *testing.T) {
	s := fresh(t)
	old, _, _ := s.CreatePage(ctx, "cli", "Sourdogh", "Feed the starter. [[Baking]]")
	day, _, _ := s.Capture(ctx, "cli", "2026-09-29", "baked, see [[Sourdogh]]", nil)
	baking, _ := s.PageID(ctx, "Baking")
	s.Link(ctx, "cli", old, baking, "related", "")
	to, err := s.Rename(ctx, "cli", old, "Sourdough")
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.PageByID(ctx, to)
	if p.Body != "Feed the starter. [[Baking]]" || titles(p.Out, "wikilink") != "Baking" || titles(p.Out, "related") != "Baking" {
		t.Errorf("the new page: %+v", p)
	}
	stub, _ := s.PageByID(ctx, old)
	if stub.Body != "#REDIRECT [[Sourdough]]" || !stub.IsStub || titles(stub.Out, "wikilink") != "" || titles(stub.Out, "related") != "" {
		t.Errorf("the stub: %+v", stub)
	}
	// backlinks follow one redirect hop: the day that names the old title counts for the new page
	if !strings.Contains(titles(p.In, "wikilink"), "2026-09-29") {
		t.Errorf("backlinks of the new page: %+v", p.In)
	}
	for _, c := range []struct {
		id    int64
		title string
		want  int
	}{
		{old, "Sour dough", 409}, // a stub is not renamed again
		{day, "Day one", 409},    // a day page keeps its title
		{to, "SOURDOUGH", 422},   // same key: a title never changes, not even its case
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
	if to, err := s.Rename(ctx, "cli", ghost, "Sam"); err != nil || to != sam {
		t.Errorf("a ghost into a person: %d, %v", to, err)
	}
	pe, _ := s.PageByID(ctx, sam)
	if !strings.Contains(titles(pe.In, "wikilink"), "2026-09-30") {
		t.Errorf("the person's backlinks after the rename: %+v", pe.In)
	}
}

func isExists(err error) bool {
	_, ok := err.(*ExistsError)
	return ok || status(err) == 409
}
