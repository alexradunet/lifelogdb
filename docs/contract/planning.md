# Personal tasks and reminder intent

The storage rules live in `tasks`, `task_occurrences` and `lifelog_meta` in
[the schema](../schema/schema.sql). [D23](../decisions/D23-no-tasks.md) explains the independent identities and
project pages; [D15](../decisions/D15-recurrence.md) explains bounded recurrence. This page specifies the calendar
and clock algorithms that a constraint cannot supply, and the writer operations that combine them. The
[task cookbook](../cookbook/tasks.md) demonstrates the SQL. Planning never establishes a recorded session,
measurement, physical presence or completion of a habit.

## Calendar expansion

Use the proleptic Gregorian calendar and the supported [exact-day range](exact-time.md). A query supplies finite,
valid inclusive `from` and `through` days, with `from <= through`. Refuse malformed windows; never substitute an
unbounded expansion. A result consists of deterministic original calendar keys, not generated database IDs.

For an anchor and positive integer interval N, consider nonnegative integer indices k beginning at zero:

- `day`: anchor plus k × N calendar days.
- `week`: anchor plus k × N × 7 calendar days.
- `month`: anchor's month plus k × N months; use the anchor's day of month, clamped to the last day of the target month.
- `year`: anchor's year plus k × N years; use its month and day, clamped to the target month's last day.

Calculate each candidate from the original anchor, never from the preceding clamped date. Keep only candidates
inside the query window and at or before the optional inclusive end. An end before the anchor yields no keys.
Stop at the supported range boundary; arithmetic must not overflow or wrap. Seek to the query window rather
than iterating an unbounded series. Weekly recurrence follows the anchor's weekday. Completion-relative rules,
multiple weekdays and arbitrary recurrence strings are outside this profile.

These vectors are the shared conformance inputs and independent expected results (executed). `-` means absent
or an empty key list. `Keys` is a comma-separated ordered list; no spaces are part of a key.

| ID | Anchor | Unit | Every | Until | From | Through | Keys |
|---|---|---|---|---|---|---|---|
| month-clamp | 2026-01-31 | month | 1 | - | 2026-01-01 | 2026-03-31 | 2026-01-31,2026-02-28,2026-03-31 |
| month-leap | 2024-01-31 | month | 1 | - | 2024-02-01 | 2024-03-31 | 2024-02-29,2024-03-31 |
| every-two-weeks | 2026-10-05 | week | 2 | - | 2026-10-01 | 2026-11-02 | 2026-10-05,2026-10-19,2026-11-02 |
| every-three-days | 2026-10-05 | day | 3 | - | 2026-10-06 | 2026-10-12 | 2026-10-08,2026-10-11 |
| leap-year-anchor | 2024-02-29 | year | 1 | - | 2025-01-01 | 2028-12-31 | 2025-02-28,2026-02-28,2027-02-28,2028-02-29 |
| inclusive-end | 2026-01-31 | month | 1 | 2026-02-28 | 2026-01-01 | 2026-04-30 | 2026-01-31,2026-02-28 |
| stopped-before-anchor | 2026-10-05 | week | 1 | 2026-10-04 | 2026-10-01 | 2026-11-30 | - |
| before-anchor | 2026-10-05 | day | 1 | - | 2026-09-01 | 2026-10-04 | - |
| lower-bound-leap | 0000-02-28 | day | 1 | - | 0000-02-28 | 0000-03-01 | 0000-02-28,0000-02-29,0000-03-01 |
| upper-bound-day | 9999-12-30 | day | 1 | - | 9999-12-30 | 9999-12-31 | 9999-12-30,9999-12-31 |
| upper-bound-month | 9999-11-30 | month | 2 | - | 9999-11-01 | 9999-12-31 | 9999-11-30 |
| huge-interval | 2026-01-01 | year | 9223372036854775807 | - | 2026-01-01 | 9999-12-31 | 2026-01-01 |

## Reading deadlines

A missing recurring slot inside the admitted sequence is virtual open work. A read does not insert it.
To read deadlines in a window:

1. Expand keys in that window for live recurring definitions.
2. Replace each virtual slot with its persisted occurrence, when present, by `(task_id, occurrence_key)` **before**
   filtering its current deadline, outcome or tombstone. A tombstoned, skipped or undated row still suppresses its slot.
3. Also include persisted occurrences whose keys are outside the window but whose current due days are inside it,
   and one-off occurrences. Deduplicate by task/key, then apply the requested state and deadline filters.
4. Active reads require a live task and a live occurrence. Historical reads retain persisted outcomes and show
   the task's and project page's tombstones explicitly; a tombstoned project page does not hide its tasks.
   For a tombstoned task, historical reads return persisted occurrences only, never invented virtual history.

An October key moved into November consequently appears only under its November deadline, alongside November's
own slot. Clearing its due day removes it from deadline windows, not from undated-work or historical reads.
Overdue is a query relative to an explicitly selected day; it is never a stored flag. A finite deadline window
must not silently claim to include all overdue work from before its lower bound.

## Writer operations

Use the normal [write transaction](connections.md) and compare revisions inside it. An occurrence operation that
uses inherited context or reminder values validates both the task and occurrence revisions. A task edit does not
needlessly rewrite every occurrence. No-ops, exhaustion and rollback obey `lifelog_meta.edit_revisions`.

Create a one-off definition and its `once` occurrence in one transaction. Materializing a recurring key first
validates its membership and live parent, then inserts its initial open row with due day equal to the key and
inherited reminder intent. On task/key conflict, read the existing row without resetting any field or reviving a
tombstone. If an import key is also supplied, verify both identity bindings agree; either conflict is an error
when it selects a different row. Import replay is not restoration.

Rescheduling preserves the original key. Reopening clears completion evidence; changing done to skipped clears
it too. A known-done occurrence with unknown completion time remains NULL. Do not infer a completion instant from
a due day, a date-only source, a reminder zone or the time of import. An ordinary mark-done operation may explicitly
supply its current instant. No operation synthesizes journal prose or observed facts.

Shortening recurrence compares the definition's revision and updates its end in the same transaction that skips
existing open occurrences whose original keys are after the cutoff. Already-done outcomes survive. An earlier key
moved past the cutoff remains open. This must produce the same active work with or without previously materialized
future slots. The schema guards the fixed cadence and end boundary; a changed cadence or an extension begins a new
definition. A historical import may retain an evidenced done/skipped outcome beyond the current end, but cannot
introduce open work there. Whole-task tombstones suppress all virtual work and reminders; explicit restoration
requires its own named writer operation, including a policy for overdue reminders.

## Resolving a reminder clock

The schema stores a default local minute clock and IANA zone, or an occurrence's `inherit`, `off` or `at` choice.
Resolve only live open occurrences of live tasks. `off` means no reminder. `at` selects the stored UTC instant.
`inherit` needs both a due day and a task default; without either there is no reminder. Use the occurrence's
current due day, not its key. A project page's tombstone has no bearing on eligibility.

For inherited intent, combine the due day and clock with seconds zero in the selected zone:

- If the local clock names one instant, use it.
- If a backward transition repeats that clock, use the earlier instant.
- If a forward transition omits that clock, advance it by the size of that transition's gap, then resolve it.
  A 30-minute gap shifts by 30 minutes; it does not round to the first valid minute.
- If the transition skips the entire local date, report **unresolved** rather than selecting another day.
- An unknown zone, unavailable transition data or a result outside the supported instant range is **unresolved**.
  Never fall back to UTC, an inferred location or the device's zone.

Return absence and unresolved intent as different results. Unresolved intent must be visible to the caller; it
must not become a guessed timestamp. The UTC result is derived using the zone data in use and is not another
stored authority. Changing defaults or due days changes inherited reminders; it never moves an absolute override.

These fixed transition vectors exercise the resolution policy (executed). They use the named zones' recorded
transitions; the zone database is needed to interpret future changes to civil-time rules.

| ID | Day | Clock | Zone | Result |
|---|---|---|---|---|
| year-one-midnight | 0001-01-01 | 00:00 | UTC | 0001-01-01T00:00:00.000Z |
| utc | 2026-10-07 | 09:00 | UTC | 2026-10-07T09:00:00.000Z |
| bucharest | 2026-10-07 | 09:00 | Europe/Bucharest | 2026-10-07T06:00:00.000Z |
| repeated-hour | 2026-11-01 | 01:30 | America/New_York | 2026-11-01T05:30:00.000Z |
| missing-hour | 2026-03-08 | 02:30 | America/New_York | 2026-03-08T07:30:00.000Z |
| repeated-half-hour | 2026-04-05 | 01:45 | Australia/Lord_Howe | 2026-04-04T14:45:00.000Z |
| missing-half-hour | 2026-10-04 | 02:15 | Australia/Lord_Howe | 2026-10-03T15:45:00.000Z |
| skipped-date | 2011-12-30 | 09:00 | Pacific/Apia | unresolved |
| device-zone | 2026-10-07 | 09:00 | Local | unresolved |
| unknown-zone | 2026-10-07 | 09:00 | Unknown/Nowhere | unresolved |

This is reminder **intent**. It specifies no notification provider, device routing, credentials, delivery retries,
acknowledgement or cross-device actions. Delivery acknowledgement is not task completion. A later delivery record
may reference a persisted occurrence ID; a snapshot restore itself is never authority to resend notifications.
