# D24 — Habits: a metric with active periods.

**Status:** accepted

- **Decision.** A habit is a unitless metric ([D7](D07-measurements.md)) that has periods in `habit_periods`: the local days
  the owner meant to do it, from `start_day` to `end_day` (inclusive; NULL = still going). Its
  check-ins stay in `measurements`, one home for a day's value: 1 = done, 0 = not done. On a day inside
  a period, no check-in is **not recorded** — never assumed either way. A restarted habit has several
  periods, which never overlap (`habit_periods_check_insert`, `habit_periods_check_update`); a period on a metric
  with a unit is refused, and a scale names its range as its unit (`1-5`, `0-10`), so Mood and every scale the owner
  registers are refused by that one rule; a wrong period is corrected by `UPDATE`, never deleted
  (`habit_periods_no_delete`). The day's habits and their completion are [habits](../cookbook/habits.md); the day view lists
  them ([the day view](../cookbook/day-view.md)).
  `habit_periods_identity_fixed` retains the period's own identity across corrections; changing its metric remains
  an explicit correction that advances both affected metric revisions.
- **Why** (the first real import, 2026-10). The vault's habits and supplements became 0/1 metrics,
  and two questions had no answer: which metrics are habits (only a note's wording set them apart
  from mood or a 0/1 lab marker), and whether a habit was meant to be done on a given day — so a day
  with no check-in could not be told from a day outside the habit, and no streak or completion rate
  could be honest.
- **Alternatives.**
  - *A separate check-in table*: rejected — a second home for "how was that day", with its own
    corrections, imports and charts, and no join with mood or weight.
  - *A `kind` column on `metrics`*: rejected — it says which metrics are habits, not when. For the
    same reason a habit is never a metric category ([D26](D26-metric-categories.md)).
  - *Start and stop columns on `metrics`*: rejected — a habit restarted (vitamin D each winter) needs
    several periods.
  - *A missing day counts as not done*: rejected — imported notes rarely say which days a habit was
    done, so every unwritten day would count against the owner. An explicit 0 says "not done".
- **Costs accepted.** A completion rate is over valid recorded days; the not-recorded days and invalid
  check-ins remain visible in the [diagnostic reads](../cookbook/habits.md). A habit's target ("three times a week") has no column yet ([non-goals](../architecture/non-goals.md)). The 0/1 range of a check-in
  is checked by the writer, like mood's 1–5 ([D6](D06-mood-is-a-measurement.md)). The schema can correct a period's
  metric or boundaries, but cannot withdraw a wholly mistaken period independently: it has neither a tombstone
  nor an empty-interval representation. Changing dates to hide such a mistake would invent intention. Tombstoning
  the metric hides the whole habit from active reads; it does not retract one period. A period-level withdrawal
  mechanism needs its own incident and decision.
