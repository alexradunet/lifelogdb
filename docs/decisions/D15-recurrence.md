# D15 — Recurrence: DEFERRED out of v1. The design is kept here for the day it returns.

**Status:** deferred

- **Decision.** Nothing repeats: there are no tasks or events ([D23](D23-no-tasks.md), [D22](D22-events.md)). A lifelog records what
  happened; a repeating appointment is planning, which the owner's calendar already does. What the
  schema still covers: **birthdays** are a query over `people.birth_day`; **"did I do it each month"**
  is a habit — a 0/1 metric with its active periods ([D24](D24-habits.md)) — whose history charts for free.
- **The deferred design** — additive later, on events if they return ([D22](D22-events.md)):
  structured, readable columns `repeat` (`none|daily|weekly|monthly|yearly`), `repeat_every`
  (NULL = 1), `repeat_weekdays` (`'mo,we,fr'`, weekly only) and `repeat_until` (inclusive); a repeating
  row is a *template*, and occurrences are expanded at read by one window-bounded recursive CTE, never
  materialized. Weeks count calendar weeks (Mon–Sun) from the week of the start; months and years
  clamp to the month's last day. The expander and its oracle are in the git history of `tests/`.
- **Alternatives kept rejected for that day.** *An RFC-5545 RRULE string*: meaning lives in a parser,
  unreadable cold. *Materialized occurrence rows*: the three-way edit problem (this / this and future /
  all) and a regeneration job [R51](../research/references.md#r51)[R52](../research/references.md#r52).
- **Reopen trigger.** A recurring event the owner wants in this database rather than the calendar.
- **Sources.** [R51](../research/references.md#r51)[R52](../research/references.md#r52).
