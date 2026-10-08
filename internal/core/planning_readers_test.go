package core

import (
	"strings"
	"testing"
)

// Hand-counted: a one-off due in the window, a monthly series with one slot written and done, one slot moved,
// and a tombstoned task whose rows appear only historically.
func TestTasksAndDeadlinesReadEveryDefinition(t *testing.T) {
	s := fresh(t)
	once := planningTask(t, s, TaskSpec{Label: "Renew passport"})
	o := planningReadOccurrence(t, s, once.ID, "once")
	dated := o.OccurrenceChanges
	dated.DueDay = "2026-10-15"
	planningEditOccurrence(t, s, once, o, dated)
	series := planningTask(t, s, TaskSpec{Label: "Monthly letter", RepeatUnit: "month", RepeatEvery: 1, AnchorDay: "2026-10-01"})
	oct := planningMaterialize(t, s, series, "2026-10-01")
	done := oct.OccurrenceChanges
	done.State, done.CompletedAt = "done", "2026-10-02T10:00:00.000Z"
	planningEditOccurrence(t, s, series, oct, done)
	nov := planningMaterialize(t, s, series, "2026-11-01")
	moved := nov.OccurrenceChanges
	moved.DueDay = "2026-11-20"
	planningEditOccurrence(t, s, series, nov, moved)
	gone := planningTask(t, s, TaskSpec{Label: "Old chore", RepeatUnit: "week", RepeatEvery: 1, AnchorDay: "2026-10-05"})
	if err := s.Do(ctx, "cli", func(tx *Tx) error { return tx.TaskLifecycle(gone.ID, gone.Version, true) }); err != nil {
		t.Fatal(err)
	}

	live, err := s.Tasks(ctx, false)
	if err != nil || len(live) != 2 || live[0].Label != "Renew passport" || live[1].Label != "Monthly letter" {
		t.Fatalf("live tasks: %+v %v", live, err)
	}
	all, err := s.Tasks(ctx, true)
	if err != nil || len(all) != 3 || all[2].DeletedAt == "" {
		t.Fatalf("all tasks: %+v %v", all, err)
	}

	line := func(ds []Deadline) string {
		var out []string
		for _, d := range ds {
			v := ""
			if d.Virtual {
				v = "*"
			}
			out = append(out, d.Label+"/"+d.OccurrenceKey+"@"+d.DueDay+":"+d.State+v)
		}
		return strings.Join(out, " ")
	}
	got, err := s.Deadlines(ctx, "2026-10-01", "2026-12-31", false)
	if err != nil {
		t.Fatal(err)
	}
	// October's slot is done; the one-off is due the 15th; November's slot moved to the 20th; December is virtual.
	// The tombstoned weekly task contributes nothing.
	want := "Monthly letter/2026-10-01@2026-10-01:done Renew passport/once@2026-10-15:open Monthly letter/2026-11-01@2026-11-20:open Monthly letter/2026-12-01@2026-12-01:open*"
	if line(got) != want {
		t.Fatalf("deadlines:\n got %s\nwant %s", line(got), want)
	}
	historical, err := s.Deadlines(ctx, "2026-10-01", "2026-10-31", true)
	if err != nil || len(historical) != 2 {
		t.Fatalf("historical reads of a tombstoned task return persisted rows only (none here): %s %v", line(historical), err)
	}
	if _, err := s.Deadlines(ctx, "2026-12-31", "2026-10-01", false); err == nil {
		t.Fatal("a reversed window was read")
	}
	if _, err := s.Deadlines(ctx, "2026-13-01", "2026-12-01", false); err == nil {
		t.Fatal("a bad day was read")
	}
}
