# 0057 — The import loop lives in scripts outside the writer, and it fails where no test sees it

- **Date:** 2026-10-09
- **Status:** open
- **Seen in:** the 2026-10 rebuild of a life log from a notes folder (559 notes) and a Takeout extraction

## What happened

The facts pass of [importing with a model](../guides/importing.md) is a loop: *status*, then one model completion for
the next file, then *write facts*, *check facts*, *apply facts*; held names are kept, refused writes are kept as
text, names are proposed when the files wait for them. No step needs a judgement between two model calls. The loop
ran as owner-local scripts that called the writer over HTTP, outside the repository and without tests. A
coordinator (a hosted model that must not read the data) sent commands to a local agent that ran the scripts. Four
failures came from the scripts, not from the writer:

1. The scripts wrote each refused write, with its name and the look-alike candidates, into the report folder that
   the coordinator reads. A name such as "Cara Example" reached a party that must see counts only.
2. The contacts script dropped each write that the writer held for the owner's decision and applied the rest, so
   the file was marked done and the names were never proposed.
3. The facts script moved the quote of a refused write into `kept_as_text`. When that quote was not in the file, the
   writer refused the whole file: 9 of 559 notes were stuck for that reason alone.
4. A closed terminal stopped a run; the coordinator waited for a log line that never came.

The local agent (a 2-bit model) took about 3 minutes per note when it ran the procedure itself, and made quoting
slips; the direct loop, one completion per note, took about 7 seconds.

Expected: the loop is part of the writer, tested like the rest of it, and the owner runs it at a terminal with a
local model, with no other party in between.

## Reproduce

A synthetic notes folder with a note that names "Cara Example", and a model whose answer writes a person
"Cara Example" (held: no decision) and a `kept_as_text` quote that is not in the note. Run the owner-local loop: the
report folder holds the name, and the note is refused whole.

## Rules involved

- [importing with a model](../guides/importing.md), "Three parties" and "The procedure for the model"
- [non-goals](../architecture/non-goals.md), the row "Generic view system, AI generation"
- the writer's decisions: README, "Owner-only actions"

## Resolution

Filled in when it closes.
