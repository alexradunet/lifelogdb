# 0025 — Personal planning has no durable task and reminder model

- **Date:** 2026-10-07
- **Status:** resolved
- **Seen in:** the owner's request to retain projects, recurring tasks and reminder intent, with schema design before application features.

## What happened

The owner reopened personal planning and selected projects, recurring tasks, and reminders on a computer and a
phone. The immediate request is a small durable schema that can support those features later. The owner agreed
with the direction of ordinary pages for project context, independent task definitions and individual task
occurrences, with reminder intent kept separately from delivery machinery.

The current contract deliberately keeps plans as prose and excludes structured tasks and recurrence. It cannot
distinguish a recurring instruction from the completion of one occurrence or retain a queryable reminder rule.
This is an owner-question incident, not a failed import or an observed database error. The examples below are
synthetic design cases, not personal records or claims that a planning import has been attempted.

## Reproduce

Inspect [D23](../decisions/D23-no-tasks.md), [D15](../decisions/D15-recurrence.md) and the tables in
[schema.sql](../schema/schema.sql). Consider these questions using a fresh database's existing concepts:

- Which open tasks belong to the page `House maintenance`?
- Was October's `Check smoke alarms` completed while November's occurrence remains open?
- If October's occurrence is moved to November, can it retain its identity without replacing November's occurrence?
- What does `09:00 Europe/Bucharest on the due day` mean for future occurrences, independently of the device that
  will eventually deliver the notification?

Page text can describe these intentions, but no current task state, occurrence identity or reminder contract
answers them as structured data. Recorded sessions, periods, measurements and location links have different
meanings and cannot be substituted for plans. This is contract inspection; no failing SQL reproduction is claimed.

## Rules involved

- [D23](../decisions/D23-no-tasks.md): the owner wanting planning here is its explicit reopen trigger; the earlier
  ambiguous-task import remains a reason to require explicit task capture.
- [D15](../decisions/D15-recurrence.md): the deferred calendar expander does not retain individual task outcomes.
- [D22](../decisions/D22-events.md): recorded periods and sessions describe observations.
- [D2](../decisions/D02-typed-strict-tables.md), [D10](../decisions/D10-time-model.md) and
  [D12](../decisions/D12-no-revision-tables.md): typed storage, honest time evidence and current state plus snapshots.
- [The change process](../process.md) and [D13](../decisions/D13-migrations-and-freeze.md).

## Resolution

Resolved on 2026-10-07 by [RFC 0007](../rfcs/0007-personal-tasks-and-occurrences.md). The canonical schema now has
independent tasks and occurrences, ordinary project-page references, and stored reminder intent. The
[planning contract](../contract/planning.md) defines bounded recurrence, slot identity and clock resolution with
shared vectors. Named storage guards, executable cookbook examples, integrity diagnostics and writer operations
cover the model; planning interfaces and notification delivery remain separate application work.
