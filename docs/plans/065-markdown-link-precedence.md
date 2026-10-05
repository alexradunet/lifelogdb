# 065 — Preserve ordinary Markdown links before adding wiki rendering

> Executor: preserve CommonMark destinations and safe HTML defaults. Do not change persisted link extraction to hide a renderer defect. Use synthetic strings; set plan/index IN REVIEW (owner) after gates.
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/api/markdown.go internal/api/markdown_test.go internal/text/text.go internal/text/text_test.go`.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P2. **Effort:** M. **Risk:** MED (inline parsing and safe rendering).
- **Status:** IN REVIEW (owner). **Depends on:** none; prerequisite to [055](055-truthful-photo-embeds.md).
- **Category:** bug. **Deep-audit finding:** 21. **Confidence:** HIGH, source-confirmed; new precedence cases not run.

## Why

The wiki inline parser runs before the ordinary Markdown link parser. Text that is a valid Markdown link with bracketed label can therefore become a local wiki link, replacing the intended destination and leaving literal destination syntax behind. Preserve the author's normal links while still rendering standalone wiki links and embeds.

## Current state and conventions

`internal/api/markdown.go:23` registers:

```go
util.Prioritized(wikilinkParser{}, 199), // before the link parser, which also starts at '['
```

The parser immediately consumes a matching `[[...]]` at line 64, without waiting to see whether CommonMark owns the full construct. `internal/text/text.go:textRuns` parses CommonMark first for storage extraction; those semantics must not be replaced by the renderer. Model tests after `TestMarkdownLinksWhatNamesAPage`, including its raw-HTML and dangerous-URL controls. [The title/wikilink contract](../contract/titles-and-wikilinks.md) remains unchanged.

## Scope

Only paths in the drift command plus plan/index. Text helpers may be factored for common decoded text handling, but contract vectors/results must remain unchanged. No Markdown dependency upgrade, raw-HTML enablement, persisted-link rewrite, new grammar, DDL or migrations.

## Commands

- Focus: `go test -mod=readonly -count=1 ./internal/api ./internal/text -run 'TestMarkdown|TestTitle|TestWiki|TestTag'`.
- Contract: `go test -mod=readonly -count=1 ./tests -run 'TestSuites/(title-fuzz|save-contract|doc-save-contract)$'`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and tests

1. Extend `TestMarkdownLinksWhatNamesAPage` (or add `TestMarkdownLinkPrecedence`) with inline/reference links whose labels contain brackets, ordinary images, wiki-shaped text in labels/destinations, standalone wiki/alias/embed forms, escaped punctuation, character references and code. Assert complete anchor/image structure and intended destination, not merely an occurrence of the label.
   **Verify:** focus command → baseline misroutes the bracketed-label case; safe-rendering controls pass.
2. Let CommonMark establish ordinary links/images before wiki enhancement. Prefer post-parse transformation of eligible plain text runs outside existing anchors/images/code/raw HTML; preserve source segmentation/decoded labels and no nested anchors. If changing parser priority instead, prove the full matrix rather than assuming numeric priority alone solves delimiter ownership.
   **Verify:** focus command → ordinary destinations survive and standalone wiki/tags/embeds still render.
3. Keep Goldmark's safe defaults: raw HTML omitted, dangerous URLs suppressed, generated labels escaped. Contract extraction may still name text within an ordinary link label according to its existing rules; do not create a nested rendered link to imitate the graph panel. Reuse helpers only where rendering and extraction semantics genuinely agree.
   **Verify:** focus and contract commands → all pass with unchanged contract-vector expectations.
4. Add malformed/mixed delimiters and Unicode cases so future custom parsers cannot steal an existing link destination. Preserve unchanged Markdown text fallback on conversion errors.
   **Verify:** final command → exit 0; scope-only diff.

## Done criteria

- [x] Ordinary Markdown links/images keep their intended destinations and valid HTML structure.
- [x] Standalone wiki/alias/embed/tag behavior and safe rendering remain correct.
- [x] Persisted extraction/title contract vectors are unchanged; all gates pass.
- [x] Index IN REVIEW (owner); 055 can rebaseline against the verified renderer.

Implementation evidence (2026-10-05): CommonMark parses before wiki/tag enhancement; existing anchors/images/code/HTML are not enhanced again. Standalone behavior and escaped rendering regressions pass, without changing persisted extraction vectors. Focus/package gates and integrated generation/vet/uncached full tests passed.

## STOP conditions

Stop if fixing rendering requires changing wikilink grammar, enabling unsafe HTML or serializing the whole document differently. Stop if source/decoded-text mapping cannot be made reliable without broader design. Stop on unexplained drift or two failed gates.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `markdown: preserve normal link precedence (plan 065)`, listing checks. Every new inline extension needs coexistence tests with standard links/images, not just isolated examples.
