# 059 — Apply habit domains to capture and hide tombstoned habits

> Executor: enforce existing metric/habit rules, not a new ban on Mood becoming a habit. Keep tombstoned facts intact. Use synthetic stores and finish with plan/index IN REVIEW (owner).
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/core/write.go internal/core/habits.go internal/core/core_test.go internal/core/habits_test.go internal/api/api_test.go docs/cookbook/habits.md docs/cookbook/capture.md tests/habits_test.go tests/journal_test.go tests/mutants_test.go tests/README.md`.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P2. **Effort:** M. **Risk:** MED (capture rollback and historical visibility).
- **Status:** IN REVIEW (owner). **Depends on:** implemented 035 validation behavior.
- **Category:** bug. **Deep-audit finding:** 13. **Confidence:** HIGH, source-confirmed; new cases not executed.

## Why

Capture validates mood's 1–5 range but inserts it directly, bypassing the habit 0/1 domain and live-metric lookup used by Record. Separately, tombstoned habit metrics still appear in daily/completion views and receive check-in actions. The writer and canonical query recipes need consistent domain and liveness behavior.

## Current state and conventions

`internal/core/write.go:213–220` inserts captured mood with `SELECT id ... FROM pages WHERE title_key = 'mood'`, bypassing `Record`/`validateMeasurementValue`. The latter checks both Mood and any metric with habit periods. `internal/core/habits.go:142–196` joins habit periods to pages but not live entities; `docs/cookbook/habits.md` has the same omission. The API offers actions for every returned habit.

Use `TestCorrectionDomains`, `TestCaptureCreatesTheDayAndItsLinks` and `TestHabits`; recipe suites execute docs SQL literally. [D24](../decisions/D24-habits.md), [D11](../decisions/D11-tombstones.md) and the [habit recipe](../cookbook/habits.md) are authoritative; do not add schema constraints or delete historical readings.

## Scope

Only paths in the drift command plus plan/index. No habit-period redesign, special prohibition on Mood habits, historical data repair, DDL or migrations. Update cookbook behavior and suites together; do not weaken tests to accommodate hidden rows.

## Commands

- Product: `go test -mod=readonly -count=1 ./internal/core ./internal/api -run 'TestCaptureHabitDomain|TestTombstonedHabits|TestHabits|TestCaptureCreates|TestCorrectionDomain'`.
- Recipes: `go test -mod=readonly -count=1 ./tests -run 'TestSuites/(habits|journal|document)$|TestMutants'`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and tests

1. Add `TestCaptureHabitDomain`: give Mood a habit period before non-binary readings exist, then capture mood 4 and assert refusal rolls back text/day revival/links/reading. Mood 1 must pass the intersecting existing domains. Test tombstoned Mood is not silently written and text-only capture still works. Add `TestTombstonedHabits` for daily state, completion and API actions, with revival controls.
   **Verify:** product command → new bypass/visibility assertions fail on baseline.
2. Route captured mood through the same live-metric/domain validation as Record inside the existing transaction, preserving `captured_with_id` and source. Do not merely add a one-off habit check or exclude Mood from StartHabit. Preserve the capture's all-or-nothing behavior.
   **Verify:** product command → domain and rollback cases pass, including ordinary mood capture/corrections.
3. Filter tombstoned metric entities in daily/completion queries and matching canonical recipes. Do not erase periods/readings or alter historical correction semantics. API actions disappear through the filtered data. Check recipe start/stop/capture examples also select live metrics where applicable; reference existing domain authority rather than duplicating it.
   **Verify:** product and recipe commands → literal SQL and writer outputs agree before tombstone, after tombstone and after revival.
4. Add targeted cookbook mutants for removed liveness predicates and retain correct counts in `tests/README.md`. New assertions must distinguish hidden display rows from retained underlying facts.
   **Verify:** recipes and final commands → exit 0; scope-only diff.

## Done criteria

- [x] Captured mood uses the same domain/liveness checks as other readings and rolls back atomically.
- [x] Tombstoned habits have no daily/completion entries or check-in actions; revival restores visibility.
- [x] Historical rows remain unchanged; cookbook/writer parity and intended mutant witnesses pass.
- [x] All gates pass; index IN REVIEW (owner).

Implementation evidence (2026-10-05): Capture delegates to Record in its existing transaction; rollback tests cover day revival, text, links, ghost creation and readings. Live habit queries and literal capture/start/stop recipes filter tombstones without changing history. Five targeted liveness mutants were added with corresponding recipe witnesses; legitimate recipe changes required suite changes. Focus/recipe and integrated generation/vet/uncached full tests passed.

## STOP conditions

Stop if the owner wants a different historical visibility policy or a new prohibition on Mood habits. Stop if contract behavior would change rather than its existing checks being applied consistently; use the change process first. Stop on unexplained drift or two failed gates.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `habits: unify capture domains and live views (plan 059)`, listing checks and legitimate suite changes. Future reading paths must reuse domain validation; future habit queries must explicitly choose live versus historical views.
