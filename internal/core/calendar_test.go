package core

import (
	"fmt"
	"strings"
	"testing"
)

// The calendar's reads (docs/plans/101-the-calendar-holds-every-dated-thing.md). Every expected value is worked out by
// hand from the rows the test writes.

func anniversaryLines(as []Anniversary) string {
	var out []string
	for _, a := range as {
		out = append(out, fmt.Sprintf("%s %s %s %d", a.Day, a.Kind, a.Title, a.Years))
	}
	return strings.Join(out, " | ")
}

func TestAnniversariesComeBackEachYear(t *testing.T) {
	s := fresh(t)
	person := func(title, birth, death string) int64 {
		t.Helper()
		id, err := s.CreatePerson(ctx, "cli", title, title, birth, death)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	person("Ana", "1990-05-14", "")
	person("Ion", "1930-03-01", "2020-05-14")
	person("Leap", "2000-02-29", "")
	person("Later", "2040-05-14", "")
	person("Year end", "1985-12-31", "")
	person("New year", "1985-01-02", "")
	person("No days", "", "")
	gone := person("Gone", "1990-05-14", "")
	if err := s.Tombstone(ctx, "cli", gone); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ from, through, want string }{
		// a birthday before a death on one day; a person not yet born and a tombstoned one are not there
		{"2031-05-01", "2031-05-31", "2031-05-14 birthday Ana 41 | 2031-05-14 death Ion 11"},
		// the death day itself has no years
		{"2020-05-14", "2020-05-14", "2020-05-14 birthday Ana 30 | 2020-05-14 death Ion 0"},
		// 29 February is on 28 February in a common year, and on its own day in a leap year
		{"2031-02-01", "2031-02-28", "2031-02-28 birthday Leap 31"},
		{"2032-02-27", "2032-03-01", "2032-02-29 birthday Leap 32 | 2032-03-01 birthday Ion 102"},
		// the birth day itself, and a day before any birth
		{"2000-02-29", "2000-03-01", "2000-02-29 birthday Leap 0 | 2000-03-01 birthday Ion 70"},
		{"1929-05-14", "1929-05-14", ""},
		// a week across the new year
		{"2031-12-29", "2032-01-04", "2031-12-31 birthday Year end 46 | 2032-01-02 birthday New year 47"},
	} {
		as, err := s.Anniversaries(ctx, tc.from, tc.through)
		if err != nil {
			t.Fatalf("%s to %s: %v", tc.from, tc.through, err)
		}
		if got := anniversaryLines(as); got != tc.want {
			t.Errorf("%s to %s:\n got %s\nwant %s", tc.from, tc.through, got, tc.want)
		}
	}
	for _, bad := range [][2]string{{"2031-01-01", "2032-01-02"}, {"2031-05-02", "2031-05-01"}, {"2031-5-1", "2031-05-31"}} {
		if _, err := s.Anniversaries(ctx, bad[0], bad[1]); status(err) != 422 {
			t.Errorf("%v: %v, want 422", bad, err)
		}
	}
}

func TestDatedRowsOfAWindow(t *testing.T) {
	s := fresh(t)
	dayPage, _, err := s.Capture(ctx, "cli", "2031-05-14", "A walk by the lake.", nil)
	if err != nil {
		t.Fatal(err)
	}
	page := func(title, day string) int64 {
		t.Helper()
		var id int64
		if err := s.Do(ctx, "cli", func(tx *Tx) error {
			var err error
			id, _, _, err = tx.CreatePage(title, "", day, "")
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	page("Notes on rivers", "2031-05-14")
	page("June plan", "2031-06-01")
	if err := s.Tombstone(ctx, "cli", page("Scratch", "2031-05-14")); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 21; i++ {
		page(fmt.Sprintf("Import %02d", i), "2031-05-20")
	}
	if _, err := s.AddFile(ctx, "cli", FileIn{Title: "Lake.jpg", SHA256: strings.Repeat("a", 64), MIME: "image/jpeg", Day: "2031-05-14"}); err != nil {
		t.Fatal(err)
	}
	park, err := s.CreatePlace(ctx, "cli", "Lake Park")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Link(ctx, "cli", dayPage, park, "at", ""); err != nil {
		t.Fatal(err)
	}

	rows, err := s.DatedIn(ctx, "2031-05-01", "2031-05-31", 20)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rows {
		got = append(got, fmt.Sprintf("%s %s %s %d", r.Day, r.Kind, r.Title, r.Pages))
	}
	want := []string{
		"2031-05-14 place Lake Park 0",
		"2031-05-14 file Lake.jpg 2",
		"2031-05-14 page Notes on rivers 2",
	}
	for i := 1; i <= 20; i++ {
		want = append(want, fmt.Sprintf("2031-05-20 page Import %02d 21", i))
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("dated rows:\n got %s\nwant %s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if _, err := s.DatedIn(ctx, "2031-01-01", "2032-01-02", 20); status(err) != 422 {
		t.Errorf("a window of 367 days: %v, want 422", err)
	}
}

func TestReadingsByDay(t *testing.T) {
	s := fresh(t)
	mood := 4.0
	if _, _, err := s.Capture(ctx, "cli", "2031-05-14", "", &mood); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterMetric(ctx, "cli", "weight", "kg", ""); err != nil {
		t.Fatal(err)
	}
	record := func(day string, v float64) int64 {
		t.Helper()
		id, err := s.Record(ctx, "cli", Reading{Metric: "weight", Day: day, Value: v})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	wrong := record("2031-05-14", 80)
	if _, err := s.Correct(ctx, "cli", wrong, func() *float64 { v := 79.5; return &v }()); err != nil {
		t.Fatal(err)
	}
	record("2031-05-14", 79)
	record("2031-05-15", 78)
	if _, err := s.Correct(ctx, "cli", record("2031-05-15", 70), nil); err != nil { // a retraction
		t.Fatal(err)
	}
	record("2031-06-01", 77)

	got, err := s.ReadingsByDay(ctx, "2031-05-01", "2031-05-31")
	if err != nil {
		t.Fatal(err)
	}
	// 14 May: the mood, the correction (not the reading it corrects) and 79. 15 May:
	// 78, not the retracted reading
	if fmt.Sprint(got) != "map[2031-05-14:3 2031-05-15:1]" {
		t.Errorf("readings by day: %v", got)
	}
}
