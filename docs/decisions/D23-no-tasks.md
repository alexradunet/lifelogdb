# D23 — No tasks table: a plan is a line of a page.

**Status:** accepted

- **Decision.** `life.db` keeps no `tasks` table, no occurrences and no reminders. A plan, a thing to do, a
  deadline or a repeating duty is a plan line: a list item of a page body that starts with a box. The grammar is
  the page [plan lines](../contract/plan-lines.md) and the reason is [D29](D29-plan-lines.md). The actions `plan`,
  `tick`, `carry` and `schedule` belong to the writer, not to the file. An imported checkbox is a plan line like any
  other; with no `on:` or `due:` it is listed on its own page and in the list of every open line, never under a day
  ([imports](../contract/imports.md)).
- **Why.** A table of tasks gives a plan two homes: the words of the page and the row. The answer to "what is
  planned" is read from the text, and it is stored once
  ([RFC 0022](../rfcs/0022-plan-lines-and-period-pages.md)). The history of the tasks and their removal is in
  [RFC 0019](../rfcs/0019-no-tasks.md).
- **Alternatives.**
  - *Explicit tasks and occurrences, with projects as context pages*: rejected. They give a plan two homes
    ([RFC 0019](../rfcs/0019-no-tasks.md)).
  - *Tasks as named entities*: rejected, a thing to do needs no wiki handle.
  - *A derived index of the lines*: deferred until a measured scan is slow ([non-goals](../architecture/non-goals.md)).
  - *A separate planning database*: rejected, it splits the one file ([D1](D01-single-sqlite-file.md)).
- **Trade accepted.** Nothing in the file says what is planned or refuses a bad line; a reminder comes from no
  writer of this file ([D29](D29-plan-lines.md) says what the writer and the suites hold in its place).
- **Sources.** Executed: the `identity` and `named` suites (no task entity type; no task link kind).
