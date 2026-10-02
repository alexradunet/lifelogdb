# 0007 — A lifetime file has no second copy, and the freeze checklist asks for a decision

- **Date:** 2026-10-02
- **Status:** open
- **Seen in:** the freeze checklist ([process](../process.md#before-the-freeze), item 3), read against the
  [threat model](../contract/threat-model.md) while preparing the first import into the canonical `life.db`

## What happened

The first import into the canonical `life.db` is the next step, and the freeze follows it: from then on the file holds
rows that no import can replay ([D13](../decisions/D13-migrations-and-freeze.md)). The threat model's first row, *the file is
damaged or lost*, has the residual "nothing recovers it: no second copy of the file is kept", because export,
snapshots, restore and an off-box copy are one row of the [non-goals](../architecture/non-goals.md) whose "Reopen when" is
the freeze. Item 3 of the freeze checklist cannot be ticked until the owner decides that row.

A synthetic example of the loss: a `life.db` holding ten years of day pages, a person's page and a weight series is
damaged by a writer bug that the integrity checks catch only after weeks of further writes ([integrity
checks](../contract/integrity-checks.md) find damage; they do not repair it). With no earlier copy there is nothing to
compare it with and nothing to go back to: the pages written before the damage are as lost as the ones after.

Expected: a copy of the file that is consistent (taken while the writer may be writing), dated, never overwritten by
the next one, checked before it is trusted, and a way to put it back.

## Reproduce

1. Build a fresh database from [schema.sql](../schema/schema.sql) and write a day page into it.
2. Look in the docs for how to take a copy that is not a trial import ([imports](../contract/imports.md) step 1) and how to
   restore one: there is no recipe, and the [non-goals](../architecture/non-goals.md) row cuts it.

## Rules involved

- [Non-goals](../architecture/non-goals.md): the export, snapshot and off-box copy row, reopened at the freeze.
- [Process](../process.md#before-the-freeze), item 3; [D13](../decisions/D13-migrations-and-freeze.md) (the freeze).
- [Threat model](../contract/threat-model.md): *the file is damaged or lost*.
- [Connection setup](../contract/connections.md): one writer, read-only readers; [integrity
  checks](../contract/integrity-checks.md).

## Resolution

Open.
