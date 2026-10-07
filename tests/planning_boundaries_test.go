package tests

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"lifelog/internal/core"
)

func planningBoundaries(s *S) {
	c := planningDB(s, "planning-boundaries")
	task := planningTask(c, nil)
	baseTask := M{"label": "Boundary witness", "repeat_unit": "day", "repeat_every": 1, "anchor_day": "2026-01-01", "source": "ui", "created_at": "2026-01-01T00:00:00.000Z", "updated_at": "2026-01-01T00:00:00.000Z"}
	baseOccurrence := M{"task_id": task, "occurrence_key": "2026-01-02", "source": "ui", "created_at": "2026-01-01T00:00:00.000Z", "updated_at": "2026-01-01T00:00:00.000Z"}
	probe := func(label, table string, extra M, accepted bool) {
		m := M{}
		base := baseTask
		if table == "task_occurrences" {
			base = baseOccurrence
		}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		cols, marks, args := m.split()
		c.must("SAVEPOINT planning_boundary")
		before := planningState(c)
		got := c.tryx("INSERT INTO "+table+"("+strings.Join(cols, ",")+") VALUES("+marks+")", args...)
		unchanged := planningState(c) == before
		c.must("ROLLBACK TO planning_boundary")
		c.must("RELEASE planning_boundary")
		s.K(label, (got == "OK") == accepted && (accepted || unchanged), got)
	}
	for _, tc := range []struct {
		constraint string
		fields     M
	}{
		{"tasks_label", M{"label": "   "}},
		{"tasks_project_entity_type", M{"project_entity_type": "person"}},
		{"tasks_repeat_unit", M{"repeat_unit": "fortnight"}},
		{"tasks_repeat_every", M{"repeat_every": 0}},
		{"tasks_anchor_day", M{"anchor_day": "2026-02-31"}},
		{"tasks_repeat_until_day", M{"repeat_until_day": "2026-02-31"}},
		{"tasks_recurrence", M{"repeat_every": nil}},
		{"tasks_reminder_local_time", M{"reminder_local_time": "24:00", "reminder_zone": "UTC"}},
		{"tasks_reminder_zone", M{"reminder_local_time": "09:00", "reminder_zone": "Europe Berlin"}},
		{"tasks_reminder_pair", M{"reminder_local_time": "09:00"}},
		{"tasks_revision", M{"revision": 0}},
		{"tasks_source", M{"source": "bad source"}},
	} {
		probe(tc.constraint+" rejects invalid value", "tasks", tc.fields, false)
	}
	for _, tc := range []struct {
		constraint string
		fields     M
	}{
		{"task_occurrences_key", M{"occurrence_key": "2026-02-31"}},
		{"task_occurrences_due_day", M{"due_day": "2026-02-31"}},
		{"task_occurrences_state", M{"state": "cancelled"}},
		{"task_occurrences_completion", M{"state": "open", "completed_at": "2026-01-01T00:00:00.000Z"}},
		{"task_occurrences_reminder_mode", M{"reminder_mode": "daily"}},
		{"task_occurrences_reminder_pair", M{"reminder_mode": "at"}},
		{"task_occurrences_revision", M{"revision": 0}},
		{"task_occurrences_source", M{"source": "bad source"}},
	} {
		probe(tc.constraint+" rejects invalid value", "task_occurrences", tc.fields, false)
	}
	for _, tc := range []struct {
		table, field string
		fields       M
	}{
		{"tasks", "label", M{"label": "Visible\x00hidden"}},
		{"tasks", "reminder_local_time", M{"reminder_local_time": "09:00\x00hidden", "reminder_zone": "UTC"}},
		{"tasks", "reminder_zone", M{"reminder_local_time": "09:00", "reminder_zone": "UTC\x00hidden"}},
		{"tasks", "source", M{"source": "ui\x00hidden"}},
		{"task_occurrences", "source", M{"source": "ui\x00hidden"}},
	} {
		probe(tc.table+"_"+tc.field+" rejects embedded NUL", tc.table, tc.fields, false)
	}
	for _, clock := range []string{"00:00", "23:59"} {
		probe("reminder clock accepts "+clock, "tasks", M{"reminder_local_time": clock, "reminder_zone": "UTC"}, true)
	}
	for _, clock := range []string{"9:00", "09:0", "09:00:00", "23:60", "-1:00", " 09:00"} {
		probe("reminder clock refuses "+clock, "tasks", M{"reminder_local_time": clock, "reminder_zone": "UTC"}, false)
	}
	probe("unknown syntactically valid reminder zone retains unresolved intent", "tasks", M{"reminder_local_time": "09:00", "reminder_zone": "Unknown/Nowhere"}, true)
	probe("done may have unknown completion time", "task_occurrences", M{"state": "done"}, true)
	probe("undated occurrence may select absolute reminder", "task_occurrences", M{"reminder_mode": "at", "reminder_override_at": "2026-01-01T00:00:00.000Z"}, true)
	probe("inherit cannot carry absolute reminder", "task_occurrences", M{"reminder_mode": "inherit", "reminder_override_at": "2026-01-01T00:00:00.000Z"}, false)
	probe("off cannot carry absolute reminder", "task_occurrences", M{"reminder_mode": "off", "reminder_override_at": "2026-01-01T00:00:00.000Z"}, false)
	probe("skipped cannot carry completion evidence", "task_occurrences", M{"state": "skipped", "completed_at": "2026-01-01T00:00:00.000Z"}, false)
	for _, fields := range []M{{"repeat_unit": nil}, {"anchor_day": nil}, {"repeat_unit": nil, "repeat_every": nil, "anchor_day": nil, "repeat_until_day": "2026-01-01"}, {"repeat_every": -1}} {
		probe("partial or nonpositive recurrence refused "+fmt.Sprint(fields), "tasks", fields, false)
	}

	// Every exact day/instant boundary comes from its one language-neutral home.
	vectors := regexp.MustCompile(`(?m)^\| (day|instant) \| `+"`([^`]*)`"+` \| (yes|no) \|$`).FindAllStringSubmatch(s.d.Page("contract/exact-time.md"), -1)
	s.K("planning exact-time vectors present", len(vectors) >= 20)
	for _, v := range vectors {
		kind, value, accepted := v[1], v[2], v[3] == "yes"
		if kind == "day" {
			for _, field := range []string{"anchor_day", "repeat_until_day"} {
				probe(fmt.Sprintf("exact tasks.%s %q accepted=%v", field, value, accepted), "tasks", M{field: value}, accepted)
			}
			probe(fmt.Sprintf("exact task_occurrences.due_day %q accepted=%v", value, accepted), "task_occurrences", M{"due_day": value}, accepted)
			continue
		}
		for _, table := range []string{"tasks", "task_occurrences"} {
			for _, field := range []string{"created_at", "updated_at", "deleted_at"} {
				fields := M{field: value}
				if field == "deleted_at" { // created at the earliest instant: the probe tests shape, not order
					fields["created_at"] = "0000-01-01T00:00:00.000Z"
				}
				probe(fmt.Sprintf("exact %s.%s %q accepted=%v", table, field, value, accepted), table, fields, accepted)
			}
		}
		probe(fmt.Sprintf("exact task_occurrences.completed_at %q accepted=%v", value, accepted), "task_occurrences", M{"state": "done", "completed_at": value}, accepted)
		probe(fmt.Sprintf("exact task_occurrences.reminder_override_at %q accepted=%v", value, accepted), "task_occurrences", M{"reminder_mode": "at", "reminder_override_at": value}, accepted)
	}
	planningCalendar(s)
	planningReminderVectors(s)
}

func planningReminderVectors(s *S) {
	inTable, count := false, 0
	for _, line := range strings.Split(s.d.Page("contract/planning.md"), "\n") {
		if line == "| ID | Day | Clock | Zone | Result |" {
			inTable = true
			continue
		}
		if !inTable {
			continue
		}
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 5 || strings.HasPrefix(cells[0], "---") {
			continue
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		task := core.Task{TaskSpec: core.TaskSpec{ReminderLocalTime: cells[2], ReminderZone: cells[3]}}
		occurrence := core.TaskOccurrence{OccurrenceChanges: core.OccurrenceChanges{DueDay: cells[1], State: "open", ReminderMode: "inherit"}}
		got, err := core.ResolveTaskReminder(task, occurrence)
		wantState, wantAt := "resolved", cells[4]
		if cells[4] == "unresolved" {
			wantState, wantAt = "unresolved", ""
		}
		s.K("reminder clock "+cells[0], err == nil && got.State == wantState && got.At == wantAt, got, err)
		count++
	}
	s.K("planning reminder vectors present", count >= 10, count)
}

func planningCalendar(s *S) {
	c := planningDB(s, "planning-calendar")
	lines := strings.Split(s.d.Page("contract/planning.md"), "\n")
	inTable, count := false, 0
	for _, line := range lines {
		if line == "| ID | Anchor | Unit | Every | Until | From | Through | Keys |" {
			inTable = true
			continue
		}
		if !inTable {
			continue
		}
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 8 || strings.HasPrefix(cells[0], "---") {
			continue
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		every, err := strconv.ParseInt(cells[3], 10, 64)
		if err != nil {
			stop("planning interval vector: %v", err)
		}
		var until any
		if cells[4] != "-" {
			until = cells[4]
		}
		task := planningTask(c, M{"repeat_unit": cells[2], "repeat_every": every, "anchor_day": cells[1], "repeat_until_day": until})
		want := map[string]bool{}
		if cells[7] != "-" {
			for _, key := range strings.Split(cells[7], ",") {
				want[key] = true
			}
		}
		from, err := time.Parse("2006-01-02", cells[5])
		if err != nil {
			stop("planning from vector: %v", err)
		}
		through, err := time.Parse("2006-01-02", cells[6])
		if err != nil {
			stop("planning through vector: %v", err)
		}
		// Enumerate short windows independently, including every nonmember date.
		// For the huge-interval vector use its explicit expected key and a bounded
		// set of nonmembers, avoiding millions of redundant database statements.
		var candidates []string
		if through.Sub(from) <= 5*366*24*time.Hour {
			for day := from; !day.After(through); day = day.AddDate(0, 0, 1) {
				candidates = append(candidates, day.Format("2006-01-02"))
			}
		} else {
			candidates = []string{cells[5], "2026-01-02", "2027-01-01", "9999-12-31"}
		}
		for _, key := range candidates {
			c.must("SAVEPOINT calendar_member")
			got := c.tryx("INSERT INTO task_occurrences(task_id,occurrence_key,due_day,source,created_at,updated_at) VALUES(?,?,?,'ui',"+NOW+","+NOW+")", task, key, key)
			c.must("ROLLBACK TO calendar_member")
			c.must("RELEASE calendar_member")
			s.K("calendar "+cells[0]+" key "+key, (got == "OK") == want[key], got)
		}
		count++
	}
	s.K("planning calendar vectors present", count >= 12, count)
}
