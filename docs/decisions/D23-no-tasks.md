# D23 — Explicit personal tasks, with projects as context pages

**Status:** accepted

- **Decision.** Personal planning uses independent `tasks` and `task_occurrences`, with project context in ordinary
  pages. Their storage constraints are in [schema.sql](../schema/schema.sql); bounded calendar, lifecycle and
  reminder operations are in [planning](../contract/planning.md). Task labels do not consume owned page names or
  become graph endpoints. Planned work remains distinguishable from recorded sessions, readings and presence.
- **Why.** The owner wants projects, recurring tasks and reminder intent preserved in the same lifetime file,
  independently of the eventual interface or notification provider. A repeating instruction and one occurrence's
  outcome are different facts. Ordinary project pages provide useful grouping without a project workflow model.
  Explicit task capture avoids turning historical prose and ambiguous checkboxes into unwanted obligations.
- **Alternatives.** *Tasks as named entities*: repeated labels need no unique wiki handle. *Only one mutable due
  date*: advancing it loses earlier outcomes. *A separate planning database*: it splits reference and restore
  responsibilities. *Generic properties or a plugin schema*: they add compatibility machinery without a concrete
  second use ([D2](D02-typed-strict-tables.md)).
- **Costs accepted.** Task context and occurrence outcomes are current state plus snapshots, not a history of
  every edit ([D12](D12-no-revision-tables.md)). Recurrence has a bounded profile ([D15](D15-recurrence.md)).
  Project status/hierarchy, task dependencies and delivery machinery remain [non-goals](../architecture/non-goals.md).
  Historical notes retain their prose unless the owner explicitly selects structured task capture.
