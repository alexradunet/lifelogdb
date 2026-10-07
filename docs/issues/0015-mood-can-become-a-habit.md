# 0015 — Starting Mood as a habit disables normal mood capture

- **Date:** 2026-10-06
- **Status:** proposed
- **Seen in:** a synthetic writer audit starting from the seeded database.

## What happened

Starting the unused seeded Mood metric as a habit succeeded. Capturing a mood value of `4` then failed because
habit readings must be `0` or `1`. The writer's classification depends on the existence of any habit period, so
merely ending the period is not an undo operation.

The start followed by refused capture was observed with a temporary synthetic probe against baseline `cfb7fe2`,
SQLite 3.53.4 on Windows. The persistence of classification after ending a period follows from the inspected
classification rule; a separate stop/retry regression is still needed.

## Reproduce

On a fresh database built from [schema.sql](../schema/schema.sql), through the writer:

1. Start habit `Mood` on `2026-09-01` before recording any readings.
2. Capture that day with synthetic prose and mood `4`.

Observed: step 1 succeeds and step 2 is refused as a non-binary habit value. Expected: reject the incompatible
registration before it changes the meaning of normal mood capture, leaving no period behind.

## Rules involved

- [D6](../decisions/D06-mood-is-a-measurement.md), [D7](../decisions/D07-measurements.md) and [D24](../decisions/D24-habits.md).
- The seeded metric and `habit_periods` in [schema.sql](../schema/schema.sql).

## Resolution

Open. [RFC 0006](../rfcs/0006-stable-names-and-life-periods.md) proposes protecting the seeded metric's identity from
incompatible habit registration, including after a rename. This narrow fix must not be presented as a complete
correction story for every mistaken habit registration or as permission to delete measurement history.
