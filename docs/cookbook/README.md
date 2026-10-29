# Query cookbook

Proof that the schema serves the product with plain SQL. `:named` are bind parameters.
Active views filter tombstones (`e.deleted_at IS NULL`); explicitly labeled historical queries retain them.
Every write transaction starts with
`BEGIN IMMEDIATE` ([connection setup](../contract/connections.md)); a block that writes one statement and reads nothing before it (a correction) is shown alone
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
| [Everything about a person (the ark query)](everything-about.md) | `everything-about` |
| [Metric series, corrections applied (weight, last 90 days)](metric-series.md) | `metric-series` |
| [Full-text search](full-text-search.md) | `full-text-search` |
| [Correct a wrong measurement (append-only)](correct-a-measurement.md) | `correct-a-measurement` |
| [Ghost pages (the wikilink sweep, D5)](ghost-pages.md) | `ghost-pages` |
| [Save a body with wikilinks (resolve or create each target, sync the links)](save-a-body.md) | `save-a-body` |
| [A person: create one, promote a ghost page (D20)](person.md) | `person` |
| [Keep a file: a recording, a PDF, a photo (D9)](keep-a-file.md) | `keep-a-file` |
| [Rename a page: a new page and a stub (D5)](rename-a-page.md) | `rename-a-page` |
| [Import a row once: insert it, re-run it, update a changed one](import-a-row-once.md) | `import-a-row-once` |
| [Metrics by category: file a metric, nest a category, a category's metrics, every metric grouped (D26)](metrics-by-category.md) | `metrics-by-category` |
| [Take a snapshot, check it, restore it (D25)](take-a-snapshot.md) | `take-a-snapshot` |
| [The streak: days written in a row, ending today or yesterday](streaks.md) | `streaks` |
