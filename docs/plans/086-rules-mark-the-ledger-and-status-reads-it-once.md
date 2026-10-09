# 086 — The rules mark the ledger, and status reads it once

> Dated implementation record, not permission to operate on real data. The owner runs every approval and replay.

## Status

- **Date / baseline:** 2026-10-09, `2735bcb` (master).
- **Priority:** P2. **Effort:** M. **Risk:** LOW for the import process (a mark the owner's rule already states),
  none for the schema (untouched).
- **Status:** DONE.
- **Resolves:** issue [0054](../issues/0054-rules-typed-twice-and-status-reads-the-ledger-per-file.md). No RFC: no
  decision changes.

## The change

| what | where |
|---|---|
| **Rules mark the ledger.** A `## Folders` line of an approved `rules.md` whose description starts with `skip:` or `later:` is a mark: when *ledger* adds a file, the first folder line whose pattern matches it decides, and a `skip:` or `later:` line writes the file `[-]` or `[>]` with its words as the note. A line that *ledger* wrote before stays as it is; *skip* and *defer* stay for one file or a pattern. A draft `rules.md` marks nothing. | `internal/importer/workspace.go`, `files.go` |
| **The ledger is read once per operation.** *status* gives each dry run the line it read; *replay* and *propose entities* do the same. | `internal/importer/apply.go`, `status.go`, `replay.go`, `entities.go` |
| **The listing opens again only what can lead outside.** A regular file found by the walk is not opened a second time; a link, a junction or another entry is, as before. | `internal/importer/files.go` |
| **Docs**: the guide (`rules.md` and its example, `ledger.md`, *ledger*, step 5, what an implementation must get right); the `make-ledger` and `draft-rules` descriptions of the catalog; the measurements below. | `docs/guides/importing.md`, `internal/api/import.go`, this plan |

## Done criteria

1. Tests: *ledger* marks the files of a `skip:` and a `later:` rule, the first matching line decides, a draft
   `rules.md` marks nothing, a second *ledger* changes no line; the confinement tests stay green.
2. `BenchmarkStatusLargeLedger` and `BenchmarkSourceFiles` measured before and after, with `-count=5`, recorded here.
3. The guide states each rule once; the document suite is green; baseline green on Windows.
4. The issue resolved and this plan DONE in the commit of the change.

## Measurements

Windows 11, AMD Ryzen AI 9 HX 370, go1.27.1 windows/amd64, the pinned `modernc.org/sqlite`, warm file cache,
synthetic data (`ledger_bench_test.go`). Before: `-count=3` at `3a2d43d`; after: `-count=5`. Ranges per operation
(`benchstat` is not installed). The first "after" sample of status (0.36 s) is the warm-up run.

| benchmark | before | after |
|---|---|---|
| `BenchmarkStatusLargeLedger` (20 000 lines, 300 done) | 3.29–4.53 s, 2.66 GB, 12.2 M allocs | 0.21–0.23 s, 26 MB, 166 k allocs |
| `BenchmarkSourceFiles` (20 000 files) | 0.83–0.86 s, 21 MB, 350 k allocs | 17–18 ms, 8.5 MB, 70 k allocs |

After the change, the profile of *status* shows no ledger parse under *status*: its cost grows with the done files
(each facts file loaded and dry-run), not with the done files times the ledger lines.
