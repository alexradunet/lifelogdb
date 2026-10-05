# 060 — Resolve metric actions with canonical title identity

> Executor: use the existing title-key function, not a new normalization rule. Synthetic metrics only; set plan/index IN REVIEW (owner) after gates.
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/api/categories.go internal/api/habits.go internal/api/api_test.go`.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P2. **Effort:** S. **Risk:** LOW (lookup consistency).
- **Status:** IN REVIEW (owner). **Depends on:** none; serialize API test edits with 059.
- **Category:** bug. **Deep-audit finding:** 14. **Confidence:** HIGH, source-confirmed; new Unicode cases not run.

## Why

Metric readings are fetched by canonical `title_key`, but action discovery compares names with `strings.EqualFold`. Canonically equivalent Unicode names can therefore show readings while losing their page link, filing actions or habit actions. Every part of one response must identify the same metric.

## Current state and conventions

`internal/api/categories.go:64` in `metricOf` and `internal/api/habits.go:175` in `unitless` both use:

```go
if strings.EqualFold(m.Name, name) {
    return m.Unit == ""
}
```

The precise loop receiver differs, but both lack NFC/full casefold semantics. `internal/text/text.go` already defines `TitleKey`; `core.metricID` and series SQL use it. Follow `TestViewsOfferTheMetricsInUse`, `TestMetricsAreGroupedByCategory` and existing action-name assertions. [The title contract](../contract/titles-and-wikilinks.md) is the sole normalization specification.

## Scope

Only `internal/api/categories.go`, `internal/api/habits.go`, `internal/api/api_test.go`, plan/index. No new aliases, title mutation, schema changes, normalization library or generic metric-query optimization.

## Commands

- Focus: `go test -mod=readonly -count=1 ./internal/api -run 'TestMetricCanonicalActions|TestViewsOfferTheMetricsInUse|TestMetricsAreGroupedByCategory'`.
- Package: `go test -mod=readonly -count=1 ./internal/api ./internal/text`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and tests

1. Add `TestMetricCanonicalActions` with a composed/decomposed accented metric and a full-casefold expansion such as a synthetic name containing sharp s. Request each stored and equivalent spelling. Compare page/self links and action sets for unitless, unitful, habit, unknown and tombstoned metrics.
   **Verify:** focus command → baseline equivalent-spelling actions differ or disappear.
2. Compare `text.TitleKey` values in `metricOf`; reuse that canonical helper for `unitless` rather than maintaining a second mismatching lookup. Keep the existing live metric list and stored display spelling. Do not infer unitlessness when lookup fails.
   **Verify:** focus command → equivalent spellings have identical action availability and resolve the same page; unknown/deleted controls offer no illegal action.
3. Exercise an offered action using its returned href, not a handcrafted stored spelling. Retain normal ASCII case behavior and unitful-metric restrictions.
   **Verify:** package and final commands → exit 0; `rg -n 'EqualFold' internal/api/categories.go internal/api/habits.go` has no remaining metric-identity comparison.

## Done criteria

- [x] Retrieval and action discovery agree for NFC/NFD and full casefold equivalents.
- [x] Action hrefs work, stored names remain unchanged, and absent/deleted/unitful controls stay restricted.
- [x] No replacement normalization rule; all gates pass; index IN REVIEW (owner).

Implementation evidence (2026-10-05): action lookup uses existing TitleKey identity and requires a found metric before treating it as unitless. Unicode/casefold, habit, missing/deleted/unitful and executable-action controls pass; self links retain requested spelling but resolve the same actions. Focus/package and integrated generation/vet/uncached full tests passed.

## STOP conditions

Stop if a lookup needs new alias semantics or changing `TitleKey`. Do not change metric identity to match the old UI comparison. Stop on unexplained drift, out-of-scope changes or two failed gates.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `api: use canonical identity for metric actions (plan 060)`, listing checks. Future UI lookups must use the same identity function as persistence.
