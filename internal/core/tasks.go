package core

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// TaskSpec is the current definition described by docs/contract/planning.md. The JSON names are the wire
// shape of the API (API.md, "task").
type TaskSpec struct {
	Label             string `json:"label"`
	ProjectPageID     *int64 `json:"project_page_id,omitempty"`
	RepeatUnit        string `json:"repeat_unit,omitempty"`
	RepeatEvery       int64  `json:"repeat_every,omitempty"`
	AnchorDay         string `json:"anchor_day,omitempty"`
	RepeatUntilDay    string `json:"repeat_until_day,omitempty"`
	ReminderLocalTime string `json:"reminder_time,omitempty"`
	ReminderZone      string `json:"reminder_zone,omitempty"`
}

type Task struct {
	TaskSpec
	ID               int64  `json:"id"`
	ImportKey        string `json:"import_key,omitempty"`
	Source           string `json:"source"`
	Version          string `json:"version"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
	DeletedAt        string `json:"deleted_at,omitempty"`
	ProjectTitle     string `json:"project_title,omitempty"`
	ProjectDeletedAt string `json:"project_deleted_at,omitempty"`
}

type OccurrenceChanges struct {
	DueDay             string `json:"due_day,omitempty"`
	State              string `json:"state"`
	CompletedAt        string `json:"completed_at,omitempty"`
	ReminderMode       string `json:"reminder_mode"`
	ReminderOverrideAt string `json:"reminder_at,omitempty"`
}

// OccurrenceInput is an explicitly captured outcome, including historical imports.
type OccurrenceInput struct {
	OccurrenceChanges
	OccurrenceKey string
	ImportKey     string
}

type TaskOccurrence struct {
	OccurrenceChanges
	ID               int64  `json:"id,omitempty"`
	TaskID           int64  `json:"task_id"`
	OccurrenceKey    string `json:"key"`
	ImportKey        string `json:"import_key,omitempty"`
	Source           string `json:"source,omitempty"`
	Version          string `json:"version,omitempty"`
	CreatedAt        string `json:"created_at,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty"`
	DeletedAt        string `json:"deleted_at,omitempty"`
	TaskVersion      string `json:"task_version"`
	TaskDeletedAt    string `json:"task_deleted_at,omitempty"`
	ProjectTitle     string `json:"project_title,omitempty"`
	ProjectDeletedAt string `json:"project_deleted_at,omitempty"`
	Virtual          bool   `json:"virtual"`
}

func validateTaskSpec(in TaskSpec) error {
	if !utf8.ValidString(in.Label) || strings.ContainsRune(in.Label, 0) || strings.Trim(in.Label, " ") == "" {
		return Invalid("task label must be nonempty text without NUL")
	}
	if in.ProjectPageID != nil && *in.ProjectPageID <= 0 {
		return Invalid("task project ID must be positive")
	}
	if err := validateTaskSchedule(in); err != nil {
		return err
	}
	if in.ReminderLocalTime != "" || in.ReminderZone != "" {
		if !validReminderClock(in.ReminderLocalTime) || !validPlanningZone(in.ReminderZone) {
			return Invalid("task reminder needs a local minute clock and zone")
		}
	}
	return nil
}

func validateOccurrenceChanges(in OccurrenceChanges) error {
	if err := checkDay("due_day", in.DueDay); in.DueDay != "" && err != nil {
		return err
	}
	if in.State != StateOpen && in.State != StateDone && in.State != StateSkipped {
		return Invalid("unknown task occurrence state")
	}
	if in.CompletedAt != "" && (in.State != StateDone || !IsInstant(in.CompletedAt)) {
		return Invalid("completion instant requires a done occurrence")
	}
	switch in.ReminderMode {
	case ReminderInherit, ReminderOff:
		if in.ReminderOverrideAt != "" {
			return Invalid("reminder override requires at mode")
		}
	case ReminderAt:
		if !IsInstant(in.ReminderOverrideAt) {
			return Invalid("absolute reminder requires an exact instant")
		}
	default:
		return Invalid("unknown task reminder mode")
	}
	return nil
}

const taskSelect = `SELECT t.id,t.label,t.project_page_id,coalesce(t.repeat_unit,''),coalesce(t.repeat_every,0),coalesce(t.anchor_day,''),coalesce(t.repeat_until_day,''),coalesce(t.reminder_local_time,''),coalesce(t.reminder_zone,''),coalesce(t.import_key,''),t.source,CAST(t.revision AS TEXT),t.created_at,t.updated_at,coalesce(t.deleted_at,''),coalesce(n.title,''),coalesce(e.deleted_at,'') FROM tasks t LEFT JOIN entities e ON e.id=t.project_page_id LEFT JOIN entity_names n ON n.entity_id=e.id AND n.name_key=e.preferred_name_key `
const occurrenceSelect = `SELECT o.id,o.task_id,o.occurrence_key,coalesce(o.due_day,''),o.state,coalesce(o.completed_at,''),o.reminder_mode,coalesce(o.reminder_override_at,''),coalesce(o.import_key,''),o.source,CAST(o.revision AS TEXT),o.created_at,o.updated_at,coalesce(o.deleted_at,'') FROM task_occurrences o `

func scanTask(row interface{ Scan(...any) error }) (*Task, error) {
	p := &Task{}
	err := row.Scan(&p.ID, &p.Label, &p.ProjectPageID, &p.RepeatUnit, &p.RepeatEvery, &p.AnchorDay, &p.RepeatUntilDay, &p.ReminderLocalTime, &p.ReminderZone, &p.ImportKey, &p.Source, &p.Version, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt, &p.ProjectTitle, &p.ProjectDeletedAt)
	return p, err
}

func scanTaskOccurrence(row interface{ Scan(...any) error }) (*TaskOccurrence, error) {
	o := &TaskOccurrence{}
	err := row.Scan(&o.ID, &o.TaskID, &o.OccurrenceKey, &o.DueDay, &o.State, &o.CompletedAt, &o.ReminderMode, &o.ReminderOverrideAt, &o.ImportKey, &o.Source, &o.Version, &o.CreatedAt, &o.UpdatedAt, &o.DeletedAt)
	return o, err
}

func (t *Tx) Task(id int64) (*Task, error) {
	p, err := scanTask(t.tx.QueryRowContext(t.ctx, taskSelect+`WHERE t.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no task %d", id)
	}
	return p, err
}

func (s *Store) Task(ctx context.Context, id int64) (*Task, error) {
	p, err := scanTask(s.DB.R.QueryRowContext(ctx, taskSelect+`WHERE t.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no task %d", id)
	}
	return p, err
}

// Tasks lists the definitions by id, the live ones unless includeDeleted.
func (s *Store) Tasks(ctx context.Context, includeDeleted bool) ([]Task, error) {
	rows, err := s.DB.R.QueryContext(ctx, taskSelect+`WHERE (? OR t.deleted_at IS NULL) ORDER BY t.id`, includeDeleted)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		p, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// Deadline is an occurrence of any task in a window, with its task's label.
type Deadline struct {
	TaskOccurrence
	Label string `json:"label"`
}

// Deadlines merges every task's TaskOccurrences over the window (the live tasks, or all of them when
// includeDeleted), ordered by due day, key and task, bounded like a single task's read.
func (s *Store) Deadlines(ctx context.Context, from, through string, includeDeleted bool) ([]Deadline, error) {
	if err := checkWindow(from, through); err != nil {
		return nil, err
	}
	tasks, err := s.Tasks(ctx, includeDeleted)
	if err != nil {
		return nil, err
	}
	out := []Deadline{}
	for _, p := range tasks {
		os, err := s.TaskOccurrences(ctx, p.ID, from, through, includeDeleted)
		if err != nil {
			return nil, err
		}
		for _, o := range os {
			if len(out) >= maxTaskOccurrences {
				return nil, Invalid("deadline window exceeds %d occurrence limit", maxTaskOccurrences)
			}
			out = append(out, Deadline{o, p.Label})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.DueDay != b.DueDay {
			return a.DueDay < b.DueDay
		}
		if a.OccurrenceKey != b.OccurrenceKey {
			return a.OccurrenceKey < b.OccurrenceKey
		}
		return a.TaskID < b.TaskID
	})
	return out, nil
}

func (t *Tx) TaskOccurrence(taskID int64, key string) (*TaskOccurrence, error) {
	o, err := scanTaskOccurrence(t.tx.QueryRowContext(t.ctx, occurrenceSelect+`WHERE o.task_id=? AND o.occurrence_key=?`, taskID, key))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no task occurrence")
	}
	if err != nil {
		return nil, err
	}
	p, err := t.Task(taskID)
	if err != nil {
		return nil, err
	}
	o.withTask(p)
	return o, nil
}

func (o *TaskOccurrence) withTask(p *Task) {
	o.TaskVersion, o.TaskDeletedAt = p.Version, p.DeletedAt
	o.ProjectTitle, o.ProjectDeletedAt = p.ProjectTitle, p.ProjectDeletedAt
}

func (s *Store) TaskOccurrence(ctx context.Context, taskID int64, key string) (*TaskOccurrence, error) {
	// A single statement reads both edit tokens from the same SQLite snapshot.
	row := s.DB.R.QueryRowContext(ctx, `SELECT o.id,o.task_id,o.occurrence_key,coalesce(o.due_day,''),o.state,coalesce(o.completed_at,''),o.reminder_mode,coalesce(o.reminder_override_at,''),coalesce(o.import_key,''),o.source,CAST(o.revision AS TEXT),o.created_at,o.updated_at,coalesce(o.deleted_at,''),CAST(t.revision AS TEXT),coalesce(t.deleted_at,''),coalesce(n.title,''),coalesce(e.deleted_at,'') FROM task_occurrences o JOIN tasks t ON t.id=o.task_id LEFT JOIN entities e ON e.id=t.project_page_id LEFT JOIN entity_names n ON n.entity_id=e.id AND n.name_key=e.preferred_name_key WHERE o.task_id=? AND o.occurrence_key=?`, taskID, key)
	o := &TaskOccurrence{}
	err := row.Scan(&o.ID, &o.TaskID, &o.OccurrenceKey, &o.DueDay, &o.State, &o.CompletedAt, &o.ReminderMode, &o.ReminderOverrideAt, &o.ImportKey, &o.Source, &o.Version, &o.CreatedAt, &o.UpdatedAt, &o.DeletedAt, &o.TaskVersion, &o.TaskDeletedAt, &o.ProjectTitle, &o.ProjectDeletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no task occurrence")
	}
	return o, err
}

func sameTaskSpec(a, b TaskSpec) bool {
	if (a.ProjectPageID == nil) != (b.ProjectPageID == nil) {
		return false
	}
	if a.ProjectPageID != nil && *a.ProjectPageID != *b.ProjectPageID {
		return false
	}
	a.ProjectPageID, b.ProjectPageID = nil, nil
	return a == b
}

func (t *Tx) CreateTask(in TaskSpec, importKey string) (id int64, existing bool, err error) {
	return t.createTask(in, importKey, OccurrenceInput{OccurrenceKey: "once", OccurrenceChanges: OccurrenceChanges{State: StateOpen, ReminderMode: ReminderInherit}})
}

// CaptureOneOffTask commits an explicitly supplied initial outcome with its definition.
func (t *Tx) CaptureOneOffTask(in TaskSpec, importKey string, initial OccurrenceInput) (int64, bool, error) {
	if in.RepeatUnit != "" || initial.OccurrenceKey != "once" {
		return 0, false, Invalid("one-off capture requires its once occurrence")
	}
	if err := validateOccurrenceChanges(initial.OccurrenceChanges); err != nil {
		return 0, false, err
	}
	return t.createTask(in, importKey, initial)
}

func (t *Tx) createTask(in TaskSpec, importKey string, initial OccurrenceInput) (id int64, existing bool, err error) {
	if err = validateTaskSpec(in); err != nil {
		return
	}
	if importKey != "" {
		have, e := scanTask(t.tx.QueryRowContext(t.ctx, taskSelect+`WHERE t.source=? AND t.import_key=?`, t.Source, importKey))
		if e == nil {
			if !sameTaskSpec(have.TaskSpec, in) {
				return 0, false, Conflict("task source identity has changed payload")
			}
			if initial.ImportKey != "" {
				o, e := t.TaskOccurrence(have.ID, "once")
				if e != nil {
					return 0, false, e
				}
				if o.Source != t.Source || o.ImportKey != initial.ImportKey {
					return 0, false, Conflict("one-off occurrence has a different source identity")
				}
			}
			return have.ID, true, nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return 0, false, e
		}
	}
	var every any
	if in.RepeatUnit != "" {
		every = in.RepeatEvery
	}
	err = t.tx.QueryRowContext(t.ctx, `INSERT INTO tasks(label,project_page_id,repeat_unit,repeat_every,anchor_day,repeat_until_day,reminder_local_time,reminder_zone,source,import_key,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,`+nowSQL+`,`+nowSQL+`) RETURNING id`, in.Label, in.ProjectPageID, nullIfEmpty(in.RepeatUnit), every, nullIfEmpty(in.AnchorDay), nullIfEmpty(in.RepeatUntilDay), nullIfEmpty(in.ReminderLocalTime), nullIfEmpty(in.ReminderZone), t.Source, nullIfEmpty(importKey)).Scan(&id)
	if err != nil {
		return 0, false, err
	}
	if in.RepeatUnit == "" {
		_, err = t.insertTaskOccurrence(id, initial)
	}
	return id, false, err
}

// TaskForm is a task as a surface describes it (create-task, edit-task): the definition, its project as the title of
// a page (made as an empty page when no page has the title; a ProjectPageID stands when there is no title), a
// one-off's due day, and the sender's key.
type TaskForm struct {
	TaskSpec
	Project   string
	DueDay    string
	ImportKey string
}

// spec is the form's definition with its project resolved to a page in this transaction.
func (t *Tx) spec(f TaskForm) (TaskSpec, error) {
	spec := f.TaskSpec
	if f.Project != "" {
		id, _, err := t.PageForTitle(f.Project)
		if err != nil {
			return spec, err
		}
		spec.ProjectPageID = &id
	}
	return spec, nil
}

// CreateTask is one task in its own transaction: a series, or a one-off with its once occurrence due on DueDay.
func (s *Store) CreateTask(ctx context.Context, source string, f TaskForm) (id int64, existing bool, err error) {
	if f.RepeatUnit != "" && f.DueDay != "" {
		return 0, false, Invalid("due_day is a one-off's; a series is due on its slots")
	}
	err = s.Do(ctx, source, func(t *Tx) error {
		spec, e := t.spec(f)
		if e != nil {
			return e
		}
		if spec.RepeatUnit == "" {
			initial := OccurrenceInput{OccurrenceKey: "once", OccurrenceChanges: OccurrenceChanges{DueDay: f.DueDay, State: StateOpen, ReminderMode: ReminderInherit}}
			id, existing, e = t.CaptureOneOffTask(spec, f.ImportKey, initial)
		} else {
			id, existing, e = t.CreateTask(spec, f.ImportKey)
		}
		return e
	})
	if err != nil {
		id, existing = 0, false
	}
	return
}

// EditTask changes a task's label, project and default reminder in its own transaction; the cadence is the current
// one, since it is fixed.
func (s *Store) EditTask(ctx context.Context, source string, id int64, version string, f TaskForm) error {
	return s.Do(ctx, source, func(t *Tx) error {
		cur, err := t.Task(id)
		if err != nil {
			return err
		}
		f.RepeatUnit, f.RepeatEvery, f.AnchorDay, f.RepeatUntilDay = cur.RepeatUnit, cur.RepeatEvery, cur.AnchorDay, cur.RepeatUntilDay
		f.ProjectPageID = nil
		spec, err := t.spec(f)
		if err != nil {
			return err
		}
		return t.EditTask(id, version, spec)
	})
}

func (s *Store) StopTask(ctx context.Context, source string, id int64, version, through string) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.StopTask(id, version, through) })
}

func (s *Store) TaskLifecycle(ctx context.Context, source string, id int64, version string, deleted bool) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.TaskLifecycle(id, version, deleted) })
}

// OccurrencePatch is an outcome as a surface sends it (capture-occurrence, edit-occurrence): a field set, even to
// "", replaces the value; a nil field keeps it. "now" as CompletedAt is this instant.
type OccurrencePatch struct {
	DueDay, State, CompletedAt, ReminderMode, ReminderAt *string
}

func (p OccurrencePatch) apply(c OccurrenceChanges) OccurrenceChanges {
	if p.DueDay != nil {
		c.DueDay = *p.DueDay
	}
	if p.State != nil {
		c.State = *p.State
	}
	if p.CompletedAt != nil {
		c.CompletedAt = *p.CompletedAt
		if c.CompletedAt == "now" {
			c.CompletedAt = time.Now().UTC().Format(instantLayout)
		}
	}
	if p.ReminderMode != nil {
		c.ReminderMode = *p.ReminderMode
	}
	if p.ReminderAt != nil {
		c.ReminderOverrideAt = *p.ReminderAt
	}
	return c
}

// CaptureTaskOccurrence writes one occurrence of a task in its own transaction: open and inheriting its reminder, due
// on its slot day (a one-off's once is undated), unless the patch says otherwise.
func (s *Store) CaptureTaskOccurrence(ctx context.Context, source string, taskID int64, taskVersion, key, importKey string, p OccurrencePatch) (o *TaskOccurrence, existing bool, err error) {
	base := OccurrenceChanges{State: StateOpen, ReminderMode: ReminderInherit}
	if key != "once" {
		base.DueDay = key
	}
	in := OccurrenceInput{OccurrenceKey: key, ImportKey: importKey, OccurrenceChanges: p.apply(base)}
	err = s.Do(ctx, source, func(t *Tx) (e error) { o, existing, e = t.CaptureTaskOccurrence(taskID, taskVersion, in); return })
	if err != nil {
		o, existing = nil, false
	}
	return
}

// EditTaskOccurrence changes a written occurrence in its own transaction: the patch over its current values.
func (s *Store) EditTaskOccurrence(ctx context.Context, source string, taskID int64, taskVersion, key, version string, p OccurrencePatch) error {
	return s.Do(ctx, source, func(t *Tx) error {
		cur, err := t.TaskOccurrence(taskID, key)
		if err != nil {
			return err
		}
		return t.EditTaskOccurrence(taskID, taskVersion, key, version, p.apply(cur.OccurrenceChanges))
	})
}

func (s *Store) TaskOccurrenceLifecycle(ctx context.Context, source string, taskID int64, taskVersion, key, version string, deleted bool) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.TaskOccurrenceLifecycle(taskID, taskVersion, key, version, deleted) })
}

func (t *Tx) liveTaskVersion(id int64, version string) (*Task, error) {
	p, err := t.Task(id)
	if err != nil {
		return nil, err
	}
	if p.Version != version || p.DeletedAt != "" {
		return nil, Conflict("live task and current version required")
	}
	return p, nil
}

func (t *Tx) EditTask(id int64, version string, in TaskSpec) error {
	if err := validateTaskSpec(in); err != nil {
		return err
	}
	p, err := t.liveTaskVersion(id, version)
	if err != nil {
		return err
	}
	if p.RepeatUnit != in.RepeatUnit || p.RepeatEvery != in.RepeatEvery || p.AnchorDay != in.AnchorDay || p.RepeatUntilDay != in.RepeatUntilDay {
		return Invalid("cadence is fixed; shorten an end with StopTask")
	}
	_, err = t.tx.ExecContext(t.ctx, `UPDATE tasks SET label=?,project_page_id=?,reminder_local_time=?,reminder_zone=? WHERE id=?`, in.Label, in.ProjectPageID, nullIfEmpty(in.ReminderLocalTime), nullIfEmpty(in.ReminderZone), id)
	return err
}

func (t *Tx) StopTask(id int64, version, through string) error {
	if err := checkDay("through", through); err != nil {
		return err
	}
	p, err := t.liveTaskVersion(id, version)
	if err != nil {
		return err
	}
	if p.RepeatUnit == "" {
		return Invalid("one-off task has no recurrence to end")
	}
	if p.RepeatUntilDay != "" && through > p.RepeatUntilDay {
		return Invalid("a recurrence end may only shorten")
	}
	_, err = t.tx.ExecContext(t.ctx, `UPDATE tasks SET repeat_until_day=? WHERE id=?`, through, id)
	return err
}

func (t *Tx) TaskLifecycle(id int64, version string, deleted bool) error {
	p, err := t.Task(id)
	if err != nil {
		return err
	}
	if p.Version != version {
		return Conflict("task changed since this version")
	}
	if deleted == (p.DeletedAt != "") {
		return nil
	}
	return t.setDeletedAt("tasks", id, deleted)
}

func (t *Tx) insertTaskOccurrence(taskID int64, in OccurrenceInput) (int64, error) {
	var id int64
	err := t.tx.QueryRowContext(t.ctx, `INSERT INTO task_occurrences(task_id,occurrence_key,due_day,state,completed_at,reminder_mode,reminder_override_at,source,import_key,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,`+nowSQL+`,`+nowSQL+`) RETURNING id`, taskID, in.OccurrenceKey, nullIfEmpty(in.DueDay), in.State, nullIfEmpty(in.CompletedAt), in.ReminderMode, nullIfEmpty(in.ReminderOverrideAt), t.Source, nullIfEmpty(in.ImportKey)).Scan(&id)
	return id, err
}

// CaptureTaskOccurrence preserves an explicit outcome; retry identity never overwrites current contents.
func (t *Tx) CaptureTaskOccurrence(taskID int64, taskVersion string, in OccurrenceInput) (*TaskOccurrence, bool, error) {
	if err := validateOccurrenceChanges(in.OccurrenceChanges); err != nil {
		return nil, false, err
	}
	p, err := t.Task(taskID)
	if err != nil {
		return nil, false, err
	}
	if p.Version != taskVersion {
		return nil, false, Conflict("task changed since this version")
	}
	belongs, err := taskSlot(t.ctx, p.TaskSpec, in.OccurrenceKey)
	if err != nil {
		return nil, false, err
	}
	if !belongs {
		return nil, false, Invalid("occurrence key does not belong to task")
	}
	if in.ImportKey != "" {
		have, e := scanTaskOccurrence(t.tx.QueryRowContext(t.ctx, occurrenceSelect+`WHERE o.source=? AND o.import_key=?`, t.Source, in.ImportKey))
		if e == nil {
			if have.TaskID != taskID || have.OccurrenceKey != in.OccurrenceKey {
				return nil, false, Conflict("occurrence source key belongs to another slot")
			}
			have.withTask(p)
			return have, true, nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return nil, false, e
		}
	}
	have, e := scanTaskOccurrence(t.tx.QueryRowContext(t.ctx, occurrenceSelect+`WHERE o.task_id=? AND o.occurrence_key=?`, taskID, in.OccurrenceKey))
	if e == nil {
		if in.ImportKey != "" && (have.Source != t.Source || have.ImportKey != in.ImportKey) {
			return nil, false, Conflict("occurrence slot has a different source identity")
		}
		have.withTask(p)
		return have, true, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, false, e
	}
	if p.DeletedAt != "" {
		return nil, false, Conflict("new occurrence requires a live task")
	}
	if in.State == StateOpen && p.RepeatUntilDay != "" && in.OccurrenceKey > p.RepeatUntilDay {
		return nil, false, Invalid("open occurrence is after recurrence end")
	}
	if _, err = t.insertTaskOccurrence(taskID, in); err != nil {
		return nil, false, err
	}
	o, err := t.TaskOccurrence(taskID, in.OccurrenceKey)
	return o, false, err
}

func (t *Tx) EditTaskOccurrence(taskID int64, taskVersion, key, version string, in OccurrenceChanges) error {
	if err := validateOccurrenceChanges(in); err != nil {
		return err
	}
	if _, err := t.liveTaskVersion(taskID, taskVersion); err != nil {
		return err
	}
	o, err := t.TaskOccurrence(taskID, key)
	if err != nil {
		return err
	}
	if o.Version != version || o.DeletedAt != "" {
		return Conflict("live occurrence and current version required")
	}
	_, err = t.tx.ExecContext(t.ctx, `UPDATE task_occurrences SET due_day=?,state=?,completed_at=?,reminder_mode=?,reminder_override_at=? WHERE id=?`, nullIfEmpty(in.DueDay), in.State, nullIfEmpty(in.CompletedAt), in.ReminderMode, nullIfEmpty(in.ReminderOverrideAt), o.ID)
	return err
}

func (t *Tx) TaskOccurrenceLifecycle(taskID int64, taskVersion, key, version string, deleted bool) error {
	p, err := t.liveTaskVersion(taskID, taskVersion)
	if err != nil {
		return err
	}
	o, err := t.TaskOccurrence(taskID, key)
	if err != nil {
		return err
	}
	if o.Version != version {
		return Conflict("occurrence changed since this version")
	}
	if deleted == (o.DeletedAt != "") {
		return nil
	}
	if !deleted && o.State == StateOpen && p.RepeatUntilDay != "" && key > p.RepeatUntilDay {
		return Invalid("occurrence cannot reopen after recurrence end")
	}
	return t.setDeletedAt("task_occurrences", o.ID, deleted)
}

const maxTaskOccurrences = 10000

// TaskOccurrences reads an inclusive deadline window from one consistent snapshot.
// It includes outcomes, and explicit historical reads retain tombstones. Undated rows
// remain available by identity. The contract's sparse merge runs before deadline filtering.
func (s *Store) TaskOccurrences(ctx context.Context, taskID int64, from, through string, includeDeleted bool) (_ []TaskOccurrence, err error) {
	if err := checkWindow(from, through); err != nil {
		return nil, err
	}
	tx, err := s.DB.R.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := scanTask(tx.QueryRowContext(ctx, taskSelect+`WHERE t.id=?`, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("no task %d", taskID)
	}
	if err != nil {
		return nil, err
	}
	if p.DeletedAt != "" && !includeDeleted {
		return []TaskOccurrence{}, tx.Commit()
	}
	rows, err := tx.QueryContext(ctx, occurrenceSelect+`WHERE o.task_id=? AND (o.occurrence_key BETWEEN ? AND ? OR o.due_day BETWEEN ? AND ?) ORDER BY o.occurrence_key`, taskID, from, through, from, through)
	if err != nil {
		return nil, err
	}
	persisted := map[string]TaskOccurrence{}
	for rows.Next() {
		o, e := scanTaskOccurrence(rows)
		if e != nil {
			return nil, errors.Join(e, rows.Close())
		}
		if len(persisted) >= maxTaskOccurrences {
			return nil, errors.Join(Invalid("task window exceeds %d occurrence limit", maxTaskOccurrences), rows.Close())
		}
		o.withTask(p)
		persisted[o.OccurrenceKey] = *o
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	out := []TaskOccurrence{}
	appendOccurrence := func(o TaskOccurrence) error {
		o.withTask(p)
		if (!includeDeleted && o.DeletedAt != "") || o.DueDay < from || o.DueDay > through {
			return nil
		}
		if len(out) >= maxTaskOccurrences {
			return Invalid("task window exceeds %d occurrence limit", maxTaskOccurrences)
		}
		out = append(out, o)
		return nil
	}
	for _, o := range persisted {
		if err = appendOccurrence(o); err != nil {
			return nil, err
		}
	}
	if p.DeletedAt == "" {
		err = taskCalendar(ctx, p.TaskSpec, from, through, func(key string) error {
			if _, ok := persisted[key]; ok {
				return nil
			}
			return appendOccurrence(TaskOccurrence{TaskID: taskID, TaskVersion: p.Version, OccurrenceKey: key, Virtual: true, OccurrenceChanges: OccurrenceChanges{DueDay: key, State: StateOpen, ReminderMode: ReminderInherit}})
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DueDay != out[j].DueDay {
			return out[i].DueDay < out[j].DueDay
		}
		return out[i].OccurrenceKey < out[j].OccurrenceKey
	})
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// The states of an occurrence and the modes of its reminder (docs/contract/planning.md), as the wire carries them.
const (
	StateOpen    = "open"
	StateDone    = "done"
	StateSkipped = "skipped"

	ReminderInherit = "inherit"
	ReminderOff     = "off"
	ReminderAt      = "at"
)
