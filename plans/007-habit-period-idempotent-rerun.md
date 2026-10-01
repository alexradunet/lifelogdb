# Plan 007: Document the idempotent re-run of a habit period (§6.16)

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

- **Priority**: P3
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none (execute after 002 if possible — both touch
  `tests/schema/habits.py` and `mutants.py`)
- **Category**: docs (a writer obligation the document states only half)
- **Planned at**: commit `6058f24`, 2026-10-02

## Why this matters

`habit_periods` has no `import_key`: a period is keyed by
`(metric_id, start_day)` — the owner's data, not a sender's. §3's comment
documents the no-op re-insert ("a period with the same start is left to
`UNIQUE`, so `ON CONFLICT DO NOTHING` re-runs it"), and that behaviour is
executed by `tests/schema/habits.py`. But the *correction path* — a re-run
whose `end_day` changed must follow the no-op insert with the UPDATE —
appears nowhere in SCHEMA.md. A third-party writer implementing "§2 and §3
only" cannot reproduce the reference writer's idempotent period updates:
their re-run silently changes nothing (verified by execution: inserting the
same `(metric_id, start_day)` with a different `end_day` under
`ON CONFLICT DO NOTHING` returns OK and leaves the row unchanged). The
document specifies `title_key` to the vector precisely so any language can
reproduce the writer; period re-runs deserve one sentence of the same
treatment.

The resolution is documentation, not schema: periods are owner-keyed by
design (D24 — the owner approves them), so no `import_key` column is added.

## Current state

- `SCHEMA.md` §6.16 (lines ~1936–1990). The SQL block contains:

  ```sql
  -- stop it: the open period ends on :day
  UPDATE habit_periods SET end_day = :day
   WHERE metric_id = (SELECT id FROM metrics WHERE name = :metric) AND end_day IS NULL;
  ```

  and the closing paragraph reads:

  ```
  A day outside every period is not a habit day at all: it is in no count. Two check-ins on one day
  count once (the higher wins).
  ```

- `SCHEMA.md` §3, `habit_periods` block — the insert trigger's comment
  (already correct, unchanged by this plan):

  ```sql
    -- a period with the same start is left to UNIQUE, so ON CONFLICT DO NOTHING re-runs it (this trigger fires first)
  ```

- `tests/schema/habits.py:28–32` — the existing re-run and no-delete
  expectations (model after these):

  ```python
      tryx(c, "INSERT INTO habit_periods(metric_id,start_day,source) VALUES (?, '2027-01-01', 'ui') ON CONFLICT(metric_id, start_day) DO NOTHING", (vd,)) == 'OK')
  ...
  S.K('a period is never deleted', tryx(c, 'DELETE FROM habit_periods').startswith('ERR') and one(c, 'select count(*) from habit_periods') == 3)
  ```

  `tryx(c, sql, args=())` returns `'OK'` or the error text; `one(c, sql)`
  returns the first column of the first row. The suite builds its rows with
  a `habit(c, metric, start, end, source)` helper from
  `tests/lib/kit.py:127`.

- `tests/schema/mutants.py` — entry format and `mutate(old, new, nth=0)`
  as in plan 002; the owning suite for §6.16 is `habits`.
- Repo conventions that bind this edit (AGENTS.md): *§6 shows the rule in
  use; a new rule of any kind gets a mutant; keep the mutants count in
  tests/README.md true.*

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| The full suites | `tests/.venv/Scripts/python.exe tests/run_all.py` (Windows; `python3 tests/run_all.py` elsewhere) | `20/20 suites passed`; habits one expectation higher; mutants one higher |

No Go commands: no DDL change (§6 prose + suites only). If `app/` still
exists, nothing in it is affected; if deleted, skip and say so.

## Scope

**In scope** (the only files you should modify):
- `SCHEMA.md` (§6.16, one added sentence)
- `tests/schema/habits.py` (one behavioural expectation, one text expectation)
- `tests/schema/mutants.py` (one new mutant)
- `tests/README.md` (mutants count, +1)
- `plans/README.md` (status row)

**Out of scope** (do NOT touch, even though they look related):
- The `habit_periods` table and its triggers — the behaviour is correct;
  only its documentation is missing.
- D24 — its costs already say a habit's target has no column; adding
  "no import_key" there would be a second home for this plan's sentence.
- §2.7's import steps — they cover entities and measurements; habits enter
  through the owner, and D24 says so.
- `app/`, `AGENTS.md`, `fixes.md`.

## Git workflow

- One commit on `master`, repo message style, e.g.
  `SCHEMA.md: a re-sent habit period is a no-op until the UPDATE follows — §6.16`.
  Say what you ran (`tests/run_all.py`, 20/20, mutants N/N).
- Do NOT push unless the operator instructs you to.

## Steps

### Step 1: Add the sentence to §6.16

Append to §6.16's closing paragraph (after "…the higher wins)."):

```
A re-sent period is idempotent: insert it with
`ON CONFLICT(metric_id, start_day) DO NOTHING` — the insert trigger fires
first, so a genuine overlap still raises — and apply a changed `end_day`
with the UPDATE above; periods are keyed by metric and start day, the
owner's data, not by a sender's `import_key` (D24).
```

(Reflow to match the surrounding line width. The phrase
`ON CONFLICT(metric_id, start_day) DO NOTHING` is load-bearing for Steps 3
and 4 — it must appear in §6.16's prose; §3's DDL comment has its own
wording and stays.)

**Verify**: `grep -c "A re-sent period is idempotent" SCHEMA.md` → `1`

### Step 2: The behavioural expectation

In `tests/schema/habits.py`, after the existing `ON CONFLICT` re-insert
expectation (habits.py:28), add — adapting the fixture values to the rows
that suite has already created (use an existing `(metric_id, start_day)`
and its real `end_day`):

```python
S.K('a re-run with a changed end_day is a no-op until the UPDATE follows',
    tryx(c, "INSERT INTO habit_periods(metric_id,start_day,end_day,source) VALUES (?, '2026-10-01', '2027-06-30', 'import:vault') ON CONFLICT(metric_id, start_day) DO NOTHING", (vd,)) == 'OK'
    and one(c, "select end_day from habit_periods where start_day='2026-10-01'") == '2026-12-31')
```

The assertion: the insert reports OK, and the stored `end_day` is *still
the old one* — the no-op, exactly what the new sentence tells writers to
follow with the UPDATE.

**Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` → the habits
suite passes with one more expectation than before.

### Step 3: The text expectation

In `tests/schema/habits.py`, add (the `section` helper comes from
`tests/lib/kit.py:174`, already imported by the suite):

```python
S.K('§6.16 documents the idempotent re-run', 'ON CONFLICT(metric_id, start_day) DO NOTHING' in section('### 6.16 ', '## 7 '))
```

**Verify**: the habits suite reports one more expectation still (two total
added by this plan).

### Step 4: The mutant and the count

In `tests/schema/mutants.py`, add:

```python
 ('habits', '§6.16 loses the idempotent re-run', mutate('A re-sent period is idempotent: insert it with\n`ON CONFLICT(metric_id, start_day) DO NOTHING`', 'A re-sent period is idempotent: insert it again')),
```

(The mutation must remove the load-bearing phrase from §6.16 so the
Step 3 expectation fails. Check `grep -c "ON CONFLICT(metric_id, start_day) DO NOTHING" SCHEMA.md`
first: if the exact string appears more than once outside code blocks,
narrow the mutation target with more surrounding text or the `nth=` argument.)

In `tests/README.md`, increment the mutants count (read the current number
first — plans 002/003/004 may already have raised it above 61).

**Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` →
`20/20 suites passed`, the mutants line one higher, the new mutant printed
as `caught`.

## Test plan

- New behavioural expectation (Step 2): the no-op re-insert leaves
  `end_day` unchanged — executes the documented behaviour.
- New text expectation (Step 3): §6.16's prose keeps the idempotent re-run.
- New mutant (Step 4): the sentence's removal fails the habits suite.
- Model after: habits.py:28–32 and the `('habits', '§6.16 counts a day not
  recorded as not done', …)` mutant.

## Done criteria

Machine-checkable. ALL must hold:

- [ ] `grep -c "A re-sent period is idempotent" SCHEMA.md` → `1`
- [ ] `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`,
      habits +2 expectations, mutants +1 and the new mutant `caught`
- [ ] On a fresh throwaway DB from §3: re-inserting an existing
      `(metric_id, start_day)` with a different `end_day` under
      `ON CONFLICT DO NOTHING` leaves the row unchanged (the expectation
      in Step 2 runs exactly this)
- [ ] `git status` shows changes only in the in-scope list
- [ ] `plans/README.md` status row updated

## STOP conditions

Stop and report back (do not improvise) if:

- The excerpts don't match the live files (drift since `6058f24`).
- The habits suite's fixtures have no period you can re-insert (e.g. all
  periods are created without an `end_day`) — report the fixture shape;
  create one closed period with the `habit()` helper rather than inventing
  a new fixture scheme.
- The mutants runner asserts on the target text — the sentence was reflowed
  differently than Step 1 specifies; copy the exact live text into the
  mutant, keeping the mutation semantics (the load-bearing phrase removed).

## Maintenance notes

- If `habit_periods` ever gains `import_key` (a real import of periods —
  D24's reopen trigger), this sentence, the expectations and the mutant all
  need revisiting; the key would change the idempotency story from
  owner-keyed to sender-keyed.
- Reviewer should check the sentence stays in §6.16 (use) and does not
  restate D24's decision (why) — one home per rule.
