# Plan 016: Every rule of the DDL has an expectation that fails when it is removed, and a mutant that proves it

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 4d84261..HEAD -- tests/ docs/schema/schema.sql`
> Plans 014, 015 and 017 are expected to have landed first and change some of these files; that is fine. Re-read
> every `mutate("old", …)` anchor you add against the **live** `docs/schema/schema.sql` — `mutate()` asserts its old
> text exists, so a stale anchor fails loudly at import time.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: LOW (adds expectations; changes no rule)
- **Depends on**: 015 (runner/temp cleanup), 017 (cookbook text changes some recipes). Run after both.
- **Category**: tests
- **Planned at**: commit `4d84261`, 2026-10-02

## Why this matters

The repo's product is a database design (`docs/`, DDL in `docs/schema/schema.sql`); `tests/` proves it. `AGENTS.md`
says *"a new rule of any kind gets a mutant in `tests/schema/mutants.py`"* and *"a suite loosened to pass proves
nothing"*. An audit on 2026-10-02 copied the docs tree, weakened one rule at a time and ran **all** suites:

- **About 20 rules can be deleted and every suite stays green** — e.g. the CHECKs on `entities.updated_at` /
  `deleted_at`, `people_death_day_order`, `pages_key_folded`, `habit_periods_end_day`, the `source` CHECKs of `links`
  and `habit_periods`, `link_kinds_symmetric`, `metrics.name UNIQUE`, two of the `ghost_pages` filters.
- **Two rules exist only for connections with `foreign_keys=OFF`** (the `IS NOT` in `measurements_supersede_metric`,
  the `'?'` fallback in `links_endpoint_types`) and no suite opens such a connection, though D8 calls the claim *executed*.
- **`mutants.py` counts a crashed suite as "caught"**, contrary to its own docstring, so "70/70" overstates coverage
  (one mutant — `entities_import` made non-unique — is caught only by an unrelated crash).
- **About 35 rules that suites do check have no mutant**, so a later edit could drop the guarding expectation unnoticed.
- The reference implementation's 15 `mutate=` switches (`tests/wikilinks/wikisave.py`) are never exercised.

After this plan, removing any listed rule turns at least one suite red, and `mutants.py` proves it.

## Current state

### How suites are written (match this)

`tests/lib/kit.py` gives every suite: `S = Suite('<name>')`, `S.K(label, condition, detail=None)` (one expectation),
`S.done()` (prints `<name>: X/Y met expectations`, exit 1 unless equal); `fresh(fk=True, rt=True)` (an in-memory DB
with the DDL), `tryx(c, sql, args)` → `'OK'` or `'ERR <message>'`, `one(c, sql)`, `ent(c, typ, source='ui', created=None)`,
`page(c, title, day=None, body='', created=None, key=None)`, `named(c, typ, handle=None, **cols)`, `link(c, f, t, kind, source='ui')`
→ `'OK'`/`'ERR …'`, `measure(c, metric, day, value, source='ui', **cols)` → `'OK'`/`'ERR …'`, `habit(c, metric, start, end=None, source='ui')`.
`NOW` is the SQL expression of the current instant. Labels state the expectation (e.g. `'DELETE refused'`).
Exemplar (`tests/schema/facts.py:11-16`):
```python
c = fresh(); c.execute("INSERT INTO metrics(name,unit) VALUES ('weight','kg')"); W_ = one(c, "select id from metrics where name='weight'")
S.K('mood is seeded (D6)', one(c, "select count(*) from metrics where name='mood'") == 1)
measure(c, W_, '2026-06-01', 70)
S.K('metrics.unit cannot change', tryx(c, "UPDATE metrics SET unit='lb' WHERE name='weight'").startswith('ERR'))
```

### How mutants are written (`tests/schema/mutants.py`)

```python
def mutate(old, new, nth=0):  # {file: its broken text}: the nth occurrence of old over schema.sql then the pages
MUTANTS = [   # (suite, what is broken, the broken document)
 ('dates', 'a day CHECK uses = instead of IS', mutate("CONSTRAINT pages_day CHECK (day IS NULL OR date(day) IS day)", "CONSTRAINT pages_day CHECK (day IS NULL OR date(day) = day)")),
 ...
 ('facts', 'measurements may be updated', mutate("CREATE TRIGGER measurements_no_update BEFORE UPDATE ON measurements\nBEGIN\n", "CREATE TRIGGER measurements_no_update BEFORE UPDATE ON measurements WHEN 0\nBEGIN\n")),
```
Idioms: a CHECK is disabled as `CHECK (1)`; a trigger as `… WHEN 0` (or `WHERE 0;` in its RAISE); an index loses `UNIQUE`.

The caught logic, `tests/schema/mutants.py:97-104`:
```python
for suite, name, text in MUTANTS:
    (ok, n), out = run(suite, text)
    noticed = n != -1 and ok != n
    caught += noticed
    stop = re.search(r'the suite stopped: (.*)', out)
```
`kit.Suite._stopped` turns an uncaught exception into one failed expectation `FAIL the suite stopped: …` plus the X/Y line.
The docstring (`mutants.py:1-3`): *"it must run to its end and report at least one failed expectation (a crash is not 'noticed')"*.

### The reference implementation's switches

`tests/wikilinks/wikisave.py` reads `mutate=` tuples; `tests/wikilinks/probes.py:9` builds `MUT` from its command-line
arguments that do not start with `--` (so `probes.py <ddl> no_nfc` runs with `no_nfc` on). The switch names are the
quoted words in `'<name>' in mutate` / `'<name>' not in mutate` in `wikisave.py`: `ascii_word`, `tag_no_lookbehind`,
`allow_unassigned`, `device_bare_only`, `no_parser`, `no_nfc`, `no_stub_rule`, `alias_kept`, `tags_inside_wikilinks`,
`numeric_tags`, `no_validation`, `self_links`, `no_savepoint`, `no_revive` (confirm with
`grep -o "'[a-z_]*' \(not \)\?in mutate" tests/wikilinks/wikisave.py | sort -u`). The audit found `allow_unassigned`
leaves `probes.py` at 25/25 (that rule is checked by `tests/schema/pages.py` instead).

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| One suite | `tests/.venv/Scripts/python.exe tests/schema/<suite>.py` | `<suite>: N/N met expectations` |
| Mutants | `tests/.venv/Scripts/python.exe tests/schema/mutants.py` | `mutants: M/M met expectations` |
| Everything | `tests/.venv/Scripts/python.exe tests/run_all.py` | `20/20 suites passed` |
(`python3 …` instead of the venv path on Linux/macOS.)

## Scope

**In scope**: `tests/schema/mutants.py`, `tests/schema/dates.py`, `identity.py`, `named.py`, `pages.py`, `links.py`,
`facts.py`, `writers.py`; `tests/README.md` (the mutant count only); `docs/plans/README.md` (status row).

**Out of scope**:
- `docs/` — **no rule changes.** If an expectation you write fails on the unmutated DDL, the docs and the suite
  disagree: STOP and report (do not change the DDL, do not weaken the expectation).
- `pages_fts_delete` (can never fire while `pages_no_delete` exists) — no expectation, no mutant.
- The `ghost_pages` redirect exclusion (`AND l.kind <> 'redirect'`) — its intended behaviour is an open owner decision (plan 017).
- `tests/wikilinks/wikisave.py` behaviour.

## Git workflow

- Branch: `advisor/016-every-rule-tested`; one commit per step is fine.
- Message example: `tests: every CHECK of the DDL has an expectation and a mutant; a crash is not a catch` with a
  body listing suites changed and `Ran tests/run_all.py: 20/20 suites passed, mutants N/N.`

## Steps

### Step 1: A crash is not a catch

In `mutants.py`, count a mutant as noticed only if the suite reports at least one failed expectation **other than**
the stop: `noticed = n != -1 and ok != n and not (stop and n - ok == 1)` (compute `stop` before `noticed`). Print
`stopped only` instead of `caught` for that case.

**Verify**: `tests/.venv/Scripts/python.exe tests/schema/mutants.py` → expect exactly one `stopped only` line,
for `identity … an entity may be imported twice`, and `mutants: (M-1)/M` for the current M. (If more than one, list them — each needs a
real expectation like Step 2's; add those to Step 2 and continue. If more than three, STOP.)

### Step 2: Expectations for rules nothing checks

Add each expectation to the named suite, near the related ones. Each must **pass** on the unmutated DDL (run the suite
after each group).

| # | suite | expectation (label → condition) |
|---|---|---|
| 1 | identity | `a second entity with the same (source, import_key) is refused` → two `INSERT INTO entities(...,source,import_key) VALUES (...,'import:x','k1')`, the second `tryx(...)` starts with `ERR` and contains `UNIQUE` |
| 2 | dates | `entities.updated_at without milliseconds refused` → insert an entity with `updated_at='2026-01-01T00:00:00Z'` → `ERR` |
| 3 | dates | `a tombstone that is not an instant refused` → `UPDATE entities SET deleted_at='2026-01-01' WHERE id=?` → `ERR` |
| 4 | dates | `measurements.created_at and links.created_at must be instants` → a measurement and a link with `created_at='2026-01-01 10:00:00'` → both `ERR` |
| 5 | dates | `people.death_day must round-trip` → a person with `death_day='2026-9-3'` → `ERR` |
| 6 | dates | `habit_periods.end_day must round-trip` → `habit(c, m, '2026-01-01', '2026-2-1')` on a unitless metric → `ERR` |
| 7 | named | `a death before the birth is refused` → person with `birth_day='2000-01-02', death_day='2000-01-01'` → `ERR` |
| 8 | identity | `links.source and habit_periods.source take the same GLOB as entities` → `link(..., source='UI')` and `habit(..., source='Import:x')` → both `ERR` |
| 9 | links | `link_kinds.symmetric is 0 or 1` → `INSERT INTO link_kinds(kind, symmetric) VALUES ('x-test', 2)` → `ERR` |
| 10 | facts | `a metric name is registered once` → second `INSERT INTO metrics(name) VALUES ('weight')` → `ERR` |
| 11 | pages | `a non-ASCII title's key may not hold an ASCII capital` → `page(c, 'Café Q', key='Café Q')` (wrap in `tryx`-style try/except or use raw SQL) → `ERR` |
| 12 | pages | `a title with leading space refused` → title `' padded'` → `ERR` |
| 13 | pages | `a title with a NUL byte refused` → title `'a\x00b'` bound as a parameter → `ERR` |
| 14 | facts | `with foreign_keys=OFF, a correction of a row that does not exist is refused by the trigger` → `c = fresh(fk=False)`; `measure(c, w, '2026-06-01', 1, supersedes_id=99999)` → `ERR` containing `same metric` |
| 15 | links | `with foreign_keys=OFF, a typed link to an id that does not exist is refused` → `c = fresh(fk=False)`; a day page `d`; `link(c, d, 99999, 'at')` → `ERR` containing `endpoint type` |
| 16 | named | `ghost_pages leaves a page younger than 30 days alone` → a new empty unlinked page (no back-dating) is not in `ghost_pages` |
| 17 | named | `ghost_pages leaves a tombstoned page alone` → an empty page back-dated 40 days and tombstoned is not listed |
| 18 | facts | `cookbook/mood-over-time and cookbook/metric-series skip superseded and retracted readings` → seed: mood 3 on day A then corrected to 4; mood 5 on day B then retracted (NULL correction); run the two cookbook blocks literally (see how `journal.py` / `named.py` extract a recipe's blocks with `docsql.cookbook_blocks()` and `run_block`) → mood-over-time returns exactly `[(A, 4.0)]`; the same shape for `weight` through metric-series |
| 19 | writers | `the DDL marks the file as Lifelog: application_id 0x4C494645 and user_version 1` → on a fresh file DB, `PRAGMA application_id` == `0x4C494645` and `PRAGMA user_version` == 1 |

**Verify**: each touched suite → `N/N met expectations`; then `tests/.venv/Scripts/python.exe tests/schema/mutants.py`
→ `M/M` again (row 1 fixes the Step 1 miss).

### Step 3: A mutant for each new expectation

Append to `MUTANTS` one tuple per row of Step 2, owned by that row's suite. Anchors (copy from the live `schema.sql`,
they must match exactly):

| row | old → new |
|---|---|
| 1 | `'CREATE UNIQUE INDEX entities_import'` → already exists as a mutant; it must now say `caught` (no new tuple) |
| 2 | `CONSTRAINT entities_updated_at CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', updated_at) IS updated_at)` → `CONSTRAINT entities_updated_at CHECK (1)` |
| 3 | `CONSTRAINT entities_deleted_at CHECK (deleted_at IS NULL OR strftime('%Y-%m-%dT%H:%M:%fZ', deleted_at) IS deleted_at)` → `CONSTRAINT entities_deleted_at CHECK (1)` |
| 4 | `CONSTRAINT links_created_at CHECK (strftime('%Y-%m-%dT%H:%M:%fZ', created_at) IS created_at)` → `CONSTRAINT links_created_at CHECK (1)` |
| 5 | `CONSTRAINT people_death_day CHECK (death_day IS NULL OR date(death_day) IS death_day)` → `CONSTRAINT people_death_day CHECK (1)` |
| 6 | `CONSTRAINT habit_periods_end_day CHECK (end_day IS NULL OR date(end_day) IS end_day)` → `CONSTRAINT habit_periods_end_day CHECK (1)` |
| 7 | `CONSTRAINT people_death_day_order CHECK (death_day IS NULL OR birth_day IS NULL OR death_day >= birth_day)` → `CONSTRAINT people_death_day_order CHECK (1)` |
| 8 | `CONSTRAINT links_source CHECK (length(source) BETWEEN 1 AND 64 AND source NOT GLOB '*[^a-z0-9_:.-]*')` → `CONSTRAINT links_source CHECK (1)` |
| 9 | `CONSTRAINT link_kinds_symmetric CHECK (symmetric IN (0,1))` → `CONSTRAINT link_kinds_symmetric CHECK (1)` |
| 10 | `name  TEXT NOT NULL UNIQUE,` → `name  TEXT NOT NULL,` |
| 11 | `CONSTRAINT pages_key_folded CHECK (length(title_key) >= 1 AND title_key = trim(title_key)\n                               AND title_key NOT GLOB '*[A-Z]*')` → `CONSTRAINT pages_key_folded CHECK (1)` |
| 12 | `CHECK (title = trim(title) AND length(title) >= 1` → `CHECK (length(title) >= 1` |
| 13 | `         AND instr(title, char(0)) = 0\n` → `` (empty) |
| 14 | `   WHERE (SELECT metric_id FROM measurements WHERE id = NEW.supersedes_id) IS NOT NEW.metric_id;` → `   WHERE (SELECT metric_id FROM measurements WHERE id = NEW.supersedes_id) <> NEW.metric_id;` |
| 15 | `coalesce((SELECT entity_type FROM entities WHERE id = NEW.to_id), '?')` → `coalesce((SELECT entity_type FROM entities WHERE id = NEW.to_id), 'place')` |
| 16 | `     AND e.created_at < strftime('%Y-%m-%dT%H:%M:%fZ','now','-30 day')\n` → `` |
| 17 | `   WHERE p.entity_type = 'page' AND p.body = '' AND e.deleted_at IS NULL` → `   WHERE p.entity_type = 'page' AND p.body = ''` |
| 18 | in `docs/cookbook/mood-over-time.md`: `  FROM measurement_values me\n  JOIN metrics m ON m.id = me.metric_id AND m.name = 'mood'` → `  FROM measurements me\n  JOIN metrics m ON m.id = me.metric_id AND m.name = 'mood'` |
| 19 | `PRAGMA application_id = 0x4C494645;` → `PRAGMA application_id = 0;` |

Add a separate mutant for `measurements_created_at` (owner `dates`, row 4's expectation) and `habit_periods_source`
(owner `identity`, row 8's) with the same `CHECK (1)` idiom.

**Verify**: `mutants.py` → every new line says `caught`; total `mutants: M/M` where M = the count before this plan (70 + the mutants plans 015 and 017 added) + the tuples you added.

### Step 4: Mutants for rules that are checked but unguarded

Append one tuple each (owner suite in brackets — the audit confirmed each suite catches it today):
[pages] `pages_title_fixed` (trigger `WHEN 0`); [identity] `entities_no_delete`, `pages_no_delete`, `links_fixed`,
`people_touch`, `measurements_source` (`CHECK (1)`), `entities_entity_type` (`CHECK (1)`); [facts] `measurements_no_delete`,
`metrics_unit_fixed`, `measurements_one_correction` (drop `UNIQUE`), `measurements_import` (drop `UNIQUE`),
`measurements_not_self`, `measurements_first_has_value`, `metrics_name`, the `measurement_values` retraction filter
(`WHERE me.value IS NOT NULL\n     AND NOT EXISTS` → `WHERE NOT EXISTS`); [dates] `measurements_tz`, `measurements_day` (`IS` → `=`),
`people_birth_day` (`IS` → `=`); [imports] `measurements_taken_at`; [pages] `pages_key_ascii`, `pages_title_len` 240 → 400;
[links] `links_mirror_delete` (`WHEN 0`), `links_endpoint_types` "not registered" RAISE (`WHERE 0 AND NOT EXISTS`),
`link_kinds_kind`, `link_kinds_from_types`, `link_kinds_mirror_valid`; [habits] `UNIQUE (metric_id, start_day)` removed,
`habit_periods_check_update`'s unit RAISE (`WHERE 0;` on its second statement — use `nth=1` on the unit RAISE text,
which appears twice), the same-start exemption (`AND p.start_day IS NOT NEW.start_day` removed); [writers]
`PRAGMA journal_mode  = WAL;` → `PRAGMA journal_mode  = DELETE;`.
For a trigger, make it a no-op by inserting `WHEN 0` before its `BEGIN` (or `WHEN 0 AND (…)` when it already has a WHEN).

If a mutant in this step is **not** caught by its owner suite, do not add an expectation in this step: move it to a
list in your report (that means the audit was wrong for your tree) and continue.

**Verify**: `mutants.py` → `mutants: M/M`.

### Step 5: The reference implementation's switches must each break a probe

At the end of `mutants.py` (before the summary print), for each switch name listed in "Current state" except
`allow_unassigned`, run `[sys.executable, '-W', 'ignore', os.path.join(HERE, '..', 'wikilinks', 'probes.py'), <ddl>, <switch>]`
(with `env=dict(os.environ, PYTHONUTF8='1')`, `timeout=300`) and require that its last line `X/Y probes passed` has
X < Y (and the process did not crash with a traceback). Count these in the same `caught`/`len` totals so the final line
stays `mutants: M/M met expectations`. For `allow_unassigned`, add a comment: the rule is owned by
`tests/schema/pages.py` (Cn titles), not by the probes.
For `<ddl>`, use `docsql.ddl()` written to a temp file, as `run()` does.

**Verify**: `mutants.py` → each switch line `caught`. If a switch is not caught, STOP and report it (it means a
reference-implementation rule has no probe — a finding, not something to paper over).

### Step 6: Counts and full run

Update `tests/README.md`: the `mutants.py` row's count ("70 broken copies…") to the new total, and add "and each
switch of `wikisave.py`". Run everything.

**Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`.

## Test plan

This plan *is* tests. New expectations: Step 2 rows 1–19 (plus the two extra CHECKs in Step 3). New mutants: ~21
(Step 3) + ~30 (Step 4) + 13 switch runs (Step 5). Pattern: existing `facts.py` / `identity.py` expectations and the
`MUTANTS` list.

## Done criteria

- [ ] `tests/.venv/Scripts/python.exe tests/schema/mutants.py` → `mutants: M/M met expectations`, M ≥ 115, no `stopped only`
- [ ] `grep -c "^ ('" tests/schema/mutants.py` ≥ 115 (one tuple per line, as now)
- [ ] `tests/README.md` states the same M
- [ ] `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`
- [ ] `git diff --stat -- docs/` shows only `docs/plans/README.md`
- [ ] `docs/plans/README.md` row 016 updated

## STOP conditions

- A Step 2 expectation fails on the **unmutated** DDL (the DDL does not do what the audit said — report the row).
- More than three mutants come out `stopped only` in Step 1.
- A `wikisave.py` switch is not caught by any probe (Step 5).
- The full run exceeds ~120 s (report; plan 015's note suggests running mutants in threads — do not do it here).

## Maintenance notes

- Every future rule lands with an expectation **and** a mutant in the same commit; reviewers should check
  `mutants.py` grew.
- Mutant anchors are literal DDL text: an edit of the DDL text near a rule will fail `mutate()` at import with
  `mutation target found 0x` — update the anchor in the same commit.
- Deferred: `pages_fts_delete` is unreachable while `pages_no_delete` exists; decide before the freeze whether to keep it
  as a guard for an owner who drops that trigger.
