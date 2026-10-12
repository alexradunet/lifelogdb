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

// actionableDays is how far past today a list of occurrences offers the write of each one it shows: the overdue
// ones, today's and those of the next week get it, and a later one is a link to its own page. So a long window
// stays a small answer (docs/plans/091-the-browser-for-daily-use.md).
const actionableDays = 7

// actionable reports an occurrence a list offers its outcome action for: one due on or before the last
// actionable day after today.
func actionable(o core.TaskOccurrence, today string) bool {
	last := addDays(today, actionableDays)
	return last != "" && o.DueDay != "" && o.DueDay <= last
}

// outcomeAction is the write that sets an occurrence's state: capture-occurrence for a virtual slot (its key and
// due day filled), edit-occurrence for a written one (every current value filled). Both edit tokens come from the
// occurrence itself, which every read fills from one snapshot. There is none for a tombstoned occurrence or under a
// tombstoned task: those offer only a revive, on their own pages.
func outcomeAction(o core.TaskOccurrence) (Action, bool) {
	id := strconv.FormatInt(o.TaskID, 10)
	switch {
	case o.TaskDeletedAt != "" || o.DeletedAt != "":
		return Action{}, false
	case o.Virtual:
		return action("capture-occurrence", map[string]string{"id": id}, map[string]any{"task_version": o.TaskVersion, "key": o.OccurrenceKey,
			"due_day": o.OccurrenceKey, "state": core.StateOpen, "reminder_mode": core.ReminderInherit}), true
	}
	return action("edit-occurrence", map[string]string{"id": id, "key": o.OccurrenceKey}, map[string]any{"task_version": o.TaskVersion,
		"version": o.Version, "due_day": o.DueDay, "state": o.State, "completed_at": o.CompletedAt, "reminder_mode": o.ReminderMode,
		"reminder_at": o.ReminderOverrideAt}), true
}

// outcomesOf are the outcome actions of the occurrences a list shows that are actionable against today.
func outcomesOf(views []occurrenceView, today string) []Action {
	out := []Action{}
	for _, v := range views {
		if a, ok := outcomeAction(v.TaskOccurrence); ok && actionable(v.TaskOccurrence, today) {
			out = append(out, a)
		}
	}
	return out
}

func taskWindow(q url.Values, localDay string) (from, through string, err error) {
	today, _ := dayOf(localDay)
	from, through = q.Get("from"), q.Get("through")
	if from == "" {
		from = today.AddDate(0, 0, -windowBack).Format(time.DateOnly)
	}
	if through == "" {
		through = today.AddDate(0, 0, windowOn).Format(time.DateOnly)
	}
	if !core.IsDay(from) || !core.IsDay(through) || from > through {
		return "", "", core.Invalid("from and through must be exact days, from first")
	}
	return from, through, nil
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
	today := h.localDay()
	from, through, err := taskWindow(q, today)
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
	self := taskHref(id) + "?" + url.Values{"from": {from}, "through": {through}, "include_deleted": {boolField(deleted)}}.Encode()
	props := map[string]any{"task": t, "from": from, "through": through, "include_deleted": deleted, "occurrences": views, "today": today}
	// A one-off's one occurrence, whatever its due day: an undated one is in no window, and its page must still
	// show it and offer its outcome.
	var once *core.TaskOccurrence
	if t.RepeatUnit == "" {
		o, err := h.s.TaskOccurrence(ctx, id, "once")
		var ce *core.Error
		switch {
		case err == nil:
			once = o
			props["once"] = viewOccurrence(*t, *o, "")
		case !errors.As(err, &ce) || ce.Status != 404:
			return nil, err
		}
	}
	e := &Entity{Class: []string{"task"}, Title: t.Label, Properties: props,
		Links: []Link{link("self", self, t.Label), link("up", "/tasks", "Tasks"), link("deadlines", "/deadlines", "Deadlines"), link("index", "/", "Home")}}
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
	ids := idParam(id)
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
	e.Actions = append(e.Actions, action("capture-occurrence", ids, map[string]any{"task_version": t.Version, "state": core.StateOpen, "reminder_mode": core.ReminderInherit}),
		action("tombstone-task", ids, map[string]any{"version": t.Version}))
	// Then the outcome of a one-off's one occurrence, or of each occurrence shown that is due soon.
	if once != nil {
		if a, ok := outcomeAction(*once); ok {
			e.Actions = append(e.Actions, a)
		}
	} else {
		e.Actions = append(e.Actions, outcomesOf(views, today)...)
	}
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
		return nil, core.NotFound("an occurrence key is a day of the series, or once")
	}
	t, err := h.s.Task(ctx, taskID)
	if err != nil {
		return nil, err
	}
	o, err := h.s.TaskOccurrence(ctx, taskID, key)
	if err != nil {
		var ce *core.Error
		if !errors.As(err, &ce) || ce.Status != 404 {
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
	ids := map[string]string{"id": strconv.FormatInt(taskID, 10), "key": key}
	outcome, _ := outcomeAction(*o)
	switch {
	case t.DeletedAt != "":
		e.Class = append(e.Class, "deleted")
	case o.Virtual:
		e.Class = append(e.Class, "virtual")
		e.Actions = []Action{outcome}
	case o.DeletedAt != "":
		e.Class = append(e.Class, "deleted")
		e.Actions = []Action{action("revive-occurrence", ids, map[string]any{"task_version": t.Version, "version": o.Version})}
	default:
		e.Actions = []Action{outcome, action("tombstone-occurrence", ids, map[string]any{"task_version": t.Version, "version": o.Version})}
	}
	return e, nil
}

func (h *server) deadlines(r *http.Request) (*Entity, error) {
	q := r.URL.Query()
	today := h.localDay()
	from, through, err := taskWindow(q, today)
	if err != nil {
		return nil, err
	}
	deleted, err := flag(q, "include_deleted")
	if err != nil {
		return nil, err
	}
	state := q.Get("state")
	if state == "" {
		state = core.StateOpen
	}
	if state != core.StateOpen && state != core.StateDone && state != core.StateSkipped && state != "all" {
		return nil, core.Invalid("state is open, done, skipped or all")
	}
	views, err := h.dueIn(r.Context(), from, through, state, deleted)
	if err != nil {
		return nil, err
	}
	self := "/deadlines?" + url.Values{"from": {from}, "through": {through}, "state": {state}, "include_deleted": {boolField(deleted)}}.Encode()
	e := &Entity{Class: []string{"deadlines"}, Title: "Deadlines " + from + " to " + through,
		Properties: map[string]any{"from": from, "through": through, "state": state, "include_deleted": deleted, "occurrences": views, "today": today},
		Links:      []Link{link("self", self, "Deadlines"), link("tasks", "/tasks", "Tasks"), link("index", "/", "Home")},
		Actions:    append([]Action{action("deadlines", nil, map[string]any{"from": from, "through": through, "state": state}), action("create-task", nil, nil)}, outcomesOf(views, today)...)}
	for _, v := range views {
		e.Entities = append(e.Entities, Link{Rel: []string{"occurrence"}, Href: occurrenceHref(v.TaskID, v.OccurrenceKey), Title: v.Label + ": " + occurrenceTitle(v.TaskOccurrence)})
	}
	return e, nil
}

// dueIn are the occurrences of every task due from from through through, in state (or every state, "all"), as the
// deadlines show them. The deadlines, a day and the root read what is due through it.
func (h *server) dueIn(ctx context.Context, from, through, state string, includeDeleted bool) ([]occurrenceView, error) {
	ds, err := h.s.Deadlines(ctx, from, through, includeDeleted)
	if err != nil {
		return nil, err
	}
	tasks := map[int64]core.Task{}
	views := []occurrenceView{}
	for _, d := range ds {
		if state != "all" && d.State != state {
			continue
		}
		t, ok := tasks[d.TaskID]
		if !ok {
			p, err := h.s.Task(ctx, d.TaskID)
			if err != nil {
				return nil, err
			}
			t, tasks[d.TaskID] = *p, *p
		}
		views = append(views, viewOccurrence(t, d.TaskOccurrence, d.Label))
	}
	return views, nil
}

// ---- writes: every one a core.Store method, one transaction, answered with the resource it changed

// taskForm is a task as a form describes it: the label, the project's title and the default reminder; the cadence
// fields are read by createTask alone.
func taskForm(v url.Values) core.TaskForm {
	f := core.TaskForm{Project: v.Get("project"), ImportKey: v.Get("import_key")}
	f.Label, f.ReminderLocalTime, f.ReminderZone = v.Get("label"), v.Get("reminder_time"), v.Get("reminder_zone")
	return f
}

func (h *server) createTask(r *http.Request, src string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "label"); err != nil {
		return nil, err
	}
	f := taskForm(v)
	f.RepeatUnit, f.AnchorDay, f.RepeatUntilDay, f.DueDay = v.Get("repeat_unit"), v.Get("anchor_day"), v.Get("repeat_until_day"), v.Get("due_day")
	if every := v.Get("repeat_every"); every != "" {
		n, err := strconv.ParseInt(every, 10, 64)
		if err != nil {
			return nil, core.Invalid("repeat_every must be a whole number")
		}
		f.RepeatEvery = n
	}
	id, _, err := h.s.CreateTask(r.Context(), src, f)
	if err != nil {
		return nil, err
	}
	return h.taskEntity(r.Context(), id, nil)
}

func (h *server) editTask(r *http.Request, src string) (*Entity, error) {
	id, v, err := idForm(r, "version", "label")
	if err != nil {
		return nil, err
	}
	if err := h.s.EditTask(r.Context(), src, id, v.Get("version"), taskForm(v)); err != nil {
		return nil, err
	}
	return h.taskEntity(r.Context(), id, nil)
}

func (h *server) stopTask(r *http.Request, src string) (*Entity, error) {
	id, v, err := idForm(r, "version", "through")
	if err != nil {
		return nil, err
	}
	if err := h.s.StopTask(r.Context(), src, id, v.Get("version"), v.Get("through")); err != nil {
		return nil, err
	}
	return h.taskEntity(r.Context(), id, nil)
}

func (h *server) taskLifecycle(deleted bool) func(*http.Request, string) (*Entity, error) {
	return func(r *http.Request, src string) (*Entity, error) {
		id, v, err := idForm(r, "version")
		if err != nil {
			return nil, err
		}
		if err := h.s.TaskLifecycle(r.Context(), src, id, v.Get("version"), deleted); err != nil {
			return nil, err
		}
		return h.taskEntity(r.Context(), id, url.Values{"include_deleted": {"1"}})
	}
}

// occurrencePatch reads an outcome's fields: a field sent, even empty, replaces the value; one not sent keeps it. A
// state or a reminder mode sent empty (a form's blank choice) keeps the value too.
func occurrencePatch(v url.Values) core.OccurrencePatch {
	sent := func(name string) *string {
		if !v.Has(name) {
			return nil
		}
		s := v.Get(name)
		return &s
	}
	choice := func(name string) *string {
		if v.Get(name) == "" {
			return nil
		}
		return sent(name)
	}
	return core.OccurrencePatch{DueDay: sent("due_day"), State: choice("state"), CompletedAt: sent("completed_at"),
		ReminderMode: choice("reminder_mode"), ReminderAt: sent("reminder_at")}
}

func (h *server) captureOccurrence(r *http.Request, src string) (*Entity, error) {
	id, v, err := idForm(r, "task_version", "key")
	if err != nil {
		return nil, err
	}
	key := v.Get("key")
	if _, _, err := h.s.CaptureTaskOccurrence(r.Context(), src, id, v.Get("task_version"), key, v.Get("import_key"), occurrencePatch(v)); err != nil {
		return nil, err
	}
	return h.occurrenceEntity(r.Context(), id, key)
}

func (h *server) editOccurrence(r *http.Request, src string) (*Entity, error) {
	id, v, err := idForm(r, "task_version", "version")
	if err != nil {
		return nil, err
	}
	key := r.PathValue("key")
	if err := h.s.EditTaskOccurrence(r.Context(), src, id, v.Get("task_version"), key, v.Get("version"), occurrencePatch(v)); err != nil {
		return nil, err
	}
	return h.occurrenceEntity(r.Context(), id, key)
}

func (h *server) occurrenceLifecycle(deleted bool) func(*http.Request, string) (*Entity, error) {
	return func(r *http.Request, src string) (*Entity, error) {
		id, v, err := idForm(r, "task_version", "version")
		if err != nil {
			return nil, err
		}
		key := r.PathValue("key")
		if err := h.s.TaskOccurrenceLifecycle(r.Context(), src, id, v.Get("task_version"), key, v.Get("version"), deleted); err != nil {
			return nil, err
		}
		return h.occurrenceEntity(r.Context(), id, key)
	}
}
