package core

import (
	"context"
	"math"
	"math/rand"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func planningVectors(t *testing.T, header string) [][]string {
	t.Helper()
	body, err := os.ReadFile("../../docs/contract/planning.md")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	var out [][]string
	for line := range strings.SplitSeq(string(body), "\n") {
		if line == header {
			found = true
			continue
		}
		if !found {
			continue
		}
		if !strings.HasPrefix(line, "|") {
			break
		}
		if strings.HasPrefix(line, "|---") {
			continue
		}
		var fields []string
		for _, field := range strings.Split(strings.Trim(line, "|"), "|") {
			field = strings.TrimSpace(field)
			if field == "-" {
				field = ""
			}
			fields = append(fields, field)
		}
		out = append(out, fields)
	}
	if len(out) == 0 {
		t.Fatalf("no planning vectors for %s", header)
	}
	return out
}

// Enumerating each day is intentionally independent of the production expander's
// seek-and-jump algorithm. Fuzz seeds also run during the ordinary baseline.
func FuzzTaskCalendarWindow(f *testing.F) {
	for _, seed := range []int64{0, 1, 7, 31, 2024, 20261007} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, seed int64) {
		rng := rand.New(rand.NewSource(seed))
		anchor := time.Date(2000+rng.Intn(40), time.Month(1+rng.Intn(12)), 1+rng.Intn(31), 0, 0, 0, 0, time.UTC)
		from := anchor.AddDate(0, 0, rng.Intn(1200)-100)
		through := from.AddDate(0, 0, rng.Intn(600))
		spec := TaskSpec{AnchorDay: anchor.Format(time.DateOnly), RepeatUnit: []string{"day", "week", "month", "year"}[rng.Intn(4)], RepeatEvery: int64(1 + rng.Intn(15))}
		if rng.Intn(2) == 0 {
			spec.RepeatUntilDay = anchor.AddDate(0, 0, rng.Intn(1200)-50).Format(time.DateOnly)
		}
		var want, got []string
		for day := from; !day.After(through); day = day.AddDate(0, 0, 1) {
			key := day.Format(time.DateOnly)
			if day.Before(anchor) || spec.RepeatUntilDay != "" && key > spec.RepeatUntilDay {
				continue
			}
			var matches bool
			switch spec.RepeatUnit {
			case "day", "week":
				interval := spec.RepeatEvery
				if spec.RepeatUnit == "week" {
					interval *= 7
				}
				matches = int64(day.Sub(anchor)/(24*time.Hour))%interval == 0
			case "month", "year":
				months := (day.Year()-anchor.Year())*12 + int(day.Month()-anchor.Month())
				interval := spec.RepeatEvery
				if spec.RepeatUnit == "year" {
					interval *= 12
				}
				lastDay := day.AddDate(0, 0, 1).Month() != day.Month()
				matches = int64(months)%interval == 0 && (day.Day() == anchor.Day() || lastDay && day.Day() < anchor.Day())
			}
			if matches {
				want = append(want, key)
			}
		}
		err := taskCalendar(ctx, spec, from.Format(time.DateOnly), through.Format(time.DateOnly), func(key string) error { got = append(got, key); return nil })
		if err != nil || !slices.Equal(got, want) {
			t.Fatalf("seed=%d spec=%+v window=%s..%s got=%v want=%v err=%v", seed, spec, from.Format(time.DateOnly), through.Format(time.DateOnly), got, want, err)
		}
	})
}

func TestTaskCalendarContractVectors(t *testing.T) {
	for _, row := range planningVectors(t, "| ID | Anchor | Unit | Every | Until | From | Through | Keys |") {
		if len(row) != 8 {
			t.Fatalf("bad calendar vector %v", row)
		}
		t.Run(row[0], func(t *testing.T) {
			every, err := strconv.ParseInt(row[3], 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			in := TaskSpec{Label: "Synthetic task", AnchorDay: row[1], RepeatUnit: row[2], RepeatEvery: every, RepeatUntilDay: row[4]}
			var keys []string
			err = taskCalendar(context.Background(), in, row[5], row[6], func(key string) error { keys = append(keys, key); return nil })
			if err != nil || strings.Join(keys, ",") != row[7] {
				t.Fatalf("got %v, %v; want %s", keys, err, row[7])
			}
		})
	}
}

func TestTaskReminderClockContractVectors(t *testing.T) {
	for _, row := range planningVectors(t, "| ID | Day | Clock | Zone | Result |") {
		if len(row) != 5 {
			t.Fatalf("bad reminder vector %v", row)
		}
		t.Run(row[0], func(t *testing.T) {
			task := Task{TaskSpec: TaskSpec{ReminderLocalTime: row[2], ReminderZone: row[3]}}
			o := TaskOccurrence{OccurrenceChanges: OccurrenceChanges{DueDay: row[1], State: "open", ReminderMode: "inherit"}}
			got, err := ResolveTaskReminder(task, o)
			if err != nil {
				t.Fatal(err)
			}
			if row[4] == "unresolved" {
				if got.State != "unresolved" || got.At != "" {
					t.Fatalf("got %+v, want unresolved", got)
				}
			} else if got.State != "resolved" || got.At != row[4] {
				t.Fatalf("got %+v, want %s", got, row[4])
			}
		})
	}
}

func TestTaskCalendarBoundsAndCancellation(t *testing.T) {
	in := TaskSpec{Label: "Synthetic", RepeatUnit: "day", RepeatEvery: 1, AnchorDay: "0000-01-01"}
	for _, pair := range [][2]string{{"", "2026-10-07"}, {"2026-10-08", "2026-10-07"}, {"2026-2-01", "2026-10-07"}} {
		if err := taskCalendar(ctx, in, pair[0], pair[1], func(string) error { t.Fatal("invalid window emitted a key"); return nil }); err == nil {
			t.Fatal("accepted invalid window", pair)
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := taskCalendar(ctx, in, "0000-01-01", "9999-12-31", func(string) error { t.Fatal("cancelled expansion emitted a key"); return nil }); err != context.Canceled {
		t.Fatalf("got %v", err)
	}
	for _, unit := range []string{"day", "week", "month", "year"} {
		in.RepeatUnit, in.RepeatEvery = unit, math.MaxInt64
		var got []string
		if err := taskCalendar(context.Background(), in, "0000-01-01", "9999-12-31", func(key string) error { got = append(got, key); return nil }); err != nil || len(got) != 1 || got[0] != "0000-01-01" {
			t.Fatalf("%s got%v err%v", unit, got, err)
		}
	}
}

func TestTaskReminderModesAndEligibility(t *testing.T) {
	task := Task{TaskSpec: TaskSpec{ReminderLocalTime: "09:00", ReminderZone: "UTC"}}
	base := TaskOccurrence{OccurrenceChanges: OccurrenceChanges{DueDay: "2026-10-07", State: "open", ReminderMode: "inherit"}}
	for _, tc := range []struct {
		name      string
		change    func(*Task, *TaskOccurrence)
		state, at string
	}{
		{"inherit", func(*Task, *TaskOccurrence) {}, "resolved", "2026-10-07T09:00:00.000Z"},
		{"off", func(_ *Task, o *TaskOccurrence) { o.ReminderMode = "off" }, "none", ""},
		{"absolute-undated", func(_ *Task, o *TaskOccurrence) {
			o.DueDay = ""
			o.ReminderMode = "at"
			o.ReminderOverrideAt = "2026-10-08T10:00:00.000Z"
		}, "resolved", "2026-10-08T10:00:00.000Z"},
		{"undated", func(_ *Task, o *TaskOccurrence) { o.DueDay = "" }, "none", ""},
		{"done", func(_ *Task, o *TaskOccurrence) { o.State = "done" }, "none", ""},
		{"skipped", func(_ *Task, o *TaskOccurrence) { o.State = "skipped" }, "none", ""},
		{"task-tombstone", func(p *Task, _ *TaskOccurrence) { p.DeletedAt = "2026-10-01T00:00:00.000Z" }, "none", ""},
		{"occurrence-tombstone", func(_ *Task, o *TaskOccurrence) { o.DeletedAt = "2026-10-01T00:00:00.000Z" }, "none", ""},
		{"project-tombstone", func(p *Task, _ *TaskOccurrence) { p.ProjectDeletedAt = "2026-10-01T00:00:00.000Z" }, "resolved", "2026-10-07T09:00:00.000Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, o := task, base
			tc.change(&p, &o)
			got, err := ResolveTaskReminder(p, o)
			if err != nil || got.State != tc.state || got.At != tc.at {
				t.Fatalf("got%+v err%v", got, err)
			}
		})
	}
}
