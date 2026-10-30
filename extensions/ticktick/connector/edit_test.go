package connector

import (
	"strings"
	"testing"
)

// An edit sends the whole task back. TickTick may replace the task on update, so a field that the body lacks is lost.
func TestEditKeepsTheFieldsThatTheProtocolDoesNotKnow(t *testing.T) {
	env := newServiceEnv(t)
	env.tick.add("t1", "p-home", "Pay rent")
	env.tick.keep("t1", map[string]any{"priority": 5.0, "tags": []any{"home", "money"}, "repeatFlag": "RRULE:FREQ=MONTHLY"})
	if _, err := env.do(t, "edit-task", map[string]string{"id": "t1", "title": "Pay the rent"}); err != nil {
		t.Fatal(err)
	}
	got := env.tick.wire(env.tick.tasks["t1"])
	if got["title"] != "Pay the rent" || got["priority"] != 5.0 || got["repeatFlag"] != "RRULE:FREQ=MONTHLY" {
		t.Errorf("task in TickTick = %v, want the new title and the priority and the repeat kept", got)
	}
	if tags, _ := got["tags"].([]any); len(tags) != 2 {
		t.Errorf("tags = %v, want both kept", got["tags"])
	}
}

func TestEditOfADoneTaskKeepsItsStatusAndItsCompletionTime(t *testing.T) {
	env := newServiceEnv(t)
	env.tick.add("t1", "p-home", "Pay rent")
	env.tick.finish("t1", now)
	want := env.tick.tasks["t1"].CompletedTime
	if _, err := env.do(t, "edit-task", map[string]string{"id": "t1", "title": "Pay the rent", "notes": "x"}); err != nil {
		t.Fatal(err)
	}
	tk := env.tick.tasks["t1"]
	if tk.Status != 2 || tk.CompletedTime != want || tk.Title != "Pay the rent" {
		t.Errorf("task in TickTick = %+v, want status 2 and completedTime %s", tk, want)
	}
}

func TestReopenKeepsTheOtherFields(t *testing.T) {
	env := newServiceEnv(t)
	env.tick.add("t1", "p-home", "Pay rent")
	env.tick.keep("t1", map[string]any{"priority": 3.0})
	env.tick.finish("t1", now)
	if _, err := env.do(t, "reopen-task", map[string]string{"id": "t1"}); err != nil {
		t.Fatal(err)
	}
	raw := env.tick.rawUpdates[len(env.tick.rawUpdates)-1]
	if _, there := raw["completedTime"]; there || raw["status"] != 0.0 || raw["priority"] != 3.0 {
		t.Errorf("body = %v, want status 0, no completedTime and the priority", raw)
	}
}

// A task with start = due that moves to a new day must not get a span.
func TestANewDueDayMovesTheStartDayToo(t *testing.T) {
	env := newServiceEnv(t)
	tk := env.tick.add("t1", "p-home", "Pay rent")
	tk.DueDate, tk.StartDate, tk.IsAllDay = "2030-10-30T00:00:00.000+0000", "2030-10-30T00:00:00.000+0000", true
	if _, err := env.do(t, "edit-task", map[string]string{"id": "t1", "due": "2030-11-05"}); err != nil {
		t.Fatal(err)
	}
	if tk.DueDate != tk.StartDate || !tk.IsAllDay || !strings.HasPrefix(tk.DueDate, "2030-11-05T00:00:00.000") {
		t.Errorf("due = %q, start = %q, allDay = %v, want one instant on 2030-11-05", tk.DueDate, tk.StartDate, tk.IsAllDay)
	}
}

func TestAnAddedDueDayUsesTheNameOfTheLocalZone(t *testing.T) {
	t.Setenv("TZ", "Europe/Paris")
	env := newServiceEnv(t)
	if _, err := env.do(t, "add-task", map[string]string{"title": "x", "due": "2030-03-09"}); err != nil {
		t.Fatal(err)
	}
	if len(env.tick.tasks) != 1 {
		t.Fatalf("%d tasks in TickTick, want 1", len(env.tick.tasks))
	}
	for _, m := range env.tick.tasks {
		if m.TimeZone != "Europe/Paris" || m.DueDate != m.StartDate || !strings.HasPrefix(m.DueDate, "2030-03-09T00:00:00.000+0100") {
			t.Errorf("task in TickTick = %+v, want zone Europe/Paris and midnight +0100", m)
		}
	}
}

func TestAnEditUsesTheZoneOfTheTask(t *testing.T) {
	t.Setenv("TZ", "Europe/Paris")
	env := newServiceEnv(t)
	tk := env.tick.add("t1", "p-home", "Pay rent")
	tk.TimeZone = "Asia/Tokyo"
	if _, err := env.do(t, "edit-task", map[string]string{"id": "t1", "due": "2030-03-09"}); err != nil {
		t.Fatal(err)
	}
	if tk.TimeZone != "Asia/Tokyo" || !strings.HasPrefix(tk.DueDate, "2030-03-09T00:00:00.000+0900") {
		t.Errorf("zone = %q, due = %q, want Asia/Tokyo and midnight +0900", tk.TimeZone, tk.DueDate)
	}
}

func TestZoneNames(t *testing.T) {
	for _, tc := range []struct {
		name, task, tz, link, want string
	}{
		{"the zone of the task", "Asia/Tokyo", "Europe/Paris", "", "Asia/Tokyo"},
		{"the task says Local", "Local", "Europe/Paris", "", "Europe/Paris"},
		{"no zone in the task, TZ", "", "Europe/Paris", "/usr/share/zoneinfo/Asia/Tokyo", "Europe/Paris"},
		{"TZ with a colon", "", ":Europe/Paris", "", "Europe/Paris"},
		{"TZ does not load", "", "Nowhere/Land", "/usr/share/zoneinfo/Asia/Tokyo", "Asia/Tokyo"},
		{"the link", "", "", "../usr/share/zoneinfo/America/New_York", "America/New_York"},
		{"the link is no zone", "", "", "/etc/other", ""},
		{"the link names a zone that does not load", "", "", "/usr/share/zoneinfo/Nowhere/Land", ""},
		{"nothing", "", "", "", ""},
		{"TZ is Local", "", "Local", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := zoneName(tc.task, tc.tz, tc.link); got != tc.want {
				t.Errorf("zoneName = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTheNameOfTheTaskManagerDoesNotReachTheClient(t *testing.T) {
	env := newServiceEnv(t)
	env.tick.add("t1", "p-home", "Pay rent")
	if _, err := env.c.Get(t.Context(), "/open"); err != nil { // the service now knows where t1 is
		t.Fatal(err)
	}
	env.tick.failRead = true
	_, err := env.do(t, "complete-task", map[string]string{"id": "t1"})
	wantError(t, err, 502, "upstream", "the task manager answered 500")
}
