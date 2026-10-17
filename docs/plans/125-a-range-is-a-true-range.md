# 125 — A range is a true range: the schema refuses a scale's unit with its high number first

> Dated implementation record. Tests use temporary folders and synthetic data only. The owner's live `life.db` is
> never opened by a test or by an agent: the owner runs the one change of the live file with `!` commands.

## Status

- **Date / baseline:** 2026-10-17, `63e0a36` (master, after plan 124).
- **Priority:** P3. **Effort:** S. **Risk:** LOW: one new trigger, applied to the owner's file in the same run as plan
  124's comments.
- **Status:** IN PROGRESS on the branch `plan-125-scale-range`.
- **Resolves:** the question of a score's range from [plan 121's Part 8 record](121-deep-modules-for-agents.md). The
  owner's words (2026-10-17), after the question was explained with the New metric form: "let's fix it".

## The change

A scale names its range as its unit, `lo-hi` of ASCII digits with `lo < hi` (D6), and `measurements_scale_range`
holds each reading of a scale to the whole numbers of that range. A unit of the same shape with `lo >= hi` (`5-1`,
`3-3`) is not a range, so today the schema takes it as the unit of a plain number, and `metrics_unit_fixed` keeps it
for good: one typo in the New metric form makes a metric that can never be a score. A new trigger refuses such a unit
when a metric is made, with SQLite's own sentence; nothing is written.

The owner's file gets the trigger from plan 124's program, in the same run as plan 124's comments: the program takes
new triggers, and it refuses when a metric of the file already has such a unit (the rule would then not hold for the
rows the file keeps).

## Done when

- [ ] An issue, D6 citing the trigger, `schema.sql`, a mutant and a suite witness change together; the writer's answer
      for such a unit is the schema's 422 on every surface; the baseline is green.
- [ ] The program applies plan 124's comments and this trigger together on a copy of the old schema, and refuses a
      file that keeps a metric with such a unit.
- [ ] The owner ran the change of the live file; the integrity check passes; the new binary serves.

## Results

The commits after `af93c8d`, the commit of this page:

| commits | what changed |
|---|---|
| `b3c5393` | [issue 0084](../issues/0084-a-range-out-of-order-is-a-plain-unit.md); the trigger `metrics_range_order`, the last statement of `schema.sql`, refuses a unit of the shape of a range whose low bound is not less than its high bound (`5-1`, `3-3`, `10-2`); D6 cites it; the facts suite witnesses it, and a mutant that removes it is killed |
| `6510682` | `register-metric` and the New metric forms answer the schema's 422 for such a unit, and nothing is written; the order compare in `isScale`, which repeated the trigger, goes |
| `59a48ad` | the comment of `measurements_scale_range` says what is true of its bounds and cites `metrics_range_order` (a change of the comment only) |

The checks at `59a48ad`, on Linux: the baseline with `-count=1`, `-shuffle=on`, `staticcheck`, `deadcode -test` (linux
and windows), `GOOS=windows go vet` and `gofmt`: all clean. The mutants are 261.

The owner's file gets plans 124 and 125 in one run of `meta124-bin` (`/home/cico/.local/share/lifelog-oneoff/meta124/`):
the comments of the tables `entities` and `people` and of the trigger `measurements_scale_range` by the
`writable_schema` procedure of plan 124, and the new trigger `metrics_range_order`. The program first checks that no
metric of the file has a unit that the new trigger refuses. Its `OLD.sql` is `schema.sql` at `cdb7b8e`, and its
`NEW.sql` at `59a48ad`. The outcome is recorded below when the owner has run it.
