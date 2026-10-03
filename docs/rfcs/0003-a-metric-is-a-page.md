# 0003 — A metric is a page; anything is filed in a category by `part-of`

- **Date:** 2026-10-03
- **Status:** accepted
- **Answers:** 0008, the metrics cannot be grouped (resolved; `git log -- docs/issues`), and the owner's review of
  [0002](0002-categories-are-pages.md)

## Problem

With categories as pages ([0002](0002-categories-are-pages.md)), a metric was still the one thing that was not an
entity: it was filed by a column of its own (`metrics.category_id`) while a page, a person or a place could only be
categorised another way (a `#tag`), and `[[Ferritin]]` in the journal reached a plain page rather than the series. The
owner asked for the page, the entity, to be the core object, and for one categorisation for everything.

## Options

- **A. Stop at 0002.** Metrics filed by a column, pages by `#tags`. Two mechanisms.
- **B. Widen `part-of` to any entity, keep the column.** People and places can be filed; metrics stay the exception.
- **C. A metric is a page** (entity type `metric`, a `metrics` row keyed by `pages(id, entity_type)` as `people`
  is), and `part-of` from any entity to a category page files everything. `metrics.name` and `metrics.note` go: the
  title is the name, the body the note; `category_id` goes. Widens two named CHECKs and two link kinds' endpoint
  types; after the freeze each is a `DROP`/`ADD CONSTRAINT` or a new kind, so this is decided before it.

## Recommendation

**C.** One core object and one way to file it; nothing stored twice. The cost is that a metric's name follows the
title rules and is never renamed, which a series name already never was.

## Validation

The `facts` suite: Mood seeded as entity, page and metric; the unit fixed; the name a title (fixed, one per
`title_key`); a page promoted to a metric; a metric tombstoned, never deleted; anything filed by `part-of`, a category
a plain page only. The `identity` suite: a metric among the entity types that are never deleted. The orphan query
counts a metric's row. Every recipe that named a metric looks it up by `title_key`. Mutants for each new rule.

## Outcome

Accepted: [D27](../decisions/D27-a-metric-is-a-page.md), and [D26](../decisions/D26-metric-categories.md) rewritten.
