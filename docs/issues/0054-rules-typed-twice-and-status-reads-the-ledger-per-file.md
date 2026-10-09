# 0054 — Skip and later rules are typed twice, and status reads the whole ledger once for each done file

- **Date:** 2026-10-09
- **Status:** resolved
- **Seen in:** the 2026-10 import of a Takeout extraction (80 412 files) and a notes folder

## What happened

The rules of the Takeout extraction say, for each product folder, `skip:` (never imported) or `later:` (held for a
later pass). The writer does not read those words. After *ledger*, the model typed each pattern again as a *skip* or
*defer* mark: about 30 marks, each a model step, and a pattern typed twice can differ from the rule that the owner
approved.

*status* dry-runs every done file, and each dry run reads and parses the whole ledger again to find one line. The
cost grows with the done files times the ledger lines. A synthetic workspace shows it (`BenchmarkStatusLargeLedger`,
Windows 11, AMD Ryzen AI 9 HX 370):

| ledger lines | done files | one *status* | allocated |
|---|---|---|---|
| 20 000 | 300 | 3.3–4.5 s | 2.7 GB |

The profile puts 95 % of that time in the ledger parse of `ledgerState`.

*ledger* lists the source and then opens each file again through the source root (`os.Root.Stat`) to refuse a link
that leads outside it. A regular file that the walk found cannot lead outside, so for most files the second open
does nothing (`BenchmarkSourceFiles`: 20 000 files in 0.85 s, 98 % of it in that call).

Expected: *ledger* marks the files that the approved rules skip or hold when it lists them; *status* reads the
ledger once; the listing opens again only an entry that can lead outside the source.

## Reproduce

1. A source with `Drive/a.pdf`; `rules.md` approved with ``- `Drive/**` — skip: not a life log``.
2. *ledger*: `Drive/a.pdf` is `[ ]`, and *status* names it as the next file.
3. `go test ./internal/importer -run '^$' -bench '^BenchmarkStatusLargeLedger$' -benchmem`.

## Rules involved

- [importing with a model](../guides/importing.md), "The workspace" (`rules.md`, `ledger.md`), "The writer's
  operations" (*ledger*, *skip* / *defer*, *status*), "The procedure for the model" (step 5)

## Resolution

Resolved by [plan 086](../plans/086-rules-mark-the-ledger-and-status-reads-it-once.md): *ledger* marks each file it
adds as the first matching `## Folders` line of the approved `rules.md` says (`skip:` writes `[-]`, `later:` writes
`[>]`, the line's words are the note); *status*, *replay* and *propose entities* read the ledger once; the listing
opens again only an entry that is not a regular file. On the synthetic workspace, one *status* takes 0.21 s instead
of 3.3–4.5 s, and listing 20 000 files 18 ms instead of 0.85 s (the plan has the measurements).
