# 055 — Preflight photo embeds and recognize actual rendered embeds

> Executor: land 065's Markdown precedence fix first, then rebaseline shared parsing behavior. Use synthetic photos; no title/grammar redesign. Set plan/index IN REVIEW (owner) after gates.
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/core/places.go internal/core/files.go internal/core/places_test.go internal/core/files_test.go internal/text/text.go internal/text/text_test.go internal/api/files_test.go docs/cookbook/place-of-a-photo.md tests/places_test.go tests/mutants_test.go tests/README.md`.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P2. **Effort:** M. **Risk:** MED (rendering, graph and transaction agreement).
- **Status:** IN REVIEW (owner). **Depends on:** [065](065-markdown-link-precedence.md); serialize photo tests with 047/053.
- **Category:** bug. **Deep-audit finding:** 9. **Confidence:** HIGH, source-confirmed; new regression not run.

## Why

A valid filename/title such as `Lake [1].jpg` cannot be named by the current wikilink grammar, yet automatic embedding appends it and reports success. Literal substring deduplication also mistakes code examples for existing pictures and misses aliases. The result must reflect a real renderable embed and synchronized graph link, not just inserted punctuation.

## Current state and conventions

`internal/core/places.go:141–155` builds `mark := "![[" + title + "]]"` and updates where `instr(body, ?) = 0`, then unconditionally reports true after link sync. `internal/text/text.go` permits brackets in titles but its wiki regexp excludes them inside targets. The [photo cookbook](../cookbook/place-of-a-photo.md) repeats literal `instr` deduplication. Follow `TestAPhotoNearNoPlaceStillHasItsDay`, `TestAddFilePromotesAGhost` and the literal recipe runner in `tests/places_test.go`.

[The title/wikilink contract](../contract/titles-and-wikilinks.md) is unchanged. A valid file handle is not necessarily representable in automatic markup. Bounded remedy: refuse an operation that would need an unrepresentable automatic embed before commit, with a diagnostic requesting an explicit representable title for a new file. Do not silently rename an existing handle.

## Scope

Only the source/test/doc paths in the drift command plus plan/index. No DDL, title predicate or wikilink grammar changes, body reformatting, hash dedup changes or new photo-library behavior. A shared parsing helper may be added inside `internal/text`, not a new parsing dependency.

## Commands

- Focus: `go test -mod=readonly -count=1 ./internal/text ./internal/core ./internal/api -run 'TestAutomaticEmbed|TestPhotoEmbed|TestAPhoto|TestAddFile|TestPromotedFile'`.
- Recipe: `go test -mod=readonly -count=1 ./tests -run 'TestSuites/(places|files|save-contract|doc-save-contract|document)$|TestMutants'`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and tests

1. Add `TestAutomaticEmbedRepresentability` and `TestPhotoEmbedDeduplication`: bracket-containing titles, alias embeds, NFC/case equivalents, literal code spans/fences, ordinary Markdown links/images, a plain wikilink without `!`, and repeat keeps. Assert returned flags, body, graph, day/place rows and preview state agree.
   **Verify:** focus command → baseline false-success and false-dedup assertions fail.
2. Preflight the actual selected handle (including a hash-existing file's stored title) against unchanged extraction/rendering semantics whenever an embed is needed. On failure, roll back the whole current keep, including ghost promotion, missing-preview fill and place/day writes. An operation that needs no embed remains allowed; `TryAddFile` must return the same refusal without writes.
   **Verify:** focus command → unsupported automatic markup refuses atomically, with a useful explicit-title diagnostic.
3. Recognize actual embeds by parsed CommonMark context and canonical target identity, not raw substring or any wikilink row. Existing aliases suppress duplicates; code examples and ordinary links do not. Keep the caller's body bytes unchanged except the intentional append. Report `Embedded` only when the intended embed was added under its existing result semantics.
   **Verify:** focus command → dedup/flag/graph assertions pass, including 065's precedence behavior.
4. Update the photo cookbook to take a writer-computed parsed-embed decision and to preflight representability using the existing grammar. Execute its revised SQL with explicit bindings in `tests/places_test.go`; add a targeted cookbook mutant and update its count if needed. Do not put a second grammar in the recipe.
   **Verify:** recipe and final commands → exit 0; only scope files changed.

## Done criteria

- [x] Unrepresentable automatic embeds never commit partial keeps or claim success.
- [x] Parsed aliases deduplicate; code/ordinary links do not; graph and rendering agree.
- [x] Dry runs/refusals preserve existing files, ghosts, dates, previews and place points.
- [x] Canonical recipe, mutants and all gates pass; index IN REVIEW (owner).

Implementation evidence (2026-10-05): parsed CommonMark embed detection agrees with renderer precedence and canonical title identity. Selected-handle checks precede promotion/preview/place/day writes; final-body checks refuse appends hidden by an unclosed fence. Parent review reproduced a no-op regression for a literal `&copy;.jpg` handle with an already escaped embed; checking existing embeds before automatic-title refusal fixed it. No-picture/no-explicit-day controls remain allowed.

The photo recipe now takes a writer-computed parsed append binding, with alias/code/plain-link witnesses and a targeted mutant. Narrow `tests/cookbook_test.go` binding-registry expansion was approved; suites changed with the legitimate recipe. Parent integration preserved existing photo/feedback tests and passed the focused photo/text/API gate, all 190 mutants on three runs, generation, vet and uncached full tests. The text helper and renderer must retain matching CommonMark precedence.

## STOP conditions

Stop if a remedy requires changing title/wikilink rules or renaming an already-kept file. An existing unrepresentable handle needs an owner's separate policy, not an implicit alias. Stop on unexplained drift, scope expansion or two failed gates.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `photos: make automatic embeds truthful (plan 055)`, listing checks and legitimate recipe-suite changes. Keep rendering, target extraction and automatic embed detection aligned without redefining the contract.
