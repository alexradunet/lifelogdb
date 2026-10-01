# Plan 001: State the freeze gate's current truth — the trial import happened

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 6058f24..HEAD -- SCHEMA.md`
> If SCHEMA.md changed since this plan was written, compare the
> "Current state" excerpts against the live file before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: docs (self-consistency of the contract document)
- **Planned at**: commit `6058f24`, 2026-10-02

## Why this matters

`SCHEMA.md` is the product, and its first line tells the reader what to do
next. That line, and §2.7 step 6, say a real import "is the one test this
schema has never had" — but four decisions in the same document (D5, D22,
D23, D24) already cite "the first real import, 2026-10, an Obsidian vault"
as an accomplished fact that reshaped the design. The document contradicts
itself about its own next step. The intended distinction is: a *trial import
into a copy* (done, 2026-10, via `VACUUM INTO`) versus the *first import into
the canonical `life.db`* (not done — no canonical file exists). Both spots
must be rewritten to that truth.

## Current state

- `SCHEMA.md` — the only file in scope. 2,308 lines, §1–§8. Its suites live
  in `tests/` and are run by `tests/run_all.py`.
- `SCHEMA.md:3` (the Status line, first line of the file after the title):

  ```
  **Status:** freeze candidate. No canonical database exists yet; until one does, §3 is edited in place (D13). The next step is the capture path and one real import (§2.7), not another review.
  ```

- `SCHEMA.md:482–483` (§2.7 step 6, the last numbered item before "## 3."):

  ```
  6. **Before the freeze**, run steps 1–5 once with a real export on a copy: a real import is the one
     test this schema has never had.
  ```

- The contradicting narratives (do **not** change these; they are the truth
  being reconciled to): `SCHEMA.md:1137` (D5: "The first real import, an
  Obsidian vault of one note per day"), `SCHEMA.md:1503` (D22), `SCHEMA.md:1530`
  (D23: "the first real import, 2026-10"), `SCHEMA.md:1553` (D24).
- Repo rule that governs this edit (AGENTS.md): *"Current truth and nothing
  else … When a decision changes, rewrite it in place."* A prose-only change;
  no DDL, no diagram, no suite behaviour changes.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| The full suites | `tests/.venv/Scripts/python.exe tests/run_all.py` (Windows; `python3 tests/run_all.py` elsewhere) | last line `20/20 suites passed` (~20 s) |

No Go commands: this plan does not touch §3's DDL. (If `app/` has already
been deleted from the repo, that is expected — it is not needed here.)

## Scope

**In scope** (the only files you should modify):
- `SCHEMA.md` — two prose edits (the Status line; §2.7 step 6)
- `plans/README.md` — the status row for this plan

**Out of scope** (do NOT touch, even though they look related):
- The decision texts D5, D22, D23, D24 — they are already correct.
- `tests/` — no suite reads either sentence; nothing to change.
- `fixes.md` at the repo root — the owner deletes it when the plans land.
- Any `app/` file; `AGENTS.md`.

## Git workflow

- Commit on the current branch (`master`), one commit. Repo message style is
  a lowercase subject naming the file and the change, e.g.
  `SCHEMA.md: the 2026-10 trial import already ran; the next step is the canonical one`.
  Say in the message what you ran (`tests/run_all.py`, 20/20).
- Do NOT push unless the operator instructs you to.

## Steps

### Step 1: Rewrite the Status line (`SCHEMA.md:3`)

Replace the whole line with:

```
**Status:** freeze candidate. No canonical database exists yet; until one does, §3 is edited in place (D13). The 2026-10 trial import (a real vault, into a copy — §2.7) has already taught D5, D22, D23 and D24; the next step is the capture path and the first import into the canonical `life.db`, not another review.
```

Keep it a single line, as now.

**Verify**: `grep -c "not another review" SCHEMA.md` → `1`

### Step 2: Rewrite §2.7 step 6 (`SCHEMA.md:482–483`)

Replace the two-line item with:

```
6. **Before the freeze**, run steps 1–5 once with a real export into the canonical file: the
   2026-10 trial — a real vault, imported into a copy — already taught D5, D22, D23 and D24; an
   import into `life.db` itself is the one test this schema has never had.
```

**Verify**: `grep -n "never had" SCHEMA.md` → exactly one hit, and its line
number is inside §2.7 (between the "## 2.7" heading and "## 3.").

### Step 3: Run the suites

**Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` →
`20/20 suites passed`. The suites must pass unchanged — this edit is prose
only, and `tests/schema/document.py` (current-truth checks) must not flag
the new text. If `document.py` fails, its complaint tells you which phrase
it wants; report back rather than loosening the suite.

## Test plan

No new tests: no rule, DDL object or cookbook block changes. The existing
`document.py` suite is the guard that the document stays internally
consistent; it runs as part of Step 3.

## Done criteria

Machine-checkable. ALL must hold:

- [ ] `grep -n "never had" SCHEMA.md` returns exactly one line, inside §2.7
- [ ] `grep -c "first real import" SCHEMA.md` returns ≥ 4 (D5, D22, D23, D24 — unchanged)
- [ ] `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`
- [ ] `git status` shows changes only in `SCHEMA.md` (+ `plans/README.md`)
- [ ] `plans/README.md` status row updated

## STOP conditions

Stop and report back (do not improvise) if:

- The excerpts at `SCHEMA.md:3` or `:482–483` don't match (the file drifted).
- `document.py` fails after the edit — its "current truth only" checks
  object to the new phrasing; the resolution is the owner's, not a loosened
  suite.
- You find yourself wanting to edit D5/D22/D23/D24 to make the contradiction
  go away the other way — that rewrites history, which the repo forbids.

## Maintenance notes

- The Status line is the document's most-read sentence; keep it truthful on
  every future milestone (the freeze itself, the first canonical import).
- Reviewer should check the two new sentences against D22's and D23's
  narratives — the wording "trial … into a copy" must stay consistent with
  §2.7 step 1 (`VACUUM INTO '/tmp/trial.db'`).
