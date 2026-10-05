# 069 — Collect per-file reading-identity failures during rehearsal

> Executor: collect diagnostic failures without proceeding past an unsafe identity preflight. Recovery and real replay retain their safety gates. Use synthetic workspaces; set plan/index IN REVIEW (owner) after gates.
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/importer/reading_key_resolver.go internal/importer/replay.go internal/importer/replay_test.go internal/importer/reading_keys_test.go internal/api/replay_test.go`.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P3. **Effort:** M. **Risk:** MED (reporting must not become permission to replay).
- **Status:** IN REVIEW (owner). **Depends on:** implemented 043; execute after 056/066 to stabilize shared helpers.
- **Category:** bug/dx. **Deep-audit finding:** 25, the additional rehearsal-reporting regression.
- **Confidence:** HIGH, source-confirmed; new aggregate-report regression not run.

## Why

Rehearsal is meant to report failures together so the owner can fix a batch. Its new reading-identity preflight instead returns at the first invalid source file, requiring repeated runs to discover independent problems. Collect those preflight failures with their filenames, while still refusing all target writes.

## Current state and conventions

`internal/importer/replay.go:102–108` calls `RecoverCorrections`, then:

```go
if err := w.validateTrialReadingIdentity(ctx, trial); err != nil {
    return nil, err
}
```

`reading_key_resolver.go:306–340` returns on each load/prepare/resolve error. Existing `Failure{Step, File, Error}`, `newReplayResult` and `ReplayResult.failed` provide the result model; `Replay` already refuses when rehearsal has failures. Follow `TestReplayRehearsesBeforeItWrites` and the ambiguity fixtures in `reading_keys_test.go`.

This does not reopen the known separate limitation that vault replay stops at its first bad note, nor [the accepted non-atomic real replay](../guides/importing.md).

## Scope

Only paths in the drift command plus plan/index. No new replay engine, target transaction redesign, workspace locking, automatic identity repair, DDL/migrations or approval changes.

## Commands

- Focus: `go test -mod=readonly -count=1 ./internal/importer ./internal/api -run 'TestRehearsalIdentityFailures|TestReplayRehearsesBeforeItWrites|TestReplayDryRunAction|TestLegacyReading'`.
- Packages: `go test -mod=readonly -count=1 ./internal/importer ./internal/api`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and test plan

1. Add `TestRehearsalIdentityFailures` with two independently bad done/waiting reading files and one valid file. Include a malformed facts file, an ambiguous stored root, and controls for no-reading/skipped files. Capture target/workspace state and expected ledger-order diagnostics.
   **Verify:** focus command → baseline reports only the first failure instead of both file-scoped failures.
2. Return a deterministic collection of per-file preflight failures using the existing `Failure` shape. Continue validating independent files only; cancellation and global workspace/rules/ledger access errors still abort clearly. Do not swallow a ledger-read error (the current helper returns nil on that error). Keep source evidence and alias proof checks unchanged.
   **Verify:** focus command → all independent identity failures appear once with file/stage and unchanged rejection semantics.
3. Build a dry-run result early enough to return those failures. If identity preflight fails, return the collected result without target creation/copy/replay and without claiming integrity/count equivalence was assessed. Unperformed fields remain absent/unknown, not successful zero values. `Replay` must turn the collection into its existing refusal and leave existing/absent targets untouched.
   **Verify:** focus command → both API dry-run diagnostics and real replay refusal list all affected files; no target write occurs.
4. Preserve durable correction recovery as an explicit prerequisite; do not suppress a recovery conflict just to gather more errors. Test valid rehearsal still performs integrity/comparison, cancel terminates collection, and temporary-copy cleanup remains correct on later-stage failures.
   **Verify:** packages and final commands → exit 0; workspace changes occur only where existing explicit correction recovery requires them, never from diagnostic collection itself.

## Done criteria

- [x] Independent reading-identity failures are collected deterministically with filenames.
- [x] Failed preflight never creates/writes a target or claims unperformed integrity/comparison checks.
- [x] Approval, correction-recovery, cancellation and identity safety remain intact.
- [x] All gates pass; no broader replay refactor; index IN REVIEW (owner).

Implementation evidence (2026-10-05): rehearsal collects independent reading failures in ledger order before target creation/copy, leaving unperformed counts/integrity null. Correction recovery remains first; cancellation and shared access errors still abort. Importer fixtures combine malformed and ambiguous files, valid/skipped/no-reading controls and unchanged existing/absent targets. Parent-requested API coverage verifies a successful serialized rehearsal, two file-scoped failures with null unperformed fields, real-replay refusal and no target creation. Focus/package and integrated generation/vet/uncached full tests passed. The separate first-error vault-note limitation remains out of scope.

## STOP conditions

Stop if an error is global or dependent such that continuing cannot be safe; report that distinction rather than fabricating per-file results. Stop if the API cannot represent unperformed checks without a public shape change. Stop on unexplained drift or two failed gates.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `imports: aggregate rehearsal identity failures (plan 069)`, listing checks. New preflights must define whether they collect independent failures or abort globally, and why.
