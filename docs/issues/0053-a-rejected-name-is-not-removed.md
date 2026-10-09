# 0053 — A rejected name does not remove the row the import wrote, and it refuses every file that writes it

- **Date:** 2026-10-09
- **Status:** open
- **Seen in:** the 2026-10 import of a notes folder and a Takeout extraction; the names gate of
  [plan 084](../plans/084-owner-decides-names.md)

## What happened

The 2026-10-08 import wrote rows the owner does not want: companies and a beach as persons, first names alone as
persons. The owner found them through counts. Nothing lists what an import wrote, with the file and the quote that
wrote each row, so the owner cannot see whether a name was saved correctly.

The only way to remove such a row is a tombstone by hand. A replay into a fresh database does not carry it: the
facts write the row again. With the names gate, a replay into a fresh database needs a decision for each name, so
the tombstoned name waits as an undecided name, or comes back when its proposed row is approved.

The names gate added `rejected`, and it is not enough:

- A name the owner rejects after its row exists stops nothing: the row stays live, and every later write of the name
  finds it and links to it.
- Every done file that writes a rejected name is refused as a whole (class `rejected`). *status* reports each such
  file as a mismatch, and a replay fails it, with all its other writes.
- A held file whose name the owner rejects stays to do: its apply refuses it, and only a change of its facts file
  lets it apply.

Expected: a rejected name is never written, and the rest of each file that names it is written. The row the import
wrote under the name is tombstoned, on the trial and by each replay. A list shows every name of the import: the
owner's decision, the state of its row, and the file and the quote that wrote it.

## Reproduce

1. A notes source, the rules approved; a facts file writes the person "Cara" and a link to her; `entities.md`
   approves "Cara"; *apply facts*: the person and the link are written.
2. Change the row of "Cara" to `rejected` and stamp `entities.md`.
3. *status*: the file is a mismatch (`refused now: rejected`); the person "Cara" is still live.
4. A replay into a fresh database fails the file.

## Rules involved

- [importing with a model](../guides/importing.md), "entities.md" (the `name-decision` diagram), "The checks",
  "Trial, then the real run"
- [D11](../decisions/D11-tombstones.md) — an import revives a tombstoned row only by the owner's decision
