# 0015 — Starting Mood as a habit disables normal mood capture

- **Date:** 2026-10-06
- **Status:** resolved
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

Resolved by a convention, with no guard on one identity. The cause was that a 1-5 scale and a 0/1 habit were
indistinguishable by unit (`metrics.unit` was `''` for both), so the protection had to name metric 1. That covered
only the seeded Mood: a scale the owner registered the same way (Energy 1-5, Pain 0-10) could still become a habit
and break its own capture exactly as above. A scale now names its range as its unit. Mood is seeded with unit `1-5`,
the `metrics.unit` comment reads `'' only for 0/1 habits; a scale names its range ('1-5', '0-10')`, and the rule that
already existed, [D24](../decisions/D24-habits.md)'s "a habit is a unitless metric" in `habit_periods_check_insert`
and `habit_periods_check_update`, refuses Mood and every other scale with a unit. The two `metric_id = 1` guards, the
`m.id=1` term of the habit-validity integrity query and the writer's metric-id check are gone; the writer holds a
metric whose unit is `1-5`, Mood among them, to whole numbers from 1 to 5 ([D6](../decisions/D06-mood-is-a-measurement.md)).
[RFC 0006](../rfcs/0006-stable-names-and-life-periods.md)'s proposal to protect the seeded identity is not needed:
the protection follows the unit, so it survives a rename.

`metrics_unit_fixed` forbids changing a unit after creation, so this change is possible only before the schema
freeze ([D13](../decisions/D13-migrations-and-freeze.md)): a file created before it keeps a unitless Mood that this
rule does not protect, and is rebuilt, not migrated. A scale registered with unit `''` is a unitless metric by
definition: the convention, not the database, keeps it out of habits. The `habits` suite refuses periods on Mood
and on registered `1-5` and `0-10` scales on insert and update and still accepts a unitless metric; the integrity
suite detects a period written on a metric with a unit while the guard was dropped; each rule has a mutant. This
narrow fix is still not a correction story for every mistaken habit registration or permission to delete
measurement history.

The importer's rule that a reading's unit must be written in the source beside its number (so that nothing is
relabeled or converted) applies to physical units. It briefly made a source line such as `mood: 4`, which does not
write `1-5`, unimportable. A unit that is a range (two whole numbers around a hyphen, the lower below the upper) is
a property of the metric, not a quantity the source writes, so the importer takes the number alone as the evidence of
a scale's reading, the reading takes the metric's range, and the writer refuses any value that is not a whole number
inside it, for every range unit and not only `1-5`. Proposing or registering a metric whose title is an existing
scale, with no unit or with its own range, adopts that metric; a different unit is still a conflict, since a unit
never changes. `weight: 70` still needs `kg` written beside the 70. The importer tests import `mood: 4` as a Mood
reading of 4, refuse `mood: 7`, `mood: 0` and `mood: 2.5` with nothing stored, refuse a word as a scale's number,
keep requiring the unit evidence of `weight`, and register a proposed `energy` scale with unit `1-5` and import
`energy: 3`; the core tests hold `Pain` (`0-10`) and `Grade` (`1-100`) to their ranges on a reading and on a
correction. Each was observed failing before the change (registering Mood with no unit was refused as a unit conflict,
so no Mood line could be imported; Pain 11 and Grade 101 were accepted).
