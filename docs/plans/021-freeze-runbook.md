# Plan 021 (design): Say exactly what the freeze is, what must be true before it, and what changes after it

> **Executor instructions**: This is a **design plan**: it produces draft documentation for the owner to approve,
> not a change to the schema. Follow the steps, run every verification command, and honour the STOP conditions.
> Do not mark this plan DONE yourself: set it to `IN REVIEW (owner)` in `docs/plans/README.md` and list the open
> questions (Step 4) in your report. Only the owner's approval makes it DONE.
>
> **Drift check (run first)**: `git diff --stat 4d84261..HEAD -- docs/decisions/D13-migrations-and-freeze.md docs/process.md docs/README.md docs/contract/imports.md docs/architecture/non-goals.md AGENTS.md`
> Plans 014, 015, 018 and 020 edit some of these files — expected. Re-read each file before editing it.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW (docs only), but every sentence becomes a rule the owner lives with — owner review required
- **Depends on**: 014 (D13's "additive" definition), 020 (the import guide's trial-on-a-copy). Run after all other plans.
- **Category**: direction
- **Planned at**: commit `4d84261`, 2026-10-02

## Why this matters

The project's next milestone is "the freeze": the moment `life.db` holds real data and the schema can only change by
additive, numbered migrations (decision D13). Everything still in `docs/schema/schema.sql` at that moment stays
forever. Yet no page says precisely **when** the freeze happens or **what must be true first**:

- `docs/README.md:3`: "freeze candidate … the next step is the capture path and the first import into the canonical `life.db`".
- `docs/contract/imports.md` step 6: "**Before the freeze**, run steps 1–5 once with a real export into the canonical
  file" — but by D13 and `AGENTS.md` ("Numbered forward-only migrations … begin only after real data exists in a
  canonical `life.db`") that import *is* the freeze. The trigger is circular.
- `docs/architecture/non-goals.md` has a row whose reopen trigger is the freeze itself: "Markdown export of the prose;
  nightly snapshots, restore and an off-box copy; a CSV dump; continuous replication | … Until one exists there is **no
  second copy** of `life.db` … | Before the first real data enters a canonical `life.db` (the freeze, D13) at the latest".
- `docs/plans/README.md` (section "Not planned: the freeze and the next writer"): "Choosing or building the next writer,
  and with it a freeze runbook (which write is the point of no return), is the owner's decision".
- D13 does not say what happens to `schema.sql` itself after the freeze (does it stay the full DDL for new files, or
  become `0001` with migrations on top?). `docs/schema/README.md:3-4`: "there is no `0001_init.sql` file yet".

One insight the runbook should carry (it follows from the import guide): **a file whose every row can be replayed
from import workspaces can still be rebuilt** from a new `schema.sql` and a *replay*. The true point of no return is
the first row that exists nowhere else — a capture typed into the file, a hand correction, a tombstone the owner set.

## Current state

- `docs/decisions/D13-migrations-and-freeze.md` (22 lines) — the home of migrations and the freeze. Its first bullet:
  "**Decision.** No ORM, no migration framework, no down-migrations. **Until the freeze there are no migrations:**
  schema is edited in place and test databases are recreated; `user_version` stays 1. After real data exists:
  numbered plain-SQL files, `db/migrations/0002_*.sql`, … applied in order, progress in `PRAGMA user_version` …;
  additive only (…)." Third bullet: migrations run on a copy first (`VACUUM INTO`) and the four integrity checks must
  pass on the copy.
- `docs/process.md` — "How a change happens" (incident → issue → proposal → decision → schema → plan) and the
  current-truth/records table. It is reachable from `docs/README.md`.
- `AGENTS.md`, section "The one hard rule: no migrations until the schema freeze" — must stay consistent with
  whatever D13 says; it may link D13 instead of restating it ("one home per rule").
- Conventions: `docs/` outside `issues/`, `rfcs/`, `plans/` holds **current truth only** (no history, no "will be";
  write rules in the present tense: "The freeze is …"). Every relative link must resolve (`tests/schema/document.py`).
  Decisions say *why*; they are rewritten in place, numbers stable.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Docs suite | `tests/.venv/Scripts/python.exe tests/schema/document.py` | all met |
| Everything | `tests/.venv/Scripts/python.exe tests/run_all.py` | `20/20 suites passed` |

## Scope

**In scope**: `docs/decisions/D13-migrations-and-freeze.md`, `docs/process.md`, `docs/contract/imports.md` (step 6
wording only), `docs/README.md` (status line only, and only to link the new section), `AGENTS.md` (the hard-rule
section, only to link D13), `docs/plans/README.md`.

**Out of scope**: `docs/schema/schema.sql`; creating `db/migrations/` or any migration file (forbidden before the
freeze); any backup/export/snapshot design (a non-goal — the runbook only names the owner's decision about that row);
writing a writer.

## Git workflow

Branch `advisor/021-freeze-runbook`. Message e.g. `D13, process: what the freeze is, what must hold before it, what changes after`; body: "draft for the owner's review", what you ran.

## Steps

### Step 1: D13 — define the freeze and what follows it

Add a bullet to D13 after the first, titled **The freeze.** Content (present tense, adapt wording to D13's style):
- The freeze is the first write to the canonical `life.db` of a row that cannot be replayed from an import workspace
  (a capture, a correction, a tombstone, anything typed into the file). Before it, a file holding only replayable
  imports is rebuilt — new `schema.sql`, *replay* ([importing with a model](../guides/importing.md)) — instead of migrated.
- At the freeze, the commit of `schema.sql` that made the file is recorded in the status line of `docs/README.md`.
- After it: `schema.sql` is no longer edited in place; each change is a numbered migration under `db/migrations/`,
  starting at `0002_`, run on a copy first (the existing third bullet). How a new file is created after the freeze is
  **Open question Q1** (Step 4) — write the bullet so it states the recommendation, marked in the commit message as
  awaiting the owner.
- Add to "Alternatives": *freeze at the first import* — rejected: an import alone is replayable, so freezing then
  would lock in a schema no unreplayable data depends on yet.

**Verify**: `tests/.venv/Scripts/python.exe tests/schema/document.py` → all met.

### Step 2: `process.md` — the checklist before the freeze

Add a section `## Before the freeze` to `docs/process.md`: a short intro linking D13, then a checklist in which every
item is checkable by the owner from the repo:
1. Every plan in [plans](plans/README.md) is DONE or REJECTED, and every [issue](issues/README.md) is resolved or won't-fix.
2. `tests/run_all.py` is green on a SQLite ≥ 3.53 with FTS5, at the commit of `schema.sql` that will make the file.
3. The owner has decided each non-goal whose "Reopen when" is the freeze ([non-goals](architecture/non-goals.md): the export, snapshot and
   off-box copy row) — kept out, or reopened through an issue.
4. The text the file will keep has been read once more as a stranger would: the comments inside the `CREATE`
   statements, the `lifelog_meta` rows and the `link_kinds` notes ([threat model and the 2075 test](contract/threat-model.md)).
5. The writer that will make the file reproduces every vector of [titles and wikilinks](contract/titles-and-wikilinks.md) and passes the
   four [integrity checks](contract/integrity-checks.md) on a trial.
6. A trial import ran on a copy and its counts are recorded; a second run wrote nothing ([imports](contract/imports.md)).
Then one line: "The freeze itself is the first unreplayable write (D13); the status line of [the docs index](README.md)
records the commit."

### Step 3: Remove the circular trigger

`docs/contract/imports.md` step 6 → "**Before the freeze**, import a real export once into the file that will become
canonical (steps 1–5); while that file holds only replayable imports it can still be rebuilt ([D13](../decisions/D13-migrations-and-freeze.md)). The 2026-10 trial — …"
(keep the rest of the sentence about what the trial taught). In `AGENTS.md`'s hard-rule section, make the bullet that
starts "Numbered forward-only migrations (`0002_*.sql`, …) begin **only after** real data exists" say "begin only after
the freeze ([D13](docs/decisions/D13-migrations-and-freeze.md) says what that is)". In `docs/README.md`'s status line,
link the new process section once ("the checklist before it: [process](process.md#before-the-freeze)").

**Verify**: full run → `20/20 suites passed` (the new anchor `#before-the-freeze` must resolve).

### Step 4: Write the owner's open questions

Add a section `## Open questions for the owner (plan 021)` at the end of `docs/plans/README.md` with:
- **Q1. A new file after the freeze.** (a) `schema.sql` stays the full current DDL, each migration also edits it, and a
  suite proves `schema.sql` ≡ the frozen DDL + migrations (same `sqlite_master`) — *recommended*: a new file is still
  one command; or (b) `schema.sql` is frozen as `0001`, a new file is `0001` + every migration.
- **Q2. `pages_fts_delete`** can never fire while `pages_no_delete` exists. Keep it (a guard for an owner who drops
  that trigger) or cut it before the freeze?
- **Q3.** The non-goal "Agent CLI/API" reads as cut while D14 and principle 3 make it part of the writer — delete or
  reword the row?
- **Q4.** The export/snapshot/off-box non-goal row's reopen trigger is the freeze: decide it (checklist item 3).
- **Q5.** Should readers that open a `life.db` they did not write also set `trusted_schema=OFF`?

**Verify**: `tests/.venv/Scripts/python.exe tests/schema/document.py` → all met.

## Test plan

Docs only; `document.py` checks links and anchors. No suite asserts the checklist itself (it is the owner's).

## Done criteria (for the executor; DONE itself is the owner's)

- [ ] `grep -n "The freeze" docs/decisions/D13-migrations-and-freeze.md` → ≥ 1
- [ ] `grep -n "## Before the freeze" docs/process.md` → 1
- [ ] `grep -n "Open questions for the owner (plan 021)" docs/plans/README.md` → 1
- [ ] `ls db/migrations 2>/dev/null` → nothing (no migrations created)
- [ ] `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`
- [ ] status row 021 = `IN REVIEW (owner)`

## STOP conditions

- Any step seems to need a schema change or a migration file — STOP (forbidden before the freeze).
- You find a page that already defines the freeze differently from Step 1 (report both texts).
- A checklist item would require a backup or export to exist — rephrase it as the owner's decision, or STOP.

## Maintenance notes

- When the owner answers Q1, D13 and `AGENTS.md` change together, and plan 015's preflight (SQLite ≥ 3.53) becomes a
  hard requirement for migrations too.
- After the freeze, `docs/README.md`'s status line stops saying "freeze candidate"; `schema/README.md`'s "there is no
  `0001_init.sql` file yet" changes with Q1's answer.
