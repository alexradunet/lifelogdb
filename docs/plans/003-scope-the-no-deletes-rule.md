# Plan 003: Scope the no-deletes rule to life data; state the registries' own rule

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

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none (execute after 002 if possible — both edit the
  `lifelog_meta` inserts and `mutants.py`)
- **Category**: docs / self-consistency (the stated rule vs the enforced rule)
- **Planned at**: commit `6058f24`, 2026-10-02

## Why this matters

§2.3, the `lifelog_meta` `deletes` row and 2075 question 6 all state
"nothing is deleted except `links` rows". The DDL enforces that on entities,
pages, people, measurements and habit periods — but **not** on the three
registries: on a fresh §3 database, `DELETE FROM lifelog_meta` wipes all
eight rows and *three of the four integrity checks stay clean* (only the
2075 test notices — the in-file documentation, the centerpiece of D17, is
gone); `DELETE FROM metrics` / `DELETE FROM link_kinds` succeed for
unreferenced rows, and unconditionally with `foreign_keys=OFF`. All verified
by execution during the audit.

The chosen resolution is **wording, not triggers**: registry cleanup (a
typo'd metric registered by mistake, an unused link kind) is a legitimate
owner's act, and a registry's rows are administrative, not life data. The
document must say what the DDL does, in the one home each rule has: the
`deletes` meta row (the cross-table rule), a comment inside each registry's
`CREATE` statement (its own rule), and §2.3 (the prose that §2 holds).

*(The alternative — `no_delete` triggers on the three registries — was
considered and rejected: it forbids a real owner act and needs tombstone
machinery the registries don't have. If the owner disagrees, that is a
different plan.)*

## Current state

- `SCHEMA.md:126–130` (§2.3, first bullet):

  ```
  - **Nothing is deleted except `links` rows.** Entities are tombstoned (`entities.deleted_at`), and
    `BEFORE DELETE` triggers reject deleting an `entities` row or any domain row, also on a connection
    that forgot `foreign_keys` (executed). Every read path filters `deleted_at IS NULL`. Junk captured by
    accident is tombstoned like everything else.
  ```

- `SCHEMA.md:515` (§3, the `deletes` meta row):

  ```
    ('deletes',   'nothing is deleted except links rows: an entity is a tombstone (entities.deleted_at), a measurement is corrected by inserting a row; BEFORE DELETE triggers enforce it'),
  ```

- `SCHEMA.md:418` (§2.7, the 2075 table, Q6 — the phrases in the last
  column are checked against the meta value by `tests/schema/document.py`):

  ```
  | 6 | Can anything be deleted? | `deletes`, `entities_no_delete` | `tombstone`, `links` |
  ```

- `SCHEMA.md` §3, the three registries (their `CREATE` comments say nothing
  about deletion today). The `metrics` comment opens:

  ```
    -- a tiny registry that keeps time series canonical: 'weight' is one series forever, never
  ```

  The `link_kinds` comment opens:

  ```
    -- the CLOSED registry of link kinds: a link's kind must be registered first (FK), and a kind's
  ```

  The `lifelog_meta` comment opens:

  ```
    -- the rules that span tables, readable with a SELECT by someone who has only this file;
  ```

- `tests/schema/identity.py` — the suite owning the no-deletes rules (it
  already notices a mutant that drops `people_no_delete`; expectations use
  the `S.K('label', …)` / `tryx(c, "<sql>")` pattern like
  `tests/schema/habits.py:32`).
- `tests/schema/document.py` — owns the 2075 test. Its Q-loop (document.py:15–21)
  checks that each phrase in the last column appears in the meta value (or
  the named schema object) of the row the *Where* column names.
- `tests/schema/mutants.py` — broken copies of the document; entry format
  and the `mutate(old, new, nth=0)` helper as in plans 002.
- Repo conventions that bind this edit (AGENTS.md): *one home per rule (a
  table's rule is a comment inside its CREATE statement; a cross-table rule
  is a lifelog_meta row); a new rule of any kind gets a mutant; keep the
  mutants count in tests/README.md true.*

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| The full suites | `tests/.venv/Scripts/python.exe tests/run_all.py` (Windows; `python3 tests/run_all.py` elsewhere) | `20/20 suites passed`; mutants one higher than before this plan |

No Go commands: §3's DDL objects do not change (comments inside `CREATE`
statements are part of the DDL text, but the app's parity test regenerates
schema.sql from §3 verbatim — if `app/` still exists, run
`cd app && go generate ./internal/db && go test ./...` to be safe; if it is
deleted, skip and say so in the commit message).

## Scope

**In scope** (the only files you should modify):
- `SCHEMA.md` (§2.3 bullet; §3 `deletes` meta row; §3 comments inside the
  `metrics`, `link_kinds`, `lifelog_meta` CREATE statements; §2.7 2075 Q6 row)
- `tests/schema/identity.py` (two expectations documenting registry behaviour)
- `tests/schema/mutants.py` (one new mutant)
- `tests/README.md` (mutants count, +1)
- `plans/README.md` (status row)

**Out of scope** (do NOT touch, even though they look related):
- **No new triggers** — that is the rejected alternative; do not add
  `metrics_no_delete` &co.
- D11 — its decision ("tombstones, never hard deletes") already scopes
  itself to entities and facts; its "Known asymmetry" paragraph stays.
- The §2.7 threat-model table's "no hard deletes" cell — it describes the
  *control* for buggy writers on life data, which remains true.
- The four integrity checks (§2.5) — the `lifelog_meta` wipe is caught by
  the 2075 test, not by them, and the doc already says which check sees
  what; do not add a fifth check.
- `app/`, `AGENTS.md`, `fixes.md`.

## Git workflow

- One commit on `master`, repo message style, e.g.
  `SCHEMA.md: the no-deletes rule is about life data; the registries are the owner's administrative rows`.
  Say what you ran (`tests/run_all.py`, 20/20, mutants N/N).
- Do NOT push unless the operator instructs you to.

## Steps

### Step 1: Scope the §2.3 bullet

Replace `SCHEMA.md:126–130` with:

```
- **Life data is never deleted except `links` rows.** Entities are tombstoned (`entities.deleted_at`), and
  `BEFORE DELETE` triggers reject deleting an `entities` row or any domain row (measurements and habit
  periods included), also on a connection that forgot `foreign_keys` (executed). The registries —
  `metrics`, `link_kinds`, `lifelog_meta` — are the owner's administrative rows: an unreferenced one may
  be deleted, and each table's CREATE comment says so. Every read path filters `deleted_at IS NULL`. Junk
  captured by accident is tombstoned like everything else.
```

**Verify**: `grep -n "Life data is never deleted" SCHEMA.md` → one hit in §2.3.

### Step 2: Update the `deletes` meta row and 2075 Q6

1. `SCHEMA.md:515` becomes:

   ```
    ('deletes',   'life data is never deleted except links rows: an entity is a tombstone (entities.deleted_at), a measurement is corrected by inserting a row; BEFORE DELETE triggers enforce it on entities and every domain row; the registries (metrics, link_kinds, lifelog_meta) are the owner''s administrative rows, deletable while nothing references them (each CREATE comment says so)'),
   ```

   (Note the doubled apostrophe in `owner''s` — the meta rows are SQL string
   literals inside §3's INSERT.)

2. `SCHEMA.md:418` (Q6) becomes:

   ```
   | 6 | Can anything be deleted? | `deletes`, `entities_no_delete` | `tombstone`, `links`, `registries` |
   ```

   `document.py` lowercases phrases and checks them against the `deletes`
   value — `tombstone`, `links` and `registries` all appear in the new value.

**Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` → `document`
suite passes (its Q6 expectations now demand the new phrase). If it fails
with a missing phrase, the meta value lost a word — fix the value, not the
table.

### Step 3: State each registry's rule in its CREATE comment

In §3, add one comment line to each registry, inside the `CREATE` statement
(keep the existing comment lines; append after them, matching the `--`
style and column width):

- `metrics` (after the existing three comment lines that open the table):

  ```
    -- an unreferenced metric may be deleted (a mistake registered); a referenced one is refused by
    -- the foreign keys of measurements and habit_periods. A used metric stays forever (its series is life data).
  ```

- `link_kinds` (after its opening comment lines):

  ```
    -- an unreferenced kind may be deleted by the owner; a used one is refused by the links FK
  ```

- `lifelog_meta` (after its two opening comment lines):

  ```
    -- rows are rules the owner adds or removes; deleting one removes the rule from the file itself
  ```

**Verify**: `grep -c "unreferenced" SCHEMA.md` → `2` (metrics and
link_kinds comments).

### Step 4: Two identity expectations and one mutant

1. In `tests/schema/identity.py`, add two expectations that *execute the
   documented behaviour* (model after the suite's existing `S.K`/`tryx`
   style and its existing fixtures; if the suite has no spare metric row,
   create one with `INSERT INTO metrics(name, unit) VALUES ('spare', '')`
   first):

   ```python
   S.K('an unreferenced registry row may be deleted (registries are administrative)', tryx(c, "DELETE FROM metrics WHERE name='spare'") == 'OK')
   S.K('a referenced metric is refused by its foreign key', 'FOREIGN KEY' in tryx(c, 'DELETE FROM metrics WHERE name=?', <a metric that has measurements>).upper() or tryx(c, …).startswith('ERR'))
   ```

   Adapt the second one to an existing fixture metric with measurements
   (e.g. the one `facts.py`-style fixtures use); the point is: the DELETE
   raises, and the raise is the FK, not a trigger.

2. In `tests/schema/mutants.py`, add (this is the mutant that makes the
   document unable to silently lose the scoping):

   ```python
    ('document', 'the deletes row hides the registries', mutate("the registries (metrics, link_kinds, lifelog_meta) are the owner''s administrative rows", "metrics, link_kinds and lifelog_meta are the owner''s administrative rows")),
   ```

   With the word `registries` gone from the meta value, 2075 Q6's phrase
   check must fail — that is the suite noticing.

3. `tests/README.md`: increment the mutants count ("61 broken copies" →
   read the current number, add one).

**Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` →
`20/20 suites passed`, mutants one higher. If the mutants runner asserts on
the target text, re-check the exact meta value from Step 2.

## Test plan

- New expectations in `tests/schema/identity.py` (Step 4.1): unreferenced
  registry row deletable; referenced metric refused by FK.
- New mutant in `tests/schema/mutants.py` (Step 4.2): the `deletes` meta row
  losing the word `registries` must fail `document.py`'s 2075 Q6 check.
- Model after: the `people may be hard-deleted` mutant and the identity
  suite's existing no-delete expectations.

## Done criteria

Machine-checkable. ALL must hold:

- [ ] `grep -n "Life data is never deleted" SCHEMA.md` → one hit, §2.3
- [ ] `grep -n "registries" SCHEMA.md | wc -l` ≥ 3 (meta row, Q6 row, §2.3 bullet)
- [ ] On a fresh throwaway DB built from §3: `DELETE FROM metrics WHERE name='spare'`
      (unused) succeeds; `DELETE FROM lifelog_meta WHERE key='schema'` succeeds —
      both now *documented*, not changed
- [ ] `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`,
      mutants count one higher than before
- [ ] No files outside the in-scope list are modified (`git status`)
- [ ] `plans/README.md` status row updated

## STOP conditions

Stop and report back (do not improvise) if:

- The excerpts don't match the live files (drift since `6058f24`).
- `document.py` fails on a phrase you did not intend to touch — the 2075
  table's phrase columns are load-bearing; report rather than re-shuffling
  phrases.
- You find the registries' behaviour *changed* (e.g. a DELETE that used to
  succeed now raises) — that means someone added triggers; this plan's
  premise is wording-only.
- The second identity expectation cannot find a metric with measurements in
  the suite's fixtures — report the fixture shape; do not build elaborate
  new fixtures.

## Maintenance notes

- If the owner later wants registry rows undeletable, that is a new
  decision (a D25) with triggers and mutants — additive after the freeze is
  *not* possible for triggers on existing tables without a table rebuild,
  so it belongs before the freeze if at all.
- Reviewer should scrutinize the Q6 phrase addition: every backticked word
  in the last column becomes a machine-checked promise of the meta value.
- The threat-model row in §2.7 ("Another tool editing rows … bypasses every
  control") already covers the malicious case; nothing to add there.
