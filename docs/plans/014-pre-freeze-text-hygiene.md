# Plan 014: Make the text the freeze will keep forever say only true things

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 4d84261..HEAD -- docs/schema/schema.sql docs/contract/time.md docs/contract/threat-model.md docs/decisions/D05-pages-and-day-pages.md docs/decisions/D08-entities-and-links.md docs/decisions/D13-migrations-and-freeze.md docs/decisions/D17-contract-as-data.md docs/decisions/D23-no-tasks.md docs/decisions/D24-habits.md docs/architecture/goals-and-principles.md docs/architecture/non-goals.md docs/research/references.md AGENTS.md`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live files before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none (run before 015–021 because it touches `AGENTS.md` and `schema.sql` text first)
- **Category**: docs
- **Planned at**: commit `4d84261`, 2026-10-02

## Why this matters

This repo is a schema-first project: the product is the documentation tree `docs/` describing a SQLite database
`life.db`, and `docs/schema/schema.sql` is the one canonical DDL. Until the "freeze" (the first real data in a canonical
`life.db`, decision D13) the DDL is edited in place; after it, **anything still in the schema stays for good** — and the
comments *inside* `CREATE` statements and the seed rows of `lifelog_meta`/`link_kinds` are stored in every database file
(`.schema` prints them; a stranger in 2075 reads them with no other docs). Commit `c82752d` split the old single
`SCHEMA.md` into `docs/`, and left (a) four references to `SCHEMA.md` section numbers that no longer exist — one of them
inside seed **row data**, (b) two stored rules that contradict the rest of the contract, and (c) a handful of links and
words in the docs tree that now point at the wrong page or state a wrong fact. All are text-only fixes; this plan makes
them before they become permanent.

## Current state

Repo conventions you must honour (from `AGENTS.md`):
- **One home per rule.** A table's rule is its constraint/trigger plus a comment *inside* its `CREATE` statement in
  `schema.sql`; a rule spanning tables is a `lifelog_meta` row; `docs/decisions/` say *why* and cite constraint names.
- **Current truth only** in `docs/` outside `issues/`, `rfcs/`, `plans/`: no "previously…" or history notes. Rewrite in place.
- Pages link each other with relative markdown links; no section numbers. `tests/schema/document.py` fails if a
  relative link or `#anchor` does not resolve.
- The 2075 test (`docs/contract/threat-model.md`, table at the bottom) is executed by `tests/schema/document.py`:
  for each row, the named `lifelog_meta` key / object's text must contain each phrase in the last column. **Keep
  those phrases** when rewording a row (e.g. the `days` row must keep `LOCAL`, `never recomputed`, `IS, not =`;
  the `evolution` row must keep `additive`, `user_version`; the `sqlite` row `3.51.3`, `3.53`).
- `tests/schema/writers.py:44` asserts that the DDL header (the text before `PRAGMA application_id`) contains
  `3.51.3`, `foreign_keys = ON`, `recursive_triggers = ON`, `synchronous = FULL`, `trusted_schema = OFF`, `BEGIN IMMEDIATE`. Keep them.

### A. Dead section references in `docs/schema/schema.sql`

```
8:   -- and every write transaction starts with BEGIN IMMEDIATE (section 2.6).
44:  -- DO NOTHING RETURNING id: no id back = imported before, so no domain row is inserted (section 6.15).
113: -- (Zurich finds Zürich); a CJK run is ONE token (section 7).
281:   ('located-in', 0, 'place',   'place',        'containment: Tokyo → Japan; transitive — walk it with a recursive CTE (section 6.11)'),
```
Line 8 is a header comment (not stored). Lines 44 and 113 are inside `CREATE` statements (stored). Line 281 is a
`link_kinds.note` value (row data in every database).

### B. Two stored `lifelog_meta` rows that contradict the contract (`schema.sql:24`, `:29`)

```
24:  ('days',      'every *_day column (and day) is the LOCAL calendar date YYYY-MM-DD where the thing happened, written at insert, never recomputed from an instant; CHECK date(x) IS x (IS, not =: a CHECK passes on NULL, and date(''2026-9-3'') is NULL)'),
29:  ('evolution', 'after the first real data: numbered forward-only SQL migrations, additive only, counted in PRAGMA user_version; every CHECK is named, so any rule can be widened or tightened with ALTER TABLE DROP/ADD CONSTRAINT');
```
- `days` says "written at insert", but `habit_periods.end_day` is written by `UPDATE` when a habit stops
  (`docs/cookbook/habits.md`: `UPDATE habit_periods SET end_day = :day …`), and `people.birth_day`/`death_day` are
  editable. The true rule is "written by the app from the local calendar, never recomputed from an instant".
  `docs/contract/time.md:10-12` repeats the same "written at insert time in the zone of the device" — the point there is
  *the zone*, not insert-vs-update.
- "additive" is defined three different ways:
  - `docs/architecture/goals-and-principles.md:24-25` (principle 4): "schema changes are `ADD COLUMN` / `CREATE TABLE` / new indexes and numbered forward-only migrations."
  - `AGENTS.md:51-53`: "changes are additive-only (`ADD COLUMN` / `CREATE TABLE` / new indexes — principle 4 of the goals)".
  - `docs/decisions/D13-migrations-and-freeze.md:8-9`: "additive only (new tables, columns, indexes; a column rename is allowed and recorded in its migration)", and its next bullet allows replacing any named CHECK (`DROP CONSTRAINT` / `ADD CONSTRAINT`), "tightening is as safe as loosening".
  D13 is the home (it is the migrations decision). Principle 4 and AGENTS must point at it instead of listing statements.

### C. Wrong links / wrong facts in the docs tree

| file:line | now | should be |
|---|---|---|
| `docs/decisions/D17-contract-as-data.md:13` | `**The 2075 test** ([imports](../contract/imports.md))` | `([threat model and the 2075 test](../contract/threat-model.md))` |
| `docs/decisions/D17-contract-as-data.md:16` | `deliberately not encrypted ([imports](../contract/imports.md))` | `([threat model](../contract/threat-model.md))` |
| `docs/research/references.md:232` (end of R67) | `→ [imports](../contract/imports.md).` | `→ [threat model](../contract/threat-model.md).` |
| `docs/contract/threat-model.md:13` | `no credentials or account numbers, ever ([connection setup](connections.md))` | `([non-goals](../architecture/non-goals.md))` — the rule's row is "Storing account numbers, IBANs, credentials" there |
| `docs/architecture/goals-and-principles.md:33` | `written once (the reading guide above)` | `written once ([the docs index](../README.md) says where each kind of rule lives)` |
| `docs/decisions/D08-entities-and-links.md:5` | `The four linkable types share one ID space` | `The three linkable types (page, person, place, `entities_entity_type`) share one ID space` |
| `docs/decisions/D05-pages-and-day-pages.md:5-6` | `an essay, a reference\n  page, a tag, a person, and the journal.` | `… a tag, a person, a place, and the journal.` |
| `docs/decisions/D23-no-tasks.md:17` | `A habit is still a 0/1 metric ([D15](D15-recurrence.md))` | `([D24](D24-habits.md))` |
| `docs/decisions/D24-habits.md:9` | `` (`habit_periods_check_insert`, `_check_update`) `` | `` (`habit_periods_check_insert`, `habit_periods_check_update`) `` |
| `docs/architecture/non-goals.md:34` | `` a trigger on `UPDATE OF type ON entities` `` | `` a trigger on `UPDATE OF entity_type ON entities` `` (the column is `entity_type`, `schema.sql:46`) |
| `docs/architecture/product-concepts.md:19` | `` `source` on every entity, link and measurement `` | `` `source` on every entity, link, measurement and habit period `` |
| `docs/contract/threat-model.md:10` | `` `source` on every row `` | `` `source` on every entity, link, measurement and habit period `` |

### D. AGENTS.md says `trusted_schema` is read back; the contract does not

`AGENTS.md:135-137`: "`PRAGMA foreign_keys=ON`, `recursive_triggers=ON`, `synchronous=FULL`, `trusted_schema=OFF`, read
back and refused if wrong". The home, `docs/contract/connections.md:13-14`: "A writer reads these back at connect time
and refuses to run if `foreign_keys` or `recursive_triggers` is 0 or `synchronous` is not 2". AGENTS.md is a checklist,
not a home, so it must match the contract, not extend it.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| All suites (Windows) | `tests/.venv/Scripts/python.exe tests/run_all.py` | last line `20/20 suites passed` |
| All suites (elsewhere) | `python3 tests/run_all.py` | same |
| One suite | `tests/.venv/Scripts/python.exe tests/schema/document.py` | `document: N/N met expectations` |

The full run takes ~45 s (mutants ~35 s).

## Scope

**In scope** (the only files you should modify):
- `docs/schema/schema.sql` (lines 8, 24, 29, 44, 113, 281 only)
- `docs/contract/time.md`, `docs/contract/threat-model.md`
- `docs/decisions/D05-pages-and-day-pages.md`, `D08-entities-and-links.md`, `D13-migrations-and-freeze.md`, `D17-contract-as-data.md`, `D23-no-tasks.md`, `D24-habits.md`
- `docs/architecture/goals-and-principles.md`, `docs/architecture/non-goals.md`, `docs/architecture/product-concepts.md`
- `docs/research/references.md` (line 232 only)
- `AGENTS.md` (the two passages quoted above only)
- `docs/plans/README.md` (status row)

**Out of scope**:
- Any constraint, trigger, index, column or `link_kinds` structure in `schema.sql` — text only. No rule changes.
- The "Agent CLI/API" row of `non-goals.md` — whether it is cut or merely not built yet is the owner's call.
- `docs/plans/0*.md` — dated records; they keep their `§` references on purpose.
- `tests/` — no suite should need to change. If one does, that is a STOP condition.

## Git workflow

- Branch: `advisor/014-pre-freeze-text-hygiene`
- One commit is fine. Message style matches the repo (`<area>: <what is now true>` + body naming what you ran), e.g.
  `docs: the text the freeze keeps names no dead section and contradicts nothing` and a body ending with
  `Ran tests/run_all.py: 20/20 suites passed.`
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Remove the four section references from `schema.sql`

- Line 8: replace `(section 2.6).` with `(docs/contract/connections.md).` (header comment; not stored, may name a doc).
- Line 44: delete ` (section 6.15)` so the line ends `…so no domain row is inserted.`
- Line 113: replace `a CJK run is ONE token (section 7).` with `a CJK run is ONE token (a known limit of unicode61).`
- Line 281: replace the note `'containment: Tokyo → Japan; transitive — walk it with a recursive CTE (section 6.11)'`
  with `'containment: Tokyo → Japan; transitive — walk it with a recursive CTE'`.

**Verify**: `grep -n "section [0-9]" docs/schema/schema.sql` → no output.

### Step 2: Make the `days` row and `time.md` say what is true

- `schema.sql:24`: change `where the thing happened, written at insert, never recomputed from an instant;` to
  `where the thing happened, written by the app from the local calendar, never recomputed from an instant;`.
  Keep the rest of the value byte-identical (note the doubled `''` quotes inside the SQL string).
- `docs/contract/time.md:11`: change `written at insert time in the zone of the device that captured` to
  `written by the app in the zone of the device that captured`.

**Verify**: `grep -n "written at insert" docs/schema/schema.sql docs/contract/time.md` → only the `source` row
(`schema.sql:26`, `…written at insert, never changed…`) remains — that one is true and is a 2075-test phrase; leave it.

### Step 3: Give "additive" one home (D13)

- In `D13-migrations-and-freeze.md:8-9`, rewrite the parenthesis so it is the definition:
  `additive only — new tables, columns and indexes, a column rename (recorded in its migration), and replacing a named CHECK (next bullet); never a dropped table or column`.
- `goals-and-principles.md:24-25` (principle 4): replace "`schema changes are `ADD COLUMN` / `CREATE TABLE` / new indexes and numbered forward-only migrations.`" with
  "`schema changes are additive ([D13](../decisions/D13-migrations-and-freeze.md) says what that allows), in numbered forward-only migrations.`" Keep the SQLite quotation and `[R1]` that follow.
- `AGENTS.md:52-53`: replace "`changes are additive-only (`ADD COLUMN` / `CREATE TABLE` / new indexes — principle 4 of the [goals](docs/architecture/goals-and-principles.md))`" with
  "`changes are additive-only ([D13](docs/decisions/D13-migrations-and-freeze.md) defines it; principle 4 of the [goals](docs/architecture/goals-and-principles.md))`".
- `schema.sql:29` (`evolution` row): change the value to
  `'after the first real data: numbered forward-only SQL migrations, additive only (new tables, columns and indexes; a named CHECK may be replaced with ALTER TABLE DROP/ADD CONSTRAINT, so every CHECK is named), counted in PRAGMA user_version'`.
  It must still contain `additive` and `user_version`.

**Verify**: `tests/.venv/Scripts/python.exe tests/schema/document.py` → `document: N/N met expectations` with N/N equal.

### Step 4: Fix the links and facts in table C

Apply every row of table C exactly. For the threat-model `source` change, keep the rest of the cell unchanged.

**Verify**:
- `grep -n "four linkable\|reading guide above\|UPDATE OF type ON\|_check_update\`" docs -r --include=*.md | grep -v docs/plans/` → no output.
- `grep -n "imports.md" docs/decisions/D17-contract-as-data.md` → no output.
- `tests/.venv/Scripts/python.exe tests/schema/document.py` → all expectations met (links resolve).

### Step 5: Align AGENTS.md's connection checklist with the contract

`AGENTS.md:135-137`: change "`trusted_schema=OFF`, read back and refused\n  if wrong" to
"`trusted_schema=OFF` (the first three read back and refused if wrong)". Do not add `trusted_schema` to
`connections.md`'s read-back list — that would be a new rule.

**Verify**: `grep -n "read back" AGENTS.md` → the one edited line.

### Step 6: Full run

**Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`; `git status --short` lists only in-scope files.

## Test plan

No new tests: the edits are text, and the existing suites already cover what matters — `document.py` (links,
anchors, the 2075 phrases), `writers.py:44` (header phrases), `diagrams.py` (the `link_kinds` rows equal the link
map), `mutants.py` (70 mutants still caught; some mutate `schema.sql` by exact text — if one stops being caught
because its anchor text changed, that is a STOP).

## Done criteria

- [ ] `grep -rn "section [0-9]" docs/schema/schema.sql` → no output
- [ ] `grep -n "written at insert" docs/schema/schema.sql` → exactly one line (the `source` row)
- [ ] `grep -rn "four linkable\|reading guide above\|UPDATE OF type ON" docs --include=*.md | grep -v docs/plans/` → no output
- [ ] `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`, mutants `70/70`
- [ ] `git status --short` shows only in-scope files
- [ ] `docs/plans/README.md` row 014 updated

## STOP conditions

- An excerpt above does not match the live file.
- A suite fails because it asserts the old text (e.g. a mutant in `tests/schema/mutants.py` anchors on
  `(section 6.15)` or the old `days`/`evolution` wording). Report the suite and line; do not edit `tests/`.
- `document.py` reports a 2075 phrase missing after Step 2 or 3.

## Maintenance notes

- After the freeze, `lifelog_meta` rows and `CREATE` comments can only change by migration; review any future edit of
  them with the 2075 table open.
- Not done here, for the owner: the `non-goals.md` "Agent CLI/API" row reads as "cut" while D14 and principle 3 make the
  CLI/API part of the writer. Decide whether to delete it or rephrase it as sequencing.
