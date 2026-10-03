# 0002 — Metric categories are pages, nested by `part-of` links

- **Date:** 2026-10-03
- **Status:** accepted
- **Answers:** 0008, the metrics cannot be grouped (resolved; `git log -- docs/issues`), and the owner's review of
  [0001](0001-metric-categories.md) as built

## Problem

0001 made categories a registry, a tree whose parents never change, and then gave each category a `page_id` so what
the owner writes about it could live on a page. Reviewing it, the owner asked whether the registry's parent was still
needed and pointed at what the pointer had done: a category's name was stored twice, as its registry name and as its
page's title, which one home per concept forbids. The tree also stood outside the graph of `links`.

## Options

- **A. Keep the registry and its `page_id`.** The tree stays guaranteed and a rename is free; the name stays in two
  homes, and the category's place in the graph is a pointer that backlinks do not see.
- **B. Categories are pages; `part-of` links nest them.** `metrics.category_id` references `pages(id)`; one row in
  `link_kinds` (`part-of`, page → page). Drops the table, its two CHECKs and its trigger. The name is the title, the
  nesting is in `links`, so backlinks and the ark query see it. Nothing keeps the links a tree (as with
  `located-in`, D8), and a page rename must move the category's metrics and the links that end at it. Additive after
  the freeze: one nullable column and one `link_kinds` row.
- **C. Categories are pages, with no nesting.** The least schema; loses the subgroups a hundred lab values need.

## Recommendation

**B.** It stores nothing twice and puts the categories in the graph, for the cost of a tree kept by the owner rather
than by a trigger. The rename contract gains one rule that holds for any page: the typed links that end at it move,
as the ones it starts already do.

## Validation

The `facts` suite: a metric filed in a page and refused for a missing one; `part-of` between plain pages only; the
recipe, with a cycle that ends the walk and a deleted category read as not filed. The `renames` suite: a category's
page renamed by the recipe and by the writer, its metrics and both directions of `part-of` moved. Mutants for each.

## Outcome

Accepted: [D26](../decisions/D26-metric-categories.md) rewritten; replaced by [0003](0003-a-metric-is-a-page.md).
