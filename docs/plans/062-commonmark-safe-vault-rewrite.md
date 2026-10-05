# 062 — Preserve literal code while rewriting vault links

> Executor: rewrite only intended link tokens, preserving every other source byte. Use synthetic notes and the pinned CommonMark parser; set plan/index IN REVIEW (owner) after gates.
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/importer/vault.go internal/importer/importer_test.go internal/importer/vault_rewrite_test.go`. Serialize with 051/054 and rebaseline their reviewed changes.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P2. **Effort:** M. **Risk:** MED (source-text preservation).
- **Status:** IN REVIEW (owner). **Depends on:** none semantically; execute after 051/054 for shared-file safety.
- **Category:** bug. **Deep-audit finding:** 18. **Confidence:** HIGH, source-confirmed; new parsing cases not run.

## Why

Vault rewriting promises to leave code literal. It remembers only three fence characters and resets inline-code parsing on each line, so shorter inner fences can falsely close a longer fence and multiline code spans can be rewritten as prose. Importing then alters source examples instead of preserving them.

## Current state and conventions

`internal/importer/vault.go:335–374`:

```go
lines := strings.SplitAfter(src, "\n")
// ...
fence = m[1][:3]
// ...
out.WriteString(rewriteOutsideCodeSpans(line, self, idx))
```

`rewriteOutsideCodeSpans` searches for any occurrence of the opening tick string, not necessarily an equal-length closing run. `rewriteText` intentionally resolves Obsidian note forms and turns attachments into literal code. Follow `TestRewriteLinks` in `importer_test.go`, which already asserts exact strings. Goldmark is already a dependency; [the import guide](../guides/importing.md) and [CommonMark extraction contract](../contract/titles-and-wikilinks.md) remain unchanged.

## Scope

Only `internal/importer/vault.go`, `internal/importer/importer_test.go`, new `internal/importer/vault_rewrite_test.go`, plan/index. No source-file rewrite, whole-document Markdown reserialization, title/link grammar change, dependency upgrade, DDL or migrations. Renderer precedence is separately plan 065.

## Commands

- Focus: `go test -mod=readonly -count=1 ./internal/importer -run 'TestVaultRewriteCode|TestRewriteLinks|TestVault'`.
- Package: `go test -mod=readonly -count=1 ./internal/importer`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and tests

1. Add `TestVaultRewriteCode` with four/five-character fences containing shorter matching runs, tilde/backtick mismatches, closing fences with illegal trailing text, multiline spans, unequal tick runs, unterminated fences, indented blocks and CRLF. Place identical wiki forms inside and outside each code region so tests prove rewriting is selective, not simply disabled.
   **Verify:** focus command → new literal-preservation assertions fail on baseline; existing rewrites pass.
2. Derive protected code spans/block ranges from the existing CommonMark parser's source segments, or an equivalently tested offset scanner faithful to those boundaries. Account for opening/closing delimiters and multiline spans. Do not render the AST back into Markdown: it would change unrelated formatting/bytes.
   **Verify:** focus command → protected source slices are byte-identical while nearby supported links are rewritten.
3. Apply `rewriteText` only to eligible source ranges, in stable order, retaining frontmatter/body handling, aliases and attachment policy. Remove obsolete line-local code logic once unused. Add repeated-run and apply/replay assertions that body/link results are consistent and original source bytes are untouched.
   **Verify:** package and final commands → exit 0; scope-only diff.

## Done criteria

- [x] Long fences and multiline/equal-run code spans retain literal content exactly.
- [x] Outside-code links still resolve titles/aliases; attachment behavior remains intentional.
- [x] No whole-document reformatting or source mutation; trial/replay tests agree.
- [x] All gates pass; index IN REVIEW (owner).

Implementation evidence (2026-10-05): CommonMark source ranges protect literal code while rewriting eligible source slices only. Synthetic fence/span, alias and attachment cases pass with trial/replay agreement. Focus/package gates and integrated generation/vet/uncached full tests passed.

## STOP conditions

Stop if source offsets cannot be recovered reliably without changing supported input behavior; report the parser limitation rather than regex-guessing. Do not broaden this into an Obsidian parser rewrite. Stop on unexplained drift, scope expansion or two failed gates.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `imports: preserve CommonMark code during vault rewrite (plan 062)`, listing checks. New syntax handling must include an exact-byte negative fixture inside code.
