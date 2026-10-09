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

// A scale names its range as its unit, so the one rule "a habit is a unitless metric" keeps Mood and every
// registered scale out of habits, and the writer holds a '1-5' scale to whole numbers from 1 to 5 whichever
// metric it is (D6, D24, issue 0015).
func TestScalesAreNotHabitsAndAreRangeChecked(t *testing.T) {
	s := fresh(t)
	for _, m := range []struct{ name, unit string }{{"Energy", "1-5"}, {"Pain", "0-10"}} {
		if _, err := s.RegisterMetric(ctx, "cli", m.name, m.unit, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"Mood", "Energy", "Pain"} {
		if err := s.StartHabit(ctx, "cli", name, "2026-09-01", ""); status(err) != 422 {
			t.Errorf("a habit on the %s scale: %v, want 422", name, err)
		}
		var periods int
		if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM habit_periods`).Scan(&periods); err != nil || periods != 0 {
			t.Errorf("the refused %s habit left %d periods behind (%v)", name, periods, err)
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
	if err := s.StartHabit(ctx, "cli", "steps_free", "2026-09-01", ""); status(err) != 404 {
		t.Fatalf("an unregistered metric: %v", err)
	}
	if _, err := s.RegisterMetric(ctx, "cli", "steps_free", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.StartHabit(ctx, "cli", "steps_free", "2026-09-01", ""); err != nil {
		t.Errorf("a unitless metric still starts a habit: %v", err)
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

func TestCaptureHabitDomain(t *testing.T) {
	s := fresh(t)
	// Deliberate damage: ordinary registration refuses Mood, a metric with a unit (a scale names its range).
	// Preserve the capture rollback witness for an externally damaged classification.
	if _, err := s.DB.W.Exec(`DROP TRIGGER habit_periods_check_insert;
 INSERT INTO habit_periods(metric_id,start_day,source) VALUES(1,'2026-09-01','cli')`); err != nil {
		t.Fatal(err)
	}
	day, _, err := s.Capture(ctx, "cli", "2026-09-29", "original [[Kept]]", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", day); err != nil {
		t.Fatal(err)
	}
	four := float64(4)
	if _, _, err := s.Capture(ctx, "cli", "2026-09-29", "new [[Rollback target]]", &four); status(err) != 422 {
		t.Errorf("habit mood 4: %v", err)
	}
	p, err := s.PageByID(ctx, day)
	if err != nil {
		t.Fatal(err)
	}
	if p.Body != "original [[Kept]]" || titles(p.Out, "wikilink") != "Kept" {
		t.Errorf("capture not rolled back: %+v", p)
	}
	var deleted bool
	if err := s.DB.R.QueryRow(`SELECT deleted_at IS NOT NULL FROM entities WHERE id = ?`, day).Scan(&deleted); err != nil || !deleted {
		t.Errorf("day revival persisted: %v %v", deleted, err)
	}
	var n int
	if err := s.DB.R.QueryRow(`SELECT count(*) FROM entity_names WHERE name_key = 'rollback target'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("target creation persisted")
	}
	if err := s.DB.R.QueryRow(`SELECT count(*) FROM measurements`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("reading persisted")
	}
	if _, _, err := s.Capture(ctx, "cli", "2026-09-30", "new day", &four); status(err) != 422 {
		t.Errorf("new day habit mood: %v", err)
	}
	if err := s.DB.R.QueryRow(`SELECT count(*) FROM entity_names WHERE name_key = '2026-09-30'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("new day persisted")
	}
	one := float64(1)
	if _, _, err := s.Capture(ctx, "cli", "2026-09-29", "accepted", &one); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.R.QueryRow(`SELECT count(*) FROM measurements WHERE value = 1 AND source = 'cli' AND captured_with_id = ?`, day).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Error("capture provenance lost")
	}
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

func TestTombstonedHabits(t *testing.T) {
	s := fresh(t)
	if _, err := s.RegisterMetric(ctx, "cli", "Walk", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.StartHabit(ctx, "cli", "Walk", "2026-09-01", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Record(ctx, "cli", Reading{Metric: "Walk", Day: "2026-09-01", Value: 1}); err != nil {
		t.Fatal(err)
	}
	id, _ := s.PageID(ctx, "Walk")
	for i, hidden := range []bool{false, true, false} {
		var err error
		if hidden {
			err = s.Tombstone(ctx, "cli", id)
		} else if i > 0 {
			err = s.Revive(ctx, "cli", id)
		}
		if err != nil {
			t.Fatal(err)
		}
		h, err := s.Habits(ctx, "2026-09-01")
		if err != nil {
			t.Fatal(err)
		}
		c, err := s.Completion(ctx, "2026-09-01", "2026-09-02")
		if err != nil {
			t.Fatal(err)
		}
		if hidden {
			if len(h) != 0 || len(c) != 0 {
				t.Errorf("hidden habit displayed: %v %v", h, c)
			}
		} else if len(h) != 1 || h[0].State != "done" || len(c) != 1 || c[0].Done != 1 || c[0].NotRecorded != 1 {
			t.Errorf("live habit: %v %v", h, c)
		}
		d, err := s.Day(ctx, "2026-09-01")
		if err != nil {
			t.Fatal(err)
		}
		listed := false
		for _, row := range d.Rows {
			listed = listed || row.What == "habit" && row.Detail == "Walk: done"
		}
		if listed == hidden {
			t.Errorf("day habit visibility hidden=%v: %+v", hidden, d.Rows)
		}
		used, err := s.InUse(ctx, "2026-09-01", 60)
		if err != nil {
			t.Fatal(err)
		}
		if (len(used) == 1 && used[0] == "Walk") == hidden {
			t.Errorf("in-use visibility hidden=%v: %v", hidden, used)
		}
		series, err := s.Series(ctx, "Walk", "2026-08-31", "2026-09-01")
		if err != nil || len(series) != 1 || len(d.Readings) != 1 {
			t.Errorf("history hidden=%v: %v %v %v", hidden, series, d.Readings, err)
		}
		var n int
		if err := s.DB.R.QueryRow(`SELECT count(*) FROM habit_periods WHERE metric_id = ?`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Error("period lost")
		}
		if err := s.DB.R.QueryRow(`SELECT count(*) FROM measurements WHERE metric_id = ? AND value = 1`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Error("reading lost")
		}
	}
}

// A current unassociated check-in outside 0/1 breaks the writer's habit contract; only another writer's file
// can hold one. The readers label such a day invalid, even beside a valid check-in, so the four completion
// categories still sum to the active days (cookbook/habits.md, cookbook/day-view.md).
func TestHabitReadersReportInvalidCheckIns(t *testing.T) {
	s := fresh(t)
	if _, err := s.RegisterMetric(ctx, "cli", "Walk", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.StartHabit(ctx, "cli", "Walk", "2026-10-01", "2026-10-06"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Workout", ""); err != nil {
		t.Fatal(err)
	}
	var session int64
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		var err error
		session, _, err = tx.CaptureSession(SessionInput{Kind: "Workout", Day: "2026-10-05", StartLocal: "2026-10-05T08:00:00.000"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	metric, err := s.PageID(ctx, "Walk")
	if err != nil {
		t.Fatal(err)
	}
	// deliberately nonconforming rows, as another writer could leave them; the readers are under test
	external := func(day string, value float64, sessionID any) int64 {
		t.Helper()
		res, err := s.DB.W.Exec(`INSERT INTO measurements(metric_id, session_id, day, value, created_at, source)
		                         VALUES (?, ?, ?, ?, '2026-10-07T00:00:00.000Z', 'other-writer')`, metric, sessionID, day, value)
		if err != nil {
			t.Fatal(err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	record := func(day string, value float64) {
		t.Helper()
		if _, err := s.Record(ctx, "cli", Reading{Metric: "Walk", Day: day, Value: value}); err != nil {
			t.Fatal(err)
		}
	}
	external("2026-10-01", 2, nil) // invalid only
	record("2026-10-02", 1)        // a valid check-in does not hide an invalid one
	external("2026-10-02", -1, nil)
	one := 1.0 // a corrected invalid reading is no longer current
	if _, err := s.Correct(ctx, "cli", external("2026-10-03", 2, nil), &one); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Correct(ctx, "cli", external("2026-10-04", 2, nil), nil); err != nil { // nor is a retracted one
		t.Fatal(err)
	}
	external("2026-10-05", 2, session) // session-scoped facts are not daily check-ins
	record("2026-10-06", 0)

	want := map[string]string{"2026-10-01": "invalid", "2026-10-02": "invalid", "2026-10-03": "done",
		"2026-10-04": "not recorded", "2026-10-05": "not recorded", "2026-10-06": "not done"}
	for day, state := range want {
		h, err := s.Habits(ctx, day)
		if err != nil {
			t.Fatal(err)
		}
		if len(h) != 1 || h[0].State != state {
			t.Errorf("habits of %s = %+v, want Walk %s", day, h, state)
		}
		d, err := s.Day(ctx, day)
		if err != nil {
			t.Fatal(err)
		}
		var listed []string
		for _, row := range d.Rows {
			if row.What == "habit" {
				listed = append(listed, row.Detail)
			}
		}
		if len(listed) != 1 || listed[0] != "Walk: "+state {
			t.Errorf("day view of %s lists habits %q, want Walk: %s", day, listed, state)
		}
	}
	c, err := s.Completion(ctx, "2026-10-01", "2026-10-06")
	if err != nil {
		t.Fatal(err)
	}
	if wantC := (Completion{Metric: "Walk", ActiveDays: 6, Done: 1, NotDone: 1, NotRecorded: 2, Invalid: 2}); len(c) != 1 || c[0] != wantC {
		t.Errorf("completion = %+v, want %+v", c, wantC)
	}

	if err := s.Tombstone(ctx, "cli", metric); err != nil {
		t.Fatal(err)
	}
	h, err := s.Habits(ctx, "2026-10-01")
	if err != nil {
		t.Fatal(err)
	}
	c, err = s.Completion(ctx, "2026-10-01", "2026-10-06")
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 0 || len(c) != 0 {
		t.Errorf("a tombstoned habit with invalid data is listed: %+v %+v", h, c)
	}
}
