package core

import (
	"reflect"
	"testing"
)

// TestMoodsReadTheCurrentMoodOfEachDay: the current readings of the Mood metric with no session, each day's in the
// order they were recorded. A correction replaces its reading, a retraction removes it, and a reading of a session
// is not the day's mood.
func TestMoodsReadTheCurrentMoodOfEachDay(t *testing.T) {
	s := fresh(t)
	mood := func(day string, v float64) {
		t.Helper()
		if _, _, err := s.Capture(ctx, "cli", day, "", &v); err != nil {
			t.Fatal(err)
		}
	}
	readingOf := func(day string) int64 {
		t.Helper()
		d, err := s.Day(ctx, day)
		if err != nil || len(d.Readings) == 0 {
			t.Fatalf("readings of %s: %v %v", day, d, err)
		}
		return d.Readings[len(d.Readings)-1].ID
	}
	mood("2026-10-10", 4)
	mood("2026-10-10", 3)
	mood("2026-10-11", 4)
	five := 5.0
	if _, err := s.Correct(ctx, "cli", readingOf("2026-10-11"), &five); err != nil {
		t.Fatal(err)
	}
	mood("2026-10-12", 2)
	if _, err := s.Correct(ctx, "cli", readingOf("2026-10-12"), nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Run", ""); err != nil {
		t.Fatal(err)
	}
	var session int64
	if err := s.Do(ctx, "cli", func(tx *Tx) (e error) {
		session, _, e = tx.CaptureSession(SessionInput{Kind: "Run", Day: "2026-10-13", StartAt: "2026-10-13T06:00:00.000Z"})
		return
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Record(ctx, "cli", Reading{Metric: "Mood", Day: "2026-10-13", Value: 1, SessionID: session}); err != nil {
		t.Fatal(err)
	}
	mood("2026-10-14", 3) // outside the window read below

	got, err := s.Moods(ctx, "2026-10-09", "2026-10-13")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]float64{"2026-10-10": {4, 3}, "2026-10-11": {5}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Moods = %v, want %v", got, want)
	}
	if _, err := s.Moods(ctx, "2026-10-13", "2026-10-09"); status(err) != 422 {
		t.Errorf("a reversed window: %v", err)
	}
}

// TestDayEntriesListTheLiveDayPages: day pages only, live ones only, in either order, limited, with their first line.
func TestDayEntriesListTheLiveDayPages(t *testing.T) {
	s := fresh(t)
	for day, text := range map[string]string{
		"2026-10-01": "## Morning\n\nRan with [[Sam]] along the river.",
		"2026-10-03": "Quiet day.",
		"2026-10-05": "Gone.",
	} {
		if _, _, err := s.Capture(ctx, "cli", day, text, nil); err != nil {
			t.Fatal(err)
		}
	}
	four := 4.0
	if _, _, err := s.Capture(ctx, "cli", "2026-10-04", "", &four); err != nil { // a mood alone: a day page with no text
		t.Fatal(err)
	}
	if _, _, err := s.CreatePage(ctx, "cli", "Notes on 2026-10-02", "not a day page"); err != nil {
		t.Fatal(err)
	}
	gone, err := s.Day(ctx, "2026-10-05")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", gone.PageID); err != nil {
		t.Fatal(err)
	}
	show := func(es []DayEntry) []string {
		var out []string
		for _, e := range es {
			out = append(out, e.Day+" "+e.FirstLine)
		}
		return out
	}
	got, err := s.DayEntries(ctx, "2026-10-01", "2026-10-31", true, 10)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"2026-10-04 ", "2026-10-03 Quiet day.", "2026-10-01 Morning"}; !reflect.DeepEqual(show(got), want) {
		t.Errorf("newest first: %q, want %q", show(got), want)
	}
	got, err = s.DayEntries(ctx, "2026-10-02", "2026-10-31", false, 1)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"2026-10-03 Quiet day."}; !reflect.DeepEqual(show(got), want) {
		t.Errorf("oldest first, one: %q, want %q", show(got), want)
	}
	if _, err := s.DayEntries(ctx, "2026-10-02", "2026-10-31", false, 0); status(err) != 422 {
		t.Errorf("no limit: %v", err)
	}
}

func TestFirstLineIsPlainText(t *testing.T) {
	long := ""
	for range 20 {
		long += "abcdefghij"
	}
	for _, c := range []struct{ body, want string }{
		{"", ""},
		{"\n\n  \n", ""},
		{"Ran with [[Sam]] #running", "Ran with Sam #running"},
		{"## Morning\nRan", "Morning"},
		{"\n- a list item\n- another", "a list item"},
		{"> a quotation", "a quotation"},
		{"![[IMG_0001.jpg]] by the lake", "IMG_0001.jpg by the lake"},
		{"Café ünïcode", "Café ünïcode"},
		{long, long[:160] + "…"},
	} {
		if got := FirstLine(c.body); got != c.want {
			t.Errorf("FirstLine(%q) = %q, want %q", c.body, got, c.want)
		}
	}
}
