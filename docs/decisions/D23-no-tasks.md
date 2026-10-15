# D23 — No tasks: a plan is words in a page.

**Status:** accepted

- **Decision.** `life.db` keeps no tasks, occurrences, deadlines or reminders: there is no `tasks` table. A plan,
  a thing to do, a deadline or a recurring duty is words in a page, a day page or a plain page, and the owner
  reads it there. A checkbox in an imported note stays text ([imports](../contract/imports.md)).
- **Why.** The owner reviewed the writer on 2026-10-15 and kept the core only
  ([RFC 0019](../rfcs/0019-no-tasks.md)): the tasks answered "what is due" with two tables, fourteen triggers,
  a planning contract with calendar and clock vectors, three integrity queries, eleven actions, four views and a
  cookbook recipe. The owner plans in words; the life log keeps what happened.
- **Alternatives.**
  - *Explicit tasks and occurrences, with projects as context pages* (the earlier D23 and [D15](D15-recurrence.md)):
    removed by RFC 0019. The owner's tasks were written into a page as text, so no word was lost.
  - *Tasks as named entities*: rejected, a thing to do needs no wiki handle.
  - *A separate planning database*: rejected, it splits the one file ([D1](D01-single-sqlite-file.md)).
- **Trade accepted.** Nothing in the file says what is due; a reminder comes from no writer of this file.
- **Sources.** Executed: the `identity` and `named` suites (no task entity type; no task link kind).
