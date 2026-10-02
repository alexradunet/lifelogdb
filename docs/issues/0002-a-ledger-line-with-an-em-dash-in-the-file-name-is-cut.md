# 0002 — A ledger line whose file name contains " — " is cut in half

- **Date:** 2026-10-02
- **Status:** open
- **Seen in:** the import ledger (`make-ledger` / `ledgerState`), with a source file named `Protocol — Pareto Analysis.md`

## What happened

`ledger.md` writes one line per file as `- [state] <file>` and appends a note as ` — <note>`. Reading it back
splits on the first ` — `, so a file name that itself contains ` — ` is truncated and the rest of the name
becomes the note.

With a source folder holding one file, `A — B.md`:

```
$ lifelog do make-ledger
"files": [ { "file": "A", ... } ]
```

and the ledger line reads `- [ ] A — B.md`, which parses as file `A`, note `B.md`. Every later step for that
file then refuses:

```
lifelog: 422: A — B.md is not a ledger file to import (unknown or skipped)
```

The file is never imported, and it cannot be skipped either (`Skip` looks it up by name and finds no line).
Expected: the file is one ledger entry, named exactly as it is on disk.

## Reproduce

On a fresh database built from [schema.sql](../schema/schema.sql):

1. A source folder with a single file named `A — B.md`.
2. `lifelog import setup --workspace <source>.lifelog --from <db>`, then `lifelog do make-ledger`.
3. `lifelog do write-facts file="A — B.md" facts='{"file":"A — B.md","writes":[]}'` → 422 as above;
   `lifelog do write-facts file="A" ...` succeeds, which shows the ledger kept the wrong name.

## Rules involved

- [Importing](../guides/importing.md) — the ledger is the model's work list; every source file is in it.
- [Titles and wikilinks](../contract/titles-and-wikilinks.md) — a page title is a filename, so `—` is a legal
  title character; the ledger must be able to carry it.

## Resolution

_(open)_
