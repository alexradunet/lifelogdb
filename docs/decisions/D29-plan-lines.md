# D29 — No plan lines: a plan lives outside the file, in the owner's task manager; what was done comes back as an entry.

**Status:** accepted

- **Context.** A plan has two parts: what the owner means to do, and what the owner did. The first part changes
  often and needs a due date, a repeat and a list of what is open. The second part is a fact of the life log.
- **Decision.** The file keeps no column, index, table or convention for a plan. A plan is the business of the app the
  owner plans in. What the owner did there comes back to the file as an entry of the day. The app that carries it is a
  writer: it uses the capture of the writer with an `import_key` ([imports](../contract/imports.md)), so a second
  run writes nothing. A `- [ ]` in a body is prose, like any other text.
- **Why.** A plan line gave a plan two homes: the words of the page and the app the owner plans in
  ([RFC 0025](../rfcs/0025-no-plan-lines-and-connected-apps.md), [issue 0092](../issues/0092-plan-lines-cost-more-than-they-give.md)).
  The file is a life log: what happened and what was measured ([goals](../architecture/goals-and-principles.md)).
- **Alternatives.**
  - *A `tasks` table*: rejected, it gives a plan two homes ([RFC 0019](../rfcs/0019-no-tasks.md)).
  - *A plan line read from the body, with a generated column and a partial index*: rejected. The owner did not write
    them.
  - *A derived index table*: rejected. A table in a frozen file stays in its shape for good.
- **Trade accepted.** The file knows nothing of a plan, so nothing in it lists what is due. The owner's app does.
- **Sources.** Executed: the `identity`, `named` and `journal` suites (no task type, no task table, no task kinds).
