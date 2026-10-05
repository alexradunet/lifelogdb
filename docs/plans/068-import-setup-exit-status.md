# 068 — Return failure when import setup integrity is not clean

> Executor: test using a deliberately inconsistent synthetic database, never the owner's file. Preserve the diagnostic report. Set plan/index IN REVIEW (owner) after gates.
>
> Drift check: `git diff --stat 0460379..HEAD -- cmd/lifelog/main.go cmd/lifelog/main_test.go`.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P2. **Effort:** S. **Risk:** LOW (failure exit semantics).
- **Status:** IN REVIEW (owner). **Depends on:** none; serialize CLI edits with 057/063.
- **Category:** bug/dx. **Deep-audit finding:** 24. **Confidence:** HIGH, source-confirmed; new command regression not run.

## Why

`import setup` prints a failed integrity result but returns success if printing succeeded. A script or owner checking only the command exit status can proceed with a bad trial copy. It should report the checks and signal failure, as snapshot verification already does.

## Current state and conventions

`cmd/lifelog/main.go:743–750`:

```go
if !res.OK {
    return printJSON(res)
}
fmt.Println("integrity: ok")
return nil
```

`core.Integrity` supplies the four-check result; setup must not reinterpret or repair it. Follow `TestSnapshot` in `main_test.go` for command-output/return conventions. [Integrity checks](../contract/integrity-checks.md) are authoritative; no new check is required.

## Scope

Only `cmd/lifelog/main.go`, `cmd/lifelog/main_test.go`, plan/index. No setup file deletion/repair, integrity-algorithm changes, new database format, DDL or migrations.

## Commands

- Focus: `go test -mod=readonly -count=1 ./cmd/lifelog -run 'TestImportSetupIntegrity|TestSnapshot'`.
- Packages: `go test -mod=readonly -count=1 ./cmd/lifelog ./internal/importer ./internal/core`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and test plan

1. Add `TestImportSetupIntegrity` using a fresh synthetic database containing an entity without its domain row (an integrity-check violation that can be constructed without corrupting file bytes). Run setup from that file into a synthetic workspace, capture output/returned error, and include a clean control. Do not read any existing `life.db` or workspace.
   **Verify:** focus command → baseline prints `ok: false` but incorrectly returns nil in the failure case.
2. Print the failed report, then return a non-nil error explicitly naming failed trial integrity. Preserve any printing error; do not return success merely because serialization worked. Keep the clean path's success message and return unchanged.
   **Verify:** focus command → failed setup returns an error and includes the report; clean setup returns nil and prints `integrity: ok`.
3. Assert the command entrypoint maps the failure to a nonzero exit using the repository's existing command-test pattern or a test-binary subprocess (no production build artifact). Preserve the trial file for diagnosis and ensure no approval/apply/replay action runs as a side effect. Cover integrity execution errors separately from an `OK=false` result.
   **Verify:** packages and final commands → exit 0; scope-only diff.

## Done criteria

- [x] Failed integrity is both reported and represented by a nonzero command result/exit.
- [x] Clean setup remains successful; output failures cannot become success.
- [x] The diagnostic trial is retained without repair or subsequent import writes.
- [x] All gates pass; index IN REVIEW (owner).

Implementation evidence (2026-10-05): synthetic orphan, query failure, output failure and clean controls cover direct setup; a test-binary subprocess verifies exit 1 and retained diagnostic trial. No repair/import artifacts are created. Parent focused checks and integrated generation/vet/uncached full tests passed.

## STOP conditions

Stop if the synthetic invalid file is rejected before reaching integrity; choose a validly opened orphan-row fixture rather than weakening open checks. Stop on unexplained drift, scope expansion or two failed gates.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `cli: fail setup on integrity errors (plan 068)`, listing checks. Future report-only commands must distinguish “report printed” from “operation passed.”
