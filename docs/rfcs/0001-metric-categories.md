# 0001 — Metric categories: a closed registry, a tree whose parents never change

- **Date:** 2026-10-03
- **Status:** accepted
- **Answers:** 0008, the metrics cannot be grouped (resolved; `git log -- docs/issues`)

## Problem

A metric has a name, a unit and a note; nothing groups it. With about a hundred lab values registered, the
metrics cannot be listed or queried by kind (biomarkers, the body, what was consumed, self-ratings), and the
list grows with each lab import. Habits can already be told apart, by their periods
([D24](../decisions/D24-habits.md)), so that question is not part of this one.

## Options

- **A. A free-text `metrics.category`**, snake_case CHECK. One `ADD COLUMN`, additive. A typo
  (`biomarker`, `biomarkers`) makes a second group; there are no subgroups, and a hundred biomarkers stay one list.
- **B. A flat registry** `metric_categories(id, name, note)` and `metrics.category_id`. Additive; a typo
  cannot create a category. Subgroups wait for a `parent_id` column, itself additive after the freeze.
- **C. A tree whose parents never change**: B with `parent_id` (NULL = top level), a not-self CHECK and a trigger
  that fixes the parent at registration. A parent must exist before its child and never changes, so no cycle can
  form and nothing walks the tree. Any depth; a rename is one `UPDATE` (everything refers to the id). Moving a
  subcategory means registering a new one and re-filing its metrics. Additive.
- **D. Exactly two levels, keyed by name**: the name is the key, two triggers keep the depth at two, and a rename
  cascades (`ON UPDATE CASCADE`) to children and metrics. More rules for an arbitrary cap.
- **E. Tags**: a join table `metric_tags(metric_id, tag)`. A metric in several groups; the rows are deleted when a tag
  is removed, and a grouped list shows a metric more than once. Nothing has needed a second group yet.
- **Not an option: a stored "habit" category.** It would be a second home for what `habit_periods` says
  ([D24](../decisions/D24-habits.md)), and the two could disagree. The writer derives a Habits group.

All of them make nothing redundant. Each is additive after the freeze ([D13](../decisions/D13-migrations-and-freeze.md)): a
new table, and a nullable column with a foreign key.

## Recommendation

**C.** It costs one table, two CHECKs and one trigger; it holds the subgroups a hundred lab values need
(`biomarkers/lipids`, `biomarkers/thyroid`) at any depth, and stays a tree by construction. Seed the top-level
categories `biomarkers`, `body`, `self_report` and `substances`, and file `mood` in `self_report`; the rest are the
owner's, registered through the import workspace while the canonical file is still rebuilt by replay.

## Validation

The `facts` suite executes: the seeds; a subcategory and a third level; a missing parent refused; a self-parent
refused, with an explicit id and with the id SQLite is about to assign; a parent change refused, a full-row update
passing; a rename followed by children and metrics; a category with children or metrics refused on delete, an
empty one deleted; a metric re-filed; and the recipe [metrics by category](../cookbook/metrics-by-category.md).
Mutants: each CHECK made always true, the trigger never firing, the name unique no more.

## Outcome

Accepted as [D26](../decisions/D26-metric-categories.md).
