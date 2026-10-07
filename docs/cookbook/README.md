# Query cookbook

Proof that the schema serves the product with plain SQL. `:named` are bind parameters.
All examples filter tombstones (`e.deleted_at IS NULL`). Every write transaction starts with
`BEGIN IMMEDIATE` ([connection setup](../contract/connections.md)); a block that writes one statement and reads nothing before it (start a habit, a correction, an `at` link) is shown alone
— it is its own transaction; inside a larger write it goes between that write's `BEGIN IMMEDIATE` and `COMMIT`; and every block runs in `tests/`.

The order below is the order the suites run the recipes in; the key is how a suite names a recipe
(`block('save-a-body')`).

| recipe | key |
|---|---|
| [Capture: append to the day page (the universal insert convention)](capture.md) | `capture` |
| [The day view](day-view.md) | `day-view` |
| [The days that name someone or somewhere](days-that-name.md) | `days-that-name` |
| [Mood over time](mood-over-time.md) | `mood-over-time` |
| [Backlinks to a page (or to anything)](backlinks.md) | `backlinks` |
| [Everything about a person or a place (the ark query)](everything-about.md) | `everything-about` |
| [Metric series, corrections applied (weight, last 90 days)](metric-series.md) | `metric-series` |
| [Full-text search](full-text-search.md) | `full-text-search` |
| [Where was I: the places of a day, the days at a place (D16)](where-was-i.md) | `where-was-i` |
| [Correct a wrong measurement (append-only)](correct-a-measurement.md) | `correct-a-measurement` |
| [Everything inside a place (containment)](inside-a-place.md) | `inside-a-place` |
| [Ghost pages (the wikilink sweep, D5)](ghost-pages.md) | `ghost-pages` |
| [Save a body with wikilinks (resolve or create each target, sync the links)](save-a-body.md) | `save-a-body` |
| [A person or a place: create one, promote a ghost page (D20)](person-or-place.md) | `person-or-place` |
| [Keep a file: a recording, a PDF, a photo (D9)](keep-a-file.md) | `keep-a-file` |
| [The place of a photo: give a place its point, match a position, link the day (D21)](place-of-a-photo.md) | `place-of-a-photo` |
| [Rename a page: a new page and a stub (D5)](rename-a-page.md) | `rename-a-page` |
| [Import a row once: insert it, re-run it, update a changed one](import-a-row-once.md) | `import-a-row-once` |
| [Habits: start and stop one, the habits of a day, completion over a period (D24)](habits.md) | `habits` |
| [Metrics by category: file a metric, nest a category, a category's metrics, every metric grouped (D26)](metrics-by-category.md) | `metrics-by-category` |
| [Recorded sessions and labeled scoped readings](recorded-sessions.md) | `recorded-sessions` |
| [Read recorded periods](recorded-periods.md) | `recorded-periods` |
| [Take a snapshot, check it, restore it (D25)](take-a-snapshot.md) | `take-a-snapshot` |
