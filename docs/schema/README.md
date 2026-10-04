# Schema

[`schema.sql`](schema.sql) is the canonical init DDL, always the full current one: until the freeze it is
edited **in place**, and after it each migration edits it too ([D13](../decisions/D13-migrations-and-freeze.md)). A new database, a
test database included, is that file applied verbatim to a fresh file:

```
sqlite3 life.db < docs/schema/schema.sql
```

`PRAGMA application_id` marks the result as a Lifelog database. Each table's rules are comments inside its
`CREATE` statement, so `.schema` prints them; the rules that span tables are the rows of `lifelog_meta`.

**10 tables + 1 FTS5 virtual table + 2 views** (`measurement_values`, `ghost_pages`)
**+ 27 triggers.** That is the entire system. Every `CHECK` is named (`CONSTRAINT <table>_<rule>`), so
any rule can be dropped or re-added by name after the freeze ([D13](../decisions/D13-migrations-and-freeze.md)).

## The objects

What each one is for, and where it is explained. Its rules are in its `CREATE` statement, not here.

| object | holds | why | used in |
|---|---|---|---|
| `entities` | one row per linkable thing: the shared id, its type, provenance, the tombstone | [D8](../decisions/D08-entities-and-links.md), [D11](../decisions/D11-tombstones.md) | [capture](../cookbook/capture.md) |
| `pages` | every page: titled prose, the day pages of the journal, the page of a person, a place, a metric or a file | [D5](../decisions/D05-pages-and-day-pages.md), [D16](../decisions/D16-places.md), [D20](../decisions/D20-named-pages.md) | [titles and wikilinks](../contract/titles-and-wikilinks.md), [capture](../cookbook/capture.md) |
| `people` | what a person has beyond its page | [D20](../decisions/D20-named-pages.md) | [a person or a place](../cookbook/person-or-place.md) |
| `metrics` | the registry of what is measured, each metric filed in a category page | [D7](../decisions/D07-measurements.md), [D26](../decisions/D26-metric-categories.md) | [a metric series](../cookbook/metric-series.md), [metrics by category](../cookbook/metrics-by-category.md) |
| `files` | what a file the owner keeps has beyond its page: the hash and type of the original, a small picture | [D9](../decisions/D09-binary-files.md) | [keep a file](../cookbook/keep-a-file.md) |
| `measurements` | readings, append-only | [D6](../decisions/D06-mood-is-a-measurement.md), [D7](../decisions/D07-measurements.md) | [correct a measurement](../cookbook/correct-a-measurement.md) |
| `habit_periods` | when a metric is a habit | [D24](../decisions/D24-habits.md) | [habits](../cookbook/habits.md) |
| `link_kinds` | the closed registry of link kinds and their endpoint types | [D8](../decisions/D08-entities-and-links.md) | [who may link what](../architecture/link-rules.md) |
| `links` | the one graph between entities | [D8](../decisions/D08-entities-and-links.md), [D19](../decisions/D19-wikilink-save-contract.md) | [backlinks](../cookbook/backlinks.md), [save a body](../cookbook/save-a-body.md) |
| `lifelog_meta` | the rules that span tables, as data | [D17](../decisions/D17-contract-as-data.md) | [threat model and the 2075 test](../contract/threat-model.md) |
| `pages_fts` | the full-text index over `pages`, derived | [D5](../decisions/D05-pages-and-day-pages.md) | [full-text search](../cookbook/full-text-search.md) |
| `measurement_values` | the read rule of measurements: corrections applied | [D7](../decisions/D07-measurements.md) | [a metric series](../cookbook/metric-series.md) |
| `ghost_pages` | empty pages nothing points at | [D5](../decisions/D05-pages-and-day-pages.md) | [ghost pages](../cookbook/ghost-pages.md) |

The diagrams are in the [entity model](../architecture/entity-model.md).
