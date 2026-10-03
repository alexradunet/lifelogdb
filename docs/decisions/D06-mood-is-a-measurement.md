# D6 — Mood: the `mood` metric in `measurements`, not a column on `pages`.

**Status:** accepted

- **Decision.** Mood is a time series like any other: a seeded metric, the page `Mood`
  ([D27](D27-a-metric-is-a-page.md)), whose rows are appended to `measurements`. When a mood is attached to a day page, the row's `captured_with_id` is the page's id. The
  1–5 range is app-level validation on one metric row, not a schema CHECK.
- **Rule.** One home per concept, forever (principle 6): a standalone mood tap needs no second
  mechanism, and mood charts uniformly with every other series.
- **Alternatives.** *`pages.mood` column*: rejected — it splits the concept across two tables the
  moment a text-less mood tap happens. *Both*: rejected — two homes for one concept drift.
