package main

import (
	"strings"
	"testing"
	"time"

	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/db/dbtest"
)

// TestPlanCommand is `lifelog plan TEXT...` on a throwaway database: the words are joined by spaces, the page is today
// when --page is absent, and the flags become the tokens of the line. A flag of another command is refused.
func TestPlanCommand(t *testing.T) {
	live := dbtest.File(t)
	e, _, _ := testEnv(t)
	today, err := time.ParseInLocation(time.DateOnly, parityToday, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	e.clock = func() time.Time { return today.Add(12 * time.Hour) }
	run := func(args ...string) error { return runArgs(t.Context(), e, append(args, "--db", live)) }
	if err := run("plan", "Call", "the", "bank", "--due", "2031-05-24"); err != nil {
		t.Fatal(err)
	}
	if err := run("plan", "Pay", "rent", "--page", "2031-W21", "--on", "2031-05-19", "--every", "month", "--from-done"); err != nil {
		t.Fatal(err)
	}
	if err := run("plan", "Never", "--mood", "3"); err == nil || !strings.Contains(err.Error(), "does not take --mood") {
		t.Errorf("plan --mood: %v, want a refusal of the flag", err)
	}
	d, err := db.Open(t.Context(), live)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := &core.Store{DB: d}
	for title, want := range map[string]string{
		parityToday: "- [ ] Call the bank due:2031-05-24",
		"2031-W21":  "- [ ] Pay rent on:2031-05-19 every:month from:done",
	} {
		id, err := s.PageID(t.Context(), title)
		if err != nil || id == 0 {
			t.Fatalf("the page %s: %d %v", title, id, err)
		}
		if p, err := s.PageByID(t.Context(), id); err != nil || p.Body != want {
			t.Errorf("the page %s: %v %q, want %q", title, err, p.Body, want)
		}
	}
}
