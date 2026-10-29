# D23 — No tasks table: a plan lives outside the file.

**Status:** accepted

- **Decision.** `life.db` keeps no `tasks` table, no occurrences and no reminders. A plan, a thing to do, a
  deadline or a repeating duty is not in the file: [D29](D29-plan-lines.md) gives the reason and says what comes back.
- **Why.** A table of tasks gives a plan two homes: the words of the page and the row
  ([RFC 0019](../rfcs/0019-no-tasks.md)). The file is a life log, not a task manager.
- **Alternatives.**
  - *Explicit tasks and occurrences, with projects as context pages*: rejected. They give a plan two homes
    ([RFC 0019](../rfcs/0019-no-tasks.md)).
  - *Tasks as named entities*: rejected, a thing to do needs no wiki handle.
  - *A separate planning database*: rejected, it splits the one file ([D1](D01-single-sqlite-file.md)).
- **Trade accepted.** Nothing in the file says what is planned; a reminder comes from no writer of this file.
- **Sources.** Executed: the `identity` and `named` suites (no task entity type; no task link kind).
