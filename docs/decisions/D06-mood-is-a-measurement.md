# D6 — Mood: the `mood` metric in `measurements`, not a prose column.

**Status:** accepted

- **Decision.** Mood is a time series like any other: a seeded metric, the page `Mood`
  ([D27](D27-a-metric-is-a-page.md)), whose rows are appended to `measurements`. When a mood is attached to a day page, the row's `captured_with_id` is the page's id. The
  1–5 range is keyed by the metric's unit, `1-5`: a scale names its range as its unit (`metrics.unit`,
  [D7](D07-measurements.md)), `lo-hi` of ASCII digits with `lo` less than `hi`. A unit of that shape with its bounds
  out of order (`5-1`, `3-3`) is not a range: `metrics_range_order` refuses it when the metric is made, and
  `metrics_unit_fixed` keeps a unit from changing to it. `measurements_scale_range` holds every value of a metric with
  a range unit, Mood or any scale the owner registers (`0-10`), to the whole numbers inside that range; a retraction
  (a NULL value) is never held. These refusals are executed.
- **Rule.** One home per concept, forever (principle 6): a standalone mood tap needs no second
  mechanism, and mood charts uniformly with every other series.
- **Alternatives.** *`pages.mood` column*: rejected — it splits the concept across two tables the
  moment a text-less mood tap happens. *Both*: rejected — two homes for one concept drift. *A unit with its bounds out
  of order as the unit of a plain number*: rejected — the unit never changes, so one typo in a range makes a metric
  that can never be a scale.
