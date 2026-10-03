# D26 — Metric categories: a closed tree whose parents never change.

**Status:** accepted

- **Context.** The first lab imports (2026-10) registered about a hundred metrics, and the list of
  metrics became one alphabetical run in which a blood marker, an allergen, a drink count and mood sat
  side by side. "Which metrics are biomarkers?" and "what do I take in?" had no answer in the data
  ([proposal 0001](../rfcs/0001-metric-categories.md)).
- **Decision.** `metric_categories` is a closed registry, a tree: `parent_id` NULL is a top-level category,
  any other row sits under an existing one, at any depth. `metrics.category_id` files a metric in one
  category, or in none. The tree cannot grow a cycle without any walk of it: a parent must exist before its
  child (the foreign key), a row cannot be its own parent (`metric_categories_not_self`, which sees the id
  SQLite is about to assign — executed), and a parent never changes (`metric_categories_parent_fixed`).
  Everything refers to a category by id, so a rename is one `UPDATE`. Names are snake_case and registered
  once (`metric_categories_name`). The four top-level categories are seeded — `biomarkers`, `body`,
  `self_report`, `substances` — and mood is filed in `self_report`; the rest are the owner's. A category's
  path (`biomarkers/lipids`) is computed, never stored ([metrics by category](../cookbook/metrics-by-category.md)).
- **Habits are not a category.** A metric is a habit while it has a period ([D24](D24-habits.md)); a
  category saying so would be a second home for that fact, and could disagree with it.
- **Alternatives.**
  - *A free-text `metrics.category`*: rejected — `biomarker` and `biomarkers` become two groups, and there
    are no subgroups.
  - *A flat registry, with a parent column added later*: rejected only because a hundred biomarkers in one
    group were already too many to read; the column would have been additive.
  - *Exactly two levels, keyed by name*: rejected — two triggers for an arbitrary cap, and a rename that
    must cascade to every child and metric.
  - *A parent that may change, with a trigger that walks the tree for cycles*: rejected — a recursive walk
    on every update to buy a move that a new category and a re-filing already give.
  - *Tags (a metric in several categories)*: rejected for now ([non-goals](../architecture/non-goals.md)) —
    a join table of deletable rows, and a grouped list in which a metric appears twice.
- **Trade accepted.** Moving a subcategory under another parent is a new category, its metrics re-filed,
  and the old one deleted once empty. A metric sits in one category only.
- **Sources.** The probes of the `facts` suite (executed).
