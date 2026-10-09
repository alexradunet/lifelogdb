# 085 — A rejected name is skipped, and its imported row tombstoned

> Dated implementation record, not permission to operate on real data. The owner runs every approval and replay.

## Status

- **Date / baseline:** 2026-10-09, `c82127c` (master).
- **Priority:** P1. **Effort:** M. **Risk:** MED for the import process (writes are removed by a decision; rows
  are tombstoned by an agent-callable action), none for the schema (untouched).
- **Status:** IN PROGRESS.
- **Implements:** RFC [0009](../rfcs/0009-a-rejected-name-is-skipped-and-tombstoned.md); **resolves** issue
  [0053](../issues/0053-a-rejected-name-is-not-removed.md).

## The change

| what | where |
|---|---|
| **Skipped writes.** Before the checks, a person, place or page write whose name a stamped `entities.md` rejects for its kind, and a link with an end whose name it rejects for any kind, is skipped: its outcome is `rejected`, it is not checked and not written. A reading whose `with` names a rejected name is written without it. The ledger note counts the skipped writes. The class `rejected` goes away. | `internal/importer/apply.go`, `facts.go`, `files.go` |
| **tombstone-rejected** (catalog action; an agent may run it): in one transaction, each live person, place or page whose source is the import's and whose import key a done facts file derives for a rejected name of that kind is tombstoned. Returns what it tombstoned. | `internal/importer/entities.go`, `internal/core/imports.go`, `internal/api/import.go` |
| **replay**: the same step on the target, after the facts files; the rehearsal lists the rows. | `internal/importer/replay.go` |
| **status**: the number of rows to tombstone, and "do now" says so; the number of rows of the import tombstoned on the trial whose name is not rejected (a replay writes them again), and "do now" says so when nothing else is to do. A skipped write is no mismatch. | `internal/importer/status.go` |
| **import-entities** (catalog resource, read only): every name of `entities.md` and every person, place and page the done facts files write: kind, name, the decision, the state of its row (written, tombstoned, not written, held by another type), the number of done files, the first file and its quote. | `internal/importer/entities.go`, `internal/api/import.go` |
| **Docs**: the guide ("entities.md", the `name-decision` diagram, the operations, the checks, the replay order, what an implementation must get right); D11; README; the mutant of the diagram; the RFC accepted. | `docs/`, `README.md`, `tests/` |

## Done criteria

1. Tests: a done file whose name is rejected later applies without the name's writes and is no mismatch; a held
   file whose name is rejected applies; a reading keeps its value without a rejected `with`; *tombstone rejected*
   tombstones only this import's rows and writes nothing a second time; a replay tombstones the row on a target that
   holds it and never creates it on a fresh one; *status* counts both cases; *import entities* lists the decision,
   the state and the evidence; the catalog action runs for an agent.
2. The guide states each rule once; the document and diagrams suites are green; baseline green on Windows.
3. The issue resolved, the RFC accepted, this plan DONE, in the commit of the change.
