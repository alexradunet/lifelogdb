# 038 — Check complete reading values and source units

> Executor: this is a follow-up to the implemented numeric-evidence check, not permission to replace it. Work from the repository root with synthetic sources only. Run every gate; finish by setting this plan and its index row to IN REVIEW (owner).
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/importer/facts.go internal/importer/source_evidence.go internal/importer/source_evidence_test.go docs/guides/importing.md`. Compare changed symbols with the excerpts below. Rebaseline reviewed prerequisite changes; stop on unexplained drift.

## Status

- **Planned at:** `0460379`, 2026-10-05; replaces the implementation brief based on `726ffab`.
- **Priority:** P1. **Effort:** M. **Risk:** MED (supported quote and unit layouts).
- **Status:** IN REVIEW (owner). **Depends on:** none.
- **Category:** bug. **Deep-audit finding:** 2. **Confidence:** HIGH, source-confirmed; new regressions not yet executed.

## Why

The implemented helper still tokenizes a source `.5` as `5`. Its omitted-unit branch examines only the submitted quote, allowing a quote cropped before an explicit source unit to conceal that unit. A model can therefore submit a different measurement while passing an evidence check intended to prohibit conversion or inference.

## Current state and conventions

`internal/importer/source_evidence.go:37` uses the quote, not its source context:

```go
if unit == "" {
    if got := inlineUnitCandidate(quote, 0, matches); got != "" {
        return refuse("the source evidence has unit %q at value %s, but the facts omit the unit", got, numText)
    }
```

`canStartNumber` at line 145 blocks a digit after a decimal point only when another digit precedes that point. `numberTokens` starts on a digit or a sign followed by a digit. Thus leading-decimal punctuation is lost. `facts.go:256` deliberately accepts only digit-leading submitted values; this plan need not widen that grammar to recognize unsafe source fragments.

`checkReadingSourceEvidence` already binds quote offsets to source tokens and handles explicit Markdown/CSV table headers. Preserve those protections. Model tests after `TestNumericSourceEvidence` in `source_evidence_test.go`, using its synthetic fixtures and rejection-without-writes assertions. The [import guide](../guides/importing.md) and [import contract](../contract/imports.md) remain the authority; `wholeIndex` also matches names and must not be globally tightened.

## Scope

Only `internal/importer/facts.go`, `internal/importer/source_evidence.go`, `internal/importer/source_evidence_test.go`, `docs/guides/importing.md` if existing examples need clarification, and this plan/index status. No DDL, migration, facts fields, key changes, source rewriting, unit conversion or arbitrary prose inference. Preserve canonical-key compatibility from plan 043.

## Commands

- Focus: `go test -mod=readonly -count=1 ./internal/importer -run 'TestNumericSourceEvidence|TestReadingKeyIdentity|TestLegacyReadingKeyCompatibility|TestCorrectionProofBindsCheckedFacts'`.
- Package: `go test -mod=readonly -count=1 ./internal/importer`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and tests

1. Extend `TestNumericSourceEvidence` with source `.5`, `-.5`, `+.5` and comma-leading fractions submitted as integer fragments. Include a unitless metric with a quote ending at a number whose source continues with an explicit unit; cover unitless 0/1 markers too. Assert check/apply refuse and leave readings, ledger and correction files unchanged.
   **Verify:** focus command → the new unsafe-acceptance assertions fail on the baseline, existing compatibility cases pass.
2. Recognize the whole source quantity, including leading fractional punctuation/signs, even when the submitted-value grammar cannot express it. Refuse its integer fragment rather than silently dropping punctuation. Do not accept a reformatted value as “exactly as written.” Keep date/list punctuation, exponent, censoring and approximate-value cases explicit in the test table.
   **Verify:** focus command → numeric-fragment cases pass; supported signed/decimal values, names and aliases remain valid.
3. Bind omitted-unit detection to the matched source value, not merely quote bounds. Preserve explicit table-cell/header association. Reject an explicit incompatible unit outside the cropped quote without treating arbitrary following prose as a unit. Include duplicate numbers in different rows, cropped quotes, date-only marker context, unitless result words, inline units and separated/header units.
   **Verify:** package command → all pass; source-unit conflicts fail before writes and legitimate unitless imports remain accepted.
4. If clarification is necessary, update examples in the language-neutral guide, not a second numeric specification. Keep original key derivation and approved result-word behavior.
   **Verify:** final command → exit 0; inspect `git diff --name-only` for scope only.

## Done criteria

- [x] Leading-decimal/sign fragments and cropped-away explicit units are rejected with no persistence changes.
- [x] Exact supported values, separated/header units and approved result words still pass.
- [x] `wholeIndex`, reading-key formats, stored facts and source filenames are unchanged.
- [x] Focus, package and final commands pass; index is IN REVIEW (owner).

Implementation evidence (2026-10-05): leading fractional token boundaries and matched-source unit checks have red/green synthetic regressions. Plan focus/package gates and integrated generation/vet/uncached full tests passed. Outside-quote detection deliberately recognizes registered units, unmistakable symbols and a narrow conventional unit vocabulary, not arbitrary prose; unknown unregistered alphabetic units in free prose remain a limitation.

## STOP conditions

Stop if distinguishing unit evidence requires a new facts field, a semantic guess, a changed quote policy or a key-format change. Stop on unexplained drift, out-of-scope changes, or a gate failing twice after reasonable correction. Do not “fix” rejection by restoring quote-only checks.

## Git workflow and maintenance

Use an operator-selected branch/worktree; do not push or commit unless instructed. If committed, use `imports: close numeric evidence gaps (plan 038)` and list checks run. Future numeric layouts must test both full and adversarially cropped quotes; lexical rejection and table association are separate responsibilities.
