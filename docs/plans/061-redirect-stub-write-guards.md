# 061 — Refuse ordinary prose and relationship writes on redirect stubs

> Executor: preserve rename's internal transition and old wikilink targets. Do not implement redirect-chain repair. Use synthetic pages; set plan/index IN REVIEW (owner) after gates.
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/core/write.go internal/core/rename.go internal/core/core_test.go internal/core/habits_test.go internal/api/handler.go internal/api/api_test.go tests/renames_test.go`.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P2. **Effort:** M. **Risk:** MED (rename moves links through the guarded methods).
- **Status:** IN REVIEW (owner). **Depends on:** none; serialize shared core/API edits with 059/058.
- **Category:** bug. **Deep-audit finding:** 15. **Confidence:** HIGH, source-confirmed; new direct-write regressions not run.

## Why

A redirect stub should preserve an old handle and point at its replacement, not acquire independent prose or relationships. Hiding only its edit form does not enforce that boundary: direct API/core calls can still change it, and its API entity advertises link actions. Enforce the existing lifecycle at the writer boundary.

## Current state and conventions

`internal/core/write.go:261–287` checks version/deletion but not stub state in `SaveBody`, and `SetBody` directly updates pages. `Link` checks special kinds and category targets but not general stub endpoints. `internal/api/handler.go:382–404` suppresses save/promote for stubs, then adds link/unlink actions outside that branch.

`internal/core/rename.go` writes the stub body, inserts the redirect edge, then moves typed links using `Unlink`/`Link`; naive new guards can break that same transaction. Follow `TestRename` in `habits_test.go`, `TestActionsAreOfferedOnlyWhereLegal` and `tests/renames_test.go`. [Renames](../cookbook/rename-a-page.md) and [the title contract](../contract/titles-and-wikilinks.md) govern the transition.

## Scope

Only paths in the drift command plus plan/index. No DDL/trigger changes, redirect-chain flattening, automatic retargeting of new requests, legacy-data repair or prohibition on wikilinks naming an old handle. Preserve tombstone/revive semantics.

## Commands

- Focus: `go test -mod=readonly -count=1 ./internal/core ./internal/api -run 'TestRedirectStubWrites|TestRename|TestActionsAreOfferedOnlyWhereLegal|TestSaveBody'`.
- Recipe: `go test -mod=readonly -count=1 ./tests -run 'TestSuites/(renames|save-contract|doc-save-contract)$'`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and tests

1. Add `TestRedirectStubWrites`: create/rename a synthetic page, then attempt SaveBody, import-owned SetBody, and typed Link/Unlink via public core/API paths. Test a stub as source and as new typed-link target. Assert clear refusals leave stub body/redirect/relationships untouched; a normal wikilink to the old title still resolves under current behavior.
   **Verify:** focus command → baseline permits independent writes or advertises illegal actions.
2. Add explicit stub guards at the writer boundary. Return a conflict directing the caller to the target; do not silently redirect writes. Distinguish caller mutation paths from rename's internal cleanup/move operations, using narrow private helpers or a safe transactional ordering rather than a broadly exposed bypass flag.
   **Verify:** focus command → direct writes refuse; rename into a free/existing allowed target, typed-link moves, symmetric mirrors and rollback still pass.
3. Remove ordinary link/unlink actions from stub entities while preserving target navigation and permitted lifecycle actions. Test bypass attempts against endpoints rather than trusting action omission.
   **Verify:** focus and recipe commands → all pass; canonical rename behavior is unchanged.
4. Test errors leave the target unchanged too and no redirect edge can be directly removed through generic unlink. Keep the known two-hop redirect-chain gap out of scope.
   **Verify:** final command → exit 0; only scope files changed.

## Done criteria

- [x] Stub prose and ordinary relationship mutations refuse in core and API, even with a valid version.
- [x] API actions reflect those guards; target navigation and old wikilinks still work.
- [x] Rename/link-moving internals remain transactional and pass the existing recipe suite.
- [x] All gates pass; index IN REVIEW (owner).

Implementation evidence (2026-10-05): writer guards reject stub body and typed-link endpoint mutations; rename finishes the stub after moving ordinary links within the same transaction. Direct API bypass and old-title wikilink controls pass. Narrow `tests/named_test.go` expansion was approved to assert stub resave refusal with unchanged body/links, retaining ordinary named-page edits. Focus/recipe and integrated generation/vet/uncached full tests passed. Redirect-chain repair remains out of scope.

## STOP conditions

Stop if current contract text requires a different writable-stub policy, or safety requires a new schema rule. Ask through the change process rather than inventing it. Stop if rename cannot be separated from public guards without broader refactoring, on unexplained drift, or after two failed gates.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `pages: guard redirect stub mutations (plan 061)`, listing checks. Future mutations need lifecycle checks in core; hypermedia action omission is not enforcement.
