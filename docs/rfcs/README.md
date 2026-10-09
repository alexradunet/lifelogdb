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
| [0004](0004-a-file-is-a-page.md) | A file is a page: its text in the body, a small picture kept, the original outside | accepted | 0009 | [D9](../decisions/D09-binary-files.md) |
| [0005](0005-a-place-has-a-point.md) | A place has a point and a radius; a photo's place and day are taken when it is kept | accepted | 0010 | [D21](../decisions/D21-location-history.md) |
| [0006](0006-stable-names-and-life-periods.md) | Stable names, one named-object core, and recorded life periods | draft | [0011–0021](../issues/README.md) | pending owner review |
| [0007](0007-personal-tasks-and-occurrences.md) | Personal tasks, recurring occurrences and reminder intent | accepted | [0025](../issues/0025-personal-planning-has-no-durable-task-model.md) | [D23](../decisions/D23-no-tasks.md), [D15](../decisions/D15-recurrence.md) |
| [0008](0008-owner-decides-names-in-imports.md) | The owner decides each new name of an import in one stamped list | accepted | [0052](../issues/0052-names-are-written-on-the-models-word.md) | [D11](../decisions/D11-tombstones.md); the guide's "entities.md" |
| [0009](0009-a-rejected-name-is-skipped-and-tombstoned.md) | A rejected name is skipped, and the row the import wrote under it is tombstoned | accepted | [0053](../issues/0053-a-rejected-name-is-not-removed.md) | [D11](../decisions/D11-tombstones.md), [D13](../decisions/D13-migrations-and-freeze.md); the guide's "entities.md" |
| [0010](0010-the-writer-drives-a-local-model.md) | The writer drives a local model for the facts pass | accepted | [0057](../issues/0057-the-import-loop-lives-outside-the-writer.md) | the non-goals row on AI generation; the guide's "Three parties" |
| [0011](0011-no-import-process.md) | No import process: the owner imports with an agent, through the catalog | accepted | [0058](../issues/0058-the-import-machine-outweighs-the-backfill.md) | [D13](../decisions/D13-migrations-and-freeze.md), [D11](../decisions/D11-tombstones.md); [imports](../contract/imports.md) |

Status values: draft | accepted | rejected (with a one-line reason) | withdrawn.
