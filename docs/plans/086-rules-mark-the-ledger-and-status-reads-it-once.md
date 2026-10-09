# 086 — The rules mark the ledger, and status reads it once

> Dated implementation record, not permission to operate on real data. The owner runs every approval and replay.

## Status

- **Date / baseline:** 2026-10-09, `2735bcb` (master).
- **Priority:** P2. **Effort:** M. **Risk:** LOW for the import process (a mark the owner's rule already states),
  none for the schema (untouched).
- **Status:** IN PROGRESS.
- **Resolves:** issue [0054](../issues/0054-rules-typed-twice-and-status-reads-the-ledger-per-file.md). No RFC: no
  decision changes.

## The change

| what | where |
|---|---|
| **Rules mark the ledger.** A `## Folders` line of an approved `rules.md` whose description starts with `skip:` or `later:` is a mark: when *ledger* adds a file, the first folder line whose pattern matches it decides, and a `skip:` or `later:` line writes the file `[-]` or `[>]` with its words as the note. A line that *ledger* wrote before stays as it is; *skip* and *defer* stay for one file or a pattern. A draft `rules.md` marks nothing. | `internal/importer/workspace.go`, `files.go` |
| **The ledger is read once per operation.** *status* gives each dry run the line it read; *replay* and *propose entities* do the same. | `internal/importer/apply.go`, `status.go`, `replay.go`, `entities.go` |
| **The listing opens again only what can lead outside.** A regular file found by the walk is not opened a second time; a link, a junction or another entry is, as before. | `internal/importer/files.go` |
| **Docs**: the guide (`rules.md`, `ledger.md`, *ledger*, step 5, what an implementation must get right); the measurements below. | `docs/guides/importing.md`, this plan |

## Done criteria

1. Tests: *ledger* marks the files of a `skip:` and a `later:` rule, the first matching line decides, a draft
   `rules.md` marks nothing, a second *ledger* changes no line; the confinement tests stay green.
2. `BenchmarkStatusLargeLedger` and `BenchmarkSourceFiles` measured before and after, with `-count=5`, recorded here.
3. The guide states each rule once; the document suite is green; baseline green on Windows.
4. The issue resolved and this plan DONE in the commit of the change.

## Measurements

Windows 11, AMD Ryzen AI 9 HX 370, Go of `go.mod`, warm file cache, synthetic data (`ledger_bench_test.go`).

| benchmark | before | after |
|---|---|---|
| `BenchmarkStatusLargeLedger` (20 000 lines, 300 done) | 3.3–4.5 s, 2.7 GB | |
| `BenchmarkSourceFiles` (20 000 files) | 0.85 s, 21 MB | |
