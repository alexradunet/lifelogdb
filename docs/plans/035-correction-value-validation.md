# 035 — Enforce mood and habit domains on corrections

> Executor: follow the steps and gates from the repository root. Set the index row to IN REVIEW (owner) when finished. Stop rather than changing the storage contract.
>
> Drift check: `git diff --stat 726ffab..HEAD -- internal/core/write.go internal/core/core_test.go internal/core/habits_test.go internal/api/api_test.go`. Re-read the excerpts on any change; unexplained drift is a STOP condition.

## Status

- **Date / planned at:** 2026-10-05, commit `726ffab`.
- **Priority:** P1. **Effort:** S. **Risk:** LOW (sharing existing validation).
- **Status:** IN REVIEW (owner). **Depends on:** none. **Category:** bug. **Audit finding:** 2.

## Why

An ordinary mood reading must be an integer from 1 through 5; a metric with habit periods accepts only 0 or 1. The correction path bypasses both checks, so a valid reading can become an invalid current value and distort day views or completion totals. Retraction must still be possible, and corrections must remain append-only.

## Current state and conventions

`internal/core/write.go:519–524`, in `Tx.Record`:

```go
if text.TitleKey(m.Metric) == "mood" && !isMood(m.Value) {
    return 0, invalid("mood is 1-5")
}
if habit && m.Value != 0 && m.Value != 1 {
    return 0, invalid("%s is a habit: 1 = done, 0 = not done (D24)", m.Metric)
}
```

The `habit` query tests for any `habit_periods` row, not only a period containing the reading's day. `Tx.Correct` at line 562 checks only NaN/Infinity before inserting a row whose `supersedes_id` is the old row. It copies metric, day, instant, zone and capture provenance.

Use `fresh`, `ptr`, `status`, and `TestMeasurementsAreAppendOnly` in `internal/core/core_test.go`; `TestHabits` in `internal/core/habits_test.go` demonstrates registration, periods and completion. `Store.Do` supplies `BEGIN IMMEDIATE`; `invalid` returns 422. Honor [D6](../decisions/D06-mood-is-a-measurement.md), [D7](../decisions/D07-measurements.md) and [D24](../decisions/D24-habits.md). No new domain rule is proposed.

## Scope

Only `internal/core/write.go`, `internal/core/core_test.go`, `internal/core/habits_test.go`, `internal/api/api_test.go`, and this plan/index status. No DDL, migrations, new metric bounds, changed habit-period semantics, or changes to imported identities. Synthetic temporary databases only.

## Commands

- Targeted: `go test -mod=readonly -count=1 ./internal/core -run 'TestCorrectionDomains|TestHabits|TestMeasurementsAreAppendOnly'`.
- API: `go test -mod=readonly -count=1 ./internal/api`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./...`.

## Steps

1. Add `TestCorrectionDomains`. Assert out-of-range/fractional mood and non-binary habit corrections return 422 and add zero rows. Test both a current habit and one whose period ended. Include arbitrary finite non-habit measurements, valid boundary values, nil retraction, unknown id, and correction of a correction.
   **Verify:** targeted command → new invalid-value cases fail because the current implementation accepts them; existing tests pass.
2. Resolve the corrected row's metric by id, then apply the same finite/mood/habit validation as `Record` before insertion. Prefer a small shared validator rather than duplicated predicates. Use the immutable metric id/title and existing period-existence definition; do not trust request-supplied metric names. A nil value bypasses numeric-domain checks but not target existence. Preserve not-found and already-superseded behavior.
   **Verify:** targeted command → all pass, with rejected corrections leaving both total row count and current value unchanged.
3. Add `TestCorrectionDomainActions` to the API suite using `fresh`, catalog actions and `client.Error`. Cover mood, a registered habit and retract, asserting 422 versus success through the actual routes.
   **Verify:** API command, then final command → exit 0.

## Test plan and done criteria

- [ ] New core tests exercise valid/invalid values and every rejection checks absence of a new measurement row.
- [ ] Retraction and a valid second correction still work; metric/day/time/capture fields stay copied correctly.
- [ ] Non-habit unitless metrics are not accidentally restricted to binary values.
- [ ] API regression tests pass; final command exits 0.
- [ ] Only scope paths changed; index status is IN REVIEW (owner).

## STOP conditions

Stop if reproducing the problem reveals a different documented domain, if resolving a metric would require mutable request data, if the proposed helper changes the existing treatment of ended habit periods, or if a DDL constraint is proposed. Report unrelated baseline failures; do not loosen tests to pass.

## Git workflow and maintenance

Use an operator-selected branch/worktree and one logical commit, e.g. `measurements: validate corrected values (plan 035); ran go generate, go vet and go test`. Push only on separate instruction. Any future measurement write path must call the same validator; nil is a retraction, never an ordinary initial reading.
