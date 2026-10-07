# 0014 — Date round trips admit values that the writer rejects

- **Date:** 2026-10-06
- **Status:** proposed
- **Seen in:** direct DDL probes compared with the writer's date and instant validators.

## What happened

The round-trip checks accepted `2026-01-01T24:59:59.000Z` as a write timestamp and `-0001-01-01` as a measurement day.
The writer rejected both. Other hour-24 variants also passed the instant check. These are synthetic boundary inputs,
observed with a temporary probe against baseline `cfb7fe2`, SQLite 3.53.4 on Windows, not permanent suite coverage.

A successful round trip alone does not establish the intended fixed-width civil-day or UTC-instant representation.
This issue concerns exact days and instants, not the separate proposal for uncertain historical period boundaries.

## Reproduce

On a fresh database built from [schema.sql](../schema/schema.sql), with the [required connection settings](../contract/connections.md):

1. In a write transaction insert a measurement for seeded Mood, day `2026-01-01`, value `3`, source `cli`, and
   `created_at = '2026-01-01T24:59:59.000Z'`.
2. Separately insert one with day `-0001-01-01` and `created_at = '2026-01-01T00:00:00.000Z'`.
3. Compare acceptance with the writer's canonical day and instant predicates.

Observed: both DDL inserts succeed while the corresponding writer validations fail. Direct SQL is intentional:
the question is whether another conforming writer sees the same storage boundary, not whether a client can bypass
its own validator.

## Rules involved

- `lifelog_meta.days`, `lifelog_meta.instants` and the date checks in [schema.sql](../schema/schema.sql).
- [D10](../decisions/D10-time-model.md) and [D2](../decisions/D02-typed-strict-tables.md).

## Resolution

Open. [RFC 0006](../rfcs/0006-stable-names-and-life-periods.md) proposes explicit canonical shape/range checks in
addition to round trips, with shared boundary vectors and mutants. The accepted calendar range, including year
zero, must be stated consistently; this issue does not independently choose a different calendar.
