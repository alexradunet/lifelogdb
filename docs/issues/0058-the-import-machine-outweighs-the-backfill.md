# 0058 — The import machinery is larger than the life log, for a backfill done once

- **Date:** 2026-10-09
- **Status:** resolved
- **Seen in:** the 2026-10 rebuild of a life log from a notes folder (559 notes) and a Takeout extraction, and the
  owner's review of it the same day

## What happened

The import process ([importing with a model](../guides/importing.md)) puts a local model between the source and the
writer: the model writes a facts file per source file, the writer checks it, and a trial database, a replay, a
ledger and five owner stamps keep the model's work checkable and replayable. The rebuild showed what that costs and
what it gives:

| what reached the database | how | the owner's work |
|---|---|---|
| 559 note pages, 225 wikilinks, 136 categories | the notes copied as pages: no model | one stamp |
| 410 readings from bloodwork tables | the model, then the checks | a review of 137 metrics |
| 31 people, 4 places, 92 links from the prose | the model, then the names gate | three rounds of name decisions; 150 of 196 names rejected |
| 2 931 daily Fit readings | a fixed CSV profile: no model | one stamp |

The import code is about 12 000 lines (`internal/importer`, `importrun`, `takeout`, `inventory`), more than the
writes and reads of the life log (`internal/core` and `internal/api`, about 9 100). 18 of the 47 issues in the index
and 52 of 218 commits are about it. In one day the process failed six times outside the writer's checks (a leak of names to a
coordinator, dropped names, whole files refused for one quote, a stopped terminal, rows outside a table, a stale
editor copy), and each failure cost the owner's time, not the data's.

The owner's needs, stated in the review: the notes and the journal first, then people and places; the import is a
backfill done once, in batches; people and places come later as suggestions in the application, not at import; a
snapshot before an import and a restore to undo it is enough safety.

Expected: an import of a notes folder in batches by folder, written straight into the database after a snapshot,
with no model, no trial database and no stamps; bloodwork tables turned into readings by a fixed rule; a re-run that
writes nothing and never overwrites what the owner wrote.

## Reproduce

Count it: `wc -l internal/importer/*.go internal/importrun/*.go internal/takeout/*.go internal/inventory/*.go`
against `internal/core` and `internal/api`; the table above is the rebuild's own report.

## Rules involved

- [importing with a model](../guides/importing.md); [imports](../contract/imports.md), steps 1 and 6
- [D13](../decisions/D13-migrations-and-freeze.md): the freeze is defined by what an import workspace can replay
- [non-goals](../architecture/non-goals.md), the row "Generic view system, AI generation"

## Resolution

Resolved by [plan 089](../plans/089-no-import-process.md) (RFC [0011](../rfcs/0011-no-import-process.md), option C):
the import process is removed (the workspace, the facts files, the trial, the replay, the stamps, the import commands
and actions, the Takeout and ingest inventories, and the import guide). An importer is a program that loads rows
itself, or an agent that works with the owner through the catalog's MCP tools; one fixed action,
`readings-from-table`, turns a page's table into readings. The freeze of D13 is the first kept write that a rebuild
would lose.
