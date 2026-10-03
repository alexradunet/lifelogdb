# D27 — A metric is a page: its title is its name, its body its note.

**Status:** accepted

- **Context.** Categories became pages ([D26](D26-metric-categories.md)), and the owner wanted the page, the entity, to
  be the one core object: one way to file anything in a category, and a metric that the journal can name. A metric
  was the one thing measured or written about that was not an entity, so it needed a column of its own to be filed,
  and `[[Ferritin]]` in a day page reached a plain page, not the series ([proposal 0003](../rfcs/0003-a-metric-is-a-page.md)).
- **Decision.** A metric is an entity of type `metric` with a page and a `metrics` row, one id, as a person is
  ([D20](D20-named-pages.md)): `metrics(id, entity_type)` references `pages(id, entity_type)`. The page title is the
  metric's name — `title_key` keeps one series per name whatever its case, and a title never changes, so a series
  stays one series — and the page body is what the owner writes about it. `metrics` keeps only what is the
  metric's own: its `unit`, fixed (`metrics_unit_fixed`). A metric is never deleted (`metrics_no_delete`); one
  registered by mistake is tombstoned ([D11](D11-tombstones.md)). It is filed in a category by a `part-of` link, as
  anything is ([D26](D26-metric-categories.md)); `[[Weight]]` in the journal is a backlink of the metric. Mood is
  seeded as the metric page `Mood` ([D6](D06-mood-is-a-measurement.md)). A ghost page becomes a metric by the same
  promotion a person takes. Measurements, habit periods and corrections keep referencing `metrics(id)`.
- **Alternatives.**
  - *A snake_case `metrics.name` beside the title*: rejected — every metric with two names (`ferritin`,
    "Ferritin"), the duplication one home per concept forbids.
  - *Keep metrics a registry, filed by `metrics.category_id`*: rejected — a second way to file things in a
    category, and a metric the journal cannot link to.
- **Trade accepted.** A metric's name is a title: it obeys the title rules
  ([titles and wikilinks](../contract/titles-and-wikilinks.md)) and is never renamed, as a person's handle is not.
  Looking a metric up by name is a `title_key` lookup through `pages`. A mistaken metric is a tombstone, not a delete.
- **Sources.** The `facts`, `identity` and `renames` suites (executed).
