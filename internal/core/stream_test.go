package core

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"lifelog/internal/photo/phototest"
)

// streamStore is a store whose today is 2026-10-17, in UTC.
func streamStore(t *testing.T) *Store {
	t.Helper()
	s := fresh(t)
	s.Clock = func() time.Time { return time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC) }
	s.Zone = time.UTC
	return s
}

// must ends the test on a setup error.
func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

// cardWords is a card as one line: its day, its page, its words, its mood, chips, birthdays, pages and counts.
func cardWords(c Card) string {
	names := func(items []CardItem) string {
		var out []string
		for _, it := range items {
			out = append(out, it.Kind+":"+it.Title)
		}
		return strings.Join(out, ",")
	}
	return fmt.Sprintf("%s page=%v picture=%v words=%q mood=%v chips=%s+%d birthdays=%s pages=%s+%d readings=%d quiet=%d",
		c.Day, c.PageID != 0, c.Picture != 0, c.Words, c.Mood, names(c.Chips), c.MoreChips, names(c.Birthdays), names(c.Pages), c.MorePages, c.Readings, c.QuietAfter)
}

// TestTheStreamHoldsEveryDayWithSomething: the stream is the days before today with a live day page, a page, a file
// or a current reading, newest first; a tombstoned day page and a future day are not in it, and before is exclusive.
func TestTheStreamHoldsEveryDayWithSomething(t *testing.T) {
	s := streamStore(t)
	capture := func(day, text string, mood *float64) int64 { t.Helper(); return captured(t, s, day, text, mood) }
	must[int64](t)(s.CreatePerson(ctx, "cli", "Ana", "", "1990-10-17", "", ""))
	must[int64](t)(s.CreatePerson(ctx, "cli", "Cara", "", "1990-10-15", "", "")) // a birthday on a day with no card
	lake := must[int64](t)(s.CreatePlace(ctx, "cli", "Lake", ""))
	capture("2026-10-20", "A plan for a day to come.", nil)
	today := capture("2026-10-17", "Swam at the lake.\n\n## Then\nAna came by, with [[Ana]].\n![[Nothing kept]]\nline four\nline five", ptr(4))
	if err := s.Link(ctx, "cli", today, lake, "at", ""); err != nil {
		t.Fatal(err)
	}
	must[bool](t)(s.CreateMetric(ctx, "cli", NewMetric{Name: "Weight", Unit: "kg"}))
	must[int64](t)(s.Record(ctx, "cli", Reading{Metric: "Weight", Day: "2026-10-12", Value: 70}))
	must[Kept](t)(s.AddFile(ctx, "cli", FileIn{Title: "Notes.pdf", SHA256: strings.Repeat("b", 64), MIME: "application/pdf", Day: "2026-10-11"}))
	must[Kept](t)(s.AddFile(ctx, "cli", FileIn{Title: "Lake.jpg", SHA256: strings.Repeat("a", 64), MIME: "image/jpeg", Day: "2026-10-10", Preview: phototest.JPEG(8, 8)}))
	gone := capture("2026-10-09", "Deleted.", nil)
	if err := s.Tombstone(ctx, "cli", gone); err != nil {
		t.Fatal(err)
	}
	must[int64](t)(s.Record(ctx, "cli", Reading{Metric: "Mood", Day: "2026-10-08", Value: 3}))
	capture("2024-05-01", "Long ago.", nil)

	cards, more := streamPage(t, s, "", 30, 0)
	want := []string{
		`2026-10-17 page=true picture=false words="Swam at the lake.\nThen\nAna came by, with Ana.\nline four" mood=[4] chips=place:Lake,person:Ana+0 birthdays=person:Ana pages=+0 readings=0 quiet=4`,
		`2026-10-12 page=false picture=false words="" mood=[] chips=+0 birthdays= pages=+0 readings=1 quiet=0`,
		`2026-10-11 page=false picture=false words="" mood=[] chips=+0 birthdays= pages=file:Notes.pdf+0 readings=0 quiet=0`,
		// a file with a picture is shown in the page of its day: that day page has the picture, and no words
		`2026-10-10 page=true picture=true words="" mood=[] chips=+0 birthdays= pages=file:Lake.jpg+0 readings=0 quiet=1`,
		`2026-10-08 page=false picture=false words="" mood=[3] chips=+0 birthdays= pages=+0 readings=0 quiet=889`,
		`2024-05-01 page=true picture=false words="Long ago." mood=[] chips=+0 birthdays= pages=+0 readings=0 quiet=0`,
	}
	if got := words(cards); got != strings.Join(want, "\n") || more {
		t.Errorf("the stream (more %v):\n got %s\nwant %s", more, got, strings.Join(want, "\n"))
	}

	// before is exclusive, and a page of 2 says that more follow; the last card keeps its quiet days.
	cards, more = streamPage(t, s, "2026-10-12", 2, 0)
	if got := days(cards); got != "2026-10-11,2026-10-10" || !more || cards[1].QuietAfter != 1 {
		t.Errorf("before 2026-10-12: %s, more %v, %+v", got, more, cards)
	}
	if cards, more = streamPage(t, s, "2026-10-12", 2, 2); days(cards) != "2026-10-08,2024-05-01" || more {
		t.Errorf("before 2026-10-12, offset 2: %s, more %v", days(cards), more)
	}
	// A before after today shows no future day; the first day is the end of the stream.
	if cards, _ = streamPage(t, s, "2030-01-01", 1, 0); days(cards) != "2026-10-17" {
		t.Errorf("before 2030: %s", days(cards))
	}
	if cards, _ = streamPage(t, s, "0000-01-01", 5, 0); len(cards) != 0 {
		t.Errorf("before 0000-01-01: %s", days(cards))
	}
	if _, _, err := s.Stream(ctx, "2026-13-01", 5, 0); status(err) != 422 {
		t.Errorf("before 2026-13-01: %v, want a 422", err)
	}
}

// captured is the id of the day page a capture wrote.
func captured(t *testing.T, s *Store, day, text string, mood *float64) int64 {
	t.Helper()
	id, _, err := s.Capture(ctx, "cli", day, text, mood)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func streamPage(t *testing.T, s *Store, before string, limit, offset int) ([]Card, bool) {
	t.Helper()
	cards, more, err := s.Stream(ctx, before, limit, offset)
	if err != nil {
		t.Fatal(err)
	}
	return cards, more
}

func words(cards []Card) string {
	var out []string
	for _, c := range cards {
		out = append(out, cardWords(c))
	}
	return strings.Join(out, "\n")
}

func days(cards []Card) string {
	var out []string
	for _, c := range cards {
		out = append(out, c.Day)
	}
	return strings.Join(out, ",")
}

// TestACardCapsItsChipsAndItsPages: six chips, then a count; three pages, then a count. A tombstoned person is not a
// chip.
func TestACardCapsItsChipsAndItsPages(t *testing.T) {
	s := streamStore(t)
	var body []string
	for i := 1; i <= 8; i++ {
		must[int64](t)(s.CreatePerson(ctx, "cli", fmt.Sprintf("Person %d", i), "", "", "", ""))
		body = append(body, fmt.Sprintf("[[Person %d]]", i))
	}
	captured(t, s, "2026-10-17", strings.Join(body, " "), nil)
	gone := must[int64](t)(s.PageID(ctx, "Person 8")) // after the capture: a capture revives the pages it names
	if err := s.Tombstone(ctx, "cli", gone); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		if _, _, err := s.CreatePage(ctx, "cli", fmt.Sprintf("Note %d", i), "", ""); err != nil {
			t.Fatal(err)
		}
	}
	cards, _ := streamPage(t, s, "", 30, 0)
	if len(cards) != 1 {
		t.Fatalf("cards: %s", words(cards))
	}
	c := cards[0]
	if len(c.Chips) != 6 || c.MoreChips != 1 || c.Chips[0].Title != "Person 1" || len(c.Pages) != 3 || c.MorePages != 2 || c.Pages[0].Title != "Note 1" {
		t.Errorf("the card: %s", cardWords(c))
	}
}

// TestNewerBeforeIsThePageAbove: the page newer than a page of the stream starts before the day after its limit
// newer days, or is the top.
func TestNewerBeforeIsThePageAbove(t *testing.T) {
	s := streamStore(t)
	for _, d := range []string{"2026-10-01", "2026-10-02", "2026-10-03", "2026-10-04", "2026-10-05", "2026-10-30"} {
		captured(t, s, d, "x", nil)
	}
	for _, tc := range []struct {
		before, newer string
		ok            bool
	}{
		{"", "", false},
		{"2026-10-06", "", false}, // nothing newer, up to today: the page is the top
		{"2026-10-05", "", true},  // one newer day: the top
		{"2026-10-04", "", true},  // two newer days: the top
		{"2026-10-03", "2026-10-05", true},
		{"2026-10-01", "2026-10-03", true},
	} {
		newer, ok, err := s.NewerBefore(ctx, tc.before, 2)
		if err != nil || newer != tc.newer || ok != tc.ok {
			t.Errorf("NewerBefore(%q, 2) = %q, %v, %v; want %q, %v", tc.before, newer, ok, err, tc.newer, tc.ok)
		}
		if tc.newer == "" {
			continue
		}
		cards, _ := streamPage(t, s, tc.newer, 2, 0)
		if want := map[string]string{"2026-10-05": "2026-10-04,2026-10-03", "2026-10-03": "2026-10-02,2026-10-01"}[tc.newer]; days(cards) != want {
			t.Errorf("the page before %s: %s, want %s", tc.newer, days(cards), want)
		}
	}
}

// TestPlainLines: the words of a card are the first lines that have words, as plain text, each cut.
func TestPlainLines(t *testing.T) {
	long := strings.Repeat("a", 170)
	for _, tc := range []struct {
		body string
		n    int
		want string
	}{
		{"", 4, ""},
		{"![[a.jpg]]\n\n# Title\n\n- **one**\n2. two\n\n1986. Born", 4, "Title\none\n2. two\n1986. Born"},
		{"one\ntwo\nthree\nfour\nfive", 4, "one\ntwo\nthree\nfour"},
		{"one\ntwo", 1, "one"},
		{long, 2, strings.Repeat("a", 160) + "…"},
		{"[[Ana]] and [link](http://x)", 4, "Ana and link"},
	} {
		if got := plainLines(tc.body, tc.n, 160); got != tc.want {
			t.Errorf("plainLines(%q, %d) = %q, want %q", tc.body, tc.n, got, tc.want)
		}
	}
}
