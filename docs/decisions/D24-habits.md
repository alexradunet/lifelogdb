# D24 — Habits: a unitless metric with a reading of 1 or 0 a day; no periods.

**Status:** accepted

- **Decision.** A habit is a metric ([D7](D07-measurements.md)) with no unit, whose readings are 1 (done) or 0 (not
  done), one a day, recorded with `record` like any reading. There is no `habit_periods` table: when a habit applies
  is words in its page, and a day with no reading is a day with no reading. A habit is filed in a category like any
  other metric ([D26](D26-metric-categories.md)); a category named Habits is the owner's choice.
- **Why.** The owner reviewed the writer on 2026-10-15 and kept the core only
  ([RFC 0018](../rfcs/0018-no-habit-periods.md)): the periods answered "was this habit meant to be done that day",
  and the answer cost a table, eight triggers, two integrity queries, three actions, a lens of the metrics view, a
  strip on every day page and a cookbook recipe. The owner records what was done; a streak or a rate is a count
  over the readings.
- **Alternatives.**
  - *A metric with active periods* (the earlier D24): removed by RFC 0018. The owner's periods were written into
    the metric pages as text, so no word was lost.
  - *A separate check-in table*: rejected, a second home for "how was that day".
  - *A missing day counts as not done*: rejected, an unwritten day says nothing.
- **Trade accepted.** Nothing tells a habit from any other unitless metric, and nothing holds its readings to 0
  and 1: the owner writes 1 or 0. A day not recorded cannot be told from a day outside the habit.
- **Sources.** Executed: the `facts` suite (a unitless metric takes a reading of 1 and of 0).
