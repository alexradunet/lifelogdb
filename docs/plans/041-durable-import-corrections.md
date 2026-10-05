# 041 — Make database corrections and replay intent recoverable

> Executor: this is HIGH-risk coordination work. Complete the protocol/failure-matrix gate before implementation, stop at any unresolved recovery decision, and run all commands from the repository root. Set the index row to IN REVIEW (owner) only after the fault-injection tests pass.
>
> Drift check: `git diff --stat 726ffab..HEAD -- internal/api/habits.go internal/api/replay_test.go internal/core/write.go internal/core/imports.go internal/core/core_test.go internal/importer/files.go internal/importer/workspace.go internal/importer/corrections.go internal/importer/corrections_test.go internal/importer/replay.go internal/importer/status.go internal/importer/replay_test.go docs/guides/importing.md README.md`. Changes from plans 035/037 are expected: verify their tests and rebaseline their interfaces first.

## Status

- **Date / planned at:** 2026-10-05, commit `726ffab`.
- **Priority:** P1. **Effort:** L (including fault injection and compatibility). **Risk:** HIGH (two persistence domains).
- **Status:** IN REVIEW (owner). **Depends on:** [035](035-correction-value-validation.md), [037](037-import-correction-lineage.md). **Category:** bug. **Audit finding:** 8.

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

1. A correction request that reaches an import workspace first acquires `Workspace.mu`, before any database write transaction. It preflights the new request's value and imported-root workspace source before recovery, so a wrong-workspace or invalid-value request creates no new writes. While holding workspace coordination, recover earlier ready correction intents in the workspace against the trial database before accepting a valid new request; those earlier accepted intents may be recovered even if the new request later fails. If recovery reports a conflict or pending ambiguity, the new correction is refused until the owner resolves it. `status` remains read-only and reports those states; rehearsal and replay also run recovery first.
2. The workspace source from approved `rules.md` must equal the imported root's `source` before an intent is created. A missing approved source or source mismatch for a keyed imported root is refused before writing either persistence domain. Ordinary corrections of non-imported or unkeyed rows keep existing core semantics when no replay intent is needed; they do not create workspace records.
3. The writer builds an immutable generated correction event id and intent from data already verified in the open SQL transaction. Intent fields, in canonical JSON order for hashing, are: `version`, `event_key`, `workspace_source`, `root_source`, `root_import_key`, `metric`, `actor_source`, `predecessor_kind` (`legacy` or `event`), `predecessor_event_key` when the predecessor is an event, `predecessor_local_id` as a trial-only recovery check, `predecessor_value`/`predecessor_retracted`, `value`/`retracted`, and `created_at`. The event key is private writer provenance, stored as the correction row's `import_key`; no request supplies it.
4. Legacy-to-event ordering is frozen at the first event for a root. Before making that event, read `corrections.json` and compute the legacy baseline as the coalesced expected state for that root from legacy entries only. Persist deterministic immutable prefix evidence for that root (including the empty-prefix case), not only the final numeric value. First events with no legacy entries anchor to the original imported root state. Later legacy-log edits/additions for a root that already has event intents, even when they leave the final value unchanged, or an event whose predecessor cannot be placed after the frozen legacy prefix, are insufficient mixed ordering and are refused; do not derive a legacy baseline from the trial's newest event value.
5. Inside one `Store.Do`, insert the correction row with the generated event key but keep SQL uncommitted. While SQL is still uncommitted, write a temp intent, sync the file, and rename it to the ready name under `correction-intents/`. Failures before the ready name is visible (directory blocked, temp write, file sync, close, or rename) return from the transaction callback so SQL rolls back and no accepted intent exists.
6. After the ready name is visible, errors are no longer “all absent”. A directory sync failure, or a close/sync error discovered after rename, leaves a visible pending intent and returns a pending-recovery error while SQL rolls back. Only a known unsupported directory-sync capability gets limited handling: record that directory sync was unsupported and claim process-crash recovery from the visible ready file plus synced file contents; ordinary file-sync errors and hard directory-sync errors on supported platforms are failures/pending exactly as the matrix says. Do not claim untested power-loss safety. Recovery must re-establish required sync metadata where supported before it commits SQL.
7. Commit SQL only after the ready intent is published and post-rename sync handling has not reported pending ambiguity. If commit reports failure after publication, return pending recovery, not success and not “nothing changed”. The accepted intent is never discarded.
8. Recovery is idempotent. If the event row already exists with matching actor, source, root, predecessor and value, mark it recovered and do not insert another row. If no event row exists, apply the intent only when the recorded predecessor still matches: for a legacy predecessor, the root's current state must match the frozen legacy baseline; for an event predecessor, the current leaf must be that generated event key. Otherwise report a conflict for owner resolution. Re-running recovery after success changes no rows.
9. Replay reads legacy `corrections.json` and new event intents without rewriting either. A fresh target first replays legacy entries to each root's frozen/coalesced baseline, then applies event successors by generated event key. A copied/repeated target first checks whether event rows are already present; matching existing events must not trigger a legacy replay that rewinds their chain. Match existing events by generated event key plus topology, actor/source, root, predecessor and value, and advance without reinsertion. If the copied target has an unrelated later owner correction or a different row at an event key/predecessor, fail closed even if the numeric value matches. Actor/source matching recognizes both freshly replayed and already-present events; trial-local ids are only a trial recovery check, while fresh/rebuilt targets use portable evidence.

Failure matrix:

| Case | Detection point | Required safe outcome |
|---|---|---|
| Pre-insert refusal: invalid value, missing reading, unapproved/mismatched workspace source for a keyed imported root | Before event id publication and before SQL insert | Return a refusal; no new recovery for this request, no intent file, no measurement row, no legacy `corrections.json` append. |
| Ordinary non-imported or unkeyed correction with a mounted workspace | Request classification | Use existing core correction path; no replay intent is created and no workspace-source check is invented for rows that cannot replay. |
| Earlier ready intent exists, then a later valid new request enters | Workspace entry before new `Store.Do` | Recover earlier accepted pending work first; if it recovers, the new request proceeds against that current state. If it conflicts, the new request is refused without creating its own intent. |
| Workspace coordination cannot be acquired without reversing lock order | Before `Store.Do` | Wait or fail without taking the DB write lock; never hold DB then workspace locks. |
| Intent directory blocked (for example `correction-intents` is a file), temporary write fails, file sync/close fails before rename, or rename to ready name fails | Inside `Store.Do` after the SQL insert but before commit | Return the file error from the transaction callback; SQL rolls back; current leaf and row count stay unchanged; no accepted ready intent exists. |
| Ready-name visible, then directory sync unsupported | After rename before SQL commit | Keep the ready intent; record/report that directory sync is unsupported; proceed only with the limited process-crash recovery claim based on visible ready file plus synced file contents. |
| Ready-name visible, then directory sync or post-rename close/sync fails with a hard error | After rename before SQL commit | Return pending recovery and roll back SQL; keep the ready intent; recovery retries/re-establishes required sync metadata where supported before committing SQL. A post-rename fault seam must cover this separately from pre-rename failures. |
| SQL commit succeeds after the intent is ready | After `Store.Do` returns nil | Report success with the measurement entity; recovery sees matching event row and is a no-op. |
| SQL commit fails or returns ambiguous after the intent is ready | After publication, when `Store.Do` commits | Return pending recovery; leave ready intent in place; subsequent correction/rehearsal/replay runs recovery exactly once before doing other work. |
| Process crash after intent ready and before/after SQL commit is observed | Next correction, rehearsal or replay | Recovery matches an existing event row by generated key and actor/source, or inserts it only if the recorded predecessor still matches; repeated recovery does not add rows. This is a process-crash recovery claim, not an untested power-loss claim. |
| First event after legacy corrections | Event creation and replay | Freeze deterministic per-root legacy prefix evidence from `corrections.json` entries only (including empty prefix), record it in the first event, replay fresh targets to that baseline before the event, and reject later legacy-log edits/additions or missing baseline evidence even if the final value is unchanged. |
| Ready intent conflicts: predecessor no longer current, root/value mismatch, actor/source mismatch, copied target has different generated event row, or copied target has unrelated later owner correction | Recovery or replay | Fail closed with a conflict visible to owner; do not overwrite unrelated later corrections and do not silently skip the intent. |
| Fresh target replay | Before applying event intents | Rebuild facts, apply legacy coalesced baselines, then event successors in predecessor order by generated event key; repeated replay adds zero rows. |
| Copied target replay/rehearsal | Before applying event intents | Recognize already-present events by generated event key plus actor/source/root/predecessor/value; if present and matching, advance predecessor without reinsertion and without replaying legacy rows that would rewind the chain. Reject unrelated later owner leaves even when their numeric value matches. |
| Concurrent correction requests for one workspace | Workspace entry | Serialized by `Workspace.mu` before DB write lock; the second sees the first through recovery/current predecessor checks and either records the next event or reports conflict. |
| GET/status sees pending intents | Status read | Report pending/conflicting intents without recovery side effects. |

## Commands

- API: `go test -mod=readonly -count=1 ./internal/api -run 'TestCorrectionWorkspaceFailure|TestRepeatedImportedCorrectionsReplay|TestCorrectionDomainActions'`.
- Recovery: `go test -mod=readonly -count=1 ./internal/importer -run 'TestCorrectionRecovery|TestReplay|TestReadingsKeysAndReplay'`.
- Integration: `go test -mod=readonly -count=1 ./internal/core ./internal/importer ./internal/api`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./...`.

## Steps

1. Add `TestCorrectionWorkspaceFailure` using a synthetic workspace and a deterministic failing persistence seam (not OS permission bits, which behave differently on Windows). Assert that failure before durable intent leaves the measurement row count/current leaf unchanged. Add a test correcting another import's row with the wrong mounted workspace: refuse before either persistence domain changes.
   **Verify:** API command → current commit-before-file behavior fails the new no-change assertion.
2. Append a concise protocol/failure matrix to this plan and obtain owner/reviewer acceptance before coding it. Required design: immutable per-event workspace intent files with writer-generated identifiers, imported root identity, actor, predecessor identity, desired value/nil and ordering; no shared-list overwrite for new events. Insert the correction with its event key inside `Store.Do`, write/sync/publish its intent while the SQL row is still uncommitted, then commit SQL. A pre-rename file failure rolls back SQL with no accepted intent; a post-rename failure or commit failure leaves a pending intent for recovery. Acquire workspace coordination before the DB write lock, matching existing workspace→database lock order. Never hold DB→workspace locks in the reverse order.
   **Verify:** `git diff -- docs/plans/041-durable-import-corrections.md` → matrix explicitly covers pre-insert refusal, file write/sync/publish failure, SQL commit failure, crash after commit, repeated recovery, conflicting predecessor, and concurrent requests. STOP for review if any row lacks a safe outcome.
3. Implement the accepted protocol in `importer/corrections.go` and a narrow core helper for keyed, idempotent correction insertion. Keep ordinary `Store.Correct` behavior unchanged without a workspace or for rows that need no replay intent. Generated event keys are private to the writer; requests still supply only the existing id/value. Confirm keyed imported root source matches the workspace before creating an event. Ready records visible after rename are accepted pending work if a post-rename sync/commit error occurs; use portable sync/error handling and test both pre-rename and post-rename publication seams. A post-intent commit error must report pending recovery, not success or a false “nothing changed”. Do not silently discard an accepted intent.
   **Verify:** API command and integration command → write failure rolls back; source mismatch and invalid values create neither intent nor fact; normal calls still return the measurement entity.
4. Add idempotent trial recovery before further workspace corrections and before rehearsal/replay (never as a side effect of GET/status). Match an existing event row by its generated key, verify its root/value, and avoid reinsertion after an ambiguous commit outcome. If no event row exists, apply only when the recorded predecessor still matches; otherwise report a conflict for owner resolution. Status must report pending/conflicting intents. Preserve reading of legacy `corrections.json`; do not rewrite it automatically. Replay resolves portable roots, not trial row ids, and repeated replay adds zero rows. For legacy predecessors, use the root's coalesced expected final state from plan 037; a trial row id is only a local recovery check, never a required id in a rebuilt target. New event-to-event predecessors use generated event keys. Refuse insufficient or ambiguous mixed legacy/event ordering. Actor/source matching must also recognize events already present in a copied target, not only freshly replayed CLI events.
   **Verify:** recovery command → restart-style tests cover every matrix row, repeated recovery/replay, nil values, old JSON records, and target row counts. Pending failures prevent a misleading clean rehearsal.
5. Describe the recovery/error states in the application README and, language-neutrally, the import guide. Do not promise cross-file ACID or untested power-loss behavior. Document how the owner identifies and resolves a conflicting pending intent without publishing data.
   **Verify:** final command → exit 0.

## Done criteria

- [x] Accepted protocol matrix and deterministic fault-injection tests cover both persistence domains.
- [x] Pre-intent errors leave no correction; post-intent ambiguity is reported and recoverable exactly once.
- [x] Recovery refuses conflicting predecessors; status is read-only and exposes pending state.
- [x] Legacy records remain readable; replay uses portable roots and adds zero rows on repeat.
- [x] No DDL/migration, provenance rewrite, real fixture or out-of-scope change; full suite passes; index is IN REVIEW (owner).

## STOP conditions

Stop before protocol implementation without the review gate. Stop if durable publication cannot be supported on a supported local filesystem, lock ordering cannot be maintained, event identity needs a schema change, legacy and new intent ordering is ambiguous, or conflict recovery would overwrite an unrelated later owner correction. Preserve evidence in synthetic tests; do not improvise data repair.

## Git workflow and maintenance

Use an isolated operator-selected branch/worktree. Commit protocol tests and implementation as reviewable logical units, with verification commands in messages, e.g. `imports: recover correction intent (plan 041)`. Push only on instruction. Any new correction/replay entry point must use this protocol; filesystem atomic rename alone is neither durability nor database atomicity.
