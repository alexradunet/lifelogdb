# 0007 — Personal tasks, recurring occurrences and reminder intent

- **Date:** 2026-10-07
- **Status:** accepted
- **Answers:** [issue 0025](../issues/0025-personal-planning-has-no-durable-task-model.md).
- **Scope:** an accepted storage contract, implemented in the canonical init DDL with shared writer operations
  and executable contract coverage. Task interfaces, notification delivery and phone access are separate work.
  No migration is introduced.

## Problem

The owner wants personal projects, recurring tasks and reminders whose intent survives independently of an
application. The immediate priority is the smallest useful storage contract. A task's repeating instruction, one
occurrence's outcome and a notification's delivery are different facts; one mutable due date and completion flag
cannot retain all three. The request reopens [D23](../decisions/D23-no-tasks.md) without authorizing automatic
conversion of historical plans, goals or checkboxes into obligations.

## Options

| Option | Change and cost | After-freeze implications |
|---|---|---|
| Keep checklists in prose | No DDL change. Suitable for note-local plans; no defined occurrence identity or structured reminder intent. | Typed planning tables could still be added later. |
| One row per task, advanced to its next deadline | A small task table, but advancing overwrites the previous occurrence; a predecessor chain makes ownership of the repeating rule unclear. | Separating definitions and occurrences later needs an explicit interpretation of old rows; lost outcomes cannot be recovered. |
| Task definitions plus occurrences | Two typed tables, ordinary page references for project context, explicit recurrence and reminder semantics. Preserves each occurrence without creating named pages for task labels. | New tables and references to existing entities are additive in shape; existing scope, time and lifecycle contracts must also change deliberately. |
| Separate planning database | Keeps planning outside this contract, with additional reference, backup and restore responsibilities. | Could evolve independently, but the chosen single-file record would no longer contain the complete plan. |
| Generic extension or property system | Introduces type registration, validation and compatibility obligations beyond the requested planning domain. | Adds permanent machinery before a concrete second extension needs it; conflicts with [D2](../decisions/D02-typed-strict-tables.md). |

## Recommendation

Add task definitions and task occurrences. Use existing pages for project context. The following is a proposed
logical shape, not executable DDL; constraint and trigger mechanisms must be demonstrated before acceptance.

### Projects and task identity

A project is an ordinary non-journal plain page. It supplies the project's name, prose and existing links. A task
may refer to one such page through `project_page_id`; a standalone task has no project. This relationship has one
home in the task row. Tasks are independently identified records like sessions, not `entities` and not graph-link
endpoints. Their labels may repeat and are not wikilink handles. Project pages retain the usual name ownership.

Project here means context and grouping, not a workflow state. An empty project may still have a page; a project
with no open tasks is not automatically complete. A typed active/completed project facet can be added when that
question is wanted. Moving a task between projects updates its current grouping, not a history of former grouping.

Admission of a project reference requires a live non-journal plain page. Retained references prevent promotion to
an incompatible entity type and exclude the page from ghost cleanup. Tombstoning the page retains the reference
and does not complete, skip or delete its tasks; queries must be able to report the tombstoned context.

### Proposed data

| Table | Proposed domain fields | Meaning |
|---|---|---|
| `tasks` | `id`, `label`, `project_page_id` | Stable independent identity and current task description/context. |
| `tasks` | `repeat_unit`, `repeat_every`, `anchor_day`, `repeat_until_day` | Optional calendar recurrence, specified below. |
| `tasks` | `reminder_local_time`, `reminder_zone` | Optional default reminder on an occurrence's current due day. |
| `task_occurrences` | `id`, `task_id`, `occurrence_key` | Stable instance identity, unique per task and never transferred. |
| `task_occurrences` | `due_day`, `state`, `completed_at` | Current deadline and outcome; completion time is optional evidence. |
| `task_occurrences` | `reminder_mode`, `reminder_override_at` | Inherit the default, suppress a reminder, or select an absolute instant. |

Both tables also need their own creation/update timestamps, edit revision, tombstone, immutable source and optional
source-scoped import key, following the existing [identity](../architecture/entity-model.md),
[imports](../contract/imports.md) and [lifecycle](../decisions/D11-tombstones.md) conventions. References are typed
foreign keys where applicable. New tables are `STRICT`; checks are named. No free-form properties or copied project
names are introduced. A task label is plain text, not another prose body with a second wikilink-extraction path.

One-off tasks have exactly one occurrence with key `once`, created with their definition in one transaction.
Recurring tasks use the original scheduled calendar day as their occurrence key, in the contract's exact day
format. The task ID and key are immutable, with a unique pair even after tombstoning. A recurring key must belong
to its task's anchored calendar sequence. The original scheduled day is read from the key, not stored a second time.

For a newly materialized recurring occurrence, `due_day` starts as its key. It may subsequently move or be cleared;
the key does not move. Thus an October occurrence moved into November and November's own occurrence stay distinct.
Ordinary new occurrences begin `open`, with reminder mode `inherit` and no completion evidence or reminder override.
Materialization requires a live definition and a key within its current end boundary. A historical import may
retain an explicitly evidenced done/skipped outcome beyond a shortened boundary, but cannot create new open work
there; imports do not bypass parent liveness or revive tombstones.
Retrying materialization uses the unique task/key pair and preserves the existing due day, outcome and overrides.
It never revives or resets a tombstoned or edited occurrence. Imported records must verify any source-key and
task/key bindings agree; a conflict must not silently select a different occurrence.

Task labels and project references describe the current definition for all its occurrences. This design does not
retain old label text or former project membership. Editing those fields never changes occurrence identity.

### Bounded calendar recurrence

The initial profile has no recurrence, or every positive N days, weeks, months or years. No recurrence means all
four recurrence fields are NULL. Otherwise the unit, interval and anchor day are required; the inclusive end day
is optional. An end before the anchor represents an empty sequence, which permits stopping a future-starting task.
Candidate results outside the supported calendar range do not wrap or become invalid dates.

- Day and week cadences advance by N and 7N calendar days from the original anchor, respectively.
- Month and year cadences calculate each target month/year from the original anchor. Retain its day of month when
  present and otherwise use that target month's last day. January 31 therefore yields February's last day and
  then March 31. A February 29 yearly anchor returns to February 29 in a leap year.
- These are calendar days, not fixed elapsed durations. One weekly rule selects the anchor's weekday; multiple
  weekdays, completion-relative recurrence, arbitrary recurrence strings and timed appointments are deferred.
- Queries expand only an explicit finite day window. Missing occurrence rows are virtual open occurrences inside
  the admitted sequence. A read does not materialize them. A write that changes an occurrence or needs a durable
  reminder reference materializes its deterministic key. No process creates decades of empty future rows.
- Missing a scheduled occurrence leaves it open/overdue; it does not complete, skip, collapse into another date or
  advance the anchor. For deadline queries, any persisted row replaces its virtual slot by key before filtering on
  the current `due_day`. Also consider persisted occurrences whose keys lie outside the requested window but whose
  due days moved into it. A moved-out or undated row is not an open deadline in its original window. Tombstones and
  skipped rows suppress regeneration of their slots.

The anchor, unit and interval are fixed on a recurring definition. A changed cadence ends that definition and
starts another, with a new ID and explicit anchor. The old outcomes remain attached to the old definition; no
generic series-version or predecessor framework is needed. A one-off definition is not converted into a recurring
one. The inclusive end may be shortened; extending it also starts a new definition rather than resurrecting slots.

Ending a definition must behave identically whether future slots were already materialized. In the same
transaction that shortens its end, mark existing open occurrences whose original keys fall after the new end as
skipped; leave existing done outcomes unchanged. Those keys cannot subsequently be reopened under that ended
definition. An earlier occurrence moved past the cutoff remains open because its original key precedes the cutoff.
An out-of-range cutoff is refused; whole-task tombstoning remains available under the lifecycle rules below.

### Outcomes and lifecycle

An occurrence has one current state: `open`, `done` or `skipped`. `completed_at` may be present only for `done` and is
an explicitly supplied completion instant. A known-done item with unknown completion time keeps NULL; importing it
must not invent midnight or substitute its import timestamp. Ordinary capture can supply the mark-done instant.
Date-only completion evidence is not structured by this initial profile; adding a completion day later would need
its own attribution rule. The reminder zone never supplies missing completion evidence.

Reopening clears completion evidence and returns the same occurrence to `open`; it does not allocate a replacement.
Changing done to skipped also clears completion evidence. These are current outcomes plus snapshots, consistent
with [D12](../decisions/D12-no-revision-tables.md), not a log of every complete/reopen transition. Completion, skip
and reopen do not change other occurrences or create journal prose, measurements, recorded sessions or `at` links.

Tombstoning a task suppresses all its virtual occurrences, persisted active work and reminders from active reads,
while retaining its persisted occurrences for historical reads. Tombstoning one occurrence suppresses only that
slot. Explicit restoration,
including its effect on overdue reminders, needs a named writer operation; ordinary generation/import replay must
not restore it. Ending recurrence and tombstoning a task are distinct: the former can leave earlier open work active.

Writers compare revisions inside the normal write transaction. Occurrence mutations affect its revision; changes
to inherited task context, lifecycle or reminder defaults require readers to consider the task's revision too.
An operation resolving inherited values must validate both versions before applying its result. No-op, exhaustion,
rollback and immutable-identity protections need the same behavioral coverage as existing editable records.

### Reminder intent

`reminder_local_time` is an exact minute clock `HH:MM`, from `00:00` through `23:59`, and `reminder_zone` names an
IANA time zone. Both are present or both are NULL. The zone is a chosen scheduling basis, not a claim about the
owner's location. The rule is one reminder on the current due day at that clock time in that zone.

An occurrence's `reminder_mode` is one of:

| Mode | Meaning | `reminder_override_at` |
|---|---|---|
| `inherit` | Resolve the task's default against the current due day; no default or no due day means no reminder. | NULL |
| `off` | Suppress this occurrence's reminder without skipping the task. | NULL |
| `at` | Use this explicitly selected UTC instant, including for an undated task or a snooze. | Required |

Defaults can change for open occurrences that inherit them. Moving a due day recomputes an inherited reminder;
it leaves an absolute override intact. Only live open occurrences of live tasks are eligible. An override and a
default are not two reminders, and there is no stored derived `next_reminder_at` or `overdue` flag.

Proposed clock resolution is deliberately explicit: use the earlier instant for a repeated local clock; for a
missing local clock, advance by the size of the forward clock change. A gap covering a whole local date is unresolved
rather than silently selecting another day. An unknown zone or unavailable transition data is also unresolved,
not UTC or the current device's zone. The application must surface unresolved intent. Fixed transition vectors,
including non-hour changes, are required before adopting this policy; the UTC result remains derived from the
rule and the zone data in use. A user who needs a fixed instant selects `at`.

This schema records when a reminder is wanted. Device routing, credentials, delivery attempts, acknowledgement,
cross-device actions and crash-safe retry state belong to later application work. A delivery acknowledgement must
not be represented by marking a task done. Additional durable delivery state can reference occurrence IDs without
changing task identity; restoring a snapshot is not authority to resend old notifications automatically.

### Contract integration and deliberate limits

Adoption would rewrite [D23](../decisions/D23-no-tasks.md) for this bounded planning scope and
[D15](../decisions/D15-recurrence.md) for anchored tasks with persisted occurrence outcomes. The latter's existing
read-only expander sketch is insufficient by itself. [D22](../decisions/D22-events.md) would retain the separation
between intentions and recorded periods/sessions. [D10](../decisions/D10-time-model.md) and the metadata time rules
need to distinguish planned deadlines, chosen reminder instants and completion evidence from write timestamps.

The [goals](../architecture/goals-and-principles.md), [non-goals](../architecture/non-goals.md),
[entity model](../architecture/entity-model.md), [product concepts](../architecture/product-concepts.md),
[integrity checks](../contract/integrity-checks.md), [imports](../contract/imports.md),
[writer guide](../guides/building-a-writer.md), schema comments/metadata, threat-model questions, diagrams and
schema totals must agree when the DDL changes. Each rule gets one canonical home then; this dated proposal remains
the design record. Application scope statements would be updated with adoption, while feature interfaces can follow.

This proposal adds no project hierarchy/state, task dependencies, arbitrary properties, extra reminder channels,
notification service, phone access, event/attendance model or automatic habit completion. It adds no measurement
or location semantics. [D13](../decisions/D13-migrations-and-freeze.md) still governs evolution: before the freeze,
edit the canonical init DDL and recreate test files; do not introduce migrations or a migration runner.

## Validation

The following synthetic acceptance cases define the implementation scope independently of UI or notification
delivery. Storage cases run against file-backed SQLite; calendar and clock answers come from the shared
[planning vectors](../contract/planning.md), with writer tests for transactional operations and bounded reads.

| Case | Required answer |
|---|---|
| Two tasks labelled `Buy supplies`, one in each of two project pages | Distinct task IDs; no name collision or accidental merge. |
| Project page rename, promotion attempt and tombstone | Rename retains references; incompatible promotion refuses; tombstone preserves task context without completing tasks. |
| One-off creation interrupted or duplicate `once` insert | No committed definition lacking its occurrence; exactly one reserved slot. |
| Monthly anchor `2026-01-31` | Keys `2026-01-31`, `2026-02-28`, `2026-03-31`; no February-induced drift. |
| Every two weeks from `2026-10-05` | Keys `2026-10-05`, `2026-10-19`, `2026-11-02`. |
| Yearly anchor `2024-02-29` | `2025-02-28`, `2026-02-28`, `2027-02-28`, `2028-02-29`. |
| Move key `2026-10-01` to due day `2026-11-01` | Its ID/key survive; the key `2026-11-01` is a separate occurrence; deadline queries show both. |
| Move October's due day to November, then clear it | It leaves October's deadline list; after clearing it also leaves November's deadline list, retaining its original key. |
| Repeated generation/import after move, completion, skip or tombstone | No duplicate, overwritten edit, cleared outcome or implicit revival. |
| Shorten end to `2026-10-31`, with and without persisted November slots | No November virtual opens; persisted November open slots become skipped; done slots remain done. |
| Original October slot moved to November when October end is set | Earlier original slot remains open; reopening a slot originally beyond the end refuses. |
| Materialize beyond a shortened end or under a tombstoned task | No new open row; a task tombstone also hides virtual overdue slots from active reads. |
| New cadence replaces old definition | Old keys/outcomes retain meaning; new anchor governs only the new definition. |
| Done with unknown time; complete/reopen/skip | Unknown time remains NULL; reopening/skipping clears completion evidence; other occurrences are unchanged. |
| Inherit, off and absolute reminder; change deadline/default | Only inherited reminder changes; off remains off; absolute override remains fixed. |
| Repeated/missing clock, non-hour transition, skipped local date, unknown zone | Exact proposed resolution or explicit unresolved result; no device-zone fallback. |
| Malformed dates/clocks, partial reminder pairs, bad modes or cadence, invalid keys | Refuse without side effects; supported-range arithmetic terminates without overflow or wrapping. |
| Stale definition/occurrence revisions, failure halfway through stop, cancellation | Atomic refusal/rollback, retained prior state and no accidental reminders. |
| Close and reopen database; snapshot and restore | Definitions, outcomes, overrides and unique identities survive; database restoration itself sends nothing. |

Implementation must add focused contract suites and named mutants for each new enforced rule; update the
[threat model](../contract/threat-model.md) for any cross-table metadata rule; and execute cookbook examples for
creation, bounded expansion/merged reads, rescheduling, completion, stopping and reminder resolution. Calendar and
clock vectors belong in a language-neutral contract page with independent expected results, not only in a writer's
code. Storage and transaction behavior need a minimal real writer path for validation even if user interfaces and
notification delivery come later. Run the baseline in [the suite guide](../../tests/README.md); no current test
should be relaxed merely to admit this proposal.

## Outcome

Accepted and implemented on 2026-10-07 following the owner's request to proceed. The canonical DDL contains
`tasks` and `task_occurrences`; [D23](../decisions/D23-no-tasks.md) and [D15](../decisions/D15-recurrence.md) describe
the adopted scope. A generated `once_key` and deferred foreign key make a one-off definition and its occurrence
commit together. Named constraints, typed references and triggers guard the stored rules; the
[planning profile](../contract/planning.md) owns calendar expansion, sparse reads and reminder clock resolution.
The [cookbook](../cookbook/tasks.md), semantic integrity queries and contract suites exercise the storage model.
Shared writer operations provide the validation path without exposing a planning interface or sending reminders.

Focused implementation checks passed on Linux with Go 1.27.1:

- `go test ./tests -run 'TestSuites/(document|diagrams)$' -count=1`
- `go test ./internal/core -run 'Test(Integrity|PlanningIntegrity|TemporalIntegrity)' -count=1`

The integrity regression first reproduced three false clean results after deliberate semantic damage: a journal
page used as project context, an occurrence key outside its cadence, and open work beyond a shortened end. The
new diagnostics detect those cases while retaining valid historical outcomes and tombstones. These focused
checks do not assert a full repository baseline or notification delivery.
