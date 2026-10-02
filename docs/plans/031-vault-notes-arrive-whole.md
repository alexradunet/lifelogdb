# Plan 031: A vault's notes arrive whole, linked as written, and a page the owner deleted stays deleted

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat cad659b..HEAD -- internal/importer/vault.go internal/importer/apply.go internal/importer/status.go internal/importer/workspace.go internal/importer/importer_test.go internal/core/write.go internal/core/imports.go`
> Plans 029 and 030 touch `importer_test.go` and other functions of `write.go`/`imports.go` — expected. On a mismatch
> with the excerpts below, STOP.

## Status

- **Priority**: P1 — the first real import is an Obsidian vault
- **Effort**: M
- **Risk**: MED (bodies of already-imported notes may change on the next apply; trials are rebuilt anyway)
- **Depends on**: 029, 030 (same test file; run after them)
- **Category**: bug
- **Planned at**: commit `cad659b`, 2026-10-02

## Why this matters

`docs/guides/importing.md` ("An Obsidian vault") promises: every note becomes one page, copied whole; a daily note
whose day page exists is appended **once**; Obsidian's link forms are rewritten "so a reader sees the same words";
file names are read "as they are on disk (NFD on some filesystems)". The importer audit (executed at `9ac130f`)
found each promise broken in a common case:

1. **A short daily note is silently dropped**: if its text occurs anywhere inside the existing day page
   (e.g. the note `Gym` and a day page reading `Gym with Sam, then lunch.`), `appendOnce` counts it as already
   appended and the plan records it as appended.
2. **Links in nested lists are lost**: any line starting with a tab or four spaces is treated as code — but in a
   vault that is a nested list item. `\t- see [[Notes/Old name]]` is left as is, and the folder form makes no link.
3. **Table links miss their note**: `[[Old name\|shown]]` (Obsidian's escaped pipe inside a table) is looked up as
   `Old name\`; a rewritten link in a table row gets a raw `|` and breaks the table.
4. **A note embed `![[Note]]` becomes a code span**, losing the link; the guide sends only *attachments* to code spans.
5. **NFD file names cannot be read**: the ledger lists the NFC name, and opening the NFC name fails on NTFS/ext4
   (a vault copied from macOS).
6. **`fix-plan` after the vault was applied** renames the plan entry, not the page (titles never change), and other
   notes' links are rewritten to the new title — a ghost page.
7. **Tombstones are not respected**: a re-apply overwrites the body of a vault page the owner deleted and syncs its
   links (the cookbook says "a tombstoned one stays gone"); a facts `person`/`place` on a tombstoned row revives it and
   reports `existing` (so *status* stays green); a facts `page` on a tombstoned page reports `existing` and a later link
   to it fails with the misleading "not written yet".

## Current state

- `internal/importer/vault.go:286-303` — `appendOnce`:
  ```go
  if p, err := t.Lookup(n.Title); err != nil {
  	return err
  } else if p != nil && (n.Appended || strings.Contains(p.Body, body)) {
  	res.Same++
  	return nil
  }
  _, sync, err := t.Capture(n.Title, body, nil)
  ```
  `internal/importer/status.go:257` uses the same test: `if p, _ := t.Lookup(n.Title); p == nil || !strings.Contains(p.Body, want) {`.
  Capture appends "after a blank line" (`docs/cookbook/capture.md`), so an appended note is a block: at the start of
  the page or after `\n\n`, and followed by the end of the page or `\n\n`.
- `internal/importer/vault.go:331-353` — `rewriteLinks`: line-based; fences tracked with `fenceRE`; **line 346**:
  `if fence != "" || strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") { out.WriteString(line); continue }`.
- `internal/importer/vault.go:380-413` — `rewriteText`:
  ```go
  embed := strings.HasPrefix(tok, "!")
  inner := wikiTokenRE.FindStringSubmatch(tok)[1]
  target, alias, hasAlias := strings.Cut(inner, "|")
  ...
  if attachExt.MatchString(target) || embed {
  	return "`" + tok + "`" // attachments are deferred (D9)
  }
  ...
  _ = anchor
  return "[[" + title + "|" + shown + "]]"
  ```
- The writer's wikilink reader reads **decoded** CommonMark text (`internal/text/text.go:228-276`, `textRuns`, which
  skips `CodeSpan`, `CodeBlock`, `FencedCodeBlock`, `HTMLBlock`, `RawHTML`, `AutoLink`, `Image`), so in a body
  `[[Title\|shown]]` decodes to `[[Title|shown]]` and links `Title`. The repo uses goldmark (`github.com/yuin/goldmark`,
  plain CommonMark: `goldmark.New()` in `text.go:28`).
- `internal/importer/vault.go:88-138` — `validatePlan`; for a note already created by this import:
  `case mine != 0: n.Action = "create" // created by this import before` (no title comparison).
  `vault.go:140-173` — `FixPlan` changes `Title`/`Day` of a note unless `Appended`.
- `internal/importer/vault.go:252-272` (in `applyPlan`): `id, err := t.ByImportKey(n.Path)` … `t.SetBody(id, body)`.
  `internal/core/imports.go:28-36` — `ByImportKey` has no `deleted_at` filter. `internal/core/write.go:262-268` —
  `SetBody` has no tombstone guard (unlike `SaveBody`, `write.go:253-255`:
  `if deleted.Valid { return Sync{}, conflict("page %d is deleted: revive it first", id) }`).
  `docs/cookbook/import-a-row-once.md:24` keys its update on `… AND deleted_at IS NULL`.
- `internal/importer/apply.go:209-240` — `named`: `case p.Type == typ: if p.Deleted { t.Revive(p.ID) } ; o.Status = "existing"`.
  `apply.go:136-147` — the `page` branch returns `Status: "existing"` for any page with that title, deleted or not.
  `internal/importer/status.go:158-162` counts only outcomes other than `existing` as mismatches.
- `internal/importer/workspace.go:83-95` — `ReadSource(rel)` opens `SourcePath(rel)` (the NFC name) and NFC-normalises
  the **content**; `files.go` `SourceFiles()` NFC-normalises the **names** it lists.
- Test patterns: `internal/importer/importer_test.go` — `TestVault` (`:349`) and `TestRewriteLinks` (`:403`, a table of
  input → output for `rewriteLinks`). Extend those.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Importer | `go test ./internal/importer -run 'Vault|Rewrite|Tomb|NFD|Append' -v` | PASS |
| Everything | `go generate ./... && go vet ./... && go test ./...` | exit 0 |

## Scope

**In scope**: `internal/importer/vault.go`, `internal/importer/apply.go` (`named`, the `page` branch, `Outcome`
statuses), `internal/importer/status.go` (line ~257 and the mismatch rule at ~158), `internal/importer/workspace.go`
(`SourcePath`/`ReadSource`), `internal/importer/importer_test.go`, `internal/core/write.go` (`SetBody` guard),
`internal/core/imports.go` (a `Deleted(id)` read on `Tx`).

**Out of scope**: `docs/` (the guide already states each promise); the ledger format (plan 032); the facts checks
(plan 030); making the append record durable **inside** the database (would need a schema change — see Maintenance).

## Git workflow

Branch `advisor/031-vault-notes`; one commit per numbered group is fine. Message e.g. `import: a daily note is
appended unless its block is there, links in lists, tables and embeds are rewritten, NFD names open, deleted pages
stay deleted`, body + `Ran: …` + trailer.

## Steps

### Step 1: Tests first

Extend `TestRewriteLinks` with rows (input → expected output; `Old name` is a note renamed `New name` in the plan, and
`Notes/Old name.md` its path — reuse how the existing rows build the plan):
- `"- a\n\t- see [[Notes/Old name]]\n"` → the nested item becomes `[[New name|Notes/Old name]]`.
- `"    code [[Old name]]\n"` after a blank line at top level (a real indented code block) → unchanged.
- `"| [[Old name\\|shown]] |\n"` → `"| [[New name\\|shown]] |\n"`.
- `"| [[Notes/Old name]] |\n"` → `"| [[New name\\|Notes/Old name]] |\n"` (a table row keeps `\|`).
- `"![[Old name]]\n"` → `"![[New name|Old name]]\n"` (a note embed is a link; the `!` stays).
- `"![[photo.png]]\n"` → ``"`![[photo.png]]`\n"`` (an attachment is still a code span).

Add `TestAppendOnceIsByBlock`: a day page whose body is `Gym with Sam, then lunch.` and a daily note `Gym` marked
`append` → after apply the page body ends with `\n\nGym` (appended); a second apply → unchanged.

Add `TestTombstonesStayGone`: (a) apply the vault, tombstone one created note page, change that note's file on disk,
apply again → the tombstoned page's body is unchanged and no link rows were added from it; (b) a facts `person` whose
title names a tombstoned person → the outcome status is `revived` and `Status()` lists a mismatch; (c) a facts `page`
on a tombstoned page → refused with a message containing `deleted`.

Add `TestNFDFileNames`: write a source file whose name is NFD (`"Zoë.md"`), make the ledger, and
`ReadSource` of the listed (NFC) name returns its content.

**Verify**: these tests fail on the current code.

### Step 2: An appended note is a block

Add in `vault.go`:

```go
// holdsBlock reports whether page holds text as a block of its own, as capture appends it: at the start or after a
// blank line, and at the end or before a blank line (docs/cookbook/capture.md).
func holdsBlock(page, text string) bool
```

Normalise both with `strings.TrimRight(x, "\n")`; return true if `page == text`, or `page` starts with `text+"\n\n"`,
or ends with `"\n\n"+text`, or contains `"\n\n"+text+"\n\n"`. Use it in `appendOnce` instead of `strings.Contains`
and in `status.go:257`.

**Verify**: `TestAppendOnceIsByBlock` passes; `TestVault` still passes.

### Step 3: Code is what CommonMark says is code

Replace the line-based code detection of `rewriteLinks` with ranges from the CommonMark parse:
- Parse `src` with `goldmark.New().Parser().Parse(gtext.NewReader([]byte(src)))` (imports as in
  `internal/text/text.go:12-15`).
- Walk the AST and collect byte ranges `[start, stop)` of: every line of `*ast.CodeBlock`, `*ast.FencedCodeBlock`
  and `*ast.HTMLBlock` (`n.Lines()` segments; for a fenced block also include its fence lines — simplest: from the
  first line's start back to the previous newline, through the last line's stop to the closing fence's end; if that
  is awkward, take the range from the node's first segment start to the start of the next block sibling), every
  `*ast.CodeSpan` (from the backtick run before its first child segment to the backtick run after its last), and every
  `*ast.RawHTML`.
- Find all `wikiTokenRE` matches in the **whole** `src`; rewrite a match only if it does not overlap a code range;
  copy every other byte unchanged.
- Delete `rewriteOutsideCodeSpans` and the line loop if nothing else uses them.

**Verify**: the nested-list and indented-code rows of `TestRewriteLinks` pass; all old rows still pass.

### Step 4: Table pipes and embeds

In `rewriteText` (or its replacement):
- `escaped := strings.Contains(inner, \`\\|\`)`; split on the first `\|` when escaped, else on the first `|`. Use the
  unescaped target for the lookup.
- The emitted separator is `\|` when `escaped`, or when the token's line, trimmed of leading spaces, starts with `|`
  (a table row); otherwise `|`.
- An embed (`!` prefix) whose target is **not** an attachment (`attachExt`) is rewritten like a link and keeps its
  leading `!`; an attachment embed stays a code span. Remove the `_ = anchor` line.

**Verify**: all `TestRewriteLinks` rows pass.

### Step 5: Names as they are on disk

In `workspace.go`, after `SourcePath` builds `p`: if `os.Stat(p)` reports not-exist, resolve it segment by segment —
for each segment below `w.Source`, if the joined path does not exist, read the parent directory and pick the entry
whose `norm.NFC.String(name)` equals the segment; if none, keep the original (the caller then reports 404). Put this in
a helper `onDisk(root, rel string) string` used by `SourcePath`.

**Verify**: `TestNFDFileNames` passes.

### Step 6: A created note's title is fixed

In `validatePlan`, for `mine != 0`: read the page's title (`t.Lookup` cannot find by id — add `Tx.TitleOf(id)` in
`internal/core/imports.go` if no such read exists; check `read.go` for an existing one first) and, if
`text.TitleKey(title) != text.TitleKey(n.Title)`, add the problem
`fmt.Sprintf("this note's page was created as %q and a title never changes: set the title back", title)`.

**Verify**: a test in `TestVault` style — apply, `FixPlan` a created note to a new title → the plan has that problem,
and `ApplyVault` refuses.

### Step 7: Deleted pages stay deleted

- `internal/core/imports.go`: add `func (t *Tx) Deleted(id int64) (bool, error)`.
- `internal/core/write.go`, `SetBody`: refuse a tombstoned page with the same `conflict(...)` as `SaveBody`.
- `vault.go` `applyPlan`: after `ByImportKey`, if the page is deleted, count it in a new `VaultResult` field
  `Gone int \`json:"deleted_left_alone"\`` and skip it.
- `apply.go` `named`: when `p.Deleted`, set `o.Status = "revived"` (keep the revive — the guide's table says a
  tombstoned person is revived). In `status.go`'s mismatch rule, count `revived` as a mismatch with the message
  `"<title> was deleted on this database and the facts revive it"`.
- `apply.go` `page` branch: when `p != nil && p.Deleted`, return
  `fmt.Errorf("%q was deleted by the owner: ask before writing it again", title)`.

**Verify**: `TestTombstonesStayGone` passes.

### Step 8: Full run

**Verify**: `go generate ./... && go vet ./... && go test ./...` → exit 0.

## Test plan

New: `TestAppendOnceIsByBlock`, `TestTombstonesStayGone`, `TestNFDFileNames`, the `FixPlan` case; extended
`TestRewriteLinks` (six rows). Patterns: `TestVault`, `TestRewriteLinks`.

## Done criteria

- [ ] `go generate ./... && go vet ./... && go test ./...` exits 0
- [ ] `grep -n 'strings.Contains(p.Body' internal/importer/vault.go internal/importer/status.go` → no match
- [ ] `grep -n 'HasPrefix(line, "\\t")' internal/importer/vault.go` → no match
- [ ] `grep -n "_ = anchor" internal/importer/vault.go` → no match
- [ ] `git status --short` lists only in-scope files; status row for 031 updated

## STOP conditions

- goldmark's AST gives no way to get a fenced block's or code span's full byte range without guesswork — report what
  you found; do not fall back to the line heuristic.
- A `TestVault` expectation changes (a body that was rewritten before is rewritten differently) for a reason other than
  the six cases above — report the diff.
- Making `SetBody` refuse a tombstoned page breaks `Rename` or another caller in `core` — report the caller.

## Maintenance notes

- The guide asks that the append record be written "in the same transaction as the append"; `plan.json` is a file,
  so it cannot be. `holdsBlock` makes the fallback safe for short notes; a record inside `life.db` would need a schema
  change (an issue first, `docs/process.md`).
- `holdsBlock` still treats a block the owner happened to write with identical text as "appended" — acceptable; it
  is a whole paragraph, not a substring.
- When the save contract's reader changes (e.g. plan 039's escape fix), check `TestRewriteLinks` rows that rely on
  `\|` decoding.
