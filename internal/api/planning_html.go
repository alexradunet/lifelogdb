package api

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The browser's planning views (docs/plans/091-the-browser-for-daily-use.md): an occurrence in words, its outcome
// as one-button forms, and the groups a list of occurrences is shown in. Each function reads the properties as the
// JSON client reads them, and each form is an action the entity offers.

// OccurrenceAction is the outcome action the entity offers for an occurrence it shows: capture-occurrence for its
// virtual slot, edit-occurrence for its row. nil when the entity offers none (a later one, a tombstoned one).
func (v *view) OccurrenceAction(item any) *Action {
	o, _ := item.(map[string]any)
	key, _ := o["key"].(string)
	virtual, _ := o["virtual"].(bool)
	task := "/tasks/" + num(o["task_id"])
	for i := range v.Actions {
		a := &v.Actions[i]
		switch {
		case virtual && a.Name == "capture-occurrence" && a.Href == task+"/occurrences" && fieldValue(a, "key") == key:
			return a
		case !virtual && a.Name == "edit-occurrence" && a.Href == task+"/occurrences/"+url.PathEscape(key)+"/edit":
			return a
		}
	}
	return nil
}

// fieldValue is the value an action filled in for a field, "" when it filled none.
func fieldValue(a *Action, name string) string {
	for _, f := range a.Fields {
		if f.Name == name {
			return num(f.Value)
		}
	}
	return ""
}

// occRow is one occurrence as a list draws it: the occurrence, its name and when it is due in words, the one-button
// forms of its outcome when the entity offers it, and how many later slots of the same series it stands for.
type occRow struct {
	O          map[string]any
	Name, When string
	Forms      []*formView
	More       int
}

// Occ is the row of an occurrence of this entity; its forms return to this page. In a list of many tasks the row is
// named by its task's label and says when it is due. On a task's own page every row is that task, so the row is
// named by when it is due, and says the slot only when the occurrence was moved off it.
func (v *view) Occ(item any, more int) (occRow, error) {
	o, _ := item.(map[string]any)
	props, _ := v.Props.(map[string]any)
	today, _ := props["today"].(string)
	state, _ := o["state"].(string)
	forms, err := outcomes(v.OccurrenceAction(item), state, v.Here)
	row := occRow{O: o, Forms: forms, More: more, When: dueWords(o["due_day"], today)}
	row.Name, _ = o["label"].(string)
	if row.Name == "" {
		row.Name, row.When = strings.ToUpper(row.When[:1])+row.When[1:], ""
		key, _ := o["key"].(string)
		if due, _ := o["due_day"].(string); key != "once" && due != key {
			row.When = "moved from the slot of " + dueWords(key, "")
		}
	}
	return row, err
}

// outcomes are the one-button forms of an occurrence's outcome action: Done and Skip while it is open, Reopen once
// it is done or skipped. Every field keeps the value the entity filled in, except the state and the completion; a
// done one is completed now. back is the page the browser returns to.
func outcomes(a *Action, state, back string) ([]*formView, error) {
	if a == nil {
		return nil, nil
	}
	type choice struct{ button, state, completed, primary string }
	choices := []choice{{"Reopen", "open", "", "0"}}
	if state == "open" {
		choices = []choice{{"Done", "done", "now", "1"}, {"Skip", "skipped", "", "0"}}
	}
	var out []*formView
	for _, c := range choices {
		f, err := formOf(a, "state", c.state, "completed_at", c.completed, "button", c.button, "class", "tiny", "back", back, "primary", c.primary)
		if err != nil {
			return nil, err
		}
		f.Hidden, f.Shown = append(f.Hidden, f.Shown...), nil
		out = append(out, f)
	}
	return out, nil
}

// dueGroup is a group of a list of occurrences, by its due day against today.
type dueGroup struct {
	Name  string
	Items []dueItem
}

// dueItem is an occurrence of a group, and the number of later slots of its series it stands for (folded lists).
type dueItem struct {
	Occurrence any
	More       int
}

// dueGroups puts occurrences, already in due-day order, into groups: Overdue (open, due before today), Earlier
// (done or skipped, due before today), Today, Next 7 days, Later, and No due day. A group without an occurrence is
// left out. With fold, Later shows a series once, at its first slot, and counts the slots it stands for: a series
// every few days must not hide the one-off tasks of the months ahead.
func dueGroups(list any, today string, fold bool) []dueGroup {
	names := []string{"Overdue", "Earlier", "Today", "Next 7 days", "Later", "No due day"}
	groups := make([][]dueItem, len(names))
	week := ""
	if t, err := time.Parse(time.DateOnly, today); err == nil {
		week = t.AddDate(0, 0, actionableDays).Format(time.DateOnly)
	}
	first := map[string]int{} // a series in Later: where its first slot is
	items, _ := list.([]any)
	for _, it := range items {
		o, _ := it.(map[string]any)
		due, _ := o["due_day"].(string)
		state, _ := o["state"].(string)
		g := 4
		switch {
		case due == "":
			g = 5
		case due < today && state == "open":
			g = 0
		case due < today:
			g = 1
		case due == today:
			g = 2
		case due <= week:
			g = 3
		}
		if g == 4 && fold {
			task := num(o["task_id"])
			if i, ok := first[task]; ok {
				groups[g][i].More++
				continue
			}
			first[task] = len(groups[g])
		}
		groups[g] = append(groups[g], dueItem{Occurrence: it})
	}
	var out []dueGroup
	for i, g := range groups {
		if len(g) > 0 {
			out = append(out, dueGroup{Name: names[i], Items: g})
		}
	}
	return out
}

// dueWords says a due day against today: "today", "tomorrow (Tue 2026-10-13)", "in 3 days (Thu 2026-10-15)",
// "4 days ago (Thu 2026-10-08)"; a day more than a week away is "Mon 2026-11-02"; no day is "no due day". The due
// day is a property as the JSON client reads it: absent (nil) for an undated occurrence.
func dueWords(dueDay any, today string) string {
	due, _ := dueDay.(string)
	d, err1 := time.Parse(time.DateOnly, due)
	t, err2 := time.Parse(time.DateOnly, today)
	switch {
	case due == "":
		return "no due day"
	case err1 != nil:
		return due
	case err2 != nil:
		return d.Format("Mon 2006-01-02")
	}
	n := int(d.Sub(t).Hours() / 24)
	date := d.Format("Mon 2006-01-02")
	switch {
	case n == 0:
		return "today"
	case n == 1:
		return "tomorrow (" + date + ")"
	case n == -1:
		return "yesterday (" + date + ")"
	case n > 1 && n <= 7:
		return fmt.Sprintf("in %d days (%s)", n, date)
	case n < -1 && n >= -7:
		return fmt.Sprintf("%d days ago (%s)", -n, date)
	}
	return date
}

// cadence says a task's cadence: "every 3 days from 2026-10-05", "every month from 2026-10-01 until 2027-06-30";
// a one-off is "once".
func cadence(task any) string {
	t, _ := task.(map[string]any)
	unit, _ := t["repeat_unit"].(string)
	if unit == "" {
		return "once"
	}
	n, _ := t["repeat_every"].(float64)
	s := "every " + unit
	if n > 1 {
		s = "every " + strconv.FormatFloat(n, 'f', -1, 64) + " " + unit + "s"
	}
	if from, _ := t["anchor_day"].(string); from != "" {
		s += " from " + from
	}
	if until, _ := t["repeat_until_day"].(string); until != "" {
		s += " until " + until
	}
	return s
}

// reminderWords says a reminder: a task's default clock ("09:00 Europe/Bucharest"), or an occurrence's resolved
// reminder ("2026-11-01 07:00 UTC"; "unresolved" when its zone cannot name the clock); "" when there is none.
func reminderWords(x any) string {
	m, _ := x.(map[string]any)
	if r, ok := m["reminder"].(map[string]any); ok {
		switch r["state"] {
		case "resolved":
			at, _ := r["at"].(string)
			if t, err := time.Parse(time.RFC3339Nano, at); err == nil {
				return t.UTC().Format("2006-01-02 15:04") + " UTC"
			}
			return at
		case "unresolved":
			return "unresolved"
		}
		return ""
	}
	clock, _ := m["reminder_time"].(string)
	zone, _ := m["reminder_zone"].(string)
	return strings.TrimSpace(clock + " " + zone)
}
