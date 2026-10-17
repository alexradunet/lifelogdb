# 0083 — The save recipe does not say that a fault of SAVEPOINT, ROLLBACK TO or RELEASE fails the save

- **Date:** 2026-10-17
- **Status:** resolved
- **Seen in:** step F3 of [plan 122](../plans/122-what-121-left.md) (commit `eba272f`), on 2026-10-17: a reproducer of
  the writer's save on a full file, with synthetic pages. No file in use was full. The owner decided (plan
  [123](../plans/123-the-owners-contract-decisions.md), decision 5): "fix it".

## What happened

[Save a body](../cookbook/save-a-body.md) said: "Each target is its own `SAVEPOINT`, so a target that fails for any
reason is rolled back alone (no link, no orphan `entities` row) and the save carries on." It did not say what a
writer does when the `SAVEPOINT` statement itself fails, or the `ROLLBACK TO` or the `RELEASE` of a target. A writer
that reads "any reason" as these statements too counts such an error as a skipped target and goes on.

The writer did this until plan 122. On a full file, a `SAVEPOINT` failed with `SQLITE_FULL` ("database or disk is
full"), and SQLite rolled back the whole transaction. The writer counted the target as skipped, and the save went on
outside a transaction: the `DELETE` of step 4 committed alone, and the `COMMIT` failed. The page kept its old body,
but it lost its links. A `RELEASE` after a target that succeeded did the same. A writer in another language can make
the same mistake, because the recipe did not say otherwise.

Why the file is full there: FTS5 keeps the terms of a write in memory. It writes them to the file when a savepoint
opens, and when a savepoint closes after a statement in it opened its own savepoint. When the file has no room for
the terms, that `SAVEPOINT` or `RELEASE` fails.

```mermaid
flowchart LR
  B["BEGIN IMMEDIATE, the new body"] --> S["SAVEPOINT target: SQLITE_FULL"]
  S --> R["SQLite rolls back the whole transaction"]
  R --> G["the writer counts a skipped target and goes on"]
  G --> D["step 4: the DELETE of the links commits alone"]
  D --> L["the page keeps its old body and loses its links"]
```

Expected: a fault of `SAVEPOINT`, `ROLLBACK TO` or `RELEASE` fails the whole save, and the page keeps its body and
its links.

## Reproduce

On a fresh file built from [schema.sql](../schema/schema.sql), with synthetic pages:

1. Save the page `Big` with a body of 10,000 different words, and the page `Home` with `[[Old]]` and 10,000 different
   words. Save each body once more in capitals: the rows then keep their size when the next save writes them.
2. `VACUUM`, then `PRAGMA max_page_count` = `PRAGMA page_count` + 16: the file can grow by 16 pages only.
3. `BEGIN IMMEDIATE`; write the new body of `Home`: `[[New]]` and 10,000 other words of the same length.
   `SAVEPOINT target` fails with `SQLITE_FULL`. A `ROLLBACK` now fails: no transaction is active.
4. A writer that goes on runs step 4 (`DELETE FROM links ...` with `'[]'`): it commits alone. `COMMIT` fails. `Home`
   has its old body and no link to `Old`.

The `RELEASE` case: a TEMP trigger (deliberate damage) writes the long body of `Big` again with other terms when the
save inserts the link to `New`. The terms then wait in memory until the `RELEASE` of that target, which fails with
`SQLITE_FULL`.

## Rules involved

- [Save a body](../cookbook/save-a-body.md): each target is its own `SAVEPOINT`.
- [D19](../decisions/D19-wikilink-save-contract.md): a bad target never blocks a save.
- [Connection setup](../contract/connections.md): every write transaction starts with `BEGIN IMMEDIATE`.
- SQLite, "Response To Errors Within A Transaction" (<https://sqlite.org/lang_transaction.html>): after `SQLITE_FULL`
  SQLite can roll back the whole transaction.

## Resolution

Resolved 2026-10-17 without a proposal: D19 already makes one transaction of a save, so the fix changes no decision
([how a change happens](../process.md)). [Save a body](../cookbook/save-a-body.md) now says that an error of a
target's steps 1–3 rolls that target back alone, and that an error of `SAVEPOINT`, `ROLLBACK TO` or `RELEASE` fails the
whole save; its diagram draws the path of the fault. [D19](../decisions/D19-wikilink-save-contract.md) gives the
reason. The `doc-save-contract` suite runs the recipe literally on a full file: a fault of `SAVEPOINT` and a fault of
`RELEASE` each fail the save and keep the page's body and links, SQLite has no open transaction after the fault, and a
save that goes on loses the links. A mutant of the recipe's sentence holds the witness. The writer fails the save on
these faults since plan 122 (F3, `eba272f`), and its own test holds it.
