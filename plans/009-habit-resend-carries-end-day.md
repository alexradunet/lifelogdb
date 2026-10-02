# Plan 009: §6.16 says a re-sent habit period carries its `end_day`, and the suite proves why

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 3e2fcf4..HEAD -- SCHEMA.md tests/schema/habits.py tests/schema/mutants.py tests/README.md`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live files before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW (prose in §6.16, two suite expectations, one mutant; no DDL change)
- **Depends on**: none. Plans 009, 010 and 011 each add mutants and edit the count in
  `tests/README.md`: run them one at a time and **read the current count before editing it**.
- **Category**: bug (contract)
- **Planned at**: commit `3e2fcf4`, 2026-10-02

## Why this matters

A habit (D24) is a unitless metric with periods in `habit_periods`; a restarted habit has several.
§6.16 tells a writer that "a re-sent period is idempotent: insert it with
`ON CONFLICT(metric_id, start_day) DO NOTHING` … and apply a changed `end_day` with an UPDATE". It
does not say the re-sent INSERT must carry the period's `end_day`. A writer that re-sends a closed
period *without* its `end_day` inserts an **open** period, and the BEFORE INSERT overlap trigger
fires before the UNIQUE conflict is resolved: it raises "periods of one habit never overlap" whenever
a later period of the same habit exists. That is exactly how the removed Go writer failed — its
re-run of a two-period habit aborted the whole import (reproduced on a scratch database) — and the
suite only tests a re-send while the other period is **earlier**, so it never saw it. Any future
writer that follows §6.16 as written can repeat the bug.

## Current state

- `SCHEMA.md`, §6.16 ("### 6.16 Habits: start and stop one, …"), the paragraph after the SQL block,
  verbatim at commit `3e2fcf4`:

  ```
  A day outside every period is not a habit day at all: it is in no count. Two check-ins on one day
  count once (the higher wins). A re-sent period is idempotent: insert it with
  `ON CONFLICT(metric_id, start_day) DO NOTHING` — the insert trigger fires first, so a genuine
  overlap still raises — and apply a changed `end_day` with an UPDATE of the row at that metric and
  start day; periods are keyed by the owner's data, not by a sender's `import_key` (D24).
  ```

- `tests/schema/habits.py` lines 26–32 test the re-send with the only other periods **earlier**:

  ```python
  S.K('...and ON CONFLICT DO NOTHING makes it a no-op (the trigger leaves it to UNIQUE)',
      tryx(c, "INSERT INTO habit_periods(metric_id,start_day,source) VALUES (?, '2027-01-01', 'ui') ON CONFLICT(metric_id, start_day) DO NOTHING", (vd,)) == 'OK')
  S.K('a re-run with a changed end_day is a no-op until the UPDATE follows',
      tryx(c, "INSERT INTO habit_periods(metric_id,start_day,end_day,source) VALUES (?, '2026-10-01', '2026-10-15', 'ui') ON CONFLICT(metric_id, start_day) DO NOTHING", (vd,)) == 'OK'
      and one(c, "select end_day from habit_periods where start_day='2026-10-01'") == '2026-10-31')
  S.K('§6.16 documents the idempotent re-run', 'ON CONFLICT(metric_id, start_day) DO NOTHING' in section('### 6.16 ', '## 7. '))
  ```
  Helpers come from `tests/lib/kit.py` (`fresh()`, `tryx()`, `one()`, `section()`, `S.K(label, cond)`);
  `metric(c, name, unit='')` is defined at the top of `habits.py`. Every label states its expectation.

- `tests/schema/mutants.py` — a list `MUTANTS` of `(suite, what is broken, mutate(old, new))`. The
  existing habits mutant that touches this paragraph (keep its target text intact):

  ```python
  ('habits', '§6.16 loses the idempotent re-run', mutate('A re-sent period is idempotent: insert it with\n`ON CONFLICT(metric_id, start_day) DO NOTHING`', 'A re-sent period is idempotent: insert it again')),
  ```
- `tests/README.md` — the row for `mutants.py` says "66 broken copies of `SCHEMA.md`" at `3e2fcf4`.

## Commands you will need

| Purpose | Command (Git Bash, repo root) | Expected on success |
|---|---|---|
| One suite | `cd tests/schema && ../.venv/Scripts/python.exe habits.py "$DDL"` — easier: use the runner below | `habits: N/N met expectations` |
| All suites | `tests/.venv/Scripts/python.exe tests/run_all.py` (elsewhere `python3 tests/run_all.py`) | `20/20 suites passed` |
| Mutant count | `grep -cE "^ \('[a-z]+', " tests/schema/mutants.py` | the new count |

## Scope

**In scope:** `SCHEMA.md` (the one §6.16 paragraph), `tests/schema/habits.py`, `tests/schema/mutants.py`,
`tests/README.md` (the mutant count), `plans/README.md` (status row).

**Out of scope:** §3 DDL and its triggers (they behave correctly); the §6.16 SQL block; any other section.

## Git workflow

Branch `advisor/009-habit-resend-end-day`; one commit; subject like
`SCHEMA.md §6.16: a re-sent habit period carries its end_day`; body bullets per file and `Ran: …`.

## Steps

### Step 1: Prove the failure first (a probe, not a change)

Add these expectations to `tests/schema/habits.py` immediately **after** the line
`S.K('§6.16 documents the idempotent re-run', …)`:

```python
# a restarted habit: the re-send of the closed first period must carry its end_day (§6.16)
c2 = fresh(); rs = metric(c2, 'stretching')
S.K('a closed period, then a later open one (a restarted habit)',
    habit(c2, rs, '2026-01-01', '2026-01-31') == 'OK' and habit(c2, rs, '2026-03-01') == 'OK')
S.K('§6.16 re-send of the closed period WITH its end_day is a no-op beside the later period',
    tryx(c2, "INSERT INTO habit_periods(metric_id,start_day,end_day,source) VALUES (?, '2026-01-01', '2026-01-31', 'ui') ON CONFLICT(metric_id, start_day) DO NOTHING", (rs,)) == 'OK'
    and one(c2, 'select count(*) from habit_periods where metric_id=?', (rs,)) == 2)
S.K('...re-sent WITHOUT its end_day it is an open period, and the insert trigger refuses the overlap',
    'overlap' in tryx(c2, "INSERT INTO habit_periods(metric_id,start_day,source) VALUES (?, '2026-01-01', 'ui') ON CONFLICT(metric_id, start_day) DO NOTHING", (rs,)))
```

(`habit(c, metric, start, end=None)` is in `kit.py`; check its signature there before using it.)

**Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` → the `habits` line is PASS with three more
expectations than before (both new behaviours already hold — they are facts about the DDL). If either
fails, STOP: the trigger behaves differently from what this plan assumes.

### Step 2: Rewrite the §6.16 paragraph

Replace the paragraph quoted in "Current state" with (keep the first sentence pair; the substring
`A re-sent period is idempotent: insert it with\n\`ON CONFLICT(metric_id, start_day) DO NOTHING\`` must
stay byte-identical, line break included, because an existing mutant targets it):

```
A day outside every period is not a habit day at all: it is in no count. Two check-ins on one day
count once (the higher wins). A re-sent period is idempotent: insert it with
`ON CONFLICT(metric_id, start_day) DO NOTHING`, carrying the `end_day` it was sent with — the
insert trigger fires first, so a genuine overlap still raises, and a closed period re-sent without
its `end_day` is an open one that overlaps any later period of the habit (executed) — then apply a
changed `end_day` with an UPDATE of the row at that metric and start day; periods are keyed by the
owner's data, not by a sender's `import_key` (D24).
```

Then add, after the existing line `S.K('§6.16 documents the idempotent re-run', …)`:

```python
S.K('§6.16 says a re-sent period carries its end_day', 'carrying the `end_day` it was sent with' in section('### 6.16 ', '## 7. '))
```

**Verify**: run all suites → `20/20 suites passed` (`document.py` checks current-truth wording; if it
fails on a word, rephrase without changing the meaning).

### Step 3: Add a mutant

Append to `MUTANTS` in `tests/schema/mutants.py`, next to the other `habits` mutants:

```python
 ('habits', '§6.16 re-sends a period without its end_day', mutate('carrying the `end_day` it was sent with', 'as it is')),
```

Read the current count in `tests/README.md` (the `mutants.py` row: "N broken copies of `SCHEMA.md`")
and add 1.

**Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`; in the mutants
output the line `caught  habits     §6.16 re-sends a period without its end_day` appears; the
`grep -cE` count equals the number now written in `tests/README.md`.

## Test plan

Three behaviour expectations and one text expectation in `habits.py` (Steps 1–2), one mutant (Step 3),
modelled on the existing re-send checks at `habits.py:26-32`.

## Done criteria

- [ ] `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`
- [ ] `grep -n "carrying the \`end_day\` it was sent with" SCHEMA.md` → one match, in §6.16
- [ ] The mutants output shows the new mutant `caught`
- [ ] `tests/README.md`'s mutant count equals `grep -cE "^ \('[a-z]+', " tests/schema/mutants.py`
- [ ] Only the in-scope files changed (`git status --short`)
- [ ] `plans/README.md` status row for 009 updated

## STOP conditions

- Step 1's expectations fail on the unmodified DDL.
- The §6.16 paragraph differs from the quote (drift).
- `document.py` rejects the new paragraph twice after rewording.

## Maintenance notes

- Any writer's "make this habit period exist" routine must insert `(metric, start_day, end_day)`
  together, then UPDATE a changed `end_day`; inserting the period open and closing it afterwards is the
  bug this plan documents.
- If §6.16 is reworded later, keep the `carrying the \`end_day\`` phrase or move the suite check and the
  mutant with it.
