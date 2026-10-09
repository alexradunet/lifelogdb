package core

import "testing"

func TestDailyHabitAndMoodRemainIndependentOfSessionFacts(t *testing.T) {
	s := fresh(t)
	if _, _, err := s.CreatePage(ctx, "cli", "Workout", ""); err != nil {
		t.Fatal(err)
	}
	var session int64
	if err := s.Do(ctx, "cli", func(tx *Tx) error {
		var err error
		session, _, err = tx.CaptureSession(SessionInput{Kind: "Workout", Day: "2020-01-02", StartLocal: "2020-01-02T08:00:00.000"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterMetric(ctx, "cli", "Walk", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.StartHabit(ctx, "cli", "Walk", "2020-01-02", "2020-01-02"); err != nil {
		t.Fatal(err)
	}
	scoped, err := s.Record(ctx, "cli", Reading{Metric: "Walk", Day: "2020-01-02", Value: 1, SessionID: session})
	if err != nil {
		t.Fatal(err)
	}
	assertHabit := func(want string, done, notDone, missing int) {
		t.Helper()
		h, err := s.Habits(ctx, "2020-01-02")
		if err != nil || len(h) != 1 || h[0].State != want {
			t.Fatalf("habit %+v %v", h, err)
		}
		c, err := s.Completion(ctx, "2020-01-02", "2020-01-02")
		if err != nil || len(c) != 1 || c[0].Done != done || c[0].NotDone != notDone || c[0].NotRecorded != missing {
			t.Fatalf("completion %+v %v", c, err)
		}
	}
	assertHabit("not recorded", 0, 0, 1)
	zero := 0.0
	corrected, err := s.Correct(ctx, "cli", scoped, &zero)
	if err != nil {
		t.Fatal(err)
	}
	assertHabit("not recorded", 0, 0, 1)
	if _, err := s.Record(ctx, "cli", Reading{Metric: "Walk", Day: "2020-01-02", Value: 2, SessionID: session}); err == nil {
		t.Fatal("session bypassed habit value domain")
	}
	moved, err := s.RelocateReading(ctx, "cli", corrected, Reading{Metric: "Walk", Day: "2020-01-02", Value: 0})
	if err != nil {
		t.Fatal(err)
	}
	assertHabit("not done", 0, 1, 0)
	one := 1.0
	if _, err := s.Correct(ctx, "cli", moved.ReplacementID, &one); err != nil {
		t.Fatal(err)
	}
	assertHabit("done", 1, 0, 0)
	// Existing nonboolean session facts cannot become boolean merely by daily filtering.
	if _, err := s.RegisterMetric(ctx, "cli", "Nonboolean", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Record(ctx, "cli", Reading{Metric: "Nonboolean", Day: "2020-01-02", Value: 2, SessionID: session}); err != nil {
		t.Fatal(err)
	}
	if err := s.StartHabit(ctx, "cli", "Nonboolean", "2020-01-02", ""); err == nil {
		t.Fatal("habit registration ignored session domain")
	}
	if err := s.StartHabit(ctx, "cli", "Mood", "2020-01-02", ""); err == nil {
		t.Fatal("Mood registered as habit")
	}
	dailyMood, err := s.Record(ctx, "cli", Reading{Metric: "Mood", Day: "2020-01-02", Value: 3})
	if err != nil {
		t.Fatal(err)
	}
	sessionMood, err := s.Record(ctx, "cli", Reading{Metric: "Mood", Day: "2020-01-02", Value: 5, SessionID: session})
	if err != nil {
		t.Fatal(err)
	}
	bad := 6.0
	if _, err := s.Correct(ctx, "cli", sessionMood, &bad); err == nil {
		t.Fatal("scoped correction bypassed Mood domain")
	}
	assertMood := func() {
		t.Helper()
		rows, err := s.Series(ctx, "Mood", "2020-01-01", "2020-01-02")
		if err != nil || len(rows) != 1 || rows[0].ID != dailyMood || rows[0].Value != 3 {
			t.Fatalf("daily Mood %+v %v", rows, err)
		}
		assertHabit("done", 1, 0, 0)
	}
	assertMood()
	for _, deleted := range []bool{true, false} {
		have, err := s.Session(ctx, session)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.SessionLifecycle(session, have.Version, deleted) }); err != nil {
			t.Fatal(err)
		}
		assertMood()
		rows, err := s.SeriesScope(ctx, "Mood", "2020-01-01", "2020-01-02", "session", session, false)
		if err != nil || len(rows) != map[bool]int{true: 0, false: 1}[deleted] {
			t.Fatalf("session lifecycle %+v %v", rows, err)
		}
	}
}
