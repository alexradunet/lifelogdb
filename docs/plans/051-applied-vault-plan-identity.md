# 051 — Refuse identity changes to already-applied vault notes

> Executor: preserve immutable titles and import keys. This plan refuses unsafe edits; it does not rename imported pages or repair a real trial. Use synthetic workspaces; set plan/index IN REVIEW (owner) after gates.
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/importer/vault.go internal/importer/importer_test.go internal/importer/vault_identity_test.go internal/importer/replay.go internal/importer/replay_test.go internal/core/imports.go internal/core/core_test.go internal/core/write.go`.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P1. **Effort:** M. **Risk:** MED (trial/replay identity).
- **Status:** IN REVIEW (owner). **Depends on:** none.
- **Category:** bug. **Deep-audit finding:** 5. **Confidence:** HIGH, source-confirmed; new regression not executed.

## Why

A user can edit a created note's plan title/day after apply. Reapply finds the old page by import key and keeps its old identity, but a fresh replay creates the new planned identity. The same workspace then describes different databases. The bounded fix is to refuse title/day changes after creation and detect manually edited conflicts before writes.

## Current state and conventions

`internal/importer/vault.go:119–132` treats any existing key as valid:

```go
case mine != 0:
    n.Action = "create" // created by this import before
```

`FixPlan` refuses only `Appended` notes before editing title/day. `applyPlan` calls `Tx.CreateImported`, whose existing-key path in `internal/core/write.go` returns the old id without changing title/day. Model fixtures after `TestVault` and `TestReplayRehearsesBeforeItWrites`.

The [title contract](../contract/titles-and-wikilinks.md) makes titles immutable; rename creates a redirect rather than updating a handle. The [import guide](../guides/importing.md) requires trial/replay fidelity. Do not introduce an implicit rename or use mutable local IDs as portable identity.

## Scope

Modify only `internal/importer/vault.go`, `internal/importer/importer_test.go`, new `internal/importer/vault_identity_test.go`, `internal/importer/replay.go`, `internal/importer/replay_test.go`, `internal/core/imports.go`, `internal/core/core_test.go`, and plan/index. `internal/core/write.go` is a read-only reference; a narrow transaction-scoped imported-page identity reader may be added in `imports.go` because `PageRef` does not expose the nullable day. No key/title mutation, schema/migrations, real workspace rewrites, automatic trial reset or general rename changes.

## Commands

- Focus: `go test -mod=readonly -count=1 ./internal/importer -run 'TestAppliedVaultPlanIdentity|TestVault|TestReplayRehearsesBeforeItWrites'`.
- Package: `go test -mod=readonly -count=1 ./internal/importer`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and tests

1. Add `TestAppliedVaultPlanIdentity`: plan/apply a synthetic note, request a new title or day, and compare persisted plan bytes and trial rows. Include a no-op request, a not-yet-applied note, an appended note, case-only title change and a manually edited plan. Assert refused edits leave plan/body/links/keys untouched.
   **Verify:** focus command → baseline accepts the unsafe created-note edit; existing normal vault cases pass.
2. In validation, load the page for an existing `(source, note path)` key within the same core transaction, using a narrow read helper if needed, and require its stored title and nullable day to agree with the plan. Use the stored handle spelling, not only a casefold equivalence. Give an actionable conflict naming the note and advising owner-controlled fresh-trial replanning, not a silent fix. Check before `FixPlan` saves and before `ApplyVault` starts its writes.
   **Verify:** focus command → mismatches refuse, exact no-ops and unapplied edits remain allowed.
3. Apply the same safety preflight to rehearsal/replay against the trial, including manually edited plans. A fresh target must not let an inconsistent trial plan through merely because it has no keys yet. Test an unchanged workspace replays identically, and an inconsistent one leaves both existing and absent targets untouched.
   **Verify:** package and final commands → exit 0, with stable page identity/backlinks and idempotent repeat apply.

## Done criteria

- [x] Post-apply title/day changes refuse before plan or database mutation.
- [x] Manual plan edits cannot produce trial/fresh-replay identity divergence.
- [x] Unapplied edits and exact no-ops still work; immutable handles/keys are preserved.
- [x] All gates pass; only scope paths changed; index is IN REVIEW (owner).

Implementation evidence (2026-10-05): checked applied identities reject title/day drift, including manual plan changes, before fresh replay. Synthetic identity/no-op regressions and plan gates passed. Integration preserved rooted source reads and passed generation, vet and uncached full tests.

## STOP conditions

Stop if a legitimate workflow requires post-apply retitling/redating, a trial has already diverged, or replay lacks enough trial identity to verify safely. Ask for owner policy; do not rename, rebuild or rewrite provenance automatically. Stop on unrelated drift, out-of-scope edits or two failed gates.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `imports: lock applied vault plan identity (plan 051)`, listing checks. Future plan-edit fields need the same distinction between draft metadata and already-persisted identity.
