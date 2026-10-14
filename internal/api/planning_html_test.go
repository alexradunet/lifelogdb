package api

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

// occ is an occurrence as the JSON client reads it, for the view functions.
func occ(task float64, key, due, state string) map[string]any {
	o := map[string]any{"task_id": task, "key": key, "state": state, "virtual": false}
	if due != "" {
		o["due_day"] = due
	}
	return o
}

// TestDueGroupsPutEachOccurrenceInItsGroup checks the groups of a list against a hand-made table: the edges of
// today and of the week after it, the states, an undated one, and a series folded in Overdue and in Later.
func TestDueGroupsPutEachOccurrenceInItsGroup(t *testing.T) {
	list := []any{
		occ(1, "2026-10-01", "2026-10-01", "open"),    // overdue
		occ(1, "2026-10-04", "2026-10-04", "open"),    // overdue, the same series again
		occ(7, "once", "2026-10-05", "open"),          // overdue, another task
		occ(1, "2026-10-07", "2026-10-07", "open"),    // and the series again
		occ(2, "once", "2026-10-11", "done"),          // earlier: settled before today
		occ(3, "once", "2026-10-12", "open"),          // today
		occ(1, "2026-10-13", "2026-10-13", "skipped"), // next 7 days, whatever its state
		occ(4, "once", "2026-10-19", "open"),          // the last day of the week after today
		occ(1, "2026-10-20", "2026-10-20", "open"),    // later: the first day after that week
		occ(5, "once", "2026-11-02", "open"),          // later, another task
		occ(1, "2026-10-27", "2026-10-27", "open"),    // later, the same series again
		occ(1, "2026-11-03", "2026-11-03", "open"),    // and again
		occ(6, "once", "", "open"),                    // no due day
	}
	show := func(gs []dueGroup) string {
		var out []string
		for _, g := range gs {
			var items []string
			for _, it := range g.Items {
				o := it.Occurrence.(map[string]any)
				s := fmt.Sprintf("%v/%v", o["task_id"], o["key"])
				if it.More > 0 {
					s += fmt.Sprintf("+%d", it.More)
				}
				items = append(items, s)
			}
			out = append(out, g.Name+": "+strings.Join(items, " "))
		}
		return strings.Join(out, " | ")
	}
	for _, c := range []struct {
		fold bool
		want string
	}{
		{true, "Overdue: 1/2026-10-01+2 7/once | Earlier: 2/once | Today: 3/once | Next 7 days: 1/2026-10-13 4/once | " +
			"Later: 1/2026-10-20+2 5/once | No due day: 6/once"},
		{false, "Overdue: 1/2026-10-01 1/2026-10-04 7/once 1/2026-10-07 | Earlier: 2/once | Today: 3/once | Next 7 days: 1/2026-10-13 4/once | " +
			"Later: 1/2026-10-20 5/once 1/2026-10-27 1/2026-11-03 | No due day: 6/once"},
	} {
		if got := show(dueGroups(list, "2026-10-12", c.fold)); got != c.want {
			t.Errorf("fold=%v:\n got %s\nwant %s", c.fold, got, c.want)
		}
	}
	if got := dueGroups([]any{}, "2026-10-12", true); len(got) != 0 {
		t.Errorf("no occurrence makes groups: %v", got)
	}
}

func TestDueWordsSayADayAgainstToday(t *testing.T) {
	for _, c := range []struct{ due, want string }{
		{"2026-10-12", "today"},
		{"2026-10-13", "tomorrow · Tue 13 Oct"},
		{"2026-10-11", "yesterday · Sun 11 Oct"},
		{"2026-10-19", "in 7 days · Mon 19 Oct"},
		{"2026-10-20", "Tue 20 Oct"},
		{"2026-10-05", "7 days ago · Mon 5 Oct"},
		{"2026-10-04", "Sun 4 Oct"},
		{"2027-01-01", "Fri 1 Jan 2027"},
		{"", "no due day"},
		{"not a day", "not a day"},
	} {
		if got := dueWords(c.due, "2026-10-12"); got != c.want {
			t.Errorf("dueWords(%q) = %q, want %q", c.due, got, c.want)
		}
	}
	if got := dueWords("2026-10-13", ""); got != "Tue 13 Oct 2026" {
		t.Errorf("without today: %q", got)
	}
}

func TestCadenceAndReminderWords(t *testing.T) {
	for _, c := range []struct {
		task map[string]any
		want string
	}{
		{map[string]any{"label": "Buy seeds"}, "once"},
		{map[string]any{"repeat_unit": "day", "repeat_every": 3.0, "anchor_day": "2026-10-05"}, "every 3 days from Mon 5 Oct"},
		{map[string]any{"repeat_unit": "month", "repeat_every": 1.0, "anchor_day": "2026-10-01", "repeat_until_day": "2027-06-30"},
			"every month from Thu 1 Oct until Wed 30 Jun 2027"},
		{map[string]any{"repeat_unit": "week", "repeat_every": 2.0, "anchor_day": "2026-10-05"}, "every 2 weeks from Mon 5 Oct"},
	} {
		if got := cadence(c.task, clock{today: "2026-10-12"}); got != c.want {
			t.Errorf("cadence(%v) = %q, want %q", c.task, got, c.want)
		}
	}
	for _, c := range []struct {
		x    map[string]any
		want string
	}{
		{map[string]any{"reminder_time": "09:00", "reminder_zone": "Europe/Bucharest"}, "09:00 Europe/Bucharest"},
		{map[string]any{"label": "no reminder"}, ""},
		{map[string]any{"reminder": map[string]any{"state": "resolved", "at": "2026-11-01T07:00:00.000Z"}}, "Sun 1 Nov, 07:00 UTC"},
		{map[string]any{"reminder": map[string]any{"state": "none"}, "reminder_time": "09:00"}, ""},
		{map[string]any{"reminder": map[string]any{"state": "unresolved"}}, "unresolved"},
	} {
		if got := reminderWords(c.x, clock{today: "2026-10-12"}); got != c.want {
			t.Errorf("reminderWords(%v) = %q, want %q", c.x, got, c.want)
		}
	}
}

// TestNumWritesNumbersInFull keeps an id a path can carry: the JSON client reads every number as a float64.
func TestNumWritesNumbersInFull(t *testing.T) {
	for _, c := range []struct {
		v    any
		want string
	}{
		{1000000.0, "1000000"}, {123456789012.0, "123456789012"}, {71.1, "71.1"}, {0.0000001, "0.0000001"},
		{-2.5, "-2.5"}, {nil, ""}, {"text", "text"}, {int64(7), "7"}, {true, "true"},
	} {
		if got := num(c.v); got != c.want {
			t.Errorf("num(%v) = %q, want %q", c.v, got, c.want)
		}
	}
}

// TestOutcomesAreOneButtonForms draws the outcome forms of a written occurrence: every field the entity filled is
// kept hidden, the state and completion are fixed, and the form asks to return to the page that drew it. No button
// is primary, and each form's class names its outcome (docs/plans/095-the-leaf-and-its-margin.md).
func TestOutcomesAreOneButtonForms(t *testing.T) {
	a := action("edit-occurrence", map[string]string{"id": "4", "key": "once"}, map[string]any{"task_version": "3", "version": "7",
		"due_day": "2026-10-15", "state": "open", "reminder_mode": "inherit"})
	draw := func(state string) string {
		forms, err := outcomes(&a, state, "/deadlines?from=2026-10-01")
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		for _, f := range forms {
			if len(f.Shown) != 0 {
				t.Errorf("%s shows fields: %v", f.Button, f.Shown)
			}
			if err := pages.ExecuteTemplate(&b, "form", f); err != nil {
				t.Fatal(err)
			}
		}
		return b.String()
	}
	open := draw("open")
	for _, want := range []string{`action="/tasks/4/occurrences/once/edit"`, `class="tiny outcome-done">`, `<button>Done</button>`,
		`class="tiny outcome-skip">`, `<button>Skip</button>`,
		`name="state" value="done"`, `name="completed_at" value="now"`, `name="state" value="skipped"`, `name="version" value="7"`,
		`name="task_version" value="3"`, `name="due_day" value="2026-10-15"`, `name="back" value="/deadlines?from=2026-10-01"`} {
		if !strings.Contains(open, want) {
			t.Errorf("open occurrence forms lack %s:\n%s", want, open)
		}
	}
	if strings.Contains(open, "primary") {
		t.Errorf("an outcome is a primary button: %s", open)
	}
	if done := draw("done"); !strings.Contains(done, `class="tiny outcome-reopen">`) || !strings.Contains(done, `<button>Reopen</button>`) ||
		!strings.Contains(done, `name="state" value="open"`) || strings.Contains(done, "Done</button>") {
		t.Errorf("a done occurrence offers: %s", done)
	}
	if forms, err := outcomes(nil, "open", "/"); forms != nil || err != nil {
		t.Errorf("no action draws forms: %v %v", forms, err)
	}
}

// TestHereDropsTheReceipt: a form returns to the page it is on, but never with the receipt that page already took.
func TestHereDropsTheReceipt(t *testing.T) {
	for raw, want := range map[string]string{
		"/deadlines?feedback=abc&from=2026-10-01": "/deadlines?from=2026-10-01",
		"/tasks/4?feedback=abc":                   "/tasks/4",
		"/":                                       "/",
		"/pages/Caf%C3%A9":                        "/pages/Caf%C3%A9",
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := hereOf(u); got != want {
			t.Errorf("hereOf(%s) = %s, want %s", raw, got, want)
		}
	}
}

// TestARowIsNamedForItsList: in a list of many tasks a row is its task's label and says when; on a task's own page
// it is named by when, and says its slot only when it was moved off it.
func TestARowIsNamedForItsList(t *testing.T) {
	v := &view{Entity: &Entity{}, Props: map[string]any{"today": "2026-10-12"}, Here: "/tasks/2"}
	for _, c := range []struct {
		o          map[string]any
		name, when string
	}{
		{map[string]any{"label": "Buy seeds", "key": "once", "due_day": "2026-10-15", "state": "open", "task_id": 4.0}, "Buy seeds", "in 3 days · Thu 15 Oct"},
		{map[string]any{"key": "2026-10-12", "due_day": "2026-10-12", "state": "open", "task_id": 2.0}, "Today", ""},
		{map[string]any{"key": "2026-10-11", "due_day": "2026-10-14", "state": "open", "task_id": 2.0}, "In 2 days · Wed 14 Oct", "moved from the slot of Sun 11 Oct"},
		{map[string]any{"key": "once", "state": "open", "task_id": 5.0}, "No due day", ""},
	} {
		row, err := v.Occ(c.o, 0)
		if err != nil {
			t.Fatal(err)
		}
		if row.Name != c.name || row.When != c.when {
			t.Errorf("%v: name %q when %q, want %q %q", c.o, row.Name, row.When, c.name, c.when)
		}
	}
}

// TestWeeksStartOnMonday lays months out by hand-checked first weekdays: a month that starts on a Monday, one that
// starts on a Sunday, and a February of four whole weeks.
func TestWeeksStartOnMonday(t *testing.T) {
	month := func(first string, n int) []any {
		t0, _ := time.Parse(time.DateOnly, first)
		var out []any
		for i := range n {
			out = append(out, map[string]any{"day": t0.AddDate(0, 0, i).Format(time.DateOnly)})
		}
		return out
	}
	show := func(ws [][]any) string {
		var rows []string
		for _, w := range ws {
			var cells []string
			for _, d := range w {
				if d == nil {
					cells = append(cells, "--")
				} else {
					cells = append(cells, d.(map[string]any)["day"].(string)[8:])
				}
			}
			rows = append(rows, strings.Join(cells, " "))
		}
		return strings.Join(rows, " / ")
	}
	for _, c := range []struct {
		first string
		n     int
		want  string
	}{
		{"2026-06-01", 30, "01 02 03 04 05 06 07 / 08 09 10 11 12 13 14 / 15 16 17 18 19 20 21 / 22 23 24 25 26 27 28 / 29 30 -- -- -- -- --"},
		{"2026-11-01", 30, "-- -- -- -- -- -- 01 / 02 03 04 05 06 07 08 / 09 10 11 12 13 14 15 / 16 17 18 19 20 21 22 / 23 24 25 26 27 28 29 / 30 -- -- -- -- -- --"},
		{"2027-02-01", 28, "01 02 03 04 05 06 07 / 08 09 10 11 12 13 14 / 15 16 17 18 19 20 21 / 22 23 24 25 26 27 28"},
	} {
		if got := show(weeks(month(c.first, c.n))); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.first, got, c.want)
		}
	}
	if weeks([]any{}) != nil {
		t.Error("no day makes weeks")
	}
}
