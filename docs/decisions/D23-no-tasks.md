# D23 — A life log, not a project manager: no tasks.

**Status:** accepted

- **Decision.** No `tasks` table and no task links (`spawned`, `subtask`). `life.db` keeps what
  happened and what was measured — the day pages, notes, people, places and readings — and is the
  backup of that record. What is still to be done belongs to the tools made for it (a to-do app, a
  calendar); a plan the owner wrote stays the text of its page, where it is found by search ([full-text search](../cookbook/full-text-search.md)).
- **Why** (the first real import, 2026-10). The vault's plans and goals became 53 open tasks with no
  due day and no completion, the import asked question after question about which checkbox was a
  task, a habit or a rule, and the owner's verdict was that this database is a journal and a backup,
  not a project-management tool. A task is also the one entity whose rows go stale by design: open
  until closed somewhere else.
- **Alternatives.** *Keep tasks for reminders*: rejected — the owner's calendar and to-do app already
  remind, and a second list drifts from them. *Tasks as pages*: rejected — a task's name is a label,
  not a unique handle (`Dentist`), and it would collide.
- **Costs accepted.** A habit is still a 0/1 metric ([D24](D24-habits.md)); a done thing worth remembering is written in
  the day page. Tasks are additive later — a table hanging off `entities` and two link kinds — if the
  owner ever wants to-dos here.
- **Reopen trigger.** The owner wants to-dos, reminders or projects kept in this database.
