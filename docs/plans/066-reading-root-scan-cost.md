# 066 — Resolve imported reading roots once per metric and file

> Executor: characterize cost with synthetic data before optimizing. Preserve every legacy-key/proof refusal. No schema/index changes are authorized. Set plan/index IN REVIEW (owner) after gates.
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/core/imports.go internal/core/core_test.go internal/importer/reading_key_resolver.go internal/importer/reading_keys_test.go internal/importer/reading_key_proof_test.go internal/importer/reading_keys_bench_test.go`.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P2. **Effort:** M. **Risk:** MED (identity logic under a write transaction).
- **Status:** IN REVIEW (owner). **Depends on:** implemented 043; execute after 038/056 to stabilize evidence helpers.
- **Category:** perf. **Deep-audit finding:** 22. **Confidence:** HIGH for repeated scans; latency/scaling unmeasured.

## Why

Each metric/day group asks for all root readings of that metric/source, filters by file in Go, then filters by day again. A long source file repeats essentially the same scan once per day while holding a write transaction. Reduce redundant work without introducing stale caches or changing identity matching.

## Current state and conventions

`internal/importer/reading_key_resolver.go:31–40`:

```go
for _, group := range byGroup {
    roots, err := t.ImportedMeasurementRootsByFile(source, f.File, group[0].metricKey)
    // ... filter roots by group[0].day ...
}
```

`internal/core/imports.go:97–124` selects all source/metric roots, then applies `strings.HasPrefix(r.ImportKey, file+"|reading|")` in Go. `resolveReadingGroup` contains critical legacy/canonical ambiguity handling. Follow `TestReadingKeyIdentity`, `TestLegacyReadingKeyCompatibility` and `TestCorrectionProofBindsCheckedFacts`. [The import contract](../contract/imports.md) requires idempotence; performance is not permission to merge roots.

## Scope

Only paths in the drift command plus plan/index; create `reading_keys_bench_test.go`. No global/persistent cache, changed stored keys, weakened proof checks, new index/DDL/migration or real-import benchmark.

## Commands

- Correctness: `go test -mod=readonly -count=1 ./internal/core ./internal/importer`.
- Benchmark: `go test -mod=readonly ./internal/importer -run '^$' -bench BenchmarkResolveReadingKeys -benchmem -count=3`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and test plan

1. Add `BenchmarkResolveReadingKeys` for synthetic already-applied files spanning 10/100/1000 days, multiple metrics and unrelated same-source roots. Record baseline results outside tracked source. Add a deterministic regression for lookup count, using a narrow resolver-local injected loader or equivalent test seam, not a global production counter.
   **Verify:** correctness command passes; benchmark executes all sizes and the baseline count grows by metric/day groups. Do not claim a measured speedup yet.
2. Load roots once per distinct canonical metric within one `resolveReadingKeys` invocation/transaction, then bucket by day once. Keep `resolveReadingGroup` matching/order/ambiguity semantics intact. The cache must end with that call; a later call after a write must see new roots.
   **Verify:** correctness command → all identity/proof tests pass; deterministic count equals distinct metrics, independent of number of days.
3. If reducing SQL-returned rows is worthwhile, apply an exact file-prefix predicate safely in the query; avoid unescaped LIKE/GLOB patterns because filenames can contain wildcard characters. Retain a defensive exact-prefix check or equivalent. This is optional after batching, not justification for a schema change.
   **Verify:** correctness command → wildcard/unicode filename and similarly prefixed-file controls cannot cross-match; legacy replays still pass.
4. Rerun the same benchmark and report before/after per size plus allocation counts. Test repeat apply is a no-op and ambiguous roots still refuse without writes. Do not introduce wall-clock pass/fail thresholds.
   **Verify:** benchmark and final commands → exit 0; deterministic scan-count regression passes; report any remaining non-linear group matching separately.

## Done criteria

- [x] Root lookups are bounded by distinct metrics per resolver call, not metric/day groups.
- [x] Day bucketing is done once; no cross-call stale cache or key/proof behavior change.
- [x] Synthetic before/after benchmark evidence and all correctness gates are reported.
- [x] No schema/index changes; scope-only diff; index IN REVIEW (owner).

Implementation evidence (2026-10-05): per-call canonical-metric loading and one day-bucketing pass replace repeated scans without changing group matching. Deterministic tests prove one load per distinct metric and visibility of a later committed root on the next call. `go test -mod=readonly ./internal/importer -run '^$' -bench BenchmarkResolveReadingKeys -benchmem -count=3` ran before and after on Windows amd64, Ryzen AI 9 HX 370, with three metrics and equally many unrelated same-source roots.

| Days per metric | Before time/op (three-run range) | After time/op (three-run range) | Before bytes/op | After bytes/op |
|---|---|---|---|---|
| 10 | 1.73–2.35 ms | 0.298–0.376 ms | 445,928–446,313 | 131,271–131,353 |
| 100 | 125.7–127.5 ms | 2.63–2.76 ms | 30,877,677–30,897,043 | 1,212,446–1,214,132 |
| 1000 | 10.78–12.51 s | 19.77–25.96 ms | 2,696,602,568–2,696,752,488 | 11,404,163–11,408,402 |

These are synthetic measurements, not thresholds or real-import guarantees. Unrelated roots still scan once per metric; large same-day group matching remains unchanged and was not benchmarked. Identity/proof/idempotency gates and integrated generation/vet/uncached full tests passed.

## STOP conditions

Stop if profiling shows another component dominates; report it rather than broadening the optimization. Stop if batching requires weaker lineage checks, a persisted cache or a new index. Stop on unexplained drift or two failed gates.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `imports: batch reading root resolution (plan 066)`, listing checks/benchmarks. Future callers must keep any cached roots scoped to the same checked facts and transaction snapshot.
