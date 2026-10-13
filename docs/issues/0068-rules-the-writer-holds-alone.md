# 0068 — The writer holds rules the database could hold, and repeats rules the database holds

- **Date:** 2026-10-13
- **Status:** open
- **Seen in:** the owner's request to analyse the application for code to delete and for rules to enforce in
  `schema.sql` instead; checked against the code at `771c9e8` by reading every write of `internal/core` beside the DDL

## What happened

Two kinds of rule sit in Go.

**A rule the DDL already refuses, written again in Go.** The Go copy runs first, so the DDL's copy is never seen on
that path, and a second writer meets a different sentence. Each row is a fact of the code at `771c9e8`.

| Go check | DDL rule that refuses the same input |
|---|---|
| sha256 and mime grammar (`files.go`) | `files_sha256`, `files_mime` |
| point range, null island, radius (`places.go`) | `places_lat`, `places_lon`, `places_radius`, `places_not_null_island` |
| session endpoint grammar, offset, zone label, one basis, start required, UTC order (`sessions.go`) | `sessions_start_*`, `sessions_end_*`, `sessions_start_basis`, `sessions_end_basis`, `sessions_utc_order` |
| session kind is a live plain non-journal page (`sessions.go`) | `sessions_kind_insert`, `sessions_kind_update` |
| task label, recurrence fields, reminder pair and grammar (`tasks.go`, `task_time.go`) | `tasks_label`, `tasks_recurrence`, `tasks_repeat_*`, `tasks_anchor_day`, `tasks_reminder_*` |
| occurrence state, completion, reminder pair; key in the cadence; open after the end (`tasks.go`) | `task_occurrences_*`, `task_occurrences_admit`, `task_occurrences_open` |
| cadence fixed, end only shortens (`tasks.go`) | `tasks_fixed`, `tasks_end_shortens` |
| period boundary grammar and order (`periods.go`) | `periods_start_boundary`, `periods_end_boundary`, `periods_order` |
| every day and instant argument of a write (`check.go`) | the `*_day` and `*_at` CHECKs |

**A rule only the writer holds, which three decisions say so.** [D6](../decisions/D06-mood-is-a-measurement.md)
says the 1–5 range "is app-level validation, not a schema CHECK"; [D24](../decisions/D24-habits.md) says the 0/1
range of a check-in "is checked by the writer"; [D16](../decisions/D16-places.md) says "the writing application
refuses an `at` from any other page"; [D26](../decisions/D26-metric-categories.md) says a category is a plain page,
and only `Link` refuses a day page. A second writer, or `sqlite3` at the prompt, can write `Mood = 9`, a check-in of
2, an `at` from a person, or file a metric under a day page, and the four [integrity checks](../contract/integrity-checks.md)
do not notice.

**A rule with no home.** `Link` refuses a `part-of` link from a period to a tombstoned category ("a new period category
must be live"); no page of `docs/` states it.

## Reproduce

On a fresh database built from [schema.sql](../schema/schema.sql), with `sqlite3`:

```sql
INSERT INTO measurements(metric_id, day, value, source, created_at) VALUES (1, '2026-10-13', 9, 'ui', strftime('%Y-%m-%dT%H:%M:%fZ','now'));
-- accepted: Mood is a 1-5 scale
```

In Go: `s.Capture(ctx, "cli", "2026-10-13", "", ptr(9))` answers 422 before any SQL runs.

## Rules involved

- [AGENTS.md](../../AGENTS.md): "One home per rule", "a rule lives in the docs and the Go code cites it"
- [threat model](../contract/threat-model.md): "A buggy writer, importer or agent" is held by triggers
- D6, D16, D24, D26 (the sentences quoted above); D7 (`metrics_unit`); D8 (`link_kinds`)

## Resolution

Resolved by [plan 099](../plans/099-rules-into-the-schema.md): the first table's Go copies deleted, the DDL's refusal
reaching every surface as the 422 it already was; the writer-held rules of D6, D16, D24 and D26 made triggers
(`measurements_scale_range`, `measurements_habit_binary`, `habit_periods_check_*`, `links_endpoint_types`), the
decisions rewritten in place, one mutant a rule. The period-category rule stays in Go until it has a home.
