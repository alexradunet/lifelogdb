package api_test

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/client"
)

// A whole planning life through the client, in-process and over a socket: the same handler, the same rows.
func TestPlanningLifeThroughTheCatalog(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprintf("remote=%v", remote), func(t *testing.T) {
			c, h := fresh(t)
			if remote {
				srv := httptest.NewServer(h)
				t.Cleanup(srv.Close)
				c = client.Remote(srv.URL, "app:phone")
			}
			root := must(c.Get(t.Context(), "/"))
			if hrefOf(root, "tasks") != "/tasks" || hrefOf(root, "deadlines") != "/deadlines" || find(root, "create-task").Name == "" {
				t.Fatalf("root does not lead to planning: %v %v", root.Links, names(root))
			}
			must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Garden", "body": "the project"}))

			// A one-off due on a day, filed under the project page.
			once := must(c.Do(t.Context(), find(root, "create-task"), map[string]string{"label": "Buy seeds", "project": "Garden", "due_day": "2031-06-10"}))
			if once.Class[0] != "task" || once.Title != "Buy seeds" || hrefOf(once, "project") == "" {
				t.Fatalf("one-off: %+v", once)
			}
			task := props(once)["task"].(map[string]any)
			if task["label"] != "Buy seeds" || task["project_title"] != "Garden" || task["repeat_unit"] != nil {
				t.Fatalf("one-off task: %v", task)
			}
			if _, err := c.Do(t.Context(), find(root, "create-task"), map[string]string{"label": "Bad", "repeat_unit": "month", "due_day": "2031-06-10"}); !clientStatus(err, 422) {
				t.Fatalf("a series with a due day: %v", err)
			}
			// A project no page has is made as an empty page in the same write, and so is a new one on an edit.
			planted := must(c.Do(t.Context(), find(root, "create-task"), map[string]string{"label": "Plant trees", "project": "Orchard"}))
			orchard := must(c.Get(t.Context(), hrefOf(planted, "project")))
			if orchard.Title != "Orchard" || orchard.Class[0] != "page" || props(orchard)["body"] != "" || props(orchard)["is_day_page"] != false {
				t.Fatalf("the project page made for a task: %s %v %v", orchard.Title, orchard.Class, props(orchard))
			}
			if props(planted)["task"].(map[string]any)["project_title"] != "Orchard" {
				t.Fatalf("the task's project: %v", props(planted)["task"])
			}
			moved := must(c.Do(t.Context(), find(planted, "edit-task"), map[string]string{"label": "Plant trees", "project": "Greenhouse"}))
			if page := must(c.Get(t.Context(), hrefOf(moved, "project"))); page.Title != "Greenhouse" || hrefOf(moved, "project") == hrefOf(planted, "project") {
				t.Fatalf("the project page made on an edit: %s", page.Title)
			}
			if _, err := c.Do(t.Context(), find(root, "create-task"), map[string]string{"label": "Bad", "project": "[[Orchard]]"}); !clientStatus(err, 422) {
				t.Fatalf("a project that is not a valid title: %v", err)
			}
			for _, a := range []api.Action{find(root, "create-task"), find(planted, "edit-task")} {
				pages := false
				for _, f := range a.Fields {
					pages = pages || (f.Name == "project" && f.Pages)
				}
				if !pages {
					t.Fatalf("the project field of %s does not say it is a page's title: %+v", a.Name, a.Fields)
				}
			}

			// A monthly series: its slots are virtual until written.
			series := must(c.Do(t.Context(), find(root, "create-task"), map[string]string{"label": "Monthly letter", "repeat_unit": "month", "repeat_every": "1", "anchor_day": "2031-01-31", "reminder_time": "09:00", "reminder_zone": "Europe/Bucharest"}))
			seriesHref := strings.SplitN(hrefOf(series, "self"), "?", 2)[0]
			view := must(c.Get(t.Context(), seriesHref+"?from=2031-01-01&through=2031-03-31"))
			occ := props(view)["occurrences"].([]any)
			if len(occ) != 3 || occ[1].(map[string]any)["key"] != "2031-02-28" || occ[1].(map[string]any)["virtual"] != true {
				t.Fatalf("series window: %v", occ)
			}
			if rem := occ[1].(map[string]any)["reminder"].(map[string]any); rem["state"] != "resolved" || rem["at"] != "2031-02-28T07:00:00.000Z" {
				t.Fatalf("inherited reminder: %v", rem)
			}
			if got := names(view); got != "edit-task,stop-task,capture-occurrence,tombstone-task" {
				t.Fatalf("series actions: %s", got)
			}

			// Follow a virtual slot: it offers materializing, and materializing writes it.
			slot := must(c.Get(t.Context(), seriesHref+"/occurrences/2031-02-28"))
			if names(slot) != "capture-occurrence" || !strings.Contains(strings.Join(slot.Class, " "), "virtual") {
				t.Fatalf("virtual slot: %s %v", names(slot), slot.Class)
			}
			written := must(c.Do(t.Context(), find(slot, "capture-occurrence"), nil))
			if props(written)["virtual"] != false || props(written)["state"] != "open" || props(written)["due_day"] != "2031-02-28" || names(written) != "edit-occurrence,tombstone-occurrence" {
				t.Fatalf("materialized: %v %s", props(written), names(written))
			}
			again := must(c.Do(t.Context(), find(slot, "capture-occurrence"), nil))
			if fmt.Sprint(props(again)["version"]) != fmt.Sprint(props(written)["version"]) {
				t.Fatalf("a second capture changed the row: %v vs %v", props(again)["version"], props(written)["version"])
			}

			// Mark it done now, with the stale version refused first.
			stale := find(written, "edit-occurrence")
			if _, err := c.Do(t.Context(), stale, map[string]string{"state": "done", "completed_at": "now", "version": "0"}); !clientStatus(err, 409) {
				t.Fatalf("stale occurrence version: %v", err)
			} else if code, _ := codeOf(t, err); code != "stale_version" {
				t.Fatalf("stale code=%q", code)
			}
			done := must(c.Do(t.Context(), stale, map[string]string{"state": "done", "completed_at": "now"}))
			if props(done)["state"] != "done" || !strings.HasSuffix(fmt.Sprint(props(done)["completed_at"]), "Z") || props(done)["reminder"].(map[string]any)["state"] != "none" {
				t.Fatalf("done: %v", props(done))
			}
			if _, err := c.Do(t.Context(), find(written, "edit-occurrence"), map[string]string{"state": "done"}); !clientStatus(err, 409) {
				t.Fatal("the first version was accepted twice")
			}

			// Reschedule March, undated, then to a day; then stop the series after March.
			march := must(c.Do(t.Context(), find(view, "capture-occurrence"), map[string]string{"key": "2031-03-31"}))
			undated := must(c.Do(t.Context(), find(march, "edit-occurrence"), map[string]string{"due_day": ""}))
			if props(undated)["due_day"] != nil {
				t.Fatalf("undated: %v", props(undated)["due_day"])
			}
			dated := must(c.Do(t.Context(), find(undated, "edit-occurrence"), map[string]string{"due_day": "2031-04-02"}))
			if props(dated)["due_day"] != "2031-04-02" || props(dated)["key"] != "2031-03-31" {
				t.Fatalf("rescheduled: %v", props(dated))
			}
			current := must(c.Get(t.Context(), seriesHref))
			stopped := must(c.Do(t.Context(), find(current, "stop-task"), map[string]string{"through": "2031-03-31"}))
			if props(stopped)["task"].(map[string]any)["repeat_until_day"] != "2031-03-31" {
				t.Fatalf("stopped: %v", props(stopped)["task"])
			}
			if _, err := c.Do(t.Context(), find(stopped, "stop-task"), map[string]string{"through": "2031-06-30"}); !clientStatus(err, 422) {
				t.Fatalf("an end that extends: %v", err)
			}
			after := must(c.Get(t.Context(), seriesHref+"?from=2031-04-01&through=2031-12-31"))
			if occ := props(after)["occurrences"].([]any); len(occ) != 1 || occ[0].(map[string]any)["key"] != "2031-03-31" {
				t.Fatalf("after the stop only the moved March slot is due: %v", occ)
			}

			// Deadlines across tasks, open by default; every state on request.
			due := must(c.Get(t.Context(), "/deadlines?from=2031-01-01&through=2031-12-31"))
			var labels []string
			for _, o := range props(due)["occurrences"].([]any) {
				m := o.(map[string]any)
				labels = append(labels, fmt.Sprintf("%s/%s@%v", m["label"], m["key"], m["due_day"]))
			}
			if got := strings.Join(labels, " "); got != "Monthly letter/2031-01-31@2031-01-31 Monthly letter/2031-03-31@2031-04-02 Buy seeds/once@2031-06-10" {
				t.Fatalf("deadlines: %s", got)
			}
			all := must(c.Get(t.Context(), "/deadlines?from=2031-01-01&through=2031-12-31&state=all"))
			if n := len(props(all)["occurrences"].([]any)); n != 4 {
				t.Fatalf("all states: %d", n)
			}
			if _, err := c.Get(t.Context(), "/deadlines?from=2031-12-31&through=2031-01-01"); !clientStatus(err, 422) {
				t.Fatal("a reversed window was read")
			}

			// Tombstone the one-off: its work leaves the deadlines, revive brings it back.
			onceHref := strings.SplitN(hrefOf(once, "self"), "?", 2)[0]
			gone := must(c.Do(t.Context(), find(must(c.Get(t.Context(), onceHref)), "tombstone-task"), nil))
			if names(gone) != "revive-task" || !strings.Contains(strings.Join(gone.Class, " "), "deleted") {
				t.Fatalf("tombstoned task: %s %v", names(gone), gone.Class)
			}
			if _, err := c.Get(t.Context(), onceHref+"/occurrences/once"); err != nil {
				t.Fatalf("historical occurrence of a tombstoned task: %v", err)
			}
			due = must(c.Get(t.Context(), "/deadlines?from=2031-01-01&through=2031-12-31"))
			if n := len(props(due)["occurrences"].([]any)); n != 2 {
				t.Fatalf("after the tombstone: %d", n)
			}
			back := must(c.Do(t.Context(), find(gone, "revive-task"), nil))
			if names(back) != "edit-task,capture-occurrence,tombstone-task,edit-occurrence" ||
				back.Actions[3].Href != onceHref+"/occurrences/once/edit" || props(back)["once"].(map[string]any)["key"] != "once" {
				t.Fatalf("revived one-off actions: %s, once %v", names(back), props(back)["once"])
			}
			if _, err := c.Get(t.Context(), onceHref+"/occurrences/2031-01-01"); !clientStatus(err, 404) {
				t.Fatalf("a one-off has no dated slot: %v", err)
			}
			if _, err := c.Get(t.Context(), seriesHref+"/occurrences/nonsense"); !clientStatus(err, 404) {
				t.Fatalf("a bad key: %v", err)
			}

			// Provenance: the remote client's own source is on the rows.
			rows := must(c.Do(t.Context(), find(root, "query"), map[string]string{"sql": "SELECT DISTINCT source FROM tasks"}))
			want := "cli"
			if remote {
				want = "app:phone"
			}
			if got := fmtRows(rows); got != want {
				t.Fatalf("source=%q", got)
			}
		})
	}
}

func TestPlanningViewsRender(t *testing.T) {
	c, h := fresh(t)
	root := must(c.Get(t.Context(), "/"))
	series := must(c.Do(t.Context(), find(root, "create-task"), map[string]string{"label": "Water plants", "repeat_unit": "week", "repeat_every": "1", "anchor_day": "2031-01-06"}))
	href := strings.SplitN(hrefOf(series, "self"), "?", 2)[0]
	for path, want := range map[string][]string{
		"/tasks": {"Water plants", `action="/tasks"`},
		href + "?from=2031-01-01&through=2031-01-31":    {`action="` + href + `/stop"`, `name="version" value="`, `action="` + href + `/occurrences"`, "2031-01-13"},
		href + "/occurrences/2031-01-13":                {`action="` + href + `/occurrences"`, `name="key" value="2031-01-13"`},
		"/deadlines?from=2031-01-01&through=2031-01-31": {"Water plants", "and 3 more", `action="/deadlines"`},
	} {
		body := browse(t, h, path)
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("%s lacks %q:\n%.800s", path, w, body)
			}
		}
	}
}
