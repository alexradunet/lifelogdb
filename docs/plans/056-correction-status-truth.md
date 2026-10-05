# 056 — Make correction status agree with replay without weakening proof

> Executor: read the current recovery/identity helpers before editing. Status must remain diagnostic, never perform recovery or approve rules. Use synthetic workspaces; set plan/index IN REVIEW (owner) only after all lineage gates.
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/importer/corrections.go internal/importer/status.go internal/importer/reading_key_resolver.go internal/importer/replay.go internal/importer/correction_status_test.go internal/core/imports.go internal/api/replay_test.go`.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P2. **Effort:** M. **Risk:** HIGH (misclassifying recoverability or accepting another root).
- **Status:** IN REVIEW (owner). **Depends on:** implemented 041/043 semantics; run after [038](038-reading-source-evidence.md)'s follow-up.
- **Category:** bug. **Deep-audit finding:** 10. **Confidence:** HIGH, source-confirmed; new status cases not run.

## Why

Status reports a false conflict after a legacy-root correction is replayed onto canonical keys, can fail entirely when rules return to draft, and counts durably replayable agent corrections as lost work. These messages obstruct safe operator decisions. Correct classification must use the same checked identity evidence as replay, not hide conflicts by comparing only values.

## Current state and conventions

`corrections.go:715` calls `verifyEventRow` with the literal intent root; that wrapper calls `verifyEventRowWithRoot(..., intent.RootImportKey)`. Replay at `replay.go:416–433` resolves aliases/proofs first. The mismatch yields `correction intent %s matches a row with another root`.

`validateCorrectionIntent` at line 543 requires `approvedImportSource`, even for read-only status. `status.go:262` counts every `agent:%` measurement alongside direct entity/link writes and says replay will not carry them. Follow `TestCorrectionCopiedTarget`, `TestCorrectionIntentInvariants`, and `TestCorrectionProofBindsCheckedFacts`. Existing immutable facts, durable event identities and snapshot-bound alias proofs are load-bearing.

## Scope

Only paths in the drift command plus plan/index. `internal/core/imports.go` may gain a narrow parameterized read helper for classification/counts. No intent/legacy-file format changes, measurement updates, key repairs, approval bypass, automatic recovery or DDL/migrations. Read [plans 041](041-durable-import-corrections.md) and [043](043-canonical-reading-keys.md) for protocol constraints, not authorization to redesign them.

## Commands

- Focus: `go test -mod=readonly -count=1 ./internal/importer -run 'TestCorrectionStatus|TestCorrectionCopiedTarget|TestCorrectionIntent|TestCorrectionRecovery|TestCorrectionProof|TestLegacyReading'`.
- Integration: `go test -mod=readonly -count=1 ./internal/importer ./internal/api ./internal/core`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and tests

1. Add `TestCorrectionStatus` covering original trial, canonical fresh replay, a copied target, chained corrections/retraction, draft rules, wrong workspace, edited facts, ambiguous roots and an unrelated direct agent reading. Snapshot workspace bytes and database rows before/after status.
   **Verify:** focus command → baseline false-conflict/draft/lost-work assertions fail; hostile lineage tests remain rejecting.
2. Separate structural intent parsing from approved-workspace verification without relaxing write/recovery gates. With draft or stale rules, status returns an explicit “verification blocked by approval” diagnostic and gate information; it must neither claim clean/replayable nor fail just because approval is pending. Malformed/cross-workspace intents remain distinguishable conflicts.
   **Verify:** focus command → status is readable in draft state; recovery/apply still refuse unapproved rules.
3. For approved inputs, share replay's literal-root-first/checked-alias verification logic with status. Bind alias evidence to the same checked facts snapshot as 043. Use resolved root identity with `verifyEventRowWithRoot`; never accept an event merely because metric/value happen to match. Preserve predecessor and unknown-successor checks.
   **Verify:** focus command → canonical replay is clean; wrong roots, edited proof inputs and unrelated later corrections remain conflicts.
4. Exclude only positively verified durable correction rows from the “not carried by replay” count. Leave direct agent entities/links/readings and unverifiable events visible. Count exact event identity, not an import-key prefix; propagate read errors rather than suppressing warnings.
   **Verify:** integration and final commands → exit 0; tests prove status changes no rows, files or approval state.

## Done criteria

- [x] Original/canonical/copied correction states agree with replay using intact proof checks.
- [x] Draft rules yield an honest blocked-verification diagnostic, not a bypass or blanket failure.
- [x] Verified agent corrections are not falsely called lost; genuine outside writes remain reported.
- [x] Status is side-effect-free; all gates pass; index IN REVIEW (owner).

Implementation evidence (2026-10-05): structural read-only diagnostics retain approved-source gates for writing/recovery. Literal-root verification falls back to the existing checked alias resolver and unchanged event topology checks; only positively verified measurement IDs are excluded from direct-agent counts. Parent protocol review requested additional actual Status-path tests for copied/wrong-workspace/ambiguous roots, draft rules, direct entities/links and canonical chains. Every call compares all non-database workspace file bytes and all logical SQLite table rows, including the original trial. Physical database/WAL/SHM/journal bytes and filesystem metadata are excluded; logical rows are compared instead. Focus/lineage/package gates and integrated generation/vet/uncached full tests passed. Owner acceptance remains pending.

## STOP conditions

Stop if status cannot obtain sufficient alias proof; report “cannot verify” instead of declaring clean. Stop if any repair, implicit approval, mutable facts or weaker root comparison appears necessary. Stop on unexplained drift or two failed gates; require protocol-focused owner review for this HIGH-risk plan.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `imports: align correction status with checked replay (plan 056)`, listing checks. Future replay identity changes must update diagnostic verification in the same change.
