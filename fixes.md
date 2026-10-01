# fixes.md — review findings for SCHEMA.md, 2026-10-02

A thorough review of `SCHEMA.md` (2308 lines). Everything the suites can check was green at
review time: `python tests/run_all.py` 20/20 suites (dates, identity, named, pages, links,
facts, habits, journal, writers, integrity, imports, evolution, cookbook, document, diagrams,
mutants, vectors, title fuzz, save contract, doc save contract) on SQLite 3.53.4; in `app/`,
`gofmt -l` clean, `go vet ./...` clean, `go test ./...` all pass. Each finding below was
verified by executing it against a fresh database built from §3.

Not one of these is applied yet; each is a proposed edit with its evidence. Findings 1, 3–7 are
text fixes; 2 and 8 need the owner's decision.

## 1. The document contradicts itself about whether the first real import happened (stale current truth)

- Line 3 (Status): "The next step is the capture path and one real import (§2.7), not another review."
- §2.7 step 6 (line 482): "a real import is the one test this schema has never had."
- But D5 (line 1137), D22 (line 1503), D23 (line 1530) and D24 (line 1553) all cite "the first
  real import, 2026-10, an Obsidian vault" as an accomplished fact; D18 and `app/README.md` A6
  agree.

The intended distinction is surely *trial on a copy* (done, 2026-10) vs *import into the
canonical `life.db`* (not done — no canonical file exists). As written, §2.7 step 6 denies a
test that four decisions say already ran. Fix: rewrite step 6 to the current truth (run steps
1–5 into the canonical file; note the 2026-10 trial already taught D5/D22/D23/D24) and reword
the Status line to match.

## 2. "Nothing is deleted except `links` rows" is broader than the DDL enforces — a wiped `lifelog_meta` is undetectable

§2.3 (line 126) and the `lifelog_meta` `deletes` row (line 515) state the rule absolutely;
§2.3's second sentence correctly scopes the triggers to "an `entities` row or any domain row".
Executed gap:

- `DELETE FROM lifelog_meta` succeeds, all 8 rows gone; `integrity_check` = ok,
  `foreign_key_check` = no rows, the §2.5 orphan query is blind to it. Only the 2075 test
  fails — the in-file documentation, the centerpiece of D17, is gone.
- `DELETE FROM metrics` / `DELETE FROM link_kinds` succeed for any row not currently
  referenced (the seeded `mood` before its first reading; a kind with no links yet), and
  unconditionally on a connection with `foreign_keys=OFF`, a failure mode §2.6 itself lists.

The §2.7 threat model names "a buggy writer, importer or agent" as controlled by "no hard
deletes"; that control does not reach the registries. **Owner's decision:** either add
`no_delete` triggers for the three registries (and mutants in `tests/schema/mutants.py`), or
scope the §2.3/meta sentence to entity and fact tables. Today the stated rule and the enforced
rule differ on exactly the table the 2075 test depends on.

## 3. §2.2 Provenance omits `habit_periods`

Line 117: "`source` on `entities`, `links` and `measurements` names the writer". But
`habit_periods` has a checked `source` column, the `lifelog_meta` `source` row says "entities,
links, measurements **and habit_periods**", and D24 depends on it. Fix §2.2, and the same
omission in `AGENTS.md`'s conventions list, in the same commit. Related nit: the meta `source`
row's clause "the import_key a writer gives a row is unique per source" sits in the same
sentence as four table names, only two of which have `import_key`.

## 4. D12 overstates: `created_at` is not "on every table"

Line 1290: "`created_at` (on every table)". `metrics`, `habit_periods`, `link_kinds` and
`lifelog_meta` have none; `lifelog_meta.instants` already has the correct phrasing ("on every
table **that has it**"). Fix D12 to match. Note while there: a `habit_periods` row has no
write-time audit at all (no `created_at`, no `import_key`).

## 5. Two dead references: R63 and R73

Both defined in §8, cited nowhere in §1–§7 (checked mechanically; `document.py` does not check
citations). §8 claims "What each source contributed to the decisions above". R63 (UTS #39)
belongs in D5's Sources beside the `pages_title_safe` invisible-character rule; R73 (R\*Tree)
points at "§7" where no row cites it — a leftover from a cut §7 row. Cite them or drop them.

## 6. §2.6's WAL-version sentence is false for the two backports it lists

Line 334: "every version from 3.7.0 to 3.51.2 has a WAL race" — but R65 records backports
3.44.6 and 3.50.7, both inside that range, with the fix. The 3.51.3 floor is still the right
operational rule (conservative, simple). Fix the sentence ("…except the backports 3.44.6 and
3.50.7") or drop the universal claim and keep the floor.

## 7. Two convention nits

- R71 (line 2287): "cut in two (§3.1.9)" — `§` everywhere else means this document, and §3.1
  does not exist; it is RFC 7946's section. Write "RFC 7946 §3.1.9".
- D13: "`PRAGMA application_id = 'LIFE'`" — it is the integer `0x4C494645` (§3 has it right);
  the quotes could make a reader try the string form.

## 8. Observation: `habit_periods` re-run semantics are only half in the document

The DDL comment documents the no-op re-run ("a period with the same start is left to `UNIQUE`,
so `ON CONFLICT DO NOTHING` re-runs it"), and it was executed: a re-insert of the same
`(metric_id, start_day)` with a changed `end_day` is a silent no-op. The official writer
handles it (`store.HabitPeriod` = `ON CONFLICT DO NOTHING` plus an explicit `UPDATE end_day`),
but that correction path appears nowhere in SCHEMA.md, so a third-party writer implementing
"§2 and §3 only" cannot reproduce the reference writer's idempotent period updates. Defensible
(periods are owner-approved data, `metrics.md`'s `since`/`until`), but the document specifies
`title_key` to the vector precisely so any language can reproduce it. **Owner's decision:**
either one sentence in §6.16 ("a re-run that changed `until` follows the no-op insert with the
UPDATE") or an explicit note that periods are owner-keyed, not sender-keyed.

## Verified correct beyond the suites

Executed against a fresh §3 database: a page titled like a day with `day IS NULL` is refused by
`pages_day_page` (the `IS` null-safe-equality reasoning holds); promotion bumps
`entities.updated_at` via the `ON UPDATE CASCADE` + `pages_touch` (undocumented but benign);
correction chains work and `measurement_values` shows only the final value; revive fires
`entities_touch`; an `at` link from a non-day page to a place is allowed by the DDL — the
day-page restriction is the app's duty, exactly as D16's "Costs accepted" states. The habit
overlap triggers (insert and update, including the `p.id <> NEW.id` and
`p.start_day IS NOT NEW.start_day` exclusions) are correct; mirror triggers terminate; the
`habit_periods_check_insert` fires before the unique index as its comment claims. The cookbook
queries traced (§6.2 ordering, §6.16 completion counting retracted check-ins as not-recorded)
are semantically right. §3 totals (9 tables + 1 FTS + 2 views + 23 triggers), the 2075 table's
completeness, D1–D24 ordering, and reference completeness otherwise all check out.
