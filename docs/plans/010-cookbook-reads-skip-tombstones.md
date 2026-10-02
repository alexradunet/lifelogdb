# Plan 010: Every cookbook read of where the owner was skips tombstoned day pages and places

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 3e2fcf4..HEAD -- SCHEMA.md tests/schema/journal.py tests/schema/links.py tests/schema/mutants.py tests/README.md`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live files before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW (three cookbook queries gain a join or a filter; no DDL change)
- **Depends on**: none; run after 009 and before 011 if those are scheduled (all three edit
  `mutants.py` and the mutant count in `tests/README.md` — read the current count before editing it).
- **Category**: bug (contract)
- **Planned at**: commit `3e2fcf4`, 2026-10-02

## Why this matters

SCHEMA.md §2.3 states: "Every read path filters `deleted_at IS NULL`." The §6 cookbook is what any
writer or reader copies verbatim, and three of its queries about *where the owner was* each filter a
different end of the `at` / `located-in` path:

| query | filters the place | filters the day page |
|---|---|---|
| §6.2 day view, the `'at'` leg | yes | **no** |
| §6.9 "where was I on :day?" | yes | **no** |
| §6.9 "the days I was at a place" | (the caller's `:place_id`) | yes |
| §6.11 everything inside a place | **no** (neither the place shown nor the places walked) | yes |

Executed on a scratch database built from §3: after tombstoning the day page `2026-07-31`, §6.2 and
§6.9 still list its places; after tombstoning a mistaken place `Tokio (typo)` located in Japan, §6.11
still lists its day. Tombstoning is the only deletion (D11), so the reads must honour it everywhere.

## Current state

The three SQL texts at commit `3e2fcf4` (in `SCHEMA.md`):

§6.2, the `'at'` leg (inside the UNION ALL of "### 6.2 The day view"):
```sql
  SELECT 'at', NULL, pl.title
    FROM pages d
    JOIN links l    ON l.from_id = d.id AND l.kind = 'at'
    JOIN pages pl   ON pl.id = l.to_id
    JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL
   WHERE d.title_key = :day
```

§6.9, "where was I on :day?" (in "### 6.9 Where was I"):
```sql
SELECT pl.title, l.note
  FROM pages d
  JOIN links l    ON l.from_id = d.id AND l.kind = 'at'
  JOIN pages pl   ON pl.id = l.to_id
  JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL
 WHERE d.title_key = :day
 ORDER BY pl.title;
```

§6.11 ("### 6.11 Everything inside a place"):
```sql
WITH RECURSIVE inside(id) AS (
  SELECT :place_id
  UNION
  SELECT l.from_id FROM links l JOIN inside ON l.to_id = inside.id WHERE l.kind = 'located-in'
)
SELECT d.day, pl.title AS place
  FROM inside
  JOIN links l    ON l.to_id = inside.id AND l.kind = 'at'
  JOIN pages d    ON d.id = l.from_id AND d.title = d.day
  JOIN entities e ON e.id = d.id AND e.deleted_at IS NULL
  JOIN pages pl   ON pl.id = inside.id
 WHERE d.day BETWEEN :from_day AND :to_day
 ORDER BY d.day, pl.title;
```

Existing mutants that target these texts — their target strings must still be found after your edit
(`mutate()` asserts its target exists):
- `('journal', '§6.9 says I was at a tombstoned place', mutate("  JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL\n WHERE d.title_key = :day", …))`
- `('links', '§6.11 walks with UNION ALL', mutate("  SELECT :place_id\n  UNION\n", …))`
- `('cookbook', '§6.11 names a column that does not exist', mutate('SELECT d.day, pl.title AS place', …))`

Tests that run these blocks:
- `tests/schema/journal.py` — the §6.2 day view (`DV = block('6.2')`, around line 60) and §6.9
  (lines 95–109: places `par`/`cor`/`spa`, days via `W.capture`, `st = statements(block('6.9'))`,
  `st[1]` is "where was I", `st[2]` "the days at a place"; line ~108 already checks a tombstoned place).
- `tests/schema/links.py` lines 51–73 — §6.11 with Japan/Kanto/Tokyo, `P`, `WANT`, and a cycle check.
- Tombstone idiom used by the suites: `c.execute(f'UPDATE entities SET deleted_at={NOW} WHERE id=?', (x,))`
  (see `journal.py:90`).

## Commands you will need

| Purpose | Command (Git Bash, repo root) | Expected on success |
|---|---|---|
| All suites | `tests/.venv/Scripts/python.exe tests/run_all.py` (elsewhere `python3 tests/run_all.py`) | `20/20 suites passed` |
| Mutant count | `grep -cE "^ \('[a-z]+', " tests/schema/mutants.py` | the new count |

## Scope

**In scope:** the three SQL texts above in `SCHEMA.md` (and nothing else in §6.2/§6.9/§6.11 except a
clause in §6.11's prose if you choose to add one), `tests/schema/journal.py`, `tests/schema/links.py`,
`tests/schema/mutants.py`, `tests/README.md` (mutant count), `plans/README.md` (status row).

**Out of scope:** §3 DDL; §6.3 (already drops a tombstoned day, `journal.py:90-91`); other §6 blocks.

## Git workflow

Branch `advisor/010-cookbook-tombstones`; one commit; subject like
`SCHEMA.md §6.2, §6.9, §6.11: where the owner was skips tombstoned days and places`; bullets; `Ran: …`.

## Steps

### Step 1: Write the failing expectations first

In `tests/schema/journal.py`, at the end of the §6.9 section (after the line ending
`...and a tombstoned place is not where I was`), add:

```python
c.execute(f'UPDATE entities SET deleted_at={NOW} WHERE id=?', (d31,))
S.K('§6.9 where was I on a tombstoned day: nowhere', [r[0] for r in c.execute(st[1], {'day': '2026-07-31'})] == [])
S.K('§6.2 a tombstoned day lists none of its places', [r for r in c.execute(DV, {'day': '2026-07-31'}) if r[0] == 'at'] == [])
```
`DV` is defined earlier in `journal.py` (`DV = block('6.2')`); if it is defined only inside another
scope, re-read it with `block('6.2')`. `d31` is the 2026-07-31 day page created in the §6.9 section and
already linked `at` Lakeside.

In `tests/schema/links.py`, before the `link(c, japan, tokyo, 'located-in')` line that creates the
cycle, add:

```python
typo = named(c, 'place', 'Tokio (typo)'); link(c, typo, japan, 'located-in')
d6 = day_page(c, '2019-04-09', 'mistyped place'); link(c, d6, typo, 'at')
c.execute(f'UPDATE entities SET deleted_at={NOW} WHERE id=?', (typo,))
S.K('§6.11 leaves out a tombstoned place and its days', c.execute(block('6.11'), P).fetchall() == WANT, c.execute(block('6.11'), P).fetchall())
```

**Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` → `journal` and `links` FAIL, each on the
new expectations only (this proves the gap). Any other failure: STOP.

### Step 2: Fix the three queries

- §6.2 `'at'` leg and §6.9 "where was I": add a join on the day page's entity **directly after
  `FROM pages d`** (so the existing mutant target stays intact):

  ```sql
    FROM pages d
    JOIN entities de ON de.id = d.id AND de.deleted_at IS NULL
    JOIN links l    ON l.from_id = d.id AND l.kind = 'at'
  ```
  Keep the column alignment of the surrounding lines. In §6.2 the lines are indented two more spaces
  (they sit inside the UNION ALL) — match that.
- §6.11: skip tombstoned places both in the walk and in the result. Change the recursive step to

  ```sql
  SELECT l.from_id FROM links l JOIN inside ON l.to_id = inside.id
    JOIN entities ep ON ep.id = l.from_id AND ep.deleted_at IS NULL
   WHERE l.kind = 'located-in'
  ```
  and add `JOIN entities epl ON epl.id = pl.id AND epl.deleted_at IS NULL` right after
  `JOIN pages pl   ON pl.id = inside.id`. Keep `SELECT :place_id\n  UNION\n` and
  `SELECT d.day, pl.title AS place` byte-identical (mutant targets).

**Verify**: all suites → `20/20 suites passed` (`cookbook.py` runs every §6 block on a plain and a
hardened connection; `links.py`'s cycle expectation must still hold).

### Step 3: Add two mutants

Append to `MUTANTS` in `tests/schema/mutants.py`:

```python
 ('journal', '§6.9 lists the places of a tombstoned day', mutate("  FROM pages d\n  JOIN entities de ON de.id = d.id AND de.deleted_at IS NULL\n  JOIN links l    ON l.from_id = d.id AND l.kind = 'at'\n  JOIN pages pl   ON pl.id = l.to_id\n  JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL\n WHERE", "  FROM pages d\n  JOIN links l    ON l.from_id = d.id AND l.kind = 'at'\n  JOIN pages pl   ON pl.id = l.to_id\n  JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL\n WHERE")),
 ('links', '§6.11 lists a tombstoned place', mutate('JOIN entities epl ON epl.id = pl.id AND epl.deleted_at IS NULL', 'JOIN entities epl ON epl.id = pl.id')),
```
Adjust each `mutate()` target to the exact text you wrote in Step 2 (it must occur in the document;
for the first one, the target must match §6.9 and not §6.2 — §6.9's `WHERE` has one leading space,
§6.2's has three). Read the current count in `tests/README.md` and add 2.

**Verify**: all suites → `20/20 suites passed`; the mutants output shows both new lines as `caught`;
the `grep -cE` count equals the number in `tests/README.md`.

## Test plan

Step 1's three expectations (tombstoned day in §6.2 and §6.9, tombstoned place in §6.11) plus two
mutants; patterns copied from `journal.py:90-91` (§6.3's tombstoned-day check) and `links.py:51-73`.

## Done criteria

- [ ] `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`
- [ ] `grep -c "de.deleted_at IS NULL" SCHEMA.md` → `2`; `grep -c "epl.deleted_at IS NULL" SCHEMA.md` → `1`
- [ ] Both new mutants `caught`; `tests/README.md` count matches `mutants.py`
- [ ] Only in-scope files changed; `plans/README.md` status row for 010 updated

## STOP conditions

- Step 1 fails on anything other than the new expectations.
- An existing mutant reports "mutation target found 0x" after Step 2 (you changed a target string).
- `cookbook.py` fails on the hardened connection.

## Maintenance notes

- A new cookbook read that joins through `links` must filter the `entities` row of every endpoint it
  shows or walks through. A reviewer should check each new §6 block against §2.3's sentence.
