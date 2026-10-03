# Proposals (RFCs)

A proposal is a schema change argued before it is made: which issues it answers, the options, the cost of each
after the freeze, and what it makes redundant ([how a change happens](../process.md)). An accepted proposal
becomes a [decision](../decisions/README.md). One file per proposal, named `NNNN-short-slug.md`, written from the
[template](template.md).

| id | title | status | issues | decision |
|---|---|---|---|---|
| [0001](0001-metric-categories.md) | Metric categories: a closed registry, a tree whose parents never change | accepted | 0008 | [D26](../decisions/D26-metric-categories.md) |
| [0002](0002-categories-are-pages.md) | Metric categories are pages, nested by `part-of` links | accepted | 0008 | [D26](../decisions/D26-metric-categories.md) |
| [0003](0003-a-metric-is-a-page.md) | A metric is a page; anything is filed in a category by `part-of` | accepted | 0008 | [D27](../decisions/D27-a-metric-is-a-page.md), [D26](../decisions/D26-metric-categories.md) |

Status values: draft | accepted | rejected (with a one-line reason) | withdrawn.
