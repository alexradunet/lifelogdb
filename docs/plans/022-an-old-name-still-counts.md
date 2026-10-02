# Plan 022: A mention written under an old or misspelt name counts for the page it redirects to; a redirect target is never a ghost

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 4d84261..HEAD -- docs/cookbook/backlinks.md docs/cookbook/days-that-name.md docs/cookbook/ghost-pages.md docs/contract/titles-and-wikilinks.md docs/schema/schema.sql tests/schema/named.py tests/wikilinks/docchecks.py tests/schema/mutants.py`
> Plans 014, 017 and 019 change other lines of some of these files — expected. Compare the quoted lines; STOP on a mismatch in those.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW
- **Depends on**: 017 (same suites and mutants file; run after it). Run **before** 016.
- **Category**: bug
- **Planned at**: commit `4d84261`, 2026-10-02

## Why this matters

In `life.db` a page title never changes. To fix a title — or a misspelt `[[Ana Exmaple]]` that made a ghost page —
the owner makes the old page a one-line stub `#REDIRECT [[Ana Example]]` and adds `links(kind='redirect', from=old, to=new)`
(`docs/contract/titles-and-wikilinks.md`, "Renames"). That page says "**Consumers follow one hop**". But the two
mention reads do not: `docs/cookbook/days-that-name.md` ("When did I see Ana?") and `docs/cookbook/backlinks.md` match
only `l.to_id = :entity_id`, so every day written under the old or misspelt name is missing — exactly the days the
redirect was made for. Separately, the view `ghost_pages` (`docs/schema/schema.sql`) ignores inbound `redirect` links,
so an empty page that a rename points at is listed for cleanup, contradicting its own comment ("renames never create
ghosts"); tombstoning it would leave the stub pointing at a tombstone.

## Decisions (made by the owner on 2026-10-02 — do not reopen)

- The mention reads follow one `redirect` hop: a link to a stub that redirects to the entity counts as a link to the entity.
- A page that a `redirect` points at is not a ghost: drop the exclusion in the view.

## Current state

- `docs/cookbook/days-that-name.md:9-15`:
  ```sql
  SELECT DISTINCT d.day, substr(d.body, 1, 60) AS start
    FROM links l
    JOIN pages d    ON d.id = l.from_id AND d.title = d.day
    JOIN entities e ON e.id = d.id AND e.deleted_at IS NULL
   WHERE l.to_id = :entity_id AND l.kind IN ('wikilink', 'about')
   ORDER BY d.day DESC;
  ```
- `docs/cookbook/backlinks.md:3-13`:
  ```sql
  SELECT l.kind, e.entity_type, l.from_id, pg.title AS label
    FROM links l
    JOIN entities e  ON e.id = l.from_id AND e.deleted_at IS NULL
    JOIN pages    pg ON pg.id = l.from_id
   WHERE l.to_id = :page_id
     AND l.kind <> 'redirect';   -- a rename stub is not a mention of its replacement (contract/titles-and-wikilinks.md)
  ```
  followed by "A person or a place is a page, so its title labels it. Symmetric kinds are mirrored (D8), so this one direction suffices for them."
- `docs/contract/titles-and-wikilinks.md` (the "Renames" paragraph, ~line 97): "…Consumers follow one hop; `redirect` links are excluded from backlink queries ([backlinks](../cookbook/backlinks.md))."
- `docs/schema/schema.sql:348-357` (the view; the comment is stored in every database, so word it carefully):
  ```sql
  CREATE VIEW ghost_pages AS
    -- empty plain pages nobody points at, 30 days old: a link target created by a capture-time typo and
    -- never written (renames never create ghosts). The page of a person or a place is never a ghost,
    -- however empty (D20). The UI lists them; tombstoning is the owner's act.
    SELECT p.id, p.title, e.created_at
      FROM pages p JOIN entities e ON e.id = p.id
     WHERE p.entity_type = 'page' AND p.body = '' AND e.deleted_at IS NULL
       AND e.created_at < strftime('%Y-%m-%dT%H:%M:%fZ','now','-30 day')
       AND NOT EXISTS (SELECT 1 FROM links l WHERE l.to_id = p.id AND l.kind <> 'redirect')
       AND NOT EXISTS (SELECT 1 FROM links l WHERE l.from_id = p.id);
  ```
- `docs/cookbook/ghost-pages.md:7`: "The view (schema) lists empty plain pages nothing points at, 30 days old…"
- Suites: `tests/schema/named.py:104-110` runs `days-that-name` and `backlinks` on a person `bod` with day pages
  `m1` (2026-09-28), `m3` (2026-09-30), a typo day `m4` (2026-10-01) writing `[[Bbo Sample]]`, and asserts the typo day is
  **not** listed (no redirect exists there — that stays true). `tests/schema/named.py:120-123` (section D) checks
  `ghost_pages`. `tests/wikilinks/docchecks.py:92-102` (section C) runs `backlinks` with pages `new`='Diet plan',
  `old`='Diet' (a stub, `redirect` old→new) and a day page `mm` linking `new`, and asserts the result kinds are `['wikilink']`.
- Mutant idiom (`tests/schema/mutants.py`): `('<owner suite>', '<what is broken>', mutate("old", "new"))`; only
  `tests/schema/` suites can own a mutant.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Named suite | `tests/.venv/Scripts/python.exe tests/schema/named.py` | `named entities: N/N met expectations` |
| Doc save contract | `tests/.venv/Scripts/python.exe tests/wikilinks/docchecks.py` | all met |
| Everything | `tests/.venv/Scripts/python.exe tests/run_all.py` | `20/20 suites passed` |

## Scope

**In scope**: the four docs pages above, `docs/schema/schema.sql` (the `ghost_pages` view only),
`tests/schema/named.py`, `tests/wikilinks/docchecks.py`, `tests/schema/mutants.py`, `tests/README.md` (mutant count),
`docs/plans/README.md`.

**Out of scope**: `everything-about` (it already shows the stub as an incoming `redirect` row); more than one hop;
any other view or table.

## Git workflow

Branch `advisor/022-old-name-counts`. Message e.g. `cookbook: the days that name someone and backlinks follow one redirect; a redirect target is no ghost`; body: suites changed, what you ran.

## Steps

### Step 1: `days-that-name` follows one hop

Replace `WHERE l.to_id = :entity_id AND l.kind IN ('wikilink', 'about')` with
```sql
 WHERE l.to_id IN (SELECT :entity_id
                   UNION SELECT r.from_id FROM links r WHERE r.to_id = :entity_id AND r.kind = 'redirect')
   AND l.kind IN ('wikilink', 'about')
```
Add to the prose before the block: "A day that wrote an old or misspelt name now redirected to the entity counts too
(one hop, [titles and wikilinks](../contract/titles-and-wikilinks.md))."

### Step 2: `backlinks` follows one hop

Replace `WHERE l.to_id = :page_id` with the same `IN (SELECT :page_id UNION SELECT r.from_id FROM links r WHERE r.to_id = :page_id AND r.kind = 'redirect')`,
keeping `AND l.kind <> 'redirect'` and its comment. Add after the block: "A mention of a stub that redirects here is a
mention of this page (one hop); the stub's own `redirect` row is not."

### Step 3: Say it in the contract

In the "Renames" paragraph change "Consumers follow one hop; `redirect` links are excluded from backlink queries" to
"Consumers follow one hop — the days that name someone and backlinks count a stub's mentions as its replacement's
([the days that name someone](../cookbook/days-that-name.md), [backlinks](../cookbook/backlinks.md)); the `redirect` row itself is never a backlink".

**Verify** (Steps 1–3): `tests/.venv/Scripts/python.exe tests/schema/document.py` → all met; `cookbook.py` → all met.

### Step 4: A redirect target is not a ghost

In `schema.sql`, change `AND NOT EXISTS (SELECT 1 FROM links l WHERE l.to_id = p.id AND l.kind <> 'redirect')` to
`AND NOT EXISTS (SELECT 1 FROM links l WHERE l.to_id = p.id)`. Change the comment's "(renames never create ghosts)" to
"(a page anything links to, a rename's target included, is not one)". `ghost-pages.md:7` stays true ("nothing points at").

### Step 5: Suites and mutants

- `named.py` section C, after line 110: make the typo day count through a redirect — turn the ghost `Bbo Sample` into a
  stub: `bb = one(c, "select id from pages where title_key='bbo sample'")`, `W.edit_body(c, bb, '#REDIRECT [[Bob Sample]]')`,
  `link(c, bb, bod, 'redirect')`; assert `days-that-name` for `bod` now returns `['2026-10-01', '2026-09-28']`
  (m3's day is tombstoned above; check the live list first and state the expectation from it) with label
  `'...and a day that wrote a misspelt name now redirected to him (one hop)'`.
  Note the earlier `'...and not the typo'` expectation at line 103 runs before this and must stay as is.
- `named.py` section D: add a rename target to the ghost check: `tgt = page(c, 'Renamed target')`,
  `st = page(c, 'Old target name', body='#REDIRECT [[Renamed target]]')`, `link(c, st, tgt, 'redirect')`, back-date both
  with the existing `UPDATE entities SET created_at = …'-40 day'` (place them before that line), and keep the expected
  list `['Typo page']` (label: `'...and not a page a rename points at'` — or extend the existing label).
- `docchecks.py` section C: add `dd = mk('2026-09-29', '[[Diet]]', '2026-09-29')` with a `wikilink` dd→old, and change
  the expectation to `sorted((r[0], r[2]) for r in rows) == sorted([('wikilink', mm), ('wikilink', dd)])`
  with label `'C cookbook/backlinks lists the wikilinks to the page and to its stub, not the stub\'s redirect row'`.
- Mutants (owner `named`):
  1. days-that-name without the hop: `mutate("UNION SELECT r.from_id FROM links r WHERE r.to_id = :entity_id AND r.kind = 'redirect')", ")")`
  2. ghost_pages ignores redirects again: `mutate("AND NOT EXISTS (SELECT 1 FROM links l WHERE l.to_id = p.id)\n", "AND NOT EXISTS (SELECT 1 FROM links l WHERE l.to_id = p.id AND l.kind <> 'redirect')\n")`
- Update the mutant count in `tests/README.md`.

**Verify**: `named.py`, `docchecks.py` → all met; `mutants.py` → all caught; full run → `20/20 suites passed`.

## Test plan

New/changed expectations: named.py (one-hop day; rename target not a ghost), docchecks.py C (backlinks through the
stub). Two new mutants.

## Done criteria

- [ ] `grep -c "kind = 'redirect')" docs/cookbook/days-that-name.md docs/cookbook/backlinks.md` → 1 each
- [ ] `grep -n "l.kind <> 'redirect'" docs/schema/schema.sql` → no output
- [ ] `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`; mutants all caught
- [ ] `docs/plans/README.md` row 022 updated

## STOP conditions

- An excerpt does not match the live file (other than the edits of plans 014/017/019).
- `named.py`'s existing expectations change result for a reason other than the one-hop rule (report the label).
- Plan 016 has already landed and its ghost-page expectations conflict with Step 4 (report; do not loosen them).

## Maintenance notes

- One hop only: a stub of a stub is not followed. If chains appear in real data, that is an issue.
- `everything-about` deliberately still shows the stub as an incoming `redirect` row — the owner sees the old name.
