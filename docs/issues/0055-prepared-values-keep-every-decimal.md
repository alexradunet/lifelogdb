# 0055 — A prepared source writes every decimal of a value, and the owner cannot round it

- **Date:** 2026-10-09
- **Status:** resolved
- **Seen in:** the 2026-10 import of a Takeout extraction: the Fit daily aggregate CSV through `fit-date-csv-v1`

## What happened

The Fit daily aggregate writes calories, distance and mean heart rate as computed numbers with many decimals. A
synthetic row of the same shape:

```
Date,Calories (kcal),Distance (m),Average heart rate (bpm),Step count
2031-04-11,1789.3456789012,5432.123456,72.5,8123
```

The bounded preparation writes each value as the source states it, so a day shows `1789.3456789012` kcal and
`5432.123456` m. The decimals are the provider's arithmetic, not a measured precision. The owner wants whole units
for these three quantities: the nearest kcal, metre and bpm.

The preparation has no way to say this. A value cannot be changed after the apply either: a correction of each
imported reading is one row per reading, and a second apply refuses a changed value ("prepared source root
interpretation changed").

Expected: the owner chooses, in the `prepared.md` that they review and stamp, which quantities are rounded to the
nearest whole unit before they are written. The choice is part of the immutable interpretation of the file, like the
metric mapping.

## Reproduce

1. A Takeout workspace with approved rules; the CSV above as `daily.csv`; metrics `Calories` (kcal), `Distance` (m),
   `Mean heart rate` (bpm), `Steps` (steps).
2. *draft prepared* with profile `fit-date-csv-v1` and the four mappings; the owner stamps; *apply prepared*.
3. The readings of 2031-04-11 are `1789.3456789012`, `5432.123456`, `72.5` and `8123`.

## Rules involved

- [importing with a model](../guides/importing.md), "What this is": a bounded typed source interpretation, and what
  its owner review binds
- the writer's application guidance: the README, "Bounded source preparations"

## Resolution

Resolved by [plan 087](../plans/087-prepared-quantities-rounded-to-whole-units.md): *draft prepared* takes an
optional `round`, a JSON list of the quantity codes written as the nearest whole unit (half away from zero). The
list is sorted, each code once and a quantity of the file; the owner reads it in `prepared.md` and stamps it with the
rest. Apply, check, *status* and *replay* round in the one place that writes the reading, and the binding keeps the
list, so a different choice after the apply is refused. A batch without the list writes the source value, as before.
