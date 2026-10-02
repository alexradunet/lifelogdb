# Plan 011: A redirect stub may point at a person or a place

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 3e2fcf4..HEAD -- SCHEMA.md tests/schema/links.py tests/schema/mutants.py tests/README.md`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live files before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: MED — a §3 DDL change (one `link_kinds` row) that touches the link map, §2.4 and a suite
  expectation that pins today's behaviour.
- **Depends on**: **the owner's decision** (below). Run after 009 and 010 if scheduled (shared
  `mutants.py` and mutant count).
- **Category**: bug (contract)
- **Planned at**: commit `3e2fcf4`, 2026-10-02

## Owner decision required before execution

The executor must find, in this plan's row of `plans/README.md`, the status `TODO (owner chose A)`.
Any other status: **STOP** without changing anything.

- **A (recommended):** widen `redirect` to `page → page, person, place`. The stub stays a plain page;
  its replacement may be a person or a place. Fixes both contradictions below. Renaming a person's or
  place's *own* title stays unsupported (their handle is permanent; a person's display name is the
  editable `people.name`, D20).
- **B:** keep `redirect` page → page and instead say in §2.4 and §6.14 that a redirect's target is
  never promoted and a person or place is never a redirect target. (Not planned here; it needs §6.14
  to refuse a promotion, which no writer currently checks.)

## Why this matters

D20 makes a person or a place a page, and §6.14 promotes a plain page into one. §2.4's rename procedure
("create the new page, make the old page a one-line stub `#REDIRECT [[New Title]]` and add
`links(kind='redirect', from=old, to=new)`") is the only way to fix a title, since titles are permanent.
But `link_kinds` says `redirect` goes from `page` to `page` only. Executed on a scratch database from §3:

1. A ghost page `Bob Sampel` (made by a misspelt `[[Bob Sampel]]`) cannot redirect to the person
   `Bob Sample`: "link endpoint type not allowed for this kind".
2. A redirect to a plain page, followed by the §6.14 promotion of that page into a person, leaves a
   `redirect` row page → person that the kind forbids. The four §2.5 integrity checks stay green, and
   re-inserting the same row — what any rebuild or replay does — fails.

So two procedures of the contract combine into a state the DDL itself refuses.

## Current state

- `SCHEMA.md` §3, the `link_kinds` seed (around line 773):
  ```
    ('redirect', 0, 'page',      'page',         'old stub page → its replacement; renames, D5'),
  ```
- `SCHEMA.md` §4.3 "Who may link what", the mermaid `link-map` (checked by `tests/schema/diagrams.py`:
  the map must equal `link_kinds`):
  ```
      titled["page, person, place"]
      ...
      page -->|"redirect"| page
  ```
- `SCHEMA.md` §2.4, the "**Renames.**" paragraph (around line 248):
  ```
  **Renames.** A title never changes (`pages_title_fixed`, D5). To fix one: create the new page, make
  the old page a one-line stub (`#REDIRECT [[New Title]]`) and add `links(kind='redirect', from=old,
  to=new)`. Consumers follow one hop; `redirect` links are excluded from backlink queries (§6.5).
  ```
- `tests/schema/links.py` line 18–19 (inside the table of link attempts):
  ```python
      ('redirect page->page', 'OK', m1, pw, 'redirect'),
      ('redirect page->person', 'ERR', pw, pa, 'redirect'), ('about day page->person', 'OK', m1, pb, 'about'), ...
  ```
  where `pa`, `pb` are persons, `pl` a place, `m1` a day page, `pw` a plain page `Wiki`.
- `tests/schema/mutants.py` has a mutant targeting the `located-in` row with the same column layout;
  copy its shape. `tests/README.md` holds the mutant count.
- §3 totals line ("**+ 24 triggers.**" etc.) is unaffected: no object is added.

## Commands you will need

| Purpose | Command (Git Bash, repo root) | Expected on success |
|---|---|---|
| All suites | `tests/.venv/Scripts/python.exe tests/run_all.py` (elsewhere `python3 tests/run_all.py`) | `20/20 suites passed` |
| Diagrams render (optional) | `tests/.venv/Scripts/python.exe tests/run_all.py --mermaid` | `21/21 suites passed` (needs `mmdc`; skip if absent and say so) |
| Mutant count | `grep -cE "^ \('[a-z]+', " tests/schema/mutants.py` | the new count |

## Scope

**In scope:** `SCHEMA.md` (the `redirect` seed row, the §4.3 link map, the §2.4 Renames paragraph),
`tests/schema/links.py`, `tests/schema/mutants.py`, `tests/README.md` (count), `plans/README.md`.

**Out of scope:** every other `link_kinds` row; `links_endpoint_types` trigger (it reads the registry);
§6.5 (already excludes redirect rows); D5/D20 prose unless `document.py` requires a change.

## Git workflow

Branch `advisor/011-redirect-to-named`; one commit; subject like
`SCHEMA.md §3: a redirect may point at a person or a place`; bullets; `Ran: …`.

## Steps

### Step 0: Check the decision

Read this plan's row in `plans/README.md`. **Verify**: it says `owner chose A`. Otherwise STOP.

### Step 1: Failing expectations first

In `tests/schema/links.py`, change the tuple `('redirect page->person', 'ERR', pw, pa, 'redirect')` to
`('redirect page->person (the replacement may be named)', 'OK', pw, pa, 'redirect')` and add, in the same
list, `('redirect page->place', 'OK', m1, pl, 'redirect')` and
`('redirect person->page (a stub is a plain page)', 'ERR', pa, pw, 'redirect')`.
After the loop, add:

```python
g = page(c, 'Sam Bee'); t = page(c, 'Sam B')
S.K('a redirect to a plain page', link(c, g, t, 'redirect') == 'OK')
c.execute("UPDATE entities SET entity_type='person' WHERE id=?", (t,)); c.execute('INSERT INTO people(id,name) VALUES (?,?)', (t, 'Sam B'))
c.execute("DELETE FROM links WHERE from_id=? AND kind='redirect'", (g,))
S.K('...survives the promotion of its target: the same row inserts again', link(c, g, t, 'redirect') == 'OK')
```
(`page()`, `link()` are in `tests/lib/kit.py`; `links` rows are the one table that may be deleted, D11.)
Check `pa`'s and the `m1`→`pl` combinations against existing tuples so you do not create a duplicate
edge (`UNIQUE(from_id, to_id, kind)`) — pick other endpoints if one exists already.

**Verify**: run all suites → `links` FAILS on exactly the new/changed expectations.

### Step 2: The DDL row and the map

- §3: `('redirect', 0, 'page',      'page,person,place', 'old stub page → its replacement, a page, person or place; renames, D5'),`
  (keep the column alignment as well as the longer value allows).
- §4.3 link map: replace `    page -->|"redirect"| page` with `    page -->|"redirect"| titled`.

**Verify**: all suites → `20/20 suites passed` (`diagrams.py` compares the map with `link_kinds`;
`document.py` checks the §3 totals).

### Step 3: §2.4 Renames

Replace the Renames paragraph with:

```
**Renames.** A title never changes (`pages_title_fixed`, D5). To fix one: create the new page, make
the old page a one-line stub (`#REDIRECT [[New Title]]`) and add `links(kind='redirect', from=old,
to=new)`. The stub is a plain page; the replacement may be a page, a person or a place, so a ghost
made by a misspelt `[[Name]]` can point at the person, and a replacement promoted later (§6.14) keeps
its redirect. A person's or a place's own title is its permanent handle and is not renamed (a person's
display name is `people.name`, D20). Consumers follow one hop; `redirect` links are excluded from
backlink queries (§6.5).
```

**Verify**: all suites → `20/20 suites passed`.

### Step 4: A mutant

Append to `MUTANTS`:

```python
 ('links', 'a redirect may not point at a person', mutate("'page,person,place', 'old stub page", "'page',         'old stub page")),
```
(adjust the target to the exact §3 text from Step 2). Update the count in `tests/README.md` (+1).

**Verify**: all suites → `20/20 suites passed`; the new mutant is `caught` (by `links`); count matches.

## Test plan

Three changed/added endpoint expectations and a promotion round-trip in `links.py`; one mutant.

## Done criteria

- [ ] `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`
- [ ] `grep -n "('redirect', 0, 'page'" SCHEMA.md` shows `page,person,place`
- [ ] `grep -n 'page -->|"redirect"| titled' SCHEMA.md` → one match
- [ ] New mutant `caught`; count in `tests/README.md` matches
- [ ] Only in-scope files changed; `plans/README.md` row updated

## STOP conditions

- The owner has not chosen A (Step 0).
- `diagrams.py` or `document.py` fails in a way the steps do not mention.
- The §3 seed row or the link map differs from the quote (drift).

## Maintenance notes

- This changes a registry row before the freeze, which D13 allows (§3 is edited in place). After the
  freeze it would be an additive-only migration question.
- A writer still decides when to write a stub; §6.14 needs no change.
