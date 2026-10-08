package main

import (
	"strings"
	"testing"
)

// The planning shortcuts wrap catalog actions; do reaches the rest.
func TestPlanningShortcutsAndActions(t *testing.T) {
	o, err := parse([]string{"due", "--from", "2031-01-01", "--to", "2031-12-31"})
	if err != nil || o.from != "2031-01-01" || o.to != "2031-12-31" || !commandConsumesContext(o) {
		t.Fatalf("%+v %v", o, err)
	}
	if o, _ := parse([]string{"tasks"}); !commandConsumesContext(o) {
		t.Fatal("tasks does not consume the context")
	}
	c := commandClientWithWorkspace(t)
	task, err := do(c, "create-task", map[string]string{"label": "Write back", "project": "Ana", "repeat_unit": "week", "repeat_every": "2", "anchor_day": "2031-02-03"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Title != "Write back" || cmdHref(task, "project") == "" {
		t.Fatalf("create-task returned %+v", task)
	}
	list := mustEntity(c.Get("/tasks"))
	if len(list.Entities) != 1 || list.Entities[0].Title != "Write back" {
		t.Fatalf("tasks: %+v", list.Entities)
	}
	due, err := do(c, "deadlines", map[string]string{"from": "2031-02-01", "through": "2031-02-28"})
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, e := range due.Entities {
		keys = append(keys, strings.TrimPrefix(e.Href, strings.SplitN(cmdHref(task, "self"), "?", 2)[0]+"/occurrences/"))
	}
	if strings.Join(keys, ",") != "2031-02-03,2031-02-17" {
		t.Fatalf("deadlines: %v", keys)
	}
}
