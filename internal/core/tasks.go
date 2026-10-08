package core

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
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
	Key              string `json:"import_key,omitempty"`
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
		return invalid("task label must be nonempty text without NUL")
	}
	if in.ProjectPageID != nil && *in.ProjectPageID <= 0 {
		return invalid("task project ID must be positive")
	}
	if err := validateTaskSchedule(in); err != nil {
		return err
	}
	if in.ReminderLocalTime != "" || in.ReminderZone != "" {
		if !validReminderClock(in.ReminderLocalTime) || !validPlanningZone(in.ReminderZone) {
			return invalid("task reminder needs a local minute clock and zone")
		}
	}
	return nil
}

func validateOccurrenceChanges(in OccurrenceChanges) error {
	if in.DueDay != "" && !IsDay(in.DueDay) {
		return invalid("task due day must be exact")
	}
	if in.State != "open" && in.State != "done" && in.State != "skipped" {
		return invalid("unknown task occurrence state")
	}
	if in.CompletedAt != "" && (in.State != "done" || !IsInstant(in.CompletedAt)) {
		return invalid("completion instant requires a done occurrence")
	}
	switch in.ReminderMode {
	case "inherit", "off":
		if in.ReminderOverrideAt != "" {
			return invalid("reminder override requires at mode")
		}
	case "at":
		if !IsInstant(in.ReminderOverrideAt) {
			return invalid("absolute reminder requires an exact instant")
		}
	default:
		return invalid("unknown task reminder mode")
	}
	return nil
}

const taskSelect = `SELECT t.id,t.label,t.project_page_id,coalesce(t.repeat_unit,''),coalesce(t.repeat_every,0),coalesce(t.anchor_day,''),coalesce(t.repeat_until_day,''),coalesce(t.reminder_local_time,''),coalesce(t.reminder_zone,''),coalesce(t.import_key,''),t.source,CAST(t.revision AS TEXT),t.created_at,t.updated_at,coalesce(t.deleted_at,''),coalesce(n.title,''),coalesce(e.deleted_at,'') FROM tasks t LEFT JOIN entities e ON e.id=t.project_page_id LEFT JOIN entity_names n ON n.entity_id=e.id AND n.name_key=e.preferred_name_key `
const occurrenceSelect = `SELECT o.id,o.task_id,o.occurrence_key,coalesce(o.due_day,''),o.state,coalesce(o.completed_at,''),o.reminder_mode,coalesce(o.reminder_override_at,''),coalesce(o.import_key,''),o.source,CAST(o.revision AS TEXT),o.created_at,o.updated_at,coalesce(o.deleted_at,'') FROM task_occurrences o `

func scanTask(row interface{ Scan(...any) error }) (*Task, error) {
	p := &Task{}
	err := row.Scan(&p.ID, &p.Label, &p.ProjectPageID, &p.RepeatUnit, &p.RepeatEvery, &p.AnchorDay, &p.RepeatUntilDay, &p.ReminderLocalTime, &p.ReminderZone, &p.Key, &p.Source, &p.Version, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt, &p.ProjectTitle, &p.ProjectDeletedAt)
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
		return nil, notFound("no task %d", id)
	}
	return p, err
}

func (s *Store) Task(ctx context.Context, id int64) (*Task, error) {
	p, err := scanTask(s.DB.R.QueryRowContext(ctx, taskSelect+`WHERE t.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("no task %d", id)
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
	if !IsDay(from) || !IsDay(through) || from > through {
		return nil, invalid("task deadline window must have ordered exact days")
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
				return nil, invalid("deadline window exceeds 10000 occurrence limit")
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
		return nil, notFound("no task occurrence")
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
		return nil, notFound("no task occurrence")
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
	return t.createTask(in, importKey, OccurrenceInput{OccurrenceKey: "once", OccurrenceChanges: OccurrenceChanges{State: "open", ReminderMode: "inherit"}})
}

// CaptureOneOffTask commits an explicitly supplied initial outcome with its definition.
func (t *Tx) CaptureOneOffTask(in TaskSpec, importKey string, initial OccurrenceInput) (int64, bool, error) {
	if in.RepeatUnit != "" || initial.OccurrenceKey != "once" {
		return 0, false, invalid("one-off capture requires its once occurrence")
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
				return 0, false, conflict("task source identity has changed payload")
			}
			if initial.ImportKey != "" {
				o, e := t.TaskOccurrence(have.ID, "once")
				if e != nil {
					return 0, false, e
				}
				if o.Source != t.Source || o.ImportKey != initial.ImportKey {
					return 0, false, conflict("one-off occurrence has a different source identity")
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
	err = t.tx.QueryRowContext(t.ctx, `INSERT INTO tasks(label,project_page_id,repeat_unit,repeat_every,anchor_day,repeat_until_day,reminder_local_time,reminder_zone,source,import_key,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,`+Now+`,`+Now+`) RETURNING id`, in.Label, in.ProjectPageID, nullIfEmpty(in.RepeatUnit), every, nullIfEmpty(in.AnchorDay), nullIfEmpty(in.RepeatUntilDay), nullIfEmpty(in.ReminderLocalTime), nullIfEmpty(in.ReminderZone), t.Source, nullIfEmpty(importKey)).Scan(&id)
	if err != nil {
		return 0, false, err
	}
	if in.RepeatUnit == "" {
		_, err = t.insertTaskOccurrence(id, initial)
	}
	return id, false, err
}

func (s *Store) CreateTask(ctx context.Context, source string, in TaskSpec, importKey string) (id int64, existing bool, err error) {
	err = s.Do(ctx, source, func(t *Tx) error { var e error; id, existing, e = t.CreateTask(in, importKey); return e })
	if err != nil {
		id, existing = 0, false
	}
	return
}

func (t *Tx) liveTaskVersion(id int64, version string) (*Task, error) {
	p, err := t.Task(id)
	if err != nil {
		return nil, err
	}
	if p.Version != version || p.DeletedAt != "" {
		return nil, conflict("live task and current version required")
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
		return invalid("cadence is fixed; shorten an end with StopTask")
	}
	_, err = t.tx.ExecContext(t.ctx, `UPDATE tasks SET label=?,project_page_id=?,reminder_local_time=?,reminder_zone=? WHERE id=?`, in.Label, in.ProjectPageID, nullIfEmpty(in.ReminderLocalTime), nullIfEmpty(in.ReminderZone), id)
	return err
}

func (t *Tx) StopTask(id int64, version, through string) error {
	if !IsDay(through) {
		return invalid("task recurrence end must be exact")
	}
	p, err := t.liveTaskVersion(id, version)
	if err != nil {
		return err
	}
	if p.RepeatUnit == "" {
		return invalid("one-off task has no recurrence to end")
	}
	if p.RepeatUntilDay != "" && through > p.RepeatUntilDay {
		return invalid("a recurrence end may only shorten")
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
		return conflict("task changed since this version")
	}
	if deleted == (p.DeletedAt != "") {
		return nil
	}
	q := `UPDATE tasks SET deleted_at=NULL WHERE id=?`
	if deleted {
		q = `UPDATE tasks SET deleted_at=` + Now + ` WHERE id=?`
	}
	_, err = t.tx.ExecContext(t.ctx, q, id)
	return err
}

func (t *Tx) insertTaskOccurrence(taskID int64, in OccurrenceInput) (int64, error) {
	var id int64
	err := t.tx.QueryRowContext(t.ctx, `INSERT INTO task_occurrences(task_id,occurrence_key,due_day,state,completed_at,reminder_mode,reminder_override_at,source,import_key,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,`+Now+`,`+Now+`) RETURNING id`, taskID, in.OccurrenceKey, nullIfEmpty(in.DueDay), in.State, nullIfEmpty(in.CompletedAt), in.ReminderMode, nullIfEmpty(in.ReminderOverrideAt), t.Source, nullIfEmpty(in.ImportKey)).Scan(&id)
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
		return nil, false, conflict("task changed since this version")
	}
	belongs, err := taskSlot(t.ctx, p.TaskSpec, in.OccurrenceKey)
	if err != nil {
		return nil, false, err
	}
	if !belongs {
		return nil, false, invalid("occurrence key does not belong to task")
	}
	if in.ImportKey != "" {
		have, e := scanTaskOccurrence(t.tx.QueryRowContext(t.ctx, occurrenceSelect+`WHERE o.source=? AND o.import_key=?`, t.Source, in.ImportKey))
		if e == nil {
			if have.TaskID != taskID || have.OccurrenceKey != in.OccurrenceKey {
				return nil, false, conflict("occurrence source key belongs to another slot")
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
			return nil, false, conflict("occurrence slot has a different source identity")
		}
		have.withTask(p)
		return have, true, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, false, e
	}
	if p.DeletedAt != "" {
		return nil, false, conflict("new occurrence requires a live task")
	}
	if in.State == "open" && p.RepeatUntilDay != "" && in.OccurrenceKey > p.RepeatUntilDay {
		return nil, false, invalid("open occurrence is after recurrence end")
	}
	if _, err = t.insertTaskOccurrence(taskID, in); err != nil {
		return nil, false, err
	}
	o, err := t.TaskOccurrence(taskID, in.OccurrenceKey)
	return o, false, err
}

func (t *Tx) MaterializeTaskOccurrence(taskID int64, taskVersion, key string) (*TaskOccurrence, bool, error) {
	due := key
	if key == "once" {
		due = ""
	}
	return t.CaptureTaskOccurrence(taskID, taskVersion, OccurrenceInput{OccurrenceKey: key, OccurrenceChanges: OccurrenceChanges{DueDay: due, State: "open", ReminderMode: "inherit"}})
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
		return conflict("live occurrence and current version required")
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
		return conflict("occurrence changed since this version")
	}
	if deleted == (o.DeletedAt != "") {
		return nil
	}
	if !deleted && o.State == "open" && p.RepeatUntilDay != "" && key > p.RepeatUntilDay {
		return invalid("occurrence cannot reopen after recurrence end")
	}
	q := `UPDATE task_occurrences SET deleted_at=NULL WHERE id=?`
	if deleted {
		q = `UPDATE task_occurrences SET deleted_at=` + Now + ` WHERE id=?`
	}
	_, err = t.tx.ExecContext(t.ctx, q, o.ID)
	return err
}

const maxTaskOccurrences = 10000

// TaskOccurrences reads an inclusive deadline window from one consistent snapshot.
// It includes outcomes, and explicit historical reads retain tombstones. Undated rows
// remain available by identity. The contract's sparse merge runs before deadline filtering.
func (s *Store) TaskOccurrences(ctx context.Context, taskID int64, from, through string, includeDeleted bool) (_ []TaskOccurrence, err error) {
	if !IsDay(from) || !IsDay(through) || from > through {
		return nil, invalid("task deadline window must have ordered exact days")
	}
	tx, err := s.DB.R.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := scanTask(tx.QueryRowContext(ctx, taskSelect+`WHERE t.id=?`, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("no task %d", taskID)
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
			return nil, errors.Join(invalid("task window exceeds 10000 occurrence limit"), rows.Close())
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
			return invalid("task window exceeds 10000 occurrence limit")
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
			return appendOccurrence(TaskOccurrence{TaskID: taskID, TaskVersion: p.Version, OccurrenceKey: key, Virtual: true, OccurrenceChanges: OccurrenceChanges{DueDay: key, State: "open", ReminderMode: "inherit"}})
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
