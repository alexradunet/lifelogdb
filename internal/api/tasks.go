package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"lifelog/internal/core"
)

// Planning on the wire (docs/plans/081-planning-on-every-surface.md): tasks, their occurrences and the deadlines
// of a window, as resources and catalog actions, so the CLI, the MCP tools and the browser carry them alike. The
// rules are the planning contract's (docs/contract/planning.md); core implements them, this file only shapes them.

// occurrenceView is an occurrence as the API shows it: the row (or the virtual slot), its task's label when the
// view is not the task's own, and its reminder resolved.
type occurrenceView struct {
	core.TaskOccurrence
	Label    string            `json:"label,omitempty"`
	Reminder core.TaskReminder `json:"reminder"`
}

func viewOccurrence(task core.Task, o core.TaskOccurrence, label string) occurrenceView {
	v := occurrenceView{TaskOccurrence: o, Label: label}
	if rem, err := core.ResolveTaskReminder(task, o); err == nil {
		v.Reminder = rem
	} else {
		v.Reminder = core.TaskReminder{State: "unresolved"}
	}
	return v
}

// The default window of a task view and of the deadlines: a month back, three months on.
const windowBack, windowOn = 30, 90

func taskWindow(q url.Values) (from, through string, err error) {
	today, _ := time.Parse(time.DateOnly, core.Today())
	from, through = q.Get("from"), q.Get("through")
	if from == "" {
		from = today.AddDate(0, 0, -windowBack).Format(time.DateOnly)
	}
	if through == "" {
		through = today.AddDate(0, 0, windowOn).Format(time.DateOnly)
	}
	if !core.IsDay(from) || !core.IsDay(through) || from > through {
		return "", "", &core.Error{Status: 422, Msg: "from and through must be exact days, from first"}
	}
	return from, through, nil
}

func flag(q url.Values, name string) (bool, error) {
	switch q.Get(name) {
	case "", "0":
		return false, nil
	case "1":
		return true, nil
	}
	return false, &core.Error{Status: 422, Msg: name + " must be 0 or 1"}
}

func taskHref(id int64) string { return "/tasks/" + strconv.FormatInt(id, 10) }
func occurrenceHref(id int64, key string) string {
	return taskHref(id) + "/occurrences/" + url.PathEscape(key)
}
func occurrenceTitle(o core.TaskOccurrence) string {
	t := o.OccurrenceKey + " · " + o.State
	if o.Virtual {
		t += " (not yet written)"
	}
	if o.DueDay != "" && o.DueDay != o.OccurrenceKey {
		t += " · due " + o.DueDay
	}
	return t
}

// ---- reads

func (h *server) tasks(r *http.Request) (*Entity, error) {
	deleted, err := flag(r.URL.Query(), "include_deleted")
	if err != nil {
		return nil, err
	}
	ts, err := h.s.Tasks(r.Context(), deleted)
	if err != nil {
		return nil, err
	}
	e := &Entity{Class: []string{"tasks"}, Title: "Tasks", Properties: map[string]any{"tasks": ts, "include_deleted": deleted},
		Links:   []Link{link("self", "/tasks", "Tasks"), link("deadlines", "/deadlines", "Deadlines"), link("index", "/", "Home")},
		Actions: []Action{action("create-task", nil, nil), action("deadlines", nil, nil)}}
	for _, t := range ts {
		e.Entities = append(e.Entities, link("item", taskHref(t.ID), t.Label))
	}
	return e, nil
}

func (h *server) task(r *http.Request) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	return h.taskEntity(r.Context(), id, r.URL.Query())
}

func (h *server) taskEntity(ctx context.Context, id int64, q url.Values) (*Entity, error) {
	from, through, err := taskWindow(q)
	if err != nil {
		return nil, err
	}
	deleted, err := flag(q, "include_deleted")
	if err != nil {
		return nil, err
	}
	t, err := h.s.Task(ctx, id)
	if err != nil {
		return nil, err
	}
	os, err := h.s.TaskOccurrences(ctx, id, from, through, deleted)
	if err != nil {
		return nil, err
	}
	views := make([]occurrenceView, 0, len(os))
	for _, o := range os {
		views = append(views, viewOccurrence(*t, o, ""))
	}
	self := taskHref(id) + "?" + url.Values{"from": {from}, "through": {through}, "include_deleted": {map[bool]string{true: "1", false: "0"}[deleted]}}.Encode()
	e := &Entity{Class: []string{"task"}, Title: t.Label,
		Properties: map[string]any{"task": t, "from": from, "through": through, "include_deleted": deleted, "occurrences": views},
		Links:      []Link{link("self", self, t.Label), link("up", "/tasks", "Tasks"), link("deadlines", "/deadlines", "Deadlines"), link("index", "/", "Home")}}
	if t.ProjectPageID != nil {
		e.Links = append(e.Links, link("project", pageHref(*t.ProjectPageID), t.ProjectTitle))
	}
	for _, o := range os {
		class := "occurrence"
		if o.Virtual {
			class = "virtual"
		}
		e.Entities = append(e.Entities, Link{Rel: []string{"occurrence"}, Href: occurrenceHref(id, o.OccurrenceKey), Title: occurrenceTitle(o), Class: []string{class}})
	}
	ids := map[string]string{"id": strconv.FormatInt(id, 10)}
	if t.DeletedAt != "" {
		e.Class = append(e.Class, "deleted")
		e.Actions = []Action{action("revive-task", ids, map[string]any{"version": t.Version})}
		return e, nil
	}
	e.Actions = append(e.Actions, action("edit-task", ids, map[string]any{"version": t.Version, "label": t.Label, "project": t.ProjectTitle,
		"reminder_time": t.ReminderLocalTime, "reminder_zone": t.ReminderZone}))
	if t.RepeatUnit != "" {
		e.Actions = append(e.Actions, action("stop-task", ids, map[string]any{"version": t.Version, "through": t.RepeatUntilDay}))
	}
	e.Actions = append(e.Actions, action("capture-occurrence", ids, map[string]any{"task_version": t.Version, "state": "open", "reminder_mode": "inherit"}),
		action("tombstone-task", ids, map[string]any{"version": t.Version}))
	return e, nil
}

func (h *server) occurrence(r *http.Request) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	return h.occurrenceEntity(r.Context(), id, r.PathValue("key"))
}

func (h *server) occurrenceEntity(ctx context.Context, taskID int64, key string) (*Entity, error) {
	if key != "once" && !core.IsDay(key) {
		return nil, &core.Error{Status: 404, Msg: "an occurrence key is a day of the series, or once"}
	}
	t, err := h.s.Task(ctx, taskID)
	if err != nil {
		return nil, err
	}
	o, err := h.s.TaskOccurrence(ctx, taskID, key)
	if err != nil {
		var ce *core.Error
		if !asError(err, &ce) || ce.Status != 404 {
			return nil, err
		}
		// Not written: a slot of a live recurring task is virtual open work (the contract, "Reading deadlines").
		os, e := h.s.TaskOccurrences(ctx, taskID, key, key, false)
		if e != nil {
			return nil, e
		}
		o = nil
		for i := range os {
			if os[i].OccurrenceKey == key && os[i].Virtual {
				o = &os[i]
			}
		}
		if o == nil {
			return nil, err
		}
	}
	v := viewOccurrence(*t, *o, t.Label)
	e := &Entity{Class: []string{"occurrence"}, Title: t.Label + " · " + key, Properties: v,
		Links: []Link{link("self", occurrenceHref(taskID, key), key), link("task", taskHref(taskID), t.Label), link("deadlines", "/deadlines", "Deadlines"), link("index", "/", "Home")}}
	if t.ProjectPageID != nil {
		e.Links = append(e.Links, link("project", pageHref(*t.ProjectPageID), t.ProjectTitle))
	}
	ids := map[string]string{"id": strconv.FormatInt(taskID, 10), "key": url.PathEscape(key)}
	switch {
	case t.DeletedAt != "":
		e.Class = append(e.Class, "deleted")
	case o.Virtual:
		e.Class = append(e.Class, "virtual")
		e.Actions = []Action{action("capture-occurrence", map[string]string{"id": ids["id"]}, map[string]any{"task_version": t.Version, "key": key, "due_day": key, "state": "open", "reminder_mode": "inherit"})}
	case o.DeletedAt != "":
		e.Class = append(e.Class, "deleted")
		e.Actions = []Action{action("revive-occurrence", ids, map[string]any{"task_version": t.Version, "version": o.Version})}
	default:
		e.Actions = []Action{action("edit-occurrence", ids, map[string]any{"task_version": t.Version, "version": o.Version, "due_day": o.DueDay, "state": o.State,
			"completed_at": o.CompletedAt, "reminder_mode": o.ReminderMode, "reminder_at": o.ReminderOverrideAt}),
			action("tombstone-occurrence", ids, map[string]any{"task_version": t.Version, "version": o.Version})}
	}
	return e, nil
}

func (h *server) deadlines(r *http.Request) (*Entity, error) {
	q := r.URL.Query()
	from, through, err := taskWindow(q)
	if err != nil {
		return nil, err
	}
	deleted, err := flag(q, "include_deleted")
	if err != nil {
		return nil, err
	}
	state := q.Get("state")
	if state == "" {
		state = "open"
	}
	if state != "open" && state != "done" && state != "skipped" && state != "all" {
		return nil, &core.Error{Status: 422, Msg: "state is open, done, skipped or all"}
	}
	ds, err := h.s.Deadlines(r.Context(), from, through, deleted)
	if err != nil {
		return nil, err
	}
	tasks := map[int64]core.Task{}
	views := []occurrenceView{}
	self := "/deadlines?" + url.Values{"from": {from}, "through": {through}, "state": {state}, "include_deleted": {map[bool]string{true: "1", false: "0"}[deleted]}}.Encode()
	e := &Entity{Class: []string{"deadlines"}, Title: "Deadlines " + from + " to " + through,
		Links:   []Link{link("self", self, "Deadlines"), link("tasks", "/tasks", "Tasks"), link("index", "/", "Home")},
		Actions: []Action{action("deadlines", nil, map[string]any{"from": from, "through": through, "state": state}), action("create-task", nil, nil)}}
	for _, d := range ds {
		if state != "all" && d.State != state {
			continue
		}
		t, ok := tasks[d.TaskID]
		if !ok {
			p, err := h.s.Task(r.Context(), d.TaskID)
			if err != nil {
				return nil, err
			}
			t, tasks[d.TaskID] = *p, *p
		}
		views = append(views, viewOccurrence(t, d.TaskOccurrence, d.Label))
		e.Entities = append(e.Entities, Link{Rel: []string{"occurrence"}, Href: occurrenceHref(d.TaskID, d.OccurrenceKey), Title: d.Label + ": " + occurrenceTitle(d.TaskOccurrence)})
	}
	e.Properties = map[string]any{"from": from, "through": through, "state": state, "include_deleted": deleted, "occurrences": views}
	return e, nil
}

// ---- writes: every one a core.Tx method inside one transaction, answered with the resource it changed

// taskSpec reads a definition's fields; project is a page title, resolved to its id.
func (h *server) taskSpec(ctx context.Context, v url.Values, base core.TaskSpec) (core.TaskSpec, error) {
	spec := base
	spec.Label = v.Get("label")
	spec.ProjectPageID = nil
	if title := v.Get("project"); title != "" {
		id, err := h.s.PageID(ctx, title)
		if err != nil {
			return spec, err
		}
		if id == 0 {
			return spec, &core.Error{Status: 404, Msg: "no page titled " + title + ": create the project page first"}
		}
		spec.ProjectPageID = &id
	}
	spec.ReminderLocalTime, spec.ReminderZone = v.Get("reminder_time"), v.Get("reminder_zone")
	return spec, nil
}

func (h *server) createTask(r *http.Request, src string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "label"); err != nil {
		return nil, err
	}
	spec, err := h.taskSpec(r.Context(), v, core.TaskSpec{})
	if err != nil {
		return nil, err
	}
	spec.RepeatUnit, spec.AnchorDay, spec.RepeatUntilDay = v.Get("repeat_unit"), v.Get("anchor_day"), v.Get("repeat_until_day")
	if every := v.Get("repeat_every"); every != "" {
		n, err := strconv.ParseInt(every, 10, 64)
		if err != nil {
			return nil, &core.Error{Status: 422, Msg: "repeat_every must be a whole number"}
		}
		spec.RepeatEvery = n
	}
	if spec.RepeatUnit != "" && v.Get("due_day") != "" {
		return nil, &core.Error{Status: 422, Msg: "due_day is a one-off's; a series is due on its slots"}
	}
	var id int64
	err = h.s.Do(r.Context(), src, func(t *core.Tx) error {
		var e error
		if spec.RepeatUnit == "" {
			initial := core.OccurrenceInput{OccurrenceKey: "once", OccurrenceChanges: core.OccurrenceChanges{DueDay: v.Get("due_day"), State: "open", ReminderMode: "inherit"}}
			id, _, e = t.CaptureOneOffTask(spec, v.Get("import_key"), initial)
		} else {
			id, _, e = t.CreateTask(spec, v.Get("import_key"))
		}
		return e
	})
	if err != nil {
		return nil, err
	}
	return h.taskEntity(r.Context(), id, nil)
}

func (h *server) editTask(r *http.Request, src string) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "version", "label"); err != nil {
		return nil, err
	}
	err = h.s.Do(r.Context(), src, func(t *core.Tx) error {
		cur, e := t.Task(id)
		if e != nil {
			return e
		}
		spec, e := h.taskSpec(r.Context(), v, cur.TaskSpec) // the cadence is the current one: it is fixed
		if e != nil {
			return e
		}
		return t.EditTask(id, v.Get("version"), spec)
	})
	if err != nil {
		return nil, staleTask(err)
	}
	return h.taskEntity(r.Context(), id, nil)
}

func (h *server) stopTask(r *http.Request, src string) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "version", "through"); err != nil {
		return nil, err
	}
	if err := h.s.Do(r.Context(), src, func(t *core.Tx) error { return t.StopTask(id, v.Get("version"), v.Get("through")) }); err != nil {
		return nil, staleTask(err)
	}
	return h.taskEntity(r.Context(), id, nil)
}

func (h *server) taskLifecycle(deleted bool) func(*http.Request, string) (*Entity, error) {
	return func(r *http.Request, src string) (*Entity, error) {
		id, err := idOf(r)
		if err != nil {
			return nil, err
		}
		v, err := form(r)
		if err != nil {
			return nil, err
		}
		if err := required(v, "version"); err != nil {
			return nil, err
		}
		if err := h.s.Do(r.Context(), src, func(t *core.Tx) error { return t.TaskLifecycle(id, v.Get("version"), deleted) }); err != nil {
			return nil, staleTask(err)
		}
		return h.taskEntity(r.Context(), id, url.Values{"include_deleted": {"1"}})
	}
}

// occurrenceChanges reads an outcome's fields over base: a field sent, even empty, replaces the value; one not
// sent keeps it. "now" as completed_at is this instant.
func occurrenceChanges(v url.Values, base core.OccurrenceChanges) core.OccurrenceChanges {
	c := base
	if v.Has("due_day") {
		c.DueDay = v.Get("due_day")
	}
	if v.Has("state") && v.Get("state") != "" {
		c.State = v.Get("state")
	}
	if v.Has("completed_at") {
		c.CompletedAt = v.Get("completed_at")
		if c.CompletedAt == "now" {
			c.CompletedAt = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		}
	}
	if v.Has("reminder_mode") && v.Get("reminder_mode") != "" {
		c.ReminderMode = v.Get("reminder_mode")
	}
	if v.Has("reminder_at") {
		c.ReminderOverrideAt = v.Get("reminder_at")
	}
	return c
}

func (h *server) captureOccurrence(r *http.Request, src string) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "task_version", "key"); err != nil {
		return nil, err
	}
	key := v.Get("key")
	base := core.OccurrenceChanges{State: "open", ReminderMode: "inherit"}
	if key != "once" {
		base.DueDay = key // a slot is due on its day unless the outcome says otherwise
	}
	in := core.OccurrenceInput{OccurrenceKey: key, ImportKey: v.Get("import_key"), OccurrenceChanges: occurrenceChanges(v, base)}
	err = h.s.Do(r.Context(), src, func(t *core.Tx) error {
		_, _, e := t.CaptureTaskOccurrence(id, v.Get("task_version"), in)
		return e
	})
	if err != nil {
		return nil, staleTask(err)
	}
	return h.occurrenceEntity(r.Context(), id, key)
}

func (h *server) editOccurrence(r *http.Request, src string) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "task_version", "version"); err != nil {
		return nil, err
	}
	key := r.PathValue("key")
	err = h.s.Do(r.Context(), src, func(t *core.Tx) error {
		cur, e := t.TaskOccurrence(id, key)
		if e != nil {
			return e
		}
		return t.EditTaskOccurrence(id, v.Get("task_version"), key, v.Get("version"), occurrenceChanges(v, cur.OccurrenceChanges))
	})
	if err != nil {
		return nil, staleTask(err)
	}
	return h.occurrenceEntity(r.Context(), id, key)
}

func (h *server) occurrenceLifecycle(deleted bool) func(*http.Request, string) (*Entity, error) {
	return func(r *http.Request, src string) (*Entity, error) {
		id, err := idOf(r)
		if err != nil {
			return nil, err
		}
		v, err := form(r)
		if err != nil {
			return nil, err
		}
		if err := required(v, "task_version", "version"); err != nil {
			return nil, err
		}
		key := r.PathValue("key")
		if err := h.s.Do(r.Context(), src, func(t *core.Tx) error {
			return t.TaskOccurrenceLifecycle(id, v.Get("task_version"), key, v.Get("version"), deleted)
		}); err != nil {
			return nil, staleTask(err)
		}
		return h.occurrenceEntity(r.Context(), id, key)
	}
}

// staleTask names a version conflict for a client: core says 409 and why, the code says what to do.
func staleTask(err error) error {
	var ce *core.Error
	if asError(err, &ce) && ce.Status == 409 && ce.Code == "" {
		return &core.Error{Status: 409, Msg: ce.Msg, Code: "stale_version"}
	}
	return err
}

func asError(err error, target **core.Error) bool { return errors.As(err, target) }
