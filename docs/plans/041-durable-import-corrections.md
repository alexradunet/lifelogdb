# 041 — Make database corrections and replay intent recoverable

> Executor: this is HIGH-risk coordination work. Complete the protocol/failure-matrix gate before implementation, stop at any unresolved recovery decision, and run all commands from the repository root. Set the index row to IN REVIEW (owner) only after the fault-injection tests pass.
>
> Drift check: `git diff --stat 726ffab..HEAD -- internal/api/habits.go internal/api/replay_test.go internal/core/write.go internal/core/imports.go internal/core/core_test.go internal/importer/files.go internal/importer/workspace.go internal/importer/corrections.go internal/importer/corrections_test.go internal/importer/replay.go internal/importer/status.go internal/importer/replay_test.go docs/guides/importing.md README.md`. Changes from plans 035/037 are expected: verify their tests and rebaseline their interfaces first.

## Status

- **Date / planned at:** 2026-10-05, commit `726ffab`.
- **Priority:** P1. **Effort:** L (including fault injection and compatibility). **Risk:** HIGH (two persistence domains).
- **Status:** TODO. **Depends on:** [035](035-correction-value-validation.md), [037](037-import-correction-lineage.md). **Category:** bug. **Audit finding:** 8.

## Why

An imported correction must survive the eventual rebuild/replay. Today the database commits before its workspace record is attempted. If the workspace is unwritable, the API returns an error after changing the database, and replay loses the correction. Reversing the two independent writes without recovery merely reverses the inconsistency window; do not pretend that a file and SQLite share one transaction.

## Current state and conventions

`internal/api/habits.go:15–25`:

```go
fix, key, err := h.s.Correct(r.Context(), src, id, value)
if err != nil {
    return nil, err
}
if h.ws != nil && core.IsImport(key.Source) && key.Key != "" {
    if err := h.ws.RecordCorrection(key); err != nil {
        return nil, err
    }
}
```

`Store.Do` commits after its callback returns nil. `RecordCorrection` locks `Workspace.mu`, reads and appends the whole JSON list, then calls `writeJSON`. `writeAtomic` uses temporary-file/rename but no file sync. Plan 037 resolves the imported root and makes legacy replay use each root's final state. `Status` is read-only; `Rehearse` already takes an explicit trial store.

Existing measurements have an optional `import_key` and a unique `(source, import_key, metric_id)` index. A writer-generated correction-event key can use those existing columns without a DDL change. Preserve [D7](../decisions/D07-measurements.md), [D13](../decisions/D13-migrations-and-freeze.md), and the [import guide](../guides/importing.md). Never modify old facts or stored provenance.

## Scope

Only the drift-check paths, this plan's protocol notes, and index status. New importer correction helpers/tests are allowed. No database schema/outbox table/migration, generic workspace locking project, whole-replay atomicity redesign, model-supplied keys, source-file edits, or real data access. Plan 043 must preserve references to both legacy and new records later.

## Protocol and failure matrix

Protocol to implement after owner approval:

1. A correction request that reaches an import workspace first acquires `Workspace.mu`, before any database write transaction. While holding that workspace coordination, recover every ready correction intent in the workspace against the trial database. If recovery reports a conflict or pending ambiguity, the new correction is refused until the owner resolves it. `status` remains read-only and reports those states; rehearsal and replay also run recovery first.
2. The workspace source from approved `rules.md` must equal the imported root's `source`. A missing approved source, a non-imported root, a root without an `import_key`, or a source mismatch is refused before writing either persistence domain.
3. The writer builds an immutable generated correction event id and intent from data already verified in the open SQL transaction: workspace source, actor/source making the correction, root `(source, import_key, metric)`, predecessor identity, desired value or retraction, and ordering. Legacy predecessors are the imported root's current final state in this workspace; event predecessors name the generated event key of the prior correction. The event id is private writer provenance, stored as the correction row's `import_key`; no request supplies it.
4. Inside one `Store.Do`, insert the correction row with the generated event key but keep SQL uncommitted. While SQL is still uncommitted, write, sync and publish a ready intent file under `correction-intents/`. Publication is all-or-absent: temporary file, file sync, atomic rename, directory sync where supported. A publication error returns from the transaction callback so SQL rolls back.
5. Commit SQL after the ready intent is published. If commit reports failure after publication, return a pending-recovery error, not success and not “nothing changed”. The accepted intent is never discarded.
6. Recovery is idempotent. If the event row already exists with matching actor, source, root, predecessor and value, mark it recovered and do not insert another row. If no event row exists, apply the intent only when the recorded predecessor still matches; otherwise report a conflict for owner resolution. Re-running recovery after success changes no rows.
7. Replay reads legacy `corrections.json` and new event intents without rewriting either. Legacy rows are coalesced to each root's final state from the trial. Event rows replay in recorded predecessor order, matching events already present in a copied target by generated event key and actor/source, not only events replay created in the current run. Ambiguous mixed legacy/event ordering is refused fail-closed.

Failure matrix:

| Case | Detection point | Required safe outcome |
|---|---|---|
| Pre-insert refusal: invalid value, missing reading, unapproved/mismatched workspace source, non-imported or unkeyed root | Before event id publication and before SQL insert | Return a refusal; no intent file; no measurement row; no legacy `corrections.json` append. |
| Workspace coordination cannot be acquired without reversing lock order | Before `Store.Do` | Wait or fail without taking the DB write lock; never hold DB then workspace locks. |
| Intent directory blocked (for example `correction-intents` is a file), temporary write fails, file sync fails, rename/publish fails, or directory sync returns a hard error | Inside `Store.Do` after the SQL insert but before commit | Return the file error from the transaction callback; SQL rolls back; current leaf and row count stay unchanged; no partial ready intent is considered accepted. |
| SQL commit succeeds after the intent is ready | After `Store.Do` returns nil | Report success with the measurement entity; recovery sees matching event row and is a no-op. |
| SQL commit fails or returns ambiguous after the intent is ready | After publication, when `Store.Do` commits | Return pending recovery; leave ready intent in place; subsequent correction/rehearsal/replay runs recovery exactly once before doing other work. |
| Crash after intent ready and before/after SQL commit is observed | Next correction, rehearsal or replay | Recovery matches an existing event row by generated key and actor/source, or inserts it only if the recorded predecessor still matches; repeated recovery does not add rows. No untested power-loss durability beyond synced local-file publication is claimed. |
| Ready intent conflicts: predecessor no longer current, root/value mismatch, actor/source mismatch, copied target has different generated event row | Recovery or replay | Fail closed with a conflict visible to owner; do not overwrite unrelated later corrections and do not silently skip the intent. |
| Concurrent correction requests for one workspace | Workspace entry | Serialized by `Workspace.mu` before DB write lock; the second sees the first through recovery/current predecessor checks and either records the next event or reports conflict. |
| Rehearsal/replay with legacy and event correction records | Before applying corrections in rehearsal/replay | Legacy roots replay to their coalesced final state, then unambiguous event-to-event successors replay by generated event key; insufficient mixed ordering is a failure and leaves the target unchanged after rehearsal. |
| GET/status sees pending intents | Status read | Report pending/conflicting intents without recovery side effects. |

## Commands

- API: `go test -mod=readonly -count=1 ./internal/api -run 'TestCorrectionWorkspaceFailure|TestRepeatedImportedCorrectionsReplay|TestCorrectionDomainActions'`.
- Recovery: `go test -mod=readonly -count=1 ./internal/importer -run 'TestCorrectionRecovery|TestReplay|TestReadingsKeysAndReplay'`.
- Integration: `go test -mod=readonly -count=1 ./internal/core ./internal/importer ./internal/api`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./...`.

## Steps

1. Add `TestCorrectionWorkspaceFailure` using a synthetic workspace and a deterministic failing persistence seam (not OS permission bits, which behave differently on Windows). Assert that failure before durable intent leaves the measurement row count/current leaf unchanged. Add a test correcting another import's row with the wrong mounted workspace: refuse before either persistence domain changes.
   **Verify:** API command → current commit-before-file behavior fails the new no-change assertion.
2. Append a concise protocol/failure matrix to this plan and obtain owner/reviewer acceptance before coding it. Required design: immutable per-event workspace intent files with writer-generated identifiers, imported root identity, actor, predecessor identity, desired value/nil and ordering; no shared-list overwrite for new events. Insert the correction with its event key inside `Store.Do`, write/sync/publish its intent while the SQL row is still uncommitted, then commit SQL. A file failure rolls back SQL. A ready intent left by commit failure/crash is detectable and recoverable. Acquire workspace coordination before the DB write lock, matching existing workspace→database lock order. Never hold DB→workspace locks in the reverse order.
   **Verify:** `git diff -- docs/plans/041-durable-import-corrections.md` → matrix explicitly covers pre-insert refusal, file write/sync/publish failure, SQL commit failure, crash after commit, repeated recovery, conflicting predecessor, and concurrent requests. STOP for review if any row lacks a safe outcome.
3. Implement the accepted protocol in `importer/corrections.go` and a narrow core helper for keyed, idempotent correction insertion. Keep ordinary `Store.Correct` behavior unchanged without a workspace. Generated event keys are private to the writer; requests still supply only the existing id/value. Confirm root source matches the workspace before creating an event. Ready records must be either fully durable or absent; use portable sync/error handling and test the publication failure seam. A post-intent commit error must report pending recovery, not success or a false “nothing changed”. Do not silently discard an accepted intent.
   **Verify:** API command and integration command → write failure rolls back; source mismatch and invalid values create neither intent nor fact; normal calls still return the measurement entity.
4. Add idempotent trial recovery before further workspace corrections and before rehearsal/replay (never as a side effect of GET/status). Match an existing event row by its generated key, verify its root/value, and avoid reinsertion after an ambiguous commit outcome. If no event row exists, apply only when the recorded predecessor still matches; otherwise report a conflict for owner resolution. Status must report pending/conflicting intents. Preserve reading of legacy `corrections.json`; do not rewrite it automatically. Replay resolves portable roots, not trial row ids, and repeated replay adds zero rows. For legacy predecessors, use the root's coalesced expected final state from plan 037; a trial row id is only a local recovery check, never a required id in a rebuilt target. New event-to-event predecessors use generated event keys. Refuse insufficient or ambiguous mixed legacy/event ordering. Actor/source matching must also recognize events already present in a copied target, not only freshly replayed CLI events.
   **Verify:** recovery command → restart-style tests cover every matrix row, repeated recovery/replay, nil values, old JSON records, and target row counts. Pending failures prevent a misleading clean rehearsal.
5. Describe the recovery/error states in the application README and, language-neutrally, the import guide. Do not promise cross-file ACID or untested power-loss behavior. Document how the owner identifies and resolves a conflicting pending intent without publishing data.
   **Verify:** final command → exit 0.

## Done criteria

- [ ] Accepted protocol matrix and deterministic fault-injection tests cover both persistence domains.
- [ ] Pre-intent errors leave no correction; post-intent ambiguity is reported and recoverable exactly once.
- [ ] Recovery refuses conflicting predecessors; status is read-only and exposes pending state.
- [ ] Legacy records remain readable; replay uses portable roots and adds zero rows on repeat.
- [ ] No DDL/migration, provenance rewrite, real fixture or out-of-scope change; full suite passes; index is IN REVIEW (owner).

## STOP conditions

Stop before protocol implementation without the review gate. Stop if durable publication cannot be supported on a supported local filesystem, lock ordering cannot be maintained, event identity needs a schema change, legacy and new intent ordering is ambiguous, or conflict recovery would overwrite an unrelated later owner correction. Preserve evidence in synthetic tests; do not improvise data repair.

## Git workflow and maintenance

Use an isolated operator-selected branch/worktree. Commit protocol tests and implementation as reviewable logical units, with verification commands in messages, e.g. `imports: recover correction intent (plan 041)`. Push only on instruction. Any new correction/replay entry point must use this protocol; filesystem atomic rename alone is neither durability nor database atomicity.
