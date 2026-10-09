# 0052 — People and places are written on the model's word, and each name decision closes the rules gate

- **Date:** 2026-10-08
- **Status:** open
- **Seen in:** the 2026-10 import of a notes folder and a contacts export (12 vCard files, 254 names)

## What happened

A facts write of a person or a place makes the row when the checks pass. The owner has no choice before the row
exists. In the contacts export, the facts wrote 261 persons from 12 files before the owner said "let me choose which
ones"; the trial was deleted and rebuilt. In the notes folder, the model wrote companies as persons, a beach as a
person and first names alone ("Andrei") as persons. The owner found them through counts, not through a list.

A name decision has two homes, both slow:

- An alias or a distinct pair is a line in `rules.md`. Each new line makes the rules gate a draft, and every *check
  facts* and *apply facts* stops until the owner stamps the whole file again. In one day the owner stamped
  `rules.md` nine times, several times for one line.
- A doubt is a question in `questions.md`, one file at a time; the answer is copied onto its `answer:` line.

A person or a place that the owner tombstoned is revived by the next facts write of the same title, also by a
replay into the real database: hand cleanup does not hold.

Expected: the writer holds a new person or place, and a new page that looks like another name, until the owner
decides it — approved, rejected, or an alias of an existing title — in one stamped list, many names in one stamp.
The rules gate closes only when the folder rules change. A tombstoned name is decided again, never revived silently.

## Reproduce

1. A notes source, the rules approved, a facts file that writes the person "Cara" and a link to her.
2. *apply facts*: the person is written. Nothing asked the owner.
3. Tombstone "Cara"; *apply facts* again: "Cara" is live again.
4. Add an alias line to `rules.md`: every *check facts* stops (`rules.md is draft`) until the owner stamps it again.

## Rules involved

- [importing with a model](../guides/importing.md), "The workspace" (`rules.md`, `questions.md`), "Gates", "The checks",
  "The procedure for the model" (steps 6d, 7, 8)
- [D11](../decisions/D11-tombstones.md) — revival clears a tombstone
- [a person or a place](../cookbook/person-or-place.md)

## Resolution

Open.
