# 047 — Keep the file-promotion cookbook in step with the writer

> Executor: the Go missing-day fix is implemented. Complete its canonical recipe and executable coverage; do not repeat or redesign the fix. Use synthetic files/databases only; finish with plan/index IN REVIEW (owner).
>
> Drift check: `git diff --stat 0460379..HEAD -- docs/cookbook/keep-a-file.md tests/files_test.go tests/mutants_test.go tests/README.md internal/core/files.go internal/core/files_test.go internal/core/places_test.go internal/api/files_test.go`. Compare changed symbols; stop on unexplained drift.

## Status

- **Planned at:** `0460379`, 2026-10-05; follow-up to the implementation brief at `726ffab`.
- **Priority:** P2. **Effort:** S. **Risk:** LOW (fill a NULL day only).
- **Status:** IN REVIEW (owner). **Depends on:** none.
- **Category:** docs/bug. **Deep-audit finding:** 17. **Confidence:** HIGH, source-confirmed; new recipe regressions not executed.

## Why

The product now fills a promoted ghost's missing file date, but the language-neutral recipe still leaves it NULL. Another writer following the canonical SQL would continue producing undated file pages. The cookbook and its literal execution tests must describe the same operation as the application.

## Current state and conventions

`internal/core/files.go:159` fills the selected date during promotion. In contrast, `docs/cookbook/keep-a-file.md:40–46` only changes entity type/revival and then:

```sql
UPDATE pages SET body = :body WHERE id = :file_id AND entity_type = 'file' AND body = '';
```

`tests/files_test.go` executes the recipe's SQL, splits it into positional steps, and checks a promoted ghost's type/body/hash/backlink but not its day. It already compares fresh-file recipe rows with `Store.AddFile`; extend that comparison to promotion. Use `TestFilePromotionDay` in `internal/core/files_test.go` as the product-side behavior reference.

Honor [D9](../decisions/D09-binary-files.md) and [the photo recipe](../cookbook/place-of-a-photo.md): the fallback day kept is not evidence that a photo has its own day. The docs must remain language-neutral and must not name the Go writer.

## Scope

Modify only `docs/cookbook/keep-a-file.md`, `tests/files_test.go`, `tests/mutants_test.go`, `tests/README.md` if its mutant count changes, and this plan/index. The four listed core/API files are read-only drift/verification references. No DDL, migrations, body-merge policy, existing non-NULL date replacement, hash/title changes, or new automatic photo links.

## Commands

- Recipe: `go test -mod=readonly -count=1 ./tests -run 'TestSuites/(files|document)$'`.
- Product: `go test -mod=readonly -count=1 ./internal/core ./internal/api -run 'TestFilePromotionDay|TestAddFilePromotesAGhost|TestPromotedFileDay|TestAPhotoWithNoDayLinksNoDay'`.
- Mutants: `go test -mod=readonly -count=1 ./tests -run TestMutants`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and tests

1. Extend the literal promotion cases in `tests/files_test.go` to assert the supplied day fills NULL, an existing day remains unchanged, and a page with existing text still gets its missing day. Compare recipe and writer row shapes including day and stable id/backlinks.
   **Verify:** recipe command → new day assertions fail; product command → existing application tests pass.
2. Update the promotion SQL so filling day does not depend on an empty body. Preserve the text-fill conditional and existing branch/refusal semantics. Either combine conditional assignments or add a separate guarded update; adjust the suite's SQL-step parser honestly if statement count changes, without dropping execution or branch assertions.
   **Verify:** recipe and product commands → all pass, including occupied-title refusals and unchanged non-NULL dates.
3. Add a cookbook mutant that removes the missing-day assignment and must be caught by the new promotion assertion. Update the count in `tests/README.md`. Keep fallback capture-day behavior separate from own-photo-day linking; preserve dry-run and rollback tests already in core/API.
   **Verify:** mutants and final commands → exit 0; `git diff --name-only` shows only allowed modifications.

## Done criteria

- [x] The literal recipe and writer agree for fresh files and promoted ghosts, including NULL/non-NULL days and nonempty bodies.
- [x] Identity, text and backlinks remain intact; undated photos gain no inferred day/place links.
- [x] The day-assignment mutant is detected for its intended rule; counts and all gates pass.
- [x] No product source or schema changed; index is IN REVIEW (owner).

Implementation evidence (2026-10-05): revised literal SQL fills only a missing promoted-file day. Recipe/writer parity and the targeted mutant passed; suite changes implement the legitimately revised recipe, not relaxed expectations. Focus/recipe gates and integrated generation/vet/uncached full tests passed.

## STOP conditions

Stop if the recipe and writer disagree beyond this date fill, existing dates require reconciliation, or the suite must be weakened rather than updated to execute revised SQL. Stop on unexplained drift or a gate failing twice. Report unrelated body-conflict differences separately.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push without instruction. Suggested commit: `docs: align promoted file dates with the recipe (plan 047)`, listing checks and the legitimate suite change. Future writer behavior changes must update and execute the canonical recipe in the same change.
