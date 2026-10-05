# 044 — Resolve normalized source names to their physical filenames

> Executor: run from the repository root, preserve existing logical identifiers, and use only synthetic source folders. Set the index row to IN REVIEW (owner) after all gates pass.
>
> Drift check: `git diff --stat 726ffab..HEAD -- internal/importer/files.go internal/importer/workspace.go internal/importer/vault.go internal/importer/importer_test.go internal/importer/source_paths_test.go`.

## Status

- **Date / planned at:** 2026-10-05, commit `726ffab`.
- **Priority:** P2. **Effort:** M (including legacy path compatibility). **Risk:** MED (workspace path identity).
- **Status:** TODO. **Depends on:** none. **Category:** bug. **Audit finding:** 11.

## Why

The inventory uses NFC logical paths, but source reading joins that normalized spelling directly to the source directory. On a filesystem where NFC and NFD filenames are byte-distinct, an inventoried note cannot be opened. Separate normalized logical identity from physical filename resolution, preserving every existing ledger/facts/plan identifier and never renaming the source.

## Current state and conventions

`internal/importer/files.go:436`, in `SourceFiles`:

```go
out = append(out, norm.NFC.String(filepath.ToSlash(rel)))
```

`internal/importer/workspace.go:77–89`:

```go
p := filepath.Join(w.Source, filepath.FromSlash(rel))
// ... lexical containment guard ...
return p, nil
// ReadSource:
b, err := os.ReadFile(p)
```

Text is then NFC-normalized intentionally. `PlanVault` uses NFC page titles, but its fallback `os.Stat(w.Source + "/" + f)` repeats the physical-path assumption. `SourcePath` rejects absolute/traversing/hidden paths; those guards must remain. `TestLedger` and `TestVault` in `importer_test.go` are the fixture patterns. The [import guide](../guides/importing.md) explicitly requires filenames to be read as on disk, NFD included.

## Scope

Only `internal/importer/files.go`, `internal/importer/workspace.go`, `internal/importer/vault.go`, `internal/importer/importer_test.go`, new `internal/importer/source_paths_test.go`, and plan/index status. No source-file rename, ledger/facts/plan rewrite, reading-key format change, persistent alias table, schema/migrations, title predicate change, or broader symlink/security policy. Do not normalize content twice or stop normalizing text.

## Commands

- Paths: `go test -mod=readonly -count=1 ./internal/importer -run 'TestSourceFilenameIdentity|TestLedger|TestVault'`.
- Package: `go test -mod=readonly -count=1 ./internal/importer`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./...`.

## Steps

1. Add `TestSourceFilenameIdentity`: create decomposed filenames and decomposed directory components under `t.TempDir()`, then inventory, inspect/read and plan/apply a vault note via the inventory's logical spelling. Assert NFC page titles/text and exact original source bytes/names after processing. Include an existing logical ledger/facts/plan reference, a unique normalized alias, and canonically equivalent sibling directories with different leaf filenames.
   **Verify:** paths command → on a byte-distinct filesystem the normalized inventory-to-open path fails. On normalization-insensitive systems explicitly report that filesystem property, while still running the non-collision cases.
2. Keep NFC logical paths, because existing workspace identifiers/keys use them. Resolve an exact full physical path first. Otherwise enumerate NFC-equivalent directory-entry candidates along the validated relative components and require exactly one complete existing physical path. Do not greedily choose an exact directory component when a different equivalent directory contains the uniquely matching leaf. Return actual disk spellings, reject ambiguous full-path matches, and keep resolution local/bounded rather than rewalking the entire vault for every note. No substring/case-insensitive guessing, compatibility folding or source renaming. Preserve the source-relative boundary and hidden-folder checks on logical and resolved components.
   **Verify:** paths command → inventoried NFD notes and directory components read successfully; traversal/hidden-path regressions remain refused.
3. Detect normalized inventory collisions before writing a new ledger/plan: two physical files collapsing to one logical path must be reported, not silently duplicated/collapsed. Add ambiguous-fallback tests where the requested spelling is not an exact physical name. Do not remap duplicate or conflicting existing logical references automatically. Change `PlanVault`'s stat fallback to use the resolver, and audit other source opens/stats for the same assumption.
   **Verify:** `rg -n 'w.Source.*[/+]|os.Open|os.ReadFile|os.Stat' internal/importer` → review all source-related matches; package command → collision and legacy-preservation cases pass.
4. Confirm repeat apply and replay retain the same paths/keys and append nothing. A source removed or renamed between inventory and reading must yield a clear existing-style error, not a replacement note guessed by title.
   **Verify:** package command, then final command → exit 0.

## Done criteria

- [ ] Inventory logical paths reliably resolve NFD files/directories on byte-distinct filesystems.
- [ ] NFC logical identifiers and page/text normalization remain stable; source files and stored workspace identifiers are unchanged.
- [ ] Ambiguous normalized names refuse rather than silently selecting/merging; hidden/traversal checks remain enforced.
- [ ] Vault date fallback uses the same resolver; repeat import remains idempotent.
- [ ] Full verification passes; only scope paths changed; index is IN REVIEW (owner).

## STOP conditions

Stop if legacy identifiers map to multiple physical files, if a fix requires changing path/key formats or existing records, or if symlink containment needs a separate policy. Do not repair real filenames or expand this into a general filesystem sandbox. Never skip the entire regression suite merely because one platform normalizes names.

## Git workflow and maintenance

Use an operator-selected branch/worktree. Commit as `imports: resolve physical Unicode filenames (plan 044); ran go generate, go vet and go test`; push only when instructed. Every source open/stat must go through the same resolver; normalized logical names are not automatically physical filesystem paths.
