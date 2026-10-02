# Plan 017: The cookbook recipes a writer copies return the right rows and write safely

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 4d84261..HEAD -- docs/cookbook/ docs/guides/importing.md tests/schema/named.py tests/schema/identity.py tests/schema/facts.py tests/schema/mutants.py tests/README.md`
> If any in-scope file changed since this plan was written, compare the "Current state" excerpts against the live
> files; on a mismatch, STOP. (`tests/schema/mutants.py` and `tests/README.md` may have grown from plan 015 — fine.)

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: 015 (ordering only). Run **before** 016 and 020 (both touch files this plan edits).
- **Category**: bug
- **Planned at**: commit `4d84261`, 2026-10-02

## Why this matters

`docs/cookbook/` is the set of canonical SQL reads and writes that any writer of `life.db` (in any language) copies
verbatim. `tests/schema/cookbook.py` proves each block *runs*; it does not prove each block returns the *right* rows. An
audit (2026-10-02, executed on throwaway databases) found five recipes that run but are wrong:

1. **`import-a-row-once`**: the "a changed note" `UPDATE` sits after `COMMIT;` with no `BEGIN IMMEDIATE`, although the
   prose says to run the link sync "in the same transaction" — a copier writes a body outside any transaction and its
   wikilinks drift from it.
2. **`everything-about`** (the ark query) returns every symmetric relation **twice** (`friend` in and out), because
   symmetric kinds are stored in both directions (`links_mirror_insert`) and both legs are `UNION ALL`ed.
3. **`metric-series`** ("last 90 days") has no upper bound and an inclusive lower bound: it returns 91 days plus any
   future-dated readings.
4. **`person-or-place`** promotion: step 0 does not look at the tombstone, so promoting a plain page the owner had
   tombstoned produces a person that every read (which filters `deleted_at IS NULL`) silently hides; and a redirect
   **stub** (a plain page whose body is `#REDIRECT [[X]]`) can be promoted, leaving a person whose outgoing `redirect`
   link violates the `link_kinds` row (`redirect` is `from_types = 'page'`).
5. The cookbook says "every write transaction starts with `BEGIN IMMEDIATE`" but several single-statement write blocks
   (habit start/stop, a correction, an `at` link) have no `BEGIN`; the README should say once why that is fine.

## Current state

Conventions (`AGENTS.md`): the cookbook *shows* rules; their homes are `schema.sql` and `docs/contract/`. Every block
runs in `tests/` — suites fetch the **first SQL block of a recipe page** with `block('<key>')` (`tests/lib/kit.py`), split
it with `statements()` and often index statements **by position**, so do not add or remove statements in a block
unless this plan says so. A changed recipe changes its suite in the same commit ("suites change with the docs, never to
make them pass") and gets a mutant in `tests/schema/mutants.py` (idiom: `mutate("old text", "new text")`, owner suite first).

### 1. `docs/cookbook/import-a-row-once.md:10-30`
```sql
BEGIN IMMEDIATE;
INSERT INTO entities(entity_type, created_at, updated_at, source, import_key)
VALUES ('page', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'import:vault', :import_key)
ON CONFLICT(source, import_key) WHERE import_key IS NOT NULL DO NOTHING
RETURNING id;   -- the app keeps it as :page_id; no row back = imported before: skip the next INSERT
INSERT INTO pages(id, title, title_key, body)
VALUES (:page_id, 'Sourdough', 'sourdough', 'Feed the starter the night before.');
COMMIT;

-- a later run finds the note changed: update the live page that has the key; a tombstoned one stays gone
UPDATE pages
   SET body = 'Feed the starter the night before; 75% water.'
 WHERE id = (SELECT id FROM entities
              WHERE source = 'import:vault' AND import_key = :import_key AND deleted_at IS NULL);
```
Prose after the block: "…A body an import writes or changes is a save like any other (D19): run the link sync of
[save a body](save-a-body.md) in the same transaction…". `tests/schema/identity.py:92-93` keeps every statement that
is not `BEGIN`/`COMMIT` and expects exactly three (`ins_ent, ins_page, upd`). `tests/schema/document.py:25` checks the
phrase `run the link sync of [save a body](save-a-body.md)` — keep it.

### 2. `docs/cookbook/everything-about.md:9-15`
```sql
SELECT l.kind, e.entity_type, l.from_id AS other_id, 'in' AS direction
  FROM links l JOIN entities e ON e.id = l.from_id
 WHERE l.to_id = :entity_id AND e.deleted_at IS NULL
UNION ALL
SELECT l.kind, e.entity_type, l.to_id, 'out'
  FROM links l JOIN entities e ON e.id = l.to_id
 WHERE l.from_id = :entity_id AND e.deleted_at IS NULL;
```
`tests/schema/named.py:100-104` runs it; line 104 asserts `block('everything-about').count('UNION ALL') == 1` — keep one `UNION ALL`.

### 3. `docs/cookbook/metric-series.md:3-9`
```sql
SELECT me.day, me.value
  FROM measurement_values me
  JOIN metrics m ON m.id = me.metric_id AND m.name = 'weight'
 WHERE me.day >= date(:day, '-90 day')
 ORDER BY me.day;
```

### 4. `docs/cookbook/person-or-place.md` (whole page, 30 lines)
Prose: "Step 0 is the resolve of save a body. No row: create it (steps 1–3). A plain page (`entity_type = 'page'`, e.g.
a ghost an earlier `[[Bob Sample]]` made) that is not a day page: promote it instead. Any other row: the handle is
taken; choose another…". Block:
```sql
-- 0. does the handle exist already?  :handle_key = title_key(:handle_title), contract/titles-and-wikilinks.md
SELECT p.id, p.entity_type FROM pages p WHERE p.title_key = :handle_key;
-- create: … (BEGIN IMMEDIATE; INSERT entities …; INSERT pages …; INSERT people …; COMMIT;)
-- promote: the plain page :ghost_id becomes a person; its links stay (the id does not change)
BEGIN IMMEDIATE;
UPDATE entities SET entity_type = 'person' WHERE id = :ghost_id AND entity_type = 'page';   -- cascades to pages.entity_type
INSERT INTO people(id, name) VALUES (:ghost_id, 'Ana Example');
COMMIT;
```
Closing prose: "A promotion cannot go wrong quietly: a page that is already named, or none at all, makes the `UPDATE`
change no row, so the `people` insert fails on its key; a day page makes the `UPDATE` itself fail (`pages_day_page_plain`)…"
`tests/schema/named.py:52-88` runs it literally: it finds step 0 as the statement starting `SELECT P.ID`, expects 10
statements with the promotion `[BEGIN, UPDATE, INSERT, COMMIT]`, reuses `promo[1]` with `.replace("'person'", "'place'", 1)`,
and asserts step 0 rows `== [(pid, 'person')]` (line 64) and `== [(gid, 'page')]` (line 75).
The save contract's revive rule this mirrors: `docs/cookbook/save-a-body.md` step 2a, "found, but tombstoned: revive it
(the UI tells the owner the save revives a deleted page) `UPDATE entities SET deleted_at = NULL WHERE id = :found_id;`".
`docs/guides/importing.md:152` (the `person` row of the writes table): "a person; promotes the plain page holding that
title ([a person or a place](../cookbook/person-or-place.md)), never a day page".

### 5. `docs/cookbook/README.md:4-5`
"All examples filter tombstones (`e.deleted_at IS NULL`). Every write transaction starts with `BEGIN IMMEDIATE`
([connection setup](../contract/connections.md)), and every block runs in `tests/`."
Suites index the single-statement write blocks by position (`journal.py:98-104` uses `st[1]`, `st[2]` of `where-was-i`;
`habits.py:53`), so **do not** wrap those statements in `BEGIN`/`COMMIT`.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| A suite | `tests/.venv/Scripts/python.exe tests/schema/named.py` (also `identity.py`, `facts.py`, `cookbook.py`) | `<name>: N/N met expectations` |
| Mutants | `tests/.venv/Scripts/python.exe tests/schema/mutants.py` | `mutants: M/M met expectations` |
| Everything | `tests/.venv/Scripts/python.exe tests/run_all.py` | `20/20 suites passed` |

## Scope

**In scope**: `docs/cookbook/import-a-row-once.md`, `everything-about.md`, `metric-series.md`, `person-or-place.md`,
`README.md`; `docs/guides/importing.md` (line 152–153 only); `tests/schema/named.py`, `identity.py`, `facts.py`,
`mutants.py`; `tests/README.md` (mutant count); `docs/plans/README.md` (status row).

**Out of scope**:
- `docs/schema/schema.sql` — no DDL change. (A DB-side re-check of link endpoints on a type change is a recorded
  deferral in `docs/architecture/non-goals.md`, "Re-checking links when an entity changes type"; do not add it.)
- Whether `days-that-name` / `backlinks` should follow a `redirect` hop, and whether `ghost_pages` should list a
  redirect's target — open owner decisions recorded in `docs/plans/README.md`; not this plan.
- The day view's "(edited)" flag.

## Git workflow

- Branch `advisor/017-cookbook-right-rows`. Message e.g. `cookbook: the ark query lists a friend once, the series is 90 days, a promotion revives and never takes a stub`, body names the suites changed and `Ran tests/run_all.py: 20/20 suites passed.`

## Steps

### Step 1: `import-a-row-once` — the update is its own `BEGIN IMMEDIATE` transaction

Replace the trailing comment+UPDATE with:
```sql
-- a later run finds the note changed: update the live page that has the key; a tombstoned one stays gone
BEGIN IMMEDIATE;
UPDATE pages
   SET body = 'Feed the starter the night before; 75% water.'
 WHERE id = (SELECT id FROM entities
              WHERE source = 'import:vault' AND import_key = :import_key AND deleted_at IS NULL);
-- then, before COMMIT, the link sync of cookbook/save-a-body.md (steps 1-4) for this page's new body
COMMIT;
```
**Verify**: `tests/.venv/Scripts/python.exe tests/schema/identity.py` and `tests/schema/document.py` → all met
(identity still finds three non-BEGIN/COMMIT statements; the comment-only line is dropped by `statements()`).

### Step 2: `everything-about` — a symmetric relation once

Add to the second leg's `WHERE`: `AND l.kind NOT IN (SELECT kind FROM link_kinds WHERE symmetric = 1)`. Add one
sentence to the prose after "…what the person's page body links to is in the second.":
"A symmetric kind (`friend`, `family`, `related`) is stored in both directions ([D8](../decisions/D08-entities-and-links.md)), so it is read in the first leg only."
In `named.py` section C (after line 99, before `ark = …`): create a second person `ana = named(c, 'person', 'Ana Example', name='Ana Example')`
and `link(c, bod, ana, 'friend')`, and add `('friend', ana, 'in')` to the expected list at line 101–102. Add a
label `'...and a friend once, not twice'` asserting `sum(1 for r in ark if r[0] == 'friend') == 1`.
Mutant (owner `named`): `mutate(" AND l.kind NOT IN (SELECT kind FROM link_kinds WHERE symmetric = 1)", "")`.
**Verify**: `named.py` → all met; the mutant → `caught`.

### Step 3: `metric-series` — exactly 90 days ending on `:day`

Change the `WHERE` to `WHERE me.day > date(:day, '-90 day') AND me.day <= :day`. In `facts.py` add (with the
existing helpers): a fresh DB, metric `weight` (unit `kg`), readings on `2026-07-04`, `2026-07-05`, `2026-10-02`,
`2026-10-03`; run `block('metric-series')` with `{'day': '2026-10-02'}` and assert the days are
`['2026-07-05', '2026-10-02']` (label: `cookbook/metric-series: the 90 days ending on :day, none after it`).
Mutants (owner `facts`): `me.day <= :day` → `1`; and `me.day > date(` → `me.day >= date(`.
**Verify**: `facts.py` → all met; both mutants `caught`. (2026-10-02 minus 90 days is 2026-07-04, excluded.)

### Step 4: `person-or-place` — revive a tombstoned page; a redirect stub is taken

- Step 0 becomes:
  ```sql
  -- 0. does the handle exist already?  :handle_key = title_key(:handle_title), contract/titles-and-wikilinks.md
  SELECT p.id, p.entity_type, e.deleted_at,
         EXISTS (SELECT 1 FROM links r WHERE r.from_id = p.id AND r.kind = 'redirect') AS is_stub
    FROM pages p JOIN entities e ON e.id = p.id
   WHERE p.title_key = :handle_key;
  ```
- The promotion's UPDATE becomes (still one statement):
  ```sql
  UPDATE entities SET entity_type = 'person', deleted_at = NULL   -- cascades to pages.entity_type; revives a tombstoned page
   WHERE id = :ghost_id AND entity_type = 'page'
     AND NOT EXISTS (SELECT 1 FROM links WHERE from_id = :ghost_id AND kind = 'redirect');
  ```
- Prose: change "A plain page (…) that is not a day page: promote it instead." to "A plain page (…) that is not a day
  page and not a redirect stub: promote it instead — a tombstoned one is revived by the promotion, as a save revives a
  page it names ([save a body](save-a-body.md) step 2a; the UI says so). A stub (`is_stub`) or any other row: the handle is
  taken; choose another…". In the closing paragraph, change "a page that is already named, or none at all," to
  "a page that is already named, a redirect stub, or none at all,".
- `docs/guides/importing.md:152`: "promotes the plain page holding that title (…), never a day page" → "…never a day
  page or a redirect stub; a tombstoned one is revived".
- `named.py`: update line 64's expected rows to `[(pid, 'person', None, 0)]` and line 75's to `[(gid, 'page', None, 0)]`.
  Add two expectations in section B after line 88:
  1. `a tombstoned ghost is promoted and revived` — `t = page(c, 'Cleo Sample')`; tombstone it
     (`UPDATE entities SET deleted_at={NOW} WHERE id=?`); run `promo[1]` and `promo[2]` with `{'ghost_id': t}`;
     assert `one(c, 'select entity_type is \'person\' and deleted_at is null from entities where id=?', (t,)) == 1`.
  2. `a redirect stub is not promoted: the UPDATE changes no row and the people insert fails` — `new = page(c, 'Dana Sample (colleague)')`,
     `stub = page(c, 'Dana', body='#REDIRECT [[Dana Sample (colleague)]]')`, `link(c, stub, new, 'redirect')`; `tryx(c, promo[1], {'ghost_id': stub}) == 'OK'`
     and `changes() == 0` and `tryx(c, promo[2], {'ghost_id': stub}).startswith('ERR')`.
- Mutants (owner `named`): `", deleted_at = NULL   -- cascades"` → `"   -- cascades"`; and remove
  `"\n     AND NOT EXISTS (SELECT 1 FROM links WHERE from_id = :ghost_id AND kind = 'redirect')"`.

**Verify**: `named.py` → all met (its structure check of 10 statements still holds); `cookbook.py` → all met; both mutants `caught`.

### Step 5: Say once why a one-statement write needs no `BEGIN`

In `docs/cookbook/README.md:4-5`, after "Every write transaction starts with `BEGIN IMMEDIATE` ([connection setup](../contract/connections.md))," insert:
"a block that writes one statement and reads nothing before it (start a habit, a correction, an `at` link) is shown alone
— it is its own transaction; inside a larger write it goes between that write's `BEGIN IMMEDIATE` and `COMMIT`".

**Verify**: `tests/.venv/Scripts/python.exe tests/schema/document.py` → all met.

### Step 6: Counts and full run

Update the mutant count in `tests/README.md` (`mutants.py` row) to the new total
(`grep -c "^ ('" tests/schema/mutants.py`). Full run.

**Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`.

## Test plan

New expectations: named.py (friend once; tombstoned ghost revived; stub refused), facts.py (90-day window). Changed:
named.py lines 64, 75 (step 0 columns). New mutants: 6 (Steps 2–4). Pattern: existing section B/C expectations in `named.py`.

## Done criteria

- [ ] `grep -n "BEGIN IMMEDIATE" docs/cookbook/import-a-row-once.md` → 2 matches
- [ ] `grep -n "symmetric = 1" docs/cookbook/everything-about.md` → 1 match
- [ ] `grep -n "me.day <= :day" docs/cookbook/metric-series.md` → 1 match
- [ ] `grep -n "is_stub\|deleted_at = NULL" docs/cookbook/person-or-place.md` → ≥ 2 matches
- [ ] `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`; mutants all caught
- [ ] `tests/README.md` mutant count equals `grep -c "^ ('" tests/schema/mutants.py`
- [ ] `git diff --stat -- docs/schema/` → empty
- [ ] `docs/plans/README.md` row 017 updated

## STOP conditions

- An excerpt does not match the live file.
- `named.py`'s statement-shape check fails (you added or removed a statement in `person-or-place`).
- The revive-on-promote choice conflicts with something you find in `docs/contract/deletion-and-corrections.md` or
  D11/D20 (e.g. a sentence saying a promotion must never revive). Report it — the owner chooses.

## Maintenance notes

- If a fourth symmetric kind or a new direction-sensitive kind is registered, re-read `everything-about`.
- The promotion's `NOT EXISTS redirect` is the recipe-level guard; the DB-level re-check stays deferred
  (`non-goals.md`). If a writer ever reports such a link, open an issue and reconsider the trigger.
