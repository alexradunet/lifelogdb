package core

import (
	"reflect"
	"strings"
	"testing"
)

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
		// a picture is not words: an embed is left out, and a line of pictures alone is passed over, so a cover put
		// first (set-cover) does not take the place of the text
		{"![[IMG_0001.jpg]] by the lake", "by the lake"},
		{"![[2026-10-04 2026-10-17.jpg]]\n\nA walk by the lake.", "A walk by the lake."},
		{"![[A.jpg]] ![[B.jpg|the lake]]\nWords", "Words"},
		{"![[A.jpg]]", ""},
		{"Café ünïcode", "Café ünïcode"},
		// the marks of the inline CommonMark are not words
		{"Finished *Piranesi* today", "Finished Piranesi today"},
		{"**Good** day, _calm_", "Good day, calm"},
		{"## A `quiet` [walk](https://x.example/a) in the park", "A quiet walk in the park"},
		{"Ran with [[Sam Smith|Sam]]", "Ran with Sam"},
		{"A &amp; B and a \\*star\\*", "A & B and a *star*"},
		// a line with no words is passed over
		{"---\nWords", "Words"},
		{"![a lake](lake.png)\nWords", "Words"},
		// a number with a dot or a parenthesis at the start is words, not the marker of an ordered list
		{"1986. Born in Cluj", "1986. Born in Cluj"},
		{"2) the second point", "2) the second point"},
		{long, long[:160] + "…"},
	} {
		if got := firstLine(c.body); got != c.want {
			t.Errorf("firstLine(%q) = %q, want %q", c.body, got, c.want)
		}
	}
}

// count is the number of rows a query counts, for a test that asks what a write left in the file.
func count(t *testing.T, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.DB.R.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestACaptureSentAgainChangesNothing (issue 0085, D28): a capture with the same key, sent again after a lost reply,
// adds no paragraph and no Mood reading, and says that it was added before. A capture with another key, or with no
// key, is a new fact.
func TestACaptureSentAgainChangesNothing(t *testing.T) {
	s := fresh(t)
	mood := 4.0
	id, _, already, err := s.CaptureWithKey(ctx, "ui", "key-1", "2026-10-17", "Walked the dog", &mood)
	if err != nil || already {
		t.Fatalf("first send: already %v, %v", already, err)
	}
	again, r, already, err := s.CaptureWithKey(ctx, "ui", "key-1", "2026-10-17", "Walked the dog", &mood)
	if err != nil || !already || again != id || len(r.Linked) != 0 {
		t.Fatalf("send again: id %d (first %d), already %v, sync %+v, %v", again, id, already, r, err)
	}
	p, _ := s.PageByID(ctx, id)
	if p.Body != "Walked the dog" {
		t.Errorf("body after a send again: %q", p.Body)
	}
	if n := count(t, s, `SELECT count(*) FROM measurements`); n != 1 {
		t.Errorf("%d readings after a send again, want 1", n)
	}
	if n := count(t, s, `SELECT count(*) FROM measurements WHERE import_key = 'key-1' AND source = 'ui' AND captured_with_id = ?`, id); n != 1 {
		t.Errorf("the Mood reading does not carry the key of its capture")
	}
	if n := count(t, s, `SELECT count(*) FROM capture_keys WHERE source = 'ui' AND import_key = 'key-1' AND page_id = ?`, id); n != 1 {
		t.Errorf("capture_keys holds %d rows for the key, want 1", n)
	}

	// another key is another capture; no key is a capture that nothing recognizes
	if _, _, already, err = s.CaptureWithKey(ctx, "ui", "key-2", "2026-10-17", "Walked the dog", &mood); err != nil || already {
		t.Fatalf("another key: already %v, %v", already, err)
	}
	for range 2 {
		if _, _, already, err = s.CaptureWithKey(ctx, "ui", "", "2026-10-17", "No key", nil); err != nil || already {
			t.Fatalf("no key: already %v, %v", already, err)
		}
	}
	p, _ = s.PageByID(ctx, id)
	if want := "Walked the dog\n\nWalked the dog\n\nNo key\n\nNo key"; p.Body != want {
		t.Errorf("body %q, want %q", p.Body, want)
	}
	if n := count(t, s, `SELECT count(*) FROM measurements`); n != 2 {
		t.Errorf("%d readings, want 2", n)
	}
	if n := count(t, s, `SELECT count(*) FROM capture_keys`); n != 2 {
		t.Errorf("%d keys, want 2: a capture with no key keeps none", n)
	}
}

// TestACaptureKeyBelongsToItsSource: one key from two writers is two captures, as an import_key is.
func TestACaptureKeyBelongsToItsSource(t *testing.T) {
	s := fresh(t)
	for _, src := range []string{"ui", "cli"} {
		if _, _, already, err := s.CaptureWithKey(ctx, src, "same", "2026-10-17", "from "+src, nil); err != nil || already {
			t.Fatalf("%s: already %v, %v", src, already, err)
		}
	}
	id, err := s.PageID(ctx, "2026-10-17")
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := s.PageByID(ctx, id); p.Body != "from ui\n\nfrom cli" {
		t.Errorf("body %q", p.Body)
	}
}

// TestACaptureSentAgainAnswersWithTheDayItWasAddedTo: the resend names another day, and nothing is written for it.
func TestACaptureSentAgainAnswersWithTheDayItWasAddedTo(t *testing.T) {
	s := fresh(t)
	id, _, _, err := s.CaptureWithKey(ctx, "ui", "k", "2026-10-17", "first", nil)
	if err != nil {
		t.Fatal(err)
	}
	again, _, already, err := s.CaptureWithKey(ctx, "ui", "k", "2026-10-18", "first", nil)
	if err != nil || !already || again != id {
		t.Fatalf("resend for another day: id %d (first %d), already %v, %v", again, id, already, err)
	}
	if n := count(t, s, `SELECT count(*) FROM entity_names WHERE name_key = '2026-10-18'`); n != 0 {
		t.Error("the resend made the page of another day")
	}
	// a page tombstoned since stays tombstoned: the capture was added, and a resend changes nothing
	if err := s.Tombstone(ctx, "ui", id); err != nil {
		t.Fatal(err)
	}
	if _, _, already, err = s.CaptureWithKey(ctx, "ui", "k", "2026-10-17", "first", nil); err != nil || !already {
		t.Fatalf("resend after a tombstone: already %v, %v", already, err)
	}
	if n := count(t, s, `SELECT count(*) FROM entities WHERE id = ? AND deleted_at IS NULL`, id); n != 0 {
		t.Error("the resend revived the page")
	}
}

// TestACaptureKeyOfABadShapeIsRefusedByTheFile: the DDL refuses the key (capture_keys_import_key), and the capture
// writes nothing.
func TestACaptureKeyOfABadShapeIsRefusedByTheFile(t *testing.T) {
	s := fresh(t)
	mood := 3.0
	for name, key := range map[string]string{"513 bytes": strings.Repeat("a", 513), "a NUL": "k\x00x"} {
		_, _, _, err := s.CaptureWithKey(ctx, "ui", key, "2026-10-17", "text", &mood)
		if status(err) != 422 || !strings.Contains(err.Error(), "capture_keys_import_key") {
			t.Errorf("%s: %v, want 422 from capture_keys_import_key", name, err)
		}
	}
	if n := count(t, s, `SELECT count(*) FROM entity_names WHERE name_key = '2026-10-17'`) + count(t, s, `SELECT count(*) FROM measurements`); n != 0 {
		t.Errorf("a refused capture left %d rows", n)
	}
}

// TestACaptureKeyUsedForAMoodReadingIsRefused: the Mood reading of a capture carries the key in the index of
// measurements. A reading of Mood recorded under the same key and source would make it a no-op, and the mood of the
// capture would be lost without a sign. The capture is refused instead, and writes nothing.
func TestACaptureKeyUsedForAMoodReadingIsRefused(t *testing.T) {
	s := fresh(t)
	if _, err := s.Record(ctx, "ui", Reading{Metric: "Mood", Day: "2026-10-16", Value: 2, ImportKey: "shared"}); err != nil {
		t.Fatal(err)
	}
	mood := 5.0
	_, _, _, err := s.CaptureWithKey(ctx, "ui", "shared", "2026-10-17", "text", &mood)
	if status(err) != 409 {
		t.Fatalf("a key of a reading: %v, want 409", err)
	}
	if n := count(t, s, `SELECT count(*) FROM entity_names WHERE name_key = '2026-10-17'`) + count(t, s, `SELECT count(*) FROM capture_keys`); n != 0 {
		t.Errorf("a refused capture left %d rows", n)
	}
}
