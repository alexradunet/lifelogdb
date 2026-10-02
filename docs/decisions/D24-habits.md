# D24 — Habits: a metric with active periods.

**Status:** accepted

- **Decision.** A habit is a unitless metric ([D7](D07-measurements.md)) that has periods in `habit_periods`: the local days
  the owner meant to do it, from `start_day` to `end_day` (inclusive; NULL = still going). Its
  check-ins stay in `measurements`, one home for a day's value: 1 = done, 0 = not done. On a day inside
  a period, no check-in is **not recorded** — never assumed either way. A restarted habit has several
  periods, which never overlap (`habit_periods_check_insert`, `_check_update`); a period on a metric
  with a unit is refused; a wrong period is corrected by `UPDATE`, never deleted
  (`habit_periods_no_delete`). The day's habits and their completion are [habits](../cookbook/habits.md); the day view lists
  them ([the day view](../cookbook/day-view.md)).
- **Why** (the first real import, 2026-10). The vault's habits and supplements became 0/1 metrics,
  and two questions had no answer: which metrics are habits (only a note's wording set them apart
  from mood or a 0/1 lab marker), and whether a habit was meant to be done on a given day — so a day
  with no check-in could not be told from a day outside the habit, and no streak or completion rate
  could be honest.
- **Alternatives.**
  - *A separate check-in table*: rejected — a second home for "how was that day", with its own
    corrections, imports and charts, and no join with mood or weight.
  - *A `kind` column on `metrics`*: rejected — it says which metrics are habits, not when.
  - *Start and stop columns on `metrics`*: rejected — a habit restarted (vitamin D each winter) needs
    several periods.
  - *A missing day counts as not done*: rejected — imported notes rarely say which days a habit was
    done, so every unwritten day would count against the owner. An explicit 0 says "not done".
- **Costs accepted.** A completion rate is over recorded days; the not-recorded days are counted, not
  hidden. A habit's target ("three times a week") has no column yet ([non-goals](../architecture/non-goals.md)). The 0/1 range of a check-in
  is checked by the app, like mood's 1–5 ([D6](D06-mood-is-a-measurement.md)).
