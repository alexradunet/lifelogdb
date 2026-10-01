# Plan 002: Enforce `habit_periods.source` like the other three provenance columns

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 6058f24..HEAD -- SCHEMA.md tests/ AGENTS.md`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none (but execute after 001 if possible — both edit SCHEMA.md prose)
- **Category**: bug (a stated rule the DDL does not enforce)
- **Planned at**: commit `6058f24`, 2026-10-02

## Why this matters

`lifelog_meta.source` says provenance is "written at insert, never changed"
for `entities`, `links`, `measurements` **and `habit_periods`** (2075
question 19 relies on it). Three of the four are enforced by DDL:
`entities_provenance_fixed` (a trigger), `links_fixed` (a trigger),
`measurements_no_update` (append-only). The fourth is not: on a fresh
database built from §3, `UPDATE habit_periods SET source='agent:x'` succeeds
(verified by execution during the audit). A writer, importer or agent can
silently rewrite the provenance of habit rows, and the file's own meta rows
overclaim. This is exactly the class of gap the document says its triggers
exist to close ("also on a connection that forgot `foreign_keys`", §2.3).

Two wording fixes ride along (the rule's prose homes): §2.2 omits
`habit_periods` from the source list, and the meta `source` row tucks an
`import_key` clause into a sentence naming four tables, only two of which
have that column.

## Current state

- `SCHEMA.md` — the contract. §3 is the canonical DDL; its totals line and
  the mutants count in `tests/README.md` must stay true.
- `tests/schema/habits.py` — the suite owning `habit_periods` (D24).
  Its expectations use the pattern `S.K('label', tryx(c, "<sql>") == 'OK')`;
  existing example (habits.py:32):

  ```python
  S.K('a period is never deleted', tryx(c, 'DELETE FROM habit_periods').startswith('ERR') and one(c, 'select count(*) from habit_periods') == 3)
  ```

- `tests/schema/mutants.py` — broken copies of SCHEMA.md; each new rule gets
  a mutant. Entry format (mutants.py:59):

  ```python
   ('habits', 'habit periods may be deleted', mutate("CREATE TRIGGER habit_periods_no_delete BEFORE DELETE ON habit_periods\nBEGIN", "CREATE TRIGGER habit_periods_no_delete BEFORE DELETE ON habit_periods WHEN 0\nBEGIN")),
  ```

  **Important**: the existing mutant (mutants.py, suite `document`)
  `('document', 'the §3 totals drift from the DDL', mutate('**+ 23 triggers.**', '**+ 24 triggers.**'))`
  hard-codes today's trigger count. Adding a 24th trigger makes its target
  string stale — this plan updates it (Step 4).

- `SCHEMA.md:117` (§2.2, the Provenance bullet — missing `habit_periods`):

  ```
  - **Provenance.** `source` on `entities`, `links` and `measurements` names the writer
  ```

- `SCHEMA.md:516` (§3, the `source` meta row — right tables, muddled
  `import_key` clause):

  ```
    ('source',    'entities, links, measurements and habit_periods: source names the writer of the row (ui, cli, api, agent:<name>, import:<name>); written at insert, never changed; the import_key a writer gives a row is unique per source'),
  ```

- `SCHEMA.md` §3, after the `habit_periods_no_delete` trigger (currently the
  last object of the `habit_periods` block, before `CREATE TABLE link_kinds`):

  ```sql
  CREATE TRIGGER habit_periods_no_delete BEFORE DELETE ON habit_periods
  BEGIN
    SELECT RAISE(ABORT, 'habit periods are never deleted: correct a wrong one with UPDATE');
  END;
  ```

- `SCHEMA.md` §3 totals line (just after the DDL code block closes):

  ```
  **9 tables + 1 FTS5 virtual table + 2 views** (`measurement_values`, `ghost_pages`)
  **+ 23 triggers.** That is the entire system.
  ```

- `AGENTS.md:119–120` (conventions checklist — same omission as §2.2):

  ```
  - **Provenance**: `source` (the writer: `ui`, `cli`, `api`, `agent:<name>`, `import:<name>`) is
    required on `entities`, `links` and `measurements`, written at insert and never changed; `import_key`
  ```

- Repo conventions that bind this edit (AGENTS.md): *a table's rule is a
  comment inside its `CREATE` statement / trigger; a DDL change requires the
  suites AND, while `app/` exists, `go generate ./internal/db && go test ./...`
  in `app/`; a new rule of any kind gets a mutant; suites change with the
  document in the same commit.*

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| The full suites | `tests/.venv/Scripts/python.exe tests/run_all.py` (Windows; `python3 tests/run_all.py` elsewhere) | `20/20 suites passed`, and the mutants line reads one higher than before (`62/62` if this is the first plan to add a mutant) |
| App parity (only if `app/` still exists) | `cd app && go generate ./internal/db && go vet ./... && go test ./...` | all pass; `app/internal/db/schema.sql` regenerates equal to §3 |

## Scope

**In scope** (the only files you should modify):
- `SCHEMA.md` (§2.2 bullet, §3: new trigger + totals line, §3 meta `source` row)
- `tests/schema/habits.py` (one new expectation)
- `tests/schema/mutants.py` (one new mutant; one updated mutant — the totals-drift entry)
- `tests/README.md` (mutants count: "61 broken copies" → 62, and the mutants line if it states the count elsewhere)
- `AGENTS.md` (the Provenance checklist line only)
- `plans/README.md` (status row)

**Out of scope** (do NOT touch, even though they look related):
- `entities`, `links`, `measurements` triggers — they are correct.
- `habit_periods_check_update` — leave it on `OF metric_id, start_day, end_day`; the new rule gets its own trigger (one rule, one home).
- D24's text — the decision cites `habit_periods_no_delete` as an example, not an exhaustive list; no change needed.
- `app/` Go code — the app only inserts periods, never updates `source`, so no code change is required; only the embedded schema regenerates. If `app/` is already deleted, skip the Go gate and say so in the commit message.
- The 2075 table (§2.7) — Q19's phrases (`written at insert`, `agent`) survive in the rewritten meta row (see Step 2).

## Git workflow

- One commit on `master`. Message style (match the repo, e.g.
  `SCHEMA.md: habit_periods.source is fixed at insert like the other three — D24 trigger, suites, mutant`).
  State what you ran: `tests/run_all.py` 20/20 (mutants 62/62) and, if run,
  the Go gate.
- Do NOT push unless the operator instructs you to.

## Steps

### Step 1: Add the trigger to §3

In `SCHEMA.md` §3, immediately after the `habit_periods_no_delete` trigger
(excerpt above), insert:

```sql
CREATE TRIGGER habit_periods_source_fixed BEFORE UPDATE OF source ON habit_periods
BEGIN
  -- provenance is written at insert and never changed, as on entities and links (lifelog_meta.source)
  SELECT RAISE(ABORT, 'habit_periods.source is written at insert and never changed');
END;
```

Then update the totals line: `**+ 23 triggers.**` → `**+ 24 triggers.**`.

**Verify**: `grep -c "CREATE TRIGGER" SCHEMA.md` → `24`

### Step 2: Fix the two prose homes

1. `SCHEMA.md:117`: change
   `` `source` on `entities`, `links` and `measurements` names the writer``
   to
   `` `source` on `entities`, `links`, `measurements` and `habit_periods` names the writer``.

2. `SCHEMA.md:516`: replace the meta `source` row's value tail
   `… never changed; the import_key a writer gives a row is unique per source`
   with
   `… never changed; import_key, on entities and on measurements, is unique per source`.
   The full row becomes:

   ```
    ('source',    'entities, links, measurements and habit_periods: source names the writer of the row (ui, cli, api, agent:<name>, import:<name>); written at insert, never changed; import_key, on entities and on measurements, is unique per source'),
   ```

   (2075 Q19 needs the phrases `written at insert` and `agent` in this value — both survive.)

3. `AGENTS.md:119–120`: change
   `` required on `entities`, `links` and `measurements`, written at insert and never changed``
   to
   `` required on `entities`, `links`, `measurements` and `habit_periods`, written at insert and never changed``.

**Verify**: `grep -n "measurements and habit_periods" SCHEMA.md AGENTS.md` →
hits in §2.2, §3 meta row, and AGENTS.md.

### Step 3: Add the habits expectation

In `tests/schema/habits.py`, after the existing
`'a period is never deleted'` expectation (habits.py:32), add (adapting the
fixture values to whatever rows that suite has already created — use the
`start_day` of an existing period):

```python
S.K("a period's source never changes", 'never changed' in tryx(c, "UPDATE habit_periods SET source='cli' WHERE start_day='2026-10-01'"))
```

The `tryx` helper returns `'OK'` or the error text; the expectation asserts
the error mentions the rule, matching the style of the neighbouring lines.

**Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` still passes
the habits suite (run the full runner; habits must report one more met
expectation than before — 32 instead of 31).

### Step 4: Add the mutant, and fix the totals mutant

In `tests/schema/mutants.py`:

1. Add to `MUTANTS` (after the `'habit periods may be deleted'` entry):

   ```python
    ('habits', 'habit_periods.source can change', mutate("CREATE TRIGGER habit_periods_source_fixed BEFORE UPDATE OF source ON habit_periods\nBEGIN", "CREATE TRIGGER habit_periods_source_fixed BEFORE UPDATE OF source ON habit_periods WHEN 0\nBEGIN")),
   ```

2. Update the existing totals-drift mutant (it targets the old count):
   `mutate('**+ 23 triggers.**', '**+ 24 triggers.**')` →
   `mutate('**+ 24 triggers.**', '**+ 25 triggers.**')`
   (the *target* is the new truth, the *mutation* the drift).

3. `tests/README.md`: update the mutants row — "61 broken copies" becomes
   "62 broken copies". **Read the current number first and increment it**;
   if other plans already added mutants, it is higher than 61.

**Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` →
`20/20 suites passed` with the mutants line at `62/62` (or the count you
computed in 4.3). If the mutants suite stops with `assert n > nth` in
`mutate`, a target string didn't match — re-check the exact text you
inserted in Step 1.

### Step 5: The app gate (conditional)

If `app/` exists in the repo: `cd app && go generate ./internal/db &&
go vet ./... && go test ./...`. `TestSchemaIsSection3` regenerates
`app/internal/db/schema.sql` from §3 — it must pass. If `app/` is gone,
skip this step and note it in the commit message.

**Verify**: Go output all-pass, or the step is skipped with a note.

## Test plan

- New expectation in `tests/schema/habits.py` (Step 3): `UPDATE … SET source`
  raises and the message names the rule.
- New mutant in `tests/schema/mutants.py` (Step 4): the trigger disabled
  (`WHEN 0`) must make the habits suite fail at least one expectation.
- Existing patterns to model after: `'a period is never deleted'`
  (habits.py:32) and `'habit periods may be deleted'` (mutants.py:59).

## Done criteria

Machine-checkable. ALL must hold:

- [ ] `grep -c "CREATE TRIGGER" SCHEMA.md` → `24`
- [ ] `grep -n "habit_periods_source_fixed" SCHEMA.md` → one hit, in §3
- [ ] A fresh probe refuses the update: on a throwaway DB built from §3,
      `UPDATE habit_periods SET source='x'` raises
      `habit_periods.source is written at insert and never changed`
- [ ] `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`,
      mutants one higher than before this plan
- [ ] `grep -n "habit_periods" AGENTS.md | head -3` shows the Provenance line includes it
- [ ] No files outside the in-scope list are modified (`git status`)
- [ ] `plans/README.md` status row updated

## STOP conditions

Stop and report back (do not improvise) if:

- The excerpts above don't match the live files (drift since `6058f24`).
- The mutants runner's `assert n > nth` fires — the target text is wrong;
  fix the target to the exact text you inserted, do not invent a different
  mutation.
- `go test ./...` fails in `app/` for a reason other than
  `TestSchemaIsSection3` needing regeneration — that means the app writes
  `habit_periods.source` somewhere; report it (the app is scheduled for
  deletion, so do not refactor it).
- You are tempted to fold the rule into `habit_periods_check_update`
  instead — that is a design change; the plan deliberately keeps one rule
  per trigger. Report the itch, don't scratch it.

## Maintenance notes

- After the schema freeze (D13) this trigger is frozen too; a future
  `habit_periods.import_key` (if ever added — see D24's costs) would want a
  matching `entities_provenance_fixed`-style guard in the same migration.
- Reviewer should scrutinize: the trigger message wording (it is quoted by
  the habits expectation), and that the meta `source` row still contains
  `written at insert` and `agent` for 2075 Q19.
- Deferred on purpose: `metrics`/`link_kinds`/`lifelog_meta` have no
  `source` and need none (plan 003 covers their rules).
