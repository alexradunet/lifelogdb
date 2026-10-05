# 049 — Confine source reads across symlinks and junctions

> Executor: apply plan 044's filename fix first, then rebaseline its reviewed changes. All path fixtures must be synthetic and beneath test-owned temporary directories. Set plan/index to IN REVIEW (owner) after verification.
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/importer/workspace.go internal/importer/files.go internal/importer/vault.go internal/importer/source_paths_test.go internal/importer/source_confinement_test.go`.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P1. **Effort:** M. **Risk:** MED (cross-platform filesystem behavior).
- **Status:** IN REVIEW (owner). **Depends on:** [044](044-source-filename-identity.md).
- **Category:** security/bug. **Deep-audit finding:** 3, physical containment portion.
- **Confidence:** HIGH for lexical-only guard; Windows junction/race cases require execution.

## Why

An apparently source-relative path can lead outside the source through a filesystem link. Source text then comes from outside the import's intended boundary, despite the method promising confinement. Protect actual opens and metadata reads, not just string construction, while retaining the Unicode filename work.

## Current state and conventions

`internal/importer/workspace.go:79–85` checks a joined pathname prefix and then follows it with `os.Stat`:

```go
if !strings.HasPrefix(exact, w.Source+string(filepath.Separator)) {
    return "", refuse("%q leaves the source", rel)
}
if _, err := os.Stat(exact); err == nil {
    return exact, nil
}
```

`ReadSource` later calls `os.ReadFile(p)`; NFC fallback uses `os.ReadDir`/`os.Stat`, and `PlanVault` calls `os.Stat` on the returned path. `SourceFiles` uses `WalkDir`, which can list link entries without proving their read target is inside the source. Use the synthetic path-fixture helpers in `source_paths_test.go`. [Importing](../guides/importing.md) supplies the source/workspace model; this is application confinement, not a schema rule.

## Scope

Only `internal/importer/workspace.go`, `internal/importer/files.go`, `internal/importer/vault.go`, `internal/importer/source_paths_test.go`, new `internal/importer/source_confinement_test.go`, and plan/index. No workspace-write sandbox redesign, source renaming, identity changes, dependency/runtime upgrade, DDL or migrations.

## Commands

- Focus: `go test -mod=readonly -count=1 ./internal/importer -run 'TestSourceConfinement|TestSourceFilename|TestSourcePath|TestLedger|TestVault'`.
- Package: `go test -mod=readonly -count=1 ./internal/importer`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and test plan

1. Add `TestSourceConfinement` for synthetic file links and intermediate directory links whose targets are outside the selected root, in-root links, broken links, and NFC fallback through linked components. On Windows include directory junction coverage where the test environment permits it. Assert outside marker content is never returned and failed inventory/check/apply creates no replay artifacts or database rows.
   **Verify:** focus command → baseline outside-link assertions fail on capable platforms. Report unsupported link creation per case, not as a full-suite skip.
2. Inspect the pinned Go standard library's `os.OpenRoot`/`os.Root` behavior and use root-relative opens/stat/enumeration for source consumption. Keep a single selected physical root per operation, preserve exact-then-unique-NFC resolution, and refuse paths the rooted API cannot safely resolve. `SourcePath` must not falsely attest an escaping path; do not rely on `EvalSymlinks` followed by an unrelated unrestricted read as a race-proof boundary.
   **Verify:** focus command → in-root supported reads work, outside targets refuse, identity/collision tests remain green.
3. Route inventory candidate checks, `ReadSource` and the vault modification-time fallback through the confined helpers. Do not descend outside while resolving NFC candidates. Keep missing-source diagnostics and hidden-component policy. Use `rg -n 'SourcePath|ReadSource|os.ReadFile|os.Stat|os.ReadDir' internal/importer` to account for every source access; workspace-file accesses are distinct and out of scope.
   **Verify:** package and final commands → exit 0; if a safe deterministic link-swap fixture is available, it must never read outside the root, not merely pass a timing test.

## Done criteria

- [x] Actual source reads/stats, not only lexical validation, refuse escaping links/reparse points.
- [x] Unicode/whitespace identity, in-root supported paths and missing-path behavior remain correct.
- [x] Handles close on every path; refused reads leave persistence unchanged.
- [x] Platform coverage/limitations are explicitly reported, all gates pass, index is IN REVIEW (owner).

Implementation evidence (2026-10-05): actual reads, stat and inventory use `os.Root`; vault source reads are preflighted before persistence. Focus/package gates and integrated generation/vet/uncached full tests passed on Go 1.27.1 Windows. Junction escape cases executed; ordinary symlink cases skipped individually without Windows privilege. No deterministic concurrent link-swap test was added; confinement relies on rooted I/O rather than a validate-then-open path check.

## STOP conditions

Stop if the pinned runtime cannot provide the intended boundary on a supported platform, compatibility requires unrestricted reads, or root selection itself is ambiguous. Do not claim resistance to hard-link aliases or hostile filesystem mounts that the rooted API does not provide. Stop on unrelated drift, out-of-scope edits or two failed gates.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `imports: confine physical source access (plan 049)`, listing checks/platforms. New source consumers must use the confined open/stat helper, not treat a validated string as a capability.
