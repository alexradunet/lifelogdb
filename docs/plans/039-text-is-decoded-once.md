# Plan 039: A body's text is decoded once, as CommonMark decodes it — no tag appears from an escaped `&`

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat cad659b..HEAD -- internal/text docs/contract/titles-and-wikilinks.md docs/issues/README.md tests/wikilinks_test.go tests/pages_test.go`
> On a mismatch with the excerpts below, STOP.

## Status

- **Priority**: P2 — the vectors become frozen text at the freeze
- **Effort**: S
- **Risk**: LOW (only bodies with an escaped `&` or a doubly encoded reference change)
- **Depends on**: none (030 adds issue 0007 and 038 adds 0008; this plan adds **0009** — take the next free number if taken)
- **Category**: bug (contract)
- **Planned at**: commit `cad659b`, 2026-10-02

## Why this matters

The save contract says a body's `[[wikilinks]]` and `#tags` are read from its CommonMark **text** — backslash escapes and
character references resolved — and "every writer must reproduce" the vector table of
`docs/contract/titles-and-wikilinks.md`. The writer resolves them in **three separate passes** (escapes, then numeric
references, then named references), so text decoded by one pass is decoded again by the next. Executed at `9ac130f`:
`text.Candidates` returns `["tag"]` for each of `\&num;tag`, `&#38;num;tag` and `\&#35;tag`. In CommonMark each of those
is the plain text `&num;tag`, `&num;tag` and `&#35;tag` — no `#` — because a backslash-escaped `&` starts no reference
and a decoded reference is never decoded again (CommonMark spec, "Backslash escapes" and "Entity and numeric character
references"). The writer therefore creates tag pages and links a conformant writer in another language would not. The
fix is a one-pass decoder, and three vectors in the contract so every writer is held to it.

## Current state

- `internal/text/text.go:269-276`:
  ```go
  // decode is the text a text node stands for: backslash escapes and character references resolved.
  func decode(t *ast.Text, src []byte) []byte {
  	v := t.Segment.Value(src)
  	if t.IsRaw() {
  		return v
  	}
  	return util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(v)))
  }
  ```
  (`util` is `github.com/yuin/goldmark/util`; it also offers `util.LookUpHTML5EntityByName(name string) *util.HTML5Entity`
  — check the exact name in the module source under `$(go env GOMODCACHE)/github.com/yuin/goldmark@v1.8.6/util/`.)
- `docs/contract/titles-and-wikilinks.md:50-55` (Known limits) says: "a `#` written as an entity (`&#35;x`) is decoded
  before the scan and counts as a tag" — that stays true (one decoding of `&#35;` gives `#`).
- The vector table starts at `docs/contract/titles-and-wikilinks.md:57` ("Test vectors — every writer must reproduce
  them. A body is shown in a code span; `\n`, `\r` and `\t` stand for …, `\uXXXX` for that code point, and `\|` for `|`.")
  and its rows look like `` | `\[[escaped]]` | `escaped` | `` and `` | `##tag` | — | ``. A backslash not followed by
  `n`, `r`, `t`, `u` or `|` is a literal backslash (as in the `\[[escaped]]` row).
- The vectors' readers: `tests/wikilinks_test.go:40-85` (`printedVectors`, `unesc`) and `internal/text/text_test.go:14-95`
  — both read the table from the page; neither needs a change for new rows, as long as the new rows use only the
  escapes above.
- Issue convention: `docs/issues/template.md`, index `docs/issues/README.md`. Mutants: a new rule gets a mutant in
  `tests/mutants_test.go` (AGENTS.md); here the rule is a vector row, and the `doc-save-contract` / `save-contract`
  suites already fail when a printed vector is not reproduced — so a mutant that deletes the new rows is not needed, but
  read the `wikilinks` mutants in `tests/mutants_test.go` and follow the pattern if vector rows have one.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Text package | `go test ./internal/text -v` | PASS |
| Wikilink suites | `go test ./tests -run 'TestSuites/(doc-save-contract|save-contract|title-fuzz|pages|document)' -v` | `N/N` each |
| Everything | `go generate ./... && go vet ./... && go test ./...` | exit 0 |

## Scope

**In scope**: `internal/text/text.go` (`decode`), `internal/text/text_test.go` (a unit test of the decoder),
`docs/contract/titles-and-wikilinks.md` (three vector rows), `docs/issues/0009-an-escaped-ampersand-makes-a-tag.md`
(create), `docs/issues/README.md` (one row), `tests/README.md` only if it states a vector count.

**Out of scope**: any other contract wording; the tag grammar; `tests/*.go` (they read the table).

## Git workflow

Branch `advisor/039-decode-once`; message e.g. `wikilinks: text is decoded in one pass, as CommonMark does — three
vectors (issue 0009)`, body + `Ran: …` + trailer.

## Steps

### Step 1: The vectors first

Add three rows to the vector table, after the `` `\[[escaped]]` `` row:

```
| `\&num;tag` | — |
| `&#38;num;tag` | — |
| `\&#35;tag` | — |
```

**Verify**: `go test ./tests -run 'TestSuites/(doc-save-contract|save-contract)' -v` → **fails** on these rows (the writer
still finds `tag`). `go test ./internal/text` → fails the same way. This proves the vectors bite.

### Step 2: One-pass decoder

Replace `decode`'s last line with a call to a new `func unescape(v []byte) []byte` that walks `v` once:
- `\` followed by an ASCII punctuation character (`!"#$%&'()*+,-./:;<=>?@[\]^_\`{|}~`) → write that character, skip both.
- `&#` + 1–7 decimal digits + `;` → write the code point (0 or invalid → U+FFFD), skip the reference.
- `&#x`/`&#X` + 1–6 hex digits + `;` → the same.
- `&` + a name + `;` where the name is an HTML5 entity (goldmark's lookup) → write its characters, skip.
- anything else → write the byte.
Decoded output is never re-scanned. Keep `t.IsRaw()` as it is.

Add `TestDecodeOnce` to `internal/text/text_test.go` with byte-level cases: `\&num;` → `&num;`; `&#38;num;` → `&num;`;
`&num;` → `#`; `&#35;` → `#`; `\\#x` → `\#x`; `&#x1F600;` → 😀; `&bogus;` → `&bogus;`; `a\b` → `a\b` (backslash before a
non-punctuation is literal).

**Verify**: `go test ./internal/text -v` → PASS; the Step 1 suites → PASS.

### Step 3: The issue

Create `docs/issues/0009-an-escaped-ampersand-makes-a-tag.md` (template): date 2026-10-02; status `resolved`; seen in
"the writer's wikilink and tag extraction, on synthetic bodies"; what happened (the three bodies above produced the tag
`tag`; CommonMark text has no `#` in them); reproduce (save a page with body `\&num;tag` and list its wikilinks); rules
involved (`contract/titles-and-wikilinks.md` — the text is CommonMark's, and its vectors; D19); resolution (plan 039:
decode once; three vector rows). Add its row to `docs/issues/README.md`.

**Verify**: `go test ./tests -run 'TestSuites/document' -v` → `N/N`.

### Step 4: Full run

**Verify**: `go generate ./... && go vet ./... && go test ./...` → exit 0 (the 400-random-edit and fuzz suites included).

## Test plan

Three contract vectors (read by both suites and by `internal/text`'s tests) and `TestDecodeOnce`.

## Done criteria

- [ ] `go generate ./... && go vet ./... && go test ./...` exits 0
- [ ] `grep -n "ResolveEntityNames(util.ResolveNumericReferences" internal/text/text.go` → no match
- [ ] the three rows are in the vector table; issue 0009 exists and is indexed
- [ ] status row for 039 updated

## STOP conditions

- Step 1 does not fail (the writer already reproduces the rows) — the bug is gone; report and stop.
- A vector reader parses `\&` as an escape of its own (the rows then mean something else) — report; do not change the
  table's escape convention.
- Any other existing vector changes result with the new decoder — report the row.

## Maintenance notes

- If a later goldmark offers a public one-pass text decoder, prefer it and keep `TestDecodeOnce`.
- Plan 031's link rewriter relies on `\|` decoding to `|` inside a wikilink; `\|` is an escape of punctuation, so it is
  unaffected.
