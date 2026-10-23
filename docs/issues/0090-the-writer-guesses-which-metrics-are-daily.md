# 0090 — The writer guesses which metrics are daily

- **Date:** 2026-10-23
- **Status:** resolved
- **Seen in:** the owner's use of the browser on 2026-10-23: "Some metrics should be logged daily. I want to be able to configure metrics to show if they should be tracked daily (like habits, or sleep, or mood) and provide an easy way to input all of them in the daily note."

## What happened

The writer guesses which metrics to ask for. The Today strip shows as a habit any metric with no unit that has a
reading in the last 14 days (`HabitDays` in `internal/core/today.go`). Three cases fail:

- A metric with a unit, such as Sleep in `h` or Mood on `1-5`, is never asked for.
- A habit that the owner did not record for 14 days leaves the strip. It leaves when the prompt is most needed.
- The owner enters the readings of a day with one "Add a reading" form per reading: pick a metric, type a value.

The data cannot answer the question "which readings am I asked for today?".

## Reproduce

1. Apply [schema.sql](../schema/schema.sql) to a fresh file.
2. Register the metric `Sleep` with the unit `h`. Record one reading.
3. Open Home. The Today strip has no prompt for Sleep.

## Rules involved

- [D7](../decisions/D07-measurements.md): a reading is a row of a metric and a day.
- [D24](../decisions/D24-habits.md): a habit is a unitless metric. A day with no reading says nothing.
- [D27](../decisions/D27-a-metric-is-a-page.md): a metric is a page. Its unit is its own.
- [RFC 0018](../rfcs/0018-no-habit-periods.md): the habit periods and the 14-day strips were cut.
- Principle 7 of the [goals](../architecture/goals-and-principles.md): a change needs a real incident.

## Resolution

[RFC 0023](../rfcs/0023-a-daily-mark-on-a-metric.md), [D31](../decisions/D31-daily-metrics.md) and the commit that
adds `metrics.daily`.
