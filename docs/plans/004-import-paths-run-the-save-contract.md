# Plan 004: §6.15 must send imported bodies through the wikilink save contract

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 6058f24..HEAD -- SCHEMA.md tests/`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none (execute after 002/003 if possible — both edit `mutants.py` and `tests/README.md`)
- **Category**: docs / correctness of the cookbook (the document teaches a contract-breaking write)
- **Planned at**: commit `6058f24`, 2026-10-02

## Why this matters

D19 and §2.4 make the save contract absolute: "saving a page body … the
page's `links(kind='wikilink')` rows, made equal to the set of pages the
body names". §6.15 — the cookbook's import path — teaches `INSERT INTO
pages(… body)` and `UPDATE pages SET body = …` for re-runs of a changed
note, and never mentions the link sync. An importer implemented from §2+§3+§6
alone (the document's own promise to third-party writers: "the contract
they implement is `SCHEMA.md` and nothing else") writes bodies whose
`wikilink` links drift from day one. The drift is silent until someone
rebuilds; D19 says a rebuild repairs it, but the cookbook must not teach
creating it. One sentence in §6.15's closing paragraph closes the gap; a
phrase check in `document.py` plus a mutant keeps it closed.

## Current state

- `SCHEMA.md` §6.15 (lines ~1913–1934). The section's closing paragraph
  after the SQL block reads:

  ```
  A run that inserts nothing the second time is the check of §2.7 step 5. `import_key` never changes
  (`entities_provenance_fixed`), so the key found on the next run is the key written on the first.
  ```

  The SQL block above it ends with the UPDATE:

  ```sql
  -- a later run finds the note changed: update the live page that has the key; a tombstoned one stays gone
  UPDATE pages
     SET body = 'Feed the starter the night before; 75% water.'
   WHERE id = (SELECT id FROM entities
                WHERE source = 'import:vault' AND import_key = :import_key AND deleted_at IS NULL);
  ```

  (Note: the example body contains no `[[wikilinks]]`, which is exactly why
  the omission is easy to miss — a real vault note usually does.)

- `tests/schema/document.py` — owns "the rules live in the file / current
  truth" checks on the document text. It uses helpers from
  `tests/lib/kit.py`: `section(start, end)` returns the document text
  between two heading markers; expectations are `S.K('label', <bool>, <detail>)`.
  Existing example (document.py:14):

  ```python
  S.K('the 2075 table has at least 20 questions, numbered 1..n without a gap', len(rows) >= 20 and [int(r[0]) for r in rows] == list(range(1, len(rows) + 1)), [r[0] for r in rows])
  ```

- `tests/schema/mutants.py` — broken copies of the document; the `mutate(old, new, nth=0)`
  helper and entry format as in plan 002. The suite key must be a script in
  `tests/schema/` (the runner executes `f'{suite}.py'` there), so the owning
  suite for this mutant is `document`, not the wikilinks suites.
- Related but **different** text (do not confuse): §6.1 already says
  "the body names [[Lifelog]]: the link sync of §6.13 runs here, inside this
  same transaction" — §6.1 is fine; §6.15 is the gap.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| The full suites | `tests/.venv/Scripts/python.exe tests/run_all.py` (Windows; `python3 tests/run_all.py` elsewhere) | `20/20 suites passed`; mutants one higher than before |

No Go commands: no DDL change. (§6 cookbook changes don't require the app
gate; if `app/` still exists and you want belt-and-braces, run
`cd app && go test ./...`; if deleted, skip.)

## Scope

**In scope** (the only files you should modify):
- `SCHEMA.md` (§6.15, one added sentence)
- `tests/schema/document.py` (one new expectation)
- `tests/schema/mutants.py` (one new mutant)
- `tests/README.md` (mutants count, +1)
- `plans/README.md` (status row)

**Out of scope** (do NOT touch, even though they look related):
- §6.13's save-flow SQL and diagram — the contract itself is correct.
- §6.15's SQL block — do not bolt the sync INTO the SQL; the sync is §6.13's
  procedure, and §6.15 should reference it, not duplicate it (one home per
  rule). Only the closing prose changes.
- `tests/wikilinks/docchecks.py` — it validates §6.13's SQL, which is
  unchanged; also it cannot be a mutants target (it lives in `tests/wikilinks/`).
- `app/`, `AGENTS.md`, `fixes.md`.

## Git workflow

- One commit on `master`, repo message style, e.g.
  `SCHEMA.md: an imported body is a save like any other — §6.15 runs the §6.13 link sync`.
  Say what you ran (`tests/run_all.py`, 20/20, mutants N/N).
- Do NOT push unless the operator instructs you to.

## Steps

### Step 1: Add the sentence to §6.15

In `SCHEMA.md` §6.15's closing paragraph, append after
"…the key found on the next run is the key written on the first.":

```
A body an import writes or changes is a save like any other (D19): run the
link sync of §6.13 in the same transaction — create or resolve each target
the body names, drop the links it no longer names — so
`links(kind='wikilink')` stays equal to the body.
```

(Reflow as one paragraph to match the surrounding line width. The phrase
`run the link sync of §6.13` is load-bearing: Step 2's check and Step 3's
mutant key on it, and it is distinct from §6.1's "the link sync of §6.13
runs here".)

**Verify**: `grep -c "run the link sync of §6.13" SCHEMA.md` → `1`

### Step 2: Guard it in document.py

In `tests/schema/document.py`, add an expectation (place it near the other
text-level checks, after the 2075 block):

```python
S.K('§6.15 sends an imported body through the save contract (D19)', 'run the link sync of §6.13' in section('### 6.15 ', '### 6.16 '))
```

`section` is already imported (`from kit import *` at the top of
document.py; it is `tests/lib/kit.py:174`).

**Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` → the
`document` suite passes with one more expectation (80 instead of 79).

### Step 3: Add the mutant and bump the count

In `tests/schema/mutants.py`, add:

```python
 ('document', '§6.15 bypasses the save contract', mutate('run the link sync of §6.13', 'skip')),
```

(Unique target — §6.1's sentence words it differently — so `nth=0` is
correct. With the sentence gone from §6.15, the document expectation fails:
the suite notices.)

In `tests/README.md`, increment the mutants count (read the current number
first; plans 002/003 may already have raised it).

**Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` →
`20/20 suites passed`, the mutants line one higher, and the mutant named
`§6.15 bypasses the save contract` printed as `caught`.

## Test plan

- New expectation in `tests/schema/document.py` (Step 2): §6.15's section
  text contains the save-contract reference.
- New mutant in `tests/schema/mutants.py` (Step 3): removing the sentence
  must fail the document suite.
- Model after: document.py's 2075 phrase checks and the existing
  `'document', 'a 2075 answer is gone from the file'` mutant.

## Done criteria

Machine-checkable. ALL must hold:

- [ ] `grep -c "run the link sync of §6.13" SCHEMA.md` → `1`
- [ ] `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`,
      mutants one higher, the new mutant listed `caught`
- [ ] The §6.15 SQL block is byte-identical to before (`git diff SCHEMA.md`
      shows only the closing paragraph changed)
- [ ] No files outside the in-scope list are modified (`git status`)
- [ ] `plans/README.md` status row updated

## STOP conditions

Stop and report back (do not improvise) if:

- The excerpts don't match the live files (drift since `6058f24`).
- `grep -c "run the link sync of §6.13" SCHEMA.md` returns 2 — another
  section already uses the exact phrase; report and pick distinct wording
  for §6.15 (then update Steps 2–3 to match).
- The mutants runner's `assert n > nth` fires — the target string is
  already gone or doubled; do not mutate a neighbouring phrase instead.

## Maintenance notes

- If §6.15 ever grows a *second* write path (e.g. importing people by key),
  the same rule applies to every body write; the expectation only guards
  the one sentence, so extend it if the section grows.
- Reviewer should check that the sentence cites D19 and §6.13 (the rule's
  homes) without restating the save contract's steps (one home per rule).
