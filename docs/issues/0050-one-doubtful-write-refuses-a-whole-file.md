# 0050 — One doubtful write refuses a whole facts file, and the refusal names only the first

- **Date:** 2026-10-08
- **Status:** resolved
- **Seen in:** the 2026-10 import of a notes folder and a contacts export (12 vCard files, one with 203 cards)

## What happened

*check facts* lists every static problem of a file in one message, but the checks against the database stop at the
first write that fails: a look-alike name, a unit, a link end not written yet. A file of 203 contact cards was
refused whole for one look-alike; 37 notes were set aside in one pass, 25 of them for one name each. The owner's
loop had to parse the refusal text with a regular expression, drop the write it named, send the file again, and
repeat — up to 80 times for one file — to learn what the writer would refuse.

The refusal is prose: a client that wants to act on it (drop the write, ask the owner, keep the value as text) has
no class to branch on, only words.

Expected: *check facts* reports every write the writer would refuse, each with a machine-readable class and, for a
look-alike, every candidate; *apply facts* keeps its invariant (a file commits whole or not at all) and carries the
class of the first refusal. The model, or any loop, fixes the facts in one round.

## Reproduce

1. A facts file with five good writes and two bad ones (a look-alike person, a reading with the wrong unit).
2. *check facts*: one 422 naming the first bad write only; after it is dropped, another 422 for the second.

## Rules involved

- [importing with a model](../guides/importing.md), "The checks", "The writer's operations" (*check facts*), "What an
  implementation must get right"

## Resolution

Resolved by [plan 083](../plans/083-check-reports-every-refusal.md): *check facts* runs every write in its own savepoint inside the rolled-back dry run and returns every refused write with a class, its candidates and the title it waits for; *apply facts* refuses the file whole with the first class on the error.
