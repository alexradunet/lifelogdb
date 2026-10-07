# D6 — Mood: the `mood` metric in `measurements`, not a prose column.

**Status:** accepted

- **Decision.** Mood is a time series like any other: a seeded metric, the page `Mood`
  ([D27](D27-a-metric-is-a-page.md)), whose rows are appended to `measurements`. When a mood is attached to a day page, the row's `captured_with_id` is the page's id. The
  1–5 range is app-level validation, not a schema CHECK; it is keyed by the metric's unit, `1-5`: a scale names
  its range as its unit (`metrics.unit`, [D7](D07-measurements.md)), and the writer holds every value of a metric
  with a range unit, Mood or any scale the owner registers (`0-10`), to the whole numbers inside that range. This
  is also why no scale is ever a habit ([D24](D24-habits.md)).
- **Rule.** One home per concept, forever (principle 6): a standalone mood tap needs no second
  mechanism, and mood charts uniformly with every other series.
- **Alternatives.** *`pages.mood` column*: rejected — it splits the concept across two tables the
  moment a text-less mood tap happens. *Both*: rejected — two homes for one concept drift.
