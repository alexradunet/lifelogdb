# Plan 019: The wikilink contract states every rule and every vector, so no writer has to read the Python

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 4d84261..HEAD -- docs/contract/titles-and-wikilinks.md docs/guides/building-a-writer.md tests/wikilinks/ tests/README.md AGENTS.md`
> On a change to the lines quoted below, compare and STOP on a mismatch.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW (pins the behaviour the reference implementation already has; no DDL change)
- **Depends on**: none (run after 015 so failing wikilink suites print their diagnostics on Windows)
- **Category**: docs
- **Planned at**: commit `4d84261`, 2026-10-02

## Why this matters

`life.db` may be written by any developer in any language; `AGENTS.md` says *"Anything a writer must compute
identically in every language (the title predicate, `title_key`, wikilink and `#tag` extraction) is specified in
titles and wikilinks with vectors … an implementation may be an example, never the only statement of a rule."*
Today `docs/contract/titles-and-wikilinks.md` prints 30 vectors while `tests/wikilinks/vectors.py` has 55, and
`docs/guides/building-a-writer.md` sends writers to the Python file. Five rules are stated loosely enough that two
honest implementations disagree, and only `tests/wikilinks/wikisave.py` (the reference implementation) decides them:
all-digit tags with hyphens (`#2026-09-29`), the `#REDIRECT` stub's whitespace, whether NFC runs before or after the
CommonMark parse, whether de-duplication happens before or after validation, and what "line break" means inside a
wikilink. Two writers disagreeing means two different `links` tables for the same bodies. This plan writes each rule
the way the reference already behaves (the 2026-10 trial import used it) and makes the contract's table **equal** to
`vectors.py`, enforced by a suite.

## Current state

- `docs/contract/titles-and-wikilinks.md` — the home of the rules. Relevant lines:
  - `:14` "*What is read.* The CommonMark **text** of `pages.body`, after NFC normalisation — not code spans, …"
  - `:19` "*Wikilink.* `[[title]]` or `[[title|alias]]`, with no `[`, `]` or line break inside, and the …"
  - `:26-29` "*Tag.* `#` followed by words of letters, marks, digits and `_` (Unicode categories L, M, N) joined by single `-`, … — and the word is not all digits (`#12` and `#2024` are not tags, `#2024-review` is)."
  - `:33-34` "*Stub pages.* A body that starts with `#REDIRECT [[` (any case, leading whitespace allowed) is a rename stub …"
  - `:53-54` "Test vectors — every writer must reproduce them (`\n`, `́`, `̈` stand for a line break and combining marks; a body is shown in a code span):"
  - `:56-87` the table `| body | links to (`title`s, in order) |`, 30 rows; `:89` "(Also: a 240-byte title is a link, a 241-byte one is not; 80 × `日` = 240 bytes is, 81 is not.)"
- `tests/wikilinks/vectors.py` — `V = [(label, body, expected_titles), …]`, 55 entries; four use computed bodies
  (`A240 = 'a' * 240; A241 = 'a' * 241; J80 = '日' * 80; J81 = '日' * 81`).
- `tests/wikilinks/docchecks.py:15-27` — parses the doc table (`re.fullmatch(r'\| (``? .*? ``?|`[^`]*`) \| (.*) \|', …)`),
  decodes `\|`, `\n`, `́`, `̈`, and asserts **"at least 25 vectors and every one reproduces"** — a subset check.
- `tests/wikilinks/wikisave.py` behaviour to pin (verified by running it on 2026-10-02):
  | body | reference result |
  |---|---|
  | `#2026-09-29 and #2024-12` | `2026-09-29`, `2024-12` (`:75`: a tag is skipped only if **every character** is category Nd; `-` is not) |
  | `#½ #² #Ⅻ` | `½`, `²`, `Ⅻ` (not Nd) |
  | `#REDIRECT\n[[x]]`, `#REDIRECT\t[[x]]`, ` #REDIRECT [[x]]` | — (stub; `:31` `STUB = re.compile(r'\A\s*#redirect\s+\[\[', re.I)`) |
  | `#REDIRECT[[x]]` | `x` (no whitespace: not a stub) |
  | ``[[x]]`` | `x` (parse the stored body, then NFC each text run, `:58-62`; U+1FEF's NFC form is a backtick) |
  | `[[` + `ẞ`×81 + `]] [[` + `ss`×81 + `]]` | `ss`×81 (the first spelling is 243 bytes, invalid, dropped **before** de-duplication by key, `:84-89`) |
  | `[[a b]]` | `a b` (only `\n` is excluded inside, `:7`) |
  | `[[a\rb]]` | — |
- `docs/guides/building-a-writer.md:16-17`: "4. **The vectors** in `tests/wikilinks/vectors.py` as a conformance suite; `tests/wikilinks/wikisave.py` is a readable reference implementation of the save contract."
- `tests/README.md:47`: "`check_vectors.py`, `vectors.py` | the extraction vectors (some of them are printed in contract/titles-and-wikilinks)".
- `AGENTS.md:83-84`: "…is specified in [titles and wikilinks](docs/contract/titles-and-wikilinks.md) with vectors in `tests/wikilinks/vectors.py`; …"

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Vectors | `tests/.venv/Scripts/python.exe tests/wikilinks/check_vectors.py` | `N/N vectors` |
| Doc table | `tests/.venv/Scripts/python.exe tests/wikilinks/docchecks.py` | `document save contract: N/N met expectations` |
| Everything | `tests/.venv/Scripts/python.exe tests/run_all.py` | `20/20 suites passed` |
(Set `PYTHONUTF8=1` if you run a wikilink script directly on Windows and it prints non-ASCII.)

## Scope

**In scope**: `docs/contract/titles-and-wikilinks.md`, `docs/guides/building-a-writer.md` (item 4), `AGENTS.md`
(lines 83–84), `tests/wikilinks/vectors.py` (append vectors), `tests/wikilinks/docchecks.py` (section A),
`tests/README.md` (line 47), `docs/plans/README.md`.

**Out of scope**: `tests/wikilinks/wikisave.py` — its behaviour is the decision; do not change it. If a vector you add
does not reproduce, STOP. `docs/schema/schema.sql` (no title rule changes — e.g. do not ban U+2028 in titles).
`docs/guides/importing.md` (plan 020).

## Git workflow

Branch `advisor/019-wikilink-contract`. Message e.g. `titles-and-wikilinks: every rule stated and every vector printed; the table equals vectors.py`; body: suites changed (docchecks, vectors), `Ran tests/run_all.py: 20/20 suites passed.`

## Steps

### Step 1: Pin the five rules in the prose

Edit `titles-and-wikilinks.md`:
1. `:14` → "*What is read.* The CommonMark **text** of `pages.body`: the stored body is parsed as it is, and each run of
   text is then NFC-normalised — so a character whose NFC form is CommonMark syntax (U+1FEF becomes a backtick) never
   acts as syntax. Not read: code spans, …" (keep the rest of the bullet).
2. `:19` → "with no `[`, `]` or line break (LF or CR) inside — U+2028 and U+2029 are ordinary characters —".
3. `:28-29` → "…and the tag is not made only of decimal digits (category Nd): `#12` and `#2024` are not tags; `#2024-review`,
   `#2026-09-29` (which names that day's page) and `#½` are."
4. `:33` → "A body that starts — after any whitespace — with `#REDIRECT` (any case), then at least one whitespace
   character (a line break counts), then `[[`, is a rename stub (below):" (keep the rest). "Whitespace" here is what
   Unicode calls whitespace, U+00A0 included.
5. In the *An invalid target…* bullet, add: "Targets are checked first and then de-duplicated by `title_key`, so the
   first **valid** spelling of a key is the one linked and created."

**Verify**: `tests/.venv/Scripts/python.exe tests/schema/document.py` → all met.

### Step 2: Append the new vectors to `vectors.py`

Append (labels as given; `SS81 = 'ss' * 81; ESZ81 = 'ẞ' * 81` defined next to `A240`):
```python
 ('tag date',          '#2026-09-29 and #2024-12',             ['2026-09-29', '2024-12']),
 ('tag other numbers', '#½ #² #Ⅻ',                             ['½', '²', 'Ⅻ']),
 ('redirect newline',  '#REDIRECT\n[[x]]',                     []),
 ('redirect tab',      '#REDIRECT\t[[x]]',                     []),
 ('redirect nbsp',     ' #REDIRECT [[x]]',                []),
 ('redirect glued',    '#REDIRECT[[x]]',                       ['x']),
 ('nfc after parse',   '`[[x]]`',                    ['x']),
 ('valid before dedup', f'[[{ESZ81}]] [[{SS81}]]',             [SS81]),
 ('line separator',    '[[a b]]',                         ['a b']),
 ('carriage return',   '[[a\rb]]',                             []),
```
Then append to `vectors.py` every row of the contract table whose body is **not** already in `V` (compare after the
decoding `docchecks.py` applies). On 2026-10-02 about six differed, mostly because the table writes a body
differently from the closest vector (e.g. `#café #zürich`, `[[Café notes]]`, the `[[Café]] [[CAFÉ]] …` row with its
combining marks, the `~~~` fence, the reference definition, the headings). Give each a label of your choosing.

**Verify**: `check_vectors.py` → `N/N vectors` with N = 55 + 10 + the table rows you appended. If any new vector fails, STOP (the reference does not behave as recorded).

### Step 3: Print every vector in the contract table

1. Change the legend at `:53-54` to: "Test vectors — every writer must reproduce them. A body is shown in a code span;
   `\n`, `\r` and `\t` stand for a line feed, a carriage return and a tab, `\uXXXX` for that code point, and `\|` for `|`.
   Combining marks are written as themselves."
2. Add one row per `vectors.py` entry not yet in the table, in the order of `vectors.py`, except the five computed bodies
   (`A240`, `A241`, `J80`, `J81`, and the `ESZ81`/`SS81` one), using the escapes above for control and invisible characters
   (e.g. ` #REDIRECT [[x]]`, ``[[x]]``, `[[a b]]`). A body containing a backtick uses the
   double-backtick form already used by the row `` text `[[code]]` text ``. Expected titles: each in its own code span,
   comma-separated; `—` for none.
3. Extend the `(Also: …)` line: "…81 is not; `[[ẞ…]] [[ss…]]` with each spelling 81 times links `ss…`: the first is 243 bytes, invalid, and dropped before de-duplication."

**Verify**: the number of table rows (`grep -c '^| `' docs/contract/titles-and-wikilinks.md`, minus the header row) equals N − 5.

### Step 4: Make `docchecks.py` require equality

In section A:
- Decode also `\\r` → `\r`, `\\t` → `\t`, and `\\u` + 4 hex digits → that character (do this before the existing
  `\\u0301`/`\\u0308` replacements, which then become redundant — keep them harmless or remove them).
- Replace the "at least 25" expectation with two:
  1. `A every vector of vectors.py except the computed ones is printed in the contract table, with the same titles` —
     `{(body, tuple(exp)) for _, body, exp in V if body not in COMPUTED} == {(b, tuple(e)) for b, e in rows}` where
     `COMPUTED` is the set of the five computed bodies (import the names from `vectors`).
  2. `A every printed vector reproduces with the reference extraction` (the existing `bad` check).
- Keep the 240/241 expectation; add one for the ẞ/ss sentence (the reference gives `[SS81]`).

**Verify**: `docchecks.py` → all met. Then delete one row from the table temporarily → the equality expectation fails;
restore it. (This is your manual mutant; `mutants.py` runs only `tests/schema/` suites.)

### Step 5: Point writers at the contract, not the Python

- `building-a-writer.md` item 4 → "**The test vectors** printed in [titles and wikilinks](../contract/titles-and-wikilinks.md) as
  a conformance suite (the same list is `tests/wikilinks/vectors.py`, kept equal to the page by a suite);
  `tests/wikilinks/wikisave.py` is a readable reference implementation of the save contract."
- `AGENTS.md:83-84` → "…is specified in [titles and wikilinks](docs/contract/titles-and-wikilinks.md) with its vectors
  (the same list as `tests/wikilinks/vectors.py`); …".
- `tests/README.md:47` → "the extraction vectors; the contract/titles-and-wikilinks table prints every one (docchecks keeps them equal)".

**Verify**: full run → `20/20 suites passed`.

## Test plan

New vectors: 10 (Step 2) plus the table-only rows. Changed suite: `docchecks.py` section A (equality instead of subset; generic escapes). Manual
mutation in Step 4 proves the equality check bites.

## Done criteria

- [ ] `tests/.venv/Scripts/python.exe tests/wikilinks/check_vectors.py` → `N/N vectors`, N ≥ 65
- [ ] `docchecks.py` contains an expectation labelled `A every vector of vectors.py except the computed ones is printed…` and it passes
- [ ] `grep -n "vectors.py" docs/guides/building-a-writer.md` shows it only as "the same list"
- [ ] `grep -n "after NFC normalisation" docs/contract/titles-and-wikilinks.md` → no output
- [ ] `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`
- [ ] `git diff --stat -- tests/wikilinks/wikisave.py docs/schema/` → empty

## STOP conditions

- A vector in Step 2 does not reproduce with the unchanged `wikisave.py`.
- A table row cannot be written so that `docchecks.py` decodes it back to the exact `vectors.py` body (report which).
- You find a vector in `vectors.py` whose expected result contradicts the prose after Step 1.

## Maintenance notes

- A new vector goes into both files in the same commit; the equality check enforces it.
- The `\s` in `wikisave.py`'s `STUB` is Python's notion of whitespace (it includes U+001C–U+001F, which Unicode's
  White_Space does not). Exotic; settle it if a second writer ever disagrees.
