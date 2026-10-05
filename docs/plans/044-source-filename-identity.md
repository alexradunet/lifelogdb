# 044 — Preserve exact source filename whitespace and Unicode identity

> Executor: retain the implemented NFC-to-physical-path resolver. This follow-up fixes whitespace identity, not the separate containment policy in plan 049. Use synthetic folders only; update plan/index to IN REVIEW (owner) after all gates.
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/importer/files.go internal/importer/workspace.go internal/importer/vault.go internal/importer/source_paths_test.go`. Compare changed symbols before editing; stop on unexplained drift.

## Status

- **Planned at:** `0460379`, 2026-10-05; rebaselined from `726ffab` after the Unicode resolver landed.
- **Priority:** P1. **Effort:** S. **Risk:** MED (durable workspace identifiers).
- **Status:** IN REVIEW (owner). **Depends on:** none; execute before [049](049-source-path-confinement.md).
- **Category:** bug. **Deep-audit finding:** 3, filename-identity portion. **Confidence:** HIGH, source-confirmed; new regressions not executed.

## Why

A leading space is a filename byte, not incidental form formatting. `SourcePath` strips it, so a ledger entry for ` Note.md` can read different content from `Note.md`. Import evidence can then be attributed to the wrong source. NFC logical identity and exact physical spellings must remain distinct without silently changing either.

## Current state and conventions

`internal/importer/workspace.go:68`:

```go
rel = filepath.ToSlash(strings.TrimSpace(rel))
```

The rest of `SourcePath` first resolves an exact complete path, then searches NFC-equivalent candidates and refuses ambiguity. `files.go:422` inventories NFC relative paths without trimming; `Ledger` and `writeLedger` preserve filename strings in `- [state] file` lines. `vault.go` already uses `SourcePath` for the date-stat fallback.

Model fixtures after `TestSourceFilenameIdentity`, `TestSourcePathExactFullPathWins` and `TestSourcePathAmbiguousEquivalentRefuses` in `source_paths_test.go`. The [import guide](../guides/importing.md) requires source filenames to be respected; title trimming and NFC content normalization are separate concerns and stay as they are.

## Scope

Only `internal/importer/workspace.go`, `internal/importer/files.go`, `internal/importer/vault.go`, `internal/importer/source_paths_test.go`, this plan/index status. No source renames, ledger/facts/plan rewrites, persistent alias table, reading-key change, title predicate change, DDL or migrations. Symlinks/junctions belong to 049.

## Commands

- Focus: `go test -mod=readonly -count=1 ./internal/importer -run 'TestSourceFilenameIdentity|TestSourcePath|TestSourceFilenameWhitespace|TestLedger|TestVault'`.
- Package: `go test -mod=readonly -count=1 ./internal/importer`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and test plan

1. Add `TestSourceFilenameWhitespace` with two differently populated siblings, one whose filename begins with a space. Exercise inventory → ledger → `SourcePath`/`ReadSource` → facts checking; a quote from the other sibling must not pass. Include spaces in directory components and trailing whitespace on platforms that can create it distinctly.
   **Verify:** focus command → the leading-space identity assertion fails on the baseline. Only unsupported trailing-name subcases may skip, with a reason.
2. Stop trimming the relative path at the filesystem boundary. Preserve the existing absolute/traversal/hidden-component checks, slash handling, exact-full-path preference and unique NFC fallback. If a requested spelling does not exist, report it as missing rather than trying a trimmed sibling. Check caller normalization with `rg -n 'SourcePath|ReadSource|TrimSpace' internal/importer`.
   **Verify:** focus command → exact leading-space content is read; unknown/ambiguous/traversing paths remain refused.
3. Assert ledger/facts/plan strings and derived keys are byte-stable across re-read and repeat apply. Exercise a Unicode-decomposed filename with leading whitespace and the vault stat fallback. Source files must retain their original bytes and names.
   **Verify:** package command, then final command → exit 0; `git diff --name-only` contains only scope paths.

## Done criteria

- [x] Whitespace-distinct filenames never silently resolve to one another.
- [x] NFC inventory names still resolve unique NFD physical paths; collisions and hidden/traversing paths still refuse.
- [x] Source names/content, existing workspace identifiers and reading-key formats are unchanged.
- [x] All verification commands pass; index is IN REVIEW (owner).

Implementation evidence (2026-10-05): exact leading-space names, NFC/NFD resolution and missing trimmed-sibling requests are covered with synthetic sources. Focus/package gates and integrated generation/vet/uncached full tests passed. Unsupported trailing-filename cases may skip individually on Windows; no source names or persistent keys were rewritten.

## STOP conditions

Stop if an existing logical identifier is ambiguous, an on-disk name must be renamed, or a legacy record would need rewriting. Do not use trimming as an error-recovery heuristic. Stop on unexplained drift, out-of-scope edits or two unsuccessful gate attempts. Platform name limitations may skip only the affected case, not the whole suite.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push without instruction. Suggested commit: `imports: preserve source filename whitespace (plan 044)`, including checks run. Future UI helpers may trim human prose but must not normalize physical paths beyond the explicit logical-path contract.
