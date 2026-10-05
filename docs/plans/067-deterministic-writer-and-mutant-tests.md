# 067 — Make writer ordering deterministic and require relevant mutant witnesses

> Executor: this is validation repair, not permission to relax a contract assertion. Use synthetic databases; do not change canonical SQL to make tests pass. Set plan/index IN REVIEW (owner) after repeated gates.
>
> Drift check: `git diff --stat 0460379..HEAD -- tests/writers_test.go tests/mutants_test.go tests/kit_test.go tests/suites_test.go tests/README.md`.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P2. **Effort:** M/L (explicit witnesses across the mutant registry).
- **Risk:** MED (test harness must remain able to detect broken rules).
- **Status:** IN REVIEW (owner). **Depends on:** none; serialize mutant-registry changes with 047/055/059.
- **Category:** tests. **Deep-audit finding:** 23. **Confidence:** HIGH for timing/crediting patterns; flake frequency unmeasured.

## Why

The writer suite schedules transactions with sleeps and asserts narrow elapsed-time windows. Under load these can fail without any contract bug. The mutant runner then credits almost any failed expectation as a successful kill, allowing unrelated scheduling failures to masquerade as proof that a changed rule was noticed.

## Current state and conventions

`tests/writers_test.go:78–105` sleeps 100/400 ms to order writers and requires the second wait exceed 250 ms. Line 164 requires an operation to finish under 500 ms. `tests/mutants_test.go:305–312` bases detection on `s.n-s.ok`, without tying failure to the mutated rule. `S.K` in `tests/kit_test.go` already receives an expectation label before appending formatted diagnostics.

`TestSuites` runs subject suites; `TestMutants` first runs clean baselines. Keep that structure and all existing [connection claims](../contract/connections.md). Follow [tests/README](../../tests/README.md); do not add a flaky “retry until green” mechanism.

## Scope

Only paths in the drift command plus plan/index. No application changes, DDL/contract weakening, benchmark threshold, removed mutants or disabled concurrency. Stable failure labels/IDs may be added to the harness without changing rule meaning.

## Commands

- Writers: `go test -mod=readonly -count=20 ./tests -run 'TestSuites/writers$'`.
- Mutants: `go test -mod=readonly -count=3 ./tests -run TestMutants`.
- Harness: `go test -mod=readonly -count=1 ./tests -run 'TestMutantWitness|TestSuites/writers$'`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and test plan

1. Replace sleep-based setup with explicit connection/transaction barriers. Prove a second writer cannot begin while the first holds the lock using a deterministic busy/locked probe with zero busy timeout; after an explicit commit signal, prove it begins and sees the one existing page. If also testing automatic waiting, use an observable driver hook, not a guessed delay. Replace the connected-reader sub-500-ms assertion with successful progress under a generous deadlock guard and known transaction states.
   **Verify:** writers command → all 20 runs pass; correctness depends on lock results/rows, not elapsed speed. Update assertion wording to describe exactly the executed evidence.
2. Retain structured failed expectation identities separately from free-form details. Give each mutant at least one explicit expected assertion witness tied to its rule; do not derive a witness merely from whichever test happened to fail. Preserve baseline checks and require normal suite completion, not a stopped suite, for a valid kill.
   **Verify:** mutants command → every existing mutant has an intended witness and all three runs detect them. A missing witness is an error, never automatic credit.
3. Add `TestMutantWitness` using synthetic harness results: unrelated failure only is not a kill, intended assertion failure is, and a stopped suite cannot receive credit. Include an unchanged/no-op mutation control. Keep totals truthful; document the stronger criterion in `tests/README.md`.
   **Verify:** harness command → all pass, including rejected false-credit controls.
4. Review each new witness against its mutant edit/trigger/constraint and rerun under ordinary parallel suite execution. Report any existing mutant without a relevant assertion as a coverage defect; add the specific assertion within scope or stop for expansion, never loosen credit rules.
   **Verify:** final command → exit 0; no application/DDL changes and no mutants removed.

## Done criteria

- [x] Writer ordering/progress tests use observable state rather than sleep schedules or narrow timing thresholds.
- [x] Every mutant requires a named relevant failed assertion and a completed suite.
- [x] False-credit controls, repeated writer/mutant runs and full verification pass.
- [x] Validation claims are not weakened; index IN REVIEW (owner).

Implementation evidence (2026-10-05): writer probes use a zero-timeout held-lock refusal, explicit commit signal, existing-row observation and known reader snapshot; a 30-second guard detects deadlock, not speed. Automatic busy-timeout waiting is not measured. Each of 190 mutants statically names an exact relevant failed assertion; stopped suites and unrelated/diagnostic-only failures earn no credit. False-credit and no-op controls were red with the old predicate and green with the new one.

Witness review exposed coverage defects, so narrow expansions to `tests/dates_test.go`, `tests/identity_test.go`, `tests/named_test.go` and `tests/facts_test.go` were approved. Added direct fractional-instant, endpoint-immutability and measurement-import-uniqueness assertions; dependent fixture work is guarded only after an explicit intended failure, allowing normal completion without removing independent checks. Clean suites retain all controls. Integration retained 061's separate stub-resave guard.

Parent final checks passed: `go test -mod=readonly -count=20 ./tests -run 'TestSuites/writers$'` (9.588 s), `go test -mod=readonly -count=3 ./tests -run TestMutants` (77.565 s), harness/named/identity/facts/dates checks, generation, vet and `go test -mod=readonly -count=1 ./...`. No mutants were removed, no canonical SQL changed, and no runtime witness discovery remains.

## STOP conditions

Stop if the pinned driver cannot expose a claimed waiting behavior deterministically; report the limit and retain equivalent executed lock evidence, not a false assertion. Stop if a mutant lacks a meaningful witness requiring out-of-scope suite edits. Stop on unexplained drift or two failed gates.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `tests: make writer and mutant evidence deterministic (plan 067)`, explaining the legitimate harness change and checks run. New mutants must name their expected witness at creation.
