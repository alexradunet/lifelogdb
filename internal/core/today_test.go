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

// TestHabitsAreTheDailyMetricsWithNoUnit: a chip is a daily metric with no unit (D31). The days since its last reading
// do not matter. A metric that is not daily is no chip, and nor is a daily metric with a unit.
func TestHabitsAreTheDailyMetricsWithNoUnit(t *testing.T) {
	s := fresh(t)
	today := "2026-10-19"
	for _, m := range []struct {
		name, unit string
		daily      bool
	}{{"Walk", "", true}, {"No phone", "", true}, {"Stretch", "", true}, {"Read", "", false}, {"Sleep", "h", true}, {"Weight", "kg", false}} {
		if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: m.name, Unit: m.unit, Daily: &m.daily}); err != nil {
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
	rec("Walk", today, 0) // the last reading of the day decides
	rec("Walk", today, 3)
	rec("No phone", today, 0)
	rec("Stretch", "2026-08-20", 1) // 60 days ago: still a chip, with nothing today
	rec("Read", "2026-10-18", 1)    // read yesterday, but not daily: no chip
	rec("Sleep", today, 7)          // daily with a unit: a box on the day page, no chip
	rec("Weight", today, 70)
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
