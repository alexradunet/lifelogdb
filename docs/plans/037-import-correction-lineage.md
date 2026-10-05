# 037 — Carry repeated imported corrections through replay

> Executor: run from the repository root, honor the append-only contract, and run every gate. Set the index row to IN REVIEW (owner) when finished.
>
> Drift check: `git diff --stat 726ffab..HEAD -- internal/core/write.go internal/core/imports.go internal/core/core_test.go internal/api/habits.go internal/api/replay_test.go internal/importer/replay.go internal/importer/files.go internal/importer/importer_test.go internal/importer/replay_test.go`. Plan 035 changes to shared validation are expected; rebaseline their excerpts/tests before editing. Stop on other unexplained drift.

## Status

- **Date / planned at:** 2026-10-05, commit `726ffab`.
- **Priority:** P1. **Effort:** M. **Risk:** MED (correction history and replay idempotency).
- **Status:** IN REVIEW (owner). **Depends on:** [035](035-correction-value-validation.md). **Category:** bug. **Audit finding:** 4.

## Why

The first imported row has a sender's key. A correction is written by `cli`, `ui` or an agent and normally has no import key. Correcting that correction therefore loses the imported root identity, and the API does not record the later value for replay. Root identity must remain discoverable without changing the provenance of any stored row.

## Current state and conventions

`internal/core/write.go:580` reads identity only from the requested row:

```go
err = t.tx.QueryRow(`SELECT me.source, me.import_key, m.title FROM measurements me JOIN pages m ON m.id = me.metric_id
                      WHERE me.id = ?`, id).Scan(&source, &k, &metric)
```

`Store.Correct` calls this before `Tx.Correct`. `internal/api/habits.go:20` records only keys with an `import:` source and a nonempty key. `RecordCorrection` appends to `corrections.json`. `replayCorrections` visits every record in order, compares each value with the current leaf, and may append corrections repeatedly when one root has several recorded values.

`CurrentOf` in `internal/core/imports.go:84` demonstrates a recursive CTE following children. For identity, follow `supersedes_id` in the opposite direction to the original row. [D7](../decisions/D07-measurements.md) requires an append-only chain; the measurement DDL fixes provenance and prevents branching/cycles. The [import guide](../guides/importing.md) requires owner corrections to survive replay. Use `TestReadingsKeysAndReplay` and synthetic API setup from `TestReplayDryRunAction`.

## Scope

Only the drift-check paths and plan/index status. No DDL, migrations, rewriting measurement source/import_key, new client fields, file durability protocol, or changed reading-key format. Durability is plan 041; key compatibility is plan 043. Synthetic fixtures only.

## Commands

- Core: `go test -mod=readonly -count=1 ./internal/core -run 'TestCorrectionRootIdentity|TestCorrectionDomains|TestMeasurementsAreAppendOnly'`.
- Import/API: `go test -mod=readonly -count=1 ./internal/importer ./internal/api`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./...`.

## Steps

1. Add `TestCorrectionRootIdentity`: import a keyed reading, correct it twice and retract the newest row. Each `Store.Correct` result must identify the same original source/key/metric, while inserted rows keep their actual writer's source. Include an unkeyed root, nonexistent id, and an intermediate row that itself has a key.
   **Verify:** core command → repeated-correction/root assertions fail on the current row-only lookup.
2. Add a narrowly named root-identity helper or correct `MeasurementKey`'s documented meaning. Resolve the oldest ancestor, not the first keyed intermediate row. Keep the selected row as `supersedes_id`; discovering the root must not redirect which row is corrected. Preserve 404 and existing constraint failures.
   **Verify:** core command → all pass, including plan 035 validation.
3. Add `TestRepeatedImportedCorrectionsReplay` through the actual API with a workspace. Correct, correct again, retract, then restore by correcting the retraction. All owner intents must be recorded under the same root. In replay, group records by source/metric identity/root key and use the latest requested state once per root. Preserve the original workspace log; do not rewrite or erase trial history. A second replay must append zero measurement rows, not merely keep equal current-reading counts. Support existing JSON records and nil values.
   **Verify:** import/API command → all pass; test asserts final values, retraction behavior, zero total-row growth on replay two, and an unchanged target on a missing-root rehearsal failure.
4. Keep workspace filesystem error handling unchanged for this plan. Add comments citing the import guide rather than restating database rules.
   **Verify:** final command → exit 0; `git diff -- docs/schema/schema.sql internal/db/schema.sql` is empty.

## Done criteria

- [ ] Every descendant resolves the original imported identity without mutating provenance.
- [ ] Correcting an already-superseded row is still refused; the helper does not bypass the chain contract.
- [ ] Repeated corrections and retractions survive replay; replay two adds no measurement rows.
- [ ] Legacy `corrections.json` stays readable; actual trial correction history is retained.
- [ ] Full verification passes; only scope paths changed; index is IN REVIEW (owner).

## STOP conditions

Stop if ancestry can be ambiguous/cyclic under canonical DDL, if replay is required to preserve every intermediate trial correction as a separately keyed canonical event (this plan carries the final owner state), or if root resolution appears to require updating stored facts. Coordinate any later event-journal design with plan 041; do not invent it here.

## Git workflow and maintenance

Use an operator-selected branch/worktree and commit as `imports: retain correction root identity (plan 037); ran go generate, go vet and go test`. Push only when instructed. Review replay idempotency using total row counts: current-value counts alone hide repeated supersessions. Future key changes must also resolve legacy correction references.
