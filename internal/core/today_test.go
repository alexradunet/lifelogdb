package core

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestStreakIsTheCookbookRecipe keeps the query of the writer equal to the recipe that the suite `streaks` of tests/
// runs, comments and spaces apart.
func TestStreakIsTheCookbookRecipe(t *testing.T) {
	b, err := os.ReadFile("../../docs/cookbook/streaks.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("(?s)```sql\n(.*?)```").FindSubmatch(b)
	if m == nil {
		t.Fatal("no sql block in cookbook/streaks.md")
	}
	flat := func(q string) string {
		q = regexp.MustCompile(`(?m)^\s*--.*$`).ReplaceAllString(q, "")
		return strings.Join(strings.Fields(strings.TrimSuffix(strings.TrimSpace(q), ";")), " ")
	}
	if got, want := flat(streakQuery), flat(string(m[1])); got != want {
		t.Errorf("the query differs from the recipe:\n got %s\nwant %s", got, want)
	}
}

func TestStreakCountsTheRunThatEndsTodayOrYesterday(t *testing.T) {
	s := fresh(t)
	today := "2026-10-19"
	day := func(back int) string {
		d, _ := time.Parse(time.DateOnly, today)
		return d.AddDate(0, 0, -back).Format(time.DateOnly)
	}
	write := func(back int) {
		t.Helper()
		if _, _, err := s.Capture(ctx, "cli", day(back), "Synthetic words.", nil); err != nil {
			t.Fatal(err)
		}
	}
	check := func(want int, written bool) {
		t.Helper()
		n, w, err := s.Streak(ctx, today)
		if err != nil || n != want || w != written {
			t.Errorf("Streak = %d, %v, %v; want %d, %v", n, w, err, want, written)
		}
	}
	check(0, false)
	write(2)
	write(3)
	check(0, false) // the run ends two days ago: it is broken
	write(1)
	check(3, false) // yesterday written, today not
	write(0)
	check(4, true)
	if _, _, err := s.Streak(ctx, "today"); status(err) != 422 {
		t.Errorf("a word that is not a day: %v", err)
	}
}

func TestHabitsAreTheUnitlessMetricsInUse(t *testing.T) {
	s := fresh(t)
	today := "2026-10-19"
	for _, m := range []struct{ name, unit string }{{"Walk", ""}, {"No phone", ""}, {"Old habit", ""}, {"Stretch", ""}, {"Weight", "kg"}} {
		if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: m.name, Unit: m.unit}); err != nil {
			t.Fatal(err)
		}
	}
	rec := func(metric, day string, v float64) {
		t.Helper()
		if _, err := s.Record(ctx, "cli", Reading{Metric: metric, Day: day, Value: v}); err != nil {
			t.Fatal(err)
		}
	}
	rec("Walk", today, 1)
	rec("No phone", today, 0)
	rec("Old habit", "2026-10-05", 1) // 14 days ago: out of use
	rec("Stretch", "2026-10-10", 1)   // in use, nothing today
	rec("Weight", today, 70)          // has a unit: not a habit
	rec("Mood", today, 4)             // a scale: not a habit
	got, err := s.Habits(ctx, today)
	if err != nil {
		t.Fatal(err)
	}
	type state struct {
		name string
		done string
	}
	var have []state
	for _, h := range got {
		d := "open"
		if h.Done != nil && *h.Done {
			d = "done"
		} else if h.Done != nil {
			d = "no"
		}
		have = append(have, state{h.Name, d})
	}
	want := []state{{"No phone", "no"}, {"Stretch", "open"}, {"Walk", "done"}}
	if len(have) != len(want) {
		t.Fatalf("habits = %v, want %v", have, want)
	}
	for i := range want {
		if have[i] != want[i] {
			t.Errorf("habit %d = %v, want %v", i, have[i], want[i])
		}
	}
}
