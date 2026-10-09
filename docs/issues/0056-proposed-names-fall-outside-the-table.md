# 0056 — Proposed names fall outside the table of entities.md, and are proposed again

- **Date:** 2026-10-09
- **Status:** open
- **Seen in:** the 2026-10 import of a notes folder: the second round of *propose entities*, after the owner had
  edited and saved `entities.md` in an editor

## What happened

*propose entities* adds its rows at the end of `entities.md`. The owner's editor had left a blank line after the
table. The new rows came after that blank line, so they were outside the table: the writer reads a table up to its
first line that is not a row. The writer did not see the rows. The names stayed undecided, the owner could not stamp
them, and the next *propose entities* added the same rows a second time.

Expected: a proposed row goes after the last row of the table, whatever follows the table in the file.

## Reproduce

1. A workspace whose `rules.md` holds the line `- "Bobby" → "Bob Sample"` under `## Aliases`.
2. `entities.md`: `status: draft`, the header, one row `| rejected | person | Dana | | | | |`, then a blank line
   and a line of the owner's words.
3. *propose entities*: it reports one row added, but the writer reads only the row of Dana.

## Rules involved

- [importing with a model](../guides/importing.md), "entities.md": *propose entities* adds a proposed row for each
  name that the owner has not decided

## Resolution

Filled in when it closes.
