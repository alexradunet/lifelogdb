package core

import (
	"reflect"
	"strings"
	"testing"
)

// TestMoodsReadTheCurrentMoodOfEachDay: the current readings of the Mood metric, each day's in the order they were
// recorded. A correction replaces its reading, and a retraction removes it.
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
	if _, _, err := s.CreatePage(ctx, "cli", "Notes on 2026-10-02", "not a day page", ""); err != nil {
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

// TestOnThisDayListsEarlierYearsOfTheDate: the live day pages with the month and day of the date in earlier years,
// the newest first, at most the limit. The date itself, another date and a tombstoned page stay out. 29 February
// matches 29 February only.
func TestOnThisDayListsEarlierYearsOfTheDate(t *testing.T) {
	s := fresh(t)
	for day, text := range map[string]string{
		"2026-10-12": "Today.",
		"2025-10-12": "## Last year\n\nRan with [[Sam]].",
		"2024-10-12": "Two years ago.",
		"2023-10-12": "Three years ago.",
		"2025-10-11": "The day before.",
		"2024-02-29": "Leap day.",
		"2023-02-28": "Not the leap day.",
		"2020-02-29": "Another leap day.",
	} {
		if _, _, err := s.Capture(ctx, "cli", day, text, nil); err != nil {
			t.Fatal(err)
		}
	}
	show := func(day string, limit int) []string {
		t.Helper()
		es, err := s.OnThisDay(ctx, day, limit)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, e := range es {
			out = append(out, e.Day+" "+e.FirstLine)
		}
		return out
	}
	three := []string{"2025-10-12 Last year", "2024-10-12 Two years ago.", "2023-10-12 Three years ago."}
	if got := show("2026-10-12", 10); !reflect.DeepEqual(got, three) {
		t.Errorf("OnThisDay = %q, want %q", got, three)
	}
	if got := show("2026-10-12", 2); !reflect.DeepEqual(got, three[:2]) {
		t.Errorf("limit 2 = %q, want %q", got, three[:2])
	}
	if got, want := show("2028-02-29", 10), []string{"2024-02-29 Leap day.", "2020-02-29 Another leap day."}; !reflect.DeepEqual(got, want) {
		t.Errorf("29 February = %q, want %q", got, want)
	}
	if got := show("2023-10-12", 10); len(got) != 0 {
		t.Errorf("the first year has earlier years: %q", got)
	}
	gone, err := s.Day(ctx, "2024-10-12")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Tombstone(ctx, "cli", gone.PageID); err != nil {
		t.Fatal(err)
	}
	want := []string{three[0], three[2]}
	if got := show("2026-10-12", 10); !reflect.DeepEqual(got, want) {
		t.Errorf("after a tombstone = %q, want %q", got, want)
	}
	if _, err := s.OnThisDay(ctx, "2026-10-12", 0); status(err) != 422 {
		t.Errorf("no limit: %v", err)
	}
	if _, err := s.OnThisDay(ctx, "2026-10", 5); status(err) != 422 {
		t.Errorf("not a day: %v", err)
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
		{"#running 5k with [[Sam]]", "#running 5k with Sam"},
		{"-5 kg today", "-5 kg today"},
		{"![[IMG_0001.jpg]] by the lake", "IMG_0001.jpg by the lake"},
		{"Café ünïcode", "Café ünïcode"},
		{long, long[:160] + "…"},
	} {
		if got := firstLine(c.body); got != c.want {
			t.Errorf("firstLine(%q) = %q, want %q", c.body, got, c.want)
		}
	}
}

// TestTitlesByPrefixFoldsLikeATitleKey: a prefix finds the names whose key starts with its title key (case, ß, a
// decomposed accent), an alias with its page's preferred title, never a tombstoned page, in key order, limited.
func TestTitlesByPrefixFoldsLikeATitleKey(t *testing.T) {
	s := fresh(t)
	page := func(title string) int64 {
		t.Helper()
		id, _, err := s.CreatePage(ctx, "cli", title, "", "")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	page("Sam Example")
	page("SAMUEL")
	page("Sb")
	page("Straße")
	page("Café")
	old := page("Samwise draft")
	if _, err := s.Rename(ctx, "cli", old, "Gardener"); err != nil {
		t.Fatal(err)
	}
	gone := page("Samantha")
	if err := s.Tombstone(ctx, "cli", gone); err != nil {
		t.Fatal(err)
	}
	show := func(prefix string, limit int) string {
		t.Helper()
		ms, err := s.TitlesByPrefix(ctx, prefix, limit)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, m := range ms {
			out = append(out, m.Name+"="+m.Title)
		}
		return strings.Join(out, ", ")
	}
	for _, c := range []struct {
		prefix string
		limit  int
		want   string
	}{
		{"sam", 10, "Sam Example=Sam Example, SAMUEL=SAMUEL, Samwise draft=Gardener"},
		{"  SAM", 2, "Sam Example=Sam Example, SAMUEL=SAMUEL"},
		{"sa", 10, "Sam Example=Sam Example, SAMUEL=SAMUEL, Samwise draft=Gardener"},
		{"STRAS", 10, "Straße=Straße"},
		{"cafe\u0301", 10, "Café=Café"},
		{"zz", 10, ""},
	} {
		if got := show(c.prefix, c.limit); got != c.want {
			t.Errorf("TitlesByPrefix(%q, %d) = %q, want %q", c.prefix, c.limit, got, c.want)
		}
	}
	if _, err := s.TitlesByPrefix(ctx, "  ", 10); status(err) != 422 {
		t.Errorf("a blank prefix: %v", err)
	}
}

// TestReadingsCountCountsTheCurrentReadingsOfAWindow: three weight readings, one of them corrected, and a steps
// reading. A correction replaces its reading, so it counts once. The bounds are both included.
func TestReadingsCountCountsTheCurrentReadingsOfAWindow(t *testing.T) {
	s := fresh(t)
	for name, unit := range map[string]string{"weight": "kg", "steps": "n"} {
		if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: name, Unit: unit}); err != nil {
			t.Fatal(err)
		}
	}
	var fixed int64
	for _, r := range []Reading{{Metric: "weight", Day: "2026-10-01", Value: 70}, {Metric: "weight", Day: "2026-10-05", Value: 71},
		{Metric: "weight", Day: "2026-10-09", Value: 72}, {Metric: "steps", Day: "2026-10-06", Value: 900}} {
		id, err := s.Record(ctx, "cli", r)
		if err != nil {
			t.Fatal(err)
		}
		if r.Day == "2026-10-05" {
			fixed = id
		}
	}
	value := 71.5
	if _, err := s.Correct(ctx, "cli", fixed, &value); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		from, through string
		want          int
	}{
		{"2026-10-01", "2026-10-31", 4}, {"2026-10-05", "2026-10-06", 2}, {"2026-10-01", "2026-10-01", 1},
		{"2026-10-02", "2026-10-04", 0}, {"2026-09-01", "2026-09-30", 0},
	} {
		got, err := s.ReadingsCount(ctx, tc.from, tc.through)
		if err != nil || got != tc.want {
			t.Errorf("ReadingsCount(%s, %s) = %d, %v; want %d", tc.from, tc.through, got, err, tc.want)
		}
	}
	if _, err := s.ReadingsCount(ctx, "2026-10-09", "2026-10-01"); status(err) != 422 {
		t.Errorf("a reversed window: %v", err)
	}
}
