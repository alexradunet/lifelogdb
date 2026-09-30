# AGENTS.md — rules for anyone (human or agent) working in this repo

## What this repo is

The design document for **Lifelog**, a lifetime-scale single-user SQLite database
(`life.db`). `SCHEMA.md` is the product: goals, storage contract, canonical DDL,
decision log (D1–D20), query cookbook, non-goals, references. It states the current truth
only. There is no application code yet; `tests/` holds the validation suites (see below).

## The one hard rule: no migrations until the schema freeze

**Do not create `db/migrations/`, `0001_init.sql`, or any migration runner.**
Until the schema is frozen (decision D13):

- `SCHEMA.md` **§3 is the single canonical init DDL and is edited in place.**
  Schema changes = edit §3 (and the decision log / cookbook / contract sections it
  touches) directly in this document.
- Test databases are throwaway: extract §3's ```sql block, apply it to a fresh file
  (e.g. `/tmp/…/life.db`), test, discard. Never migrate a test DB — recreate it.
- Numbered forward-only migrations (`0002_*.sql`, …) begin **only after** real data
  exists in a canonical `life.db`, and from then on changes are additive-only
  (`ADD COLUMN` / `CREATE TABLE` / new indexes — see principle 4 in SCHEMA.md §1).

## Editing SCHEMA.md

- Every change to §3's DDL must keep the document self-consistent: the entity-model
  overview (§4), the decision log (§5), and the query cookbook (§6) all describe the
  same schema. If you change the DDL, change them too — including the mermaid diagrams
  (§2.4, §2.9, §2.10, §4, §6.14): `tests/schema/diagrams.py` compares the ER diagrams and the
  link map with the DDL and fails when they drift. Each diagram starts with a `%% diagram: <id>` line;
  keep to `erDiagram`, `flowchart` and `stateDiagram-v2` with quoted labels, and run
  `python3 tests/run_all.py --mermaid` after editing one (it renders them).
- After changing DDL or the cookbook, run `python3 tests/run_all.py` (about 30 s): it extracts §3
  from this document, applies it to throwaway databases and runs the adversarial probes, the
  oracles, the cookbook blocks and the document-text checks. If a suite must change because the document legitimately changed,
  change it in the same edit and say so in the commit message — a suite loosened to pass proves
  nothing, and `tests/` is validation, not a migration runner (the hard rule above still holds). A new contract rule needs a row in the §2.11 table and a key in `lifelog_meta`.
- DDL conventions that must be preserved: UTC ISO-8601 instants and local-day TEXT
  columns with round-trip CHECKs (`date(x) IS x`, `strftime(...) IS x` — the `IS`
  matters, see §2.2), tombstones instead of deletes (enforced by BEFORE DELETE triggers),
  composite FK `(id, entity_type) → entities(id, type)` in every *entity* domain table
  (`pages`, `events`, `tasks`, `people`, `places`, `holdings` — not `measurements`/`metrics`/
  `balances`/`currencies`, which are facts and registries), every *named* entity (`person`, `place`, `holding`)
  owning one page through `entities.page_id` — `CHECK`-tied, unique across types, fixed after insert, inserted **first**
  (page entity, `pages` row, named entity, domain row: §6.20, D20) — so `[[Name]]` reaches it through the title and no
  `notes` column exists on them, append-only measurements
  and balances enforced by triggers (measurement corrections use `supersedes_id`; a balance is
  corrected by a newer row for the same holding+day; both are retracted with a NULL value/amount),
  importers use `ON CONFLICT … DO NOTHING`, never `OR IGNORE`/`OR REPLACE`, money as
  INTEGER minor units of the holding's currency — never REAL (D18), every CHECK NAMED
  (`CONSTRAINT <table>_<rule> CHECK …`) so any rule can be dropped or re-added later, using only functions
  the minimum SQLite has (`lifelog_meta.sqlite`), the rules a table needs written as comments *inside* its
  `CREATE` statement (comments outside are not stored in the file), ids carried with `INSERT … RETURNING id`,
  never `last_insert_rowid()` across statements, `entities.source`/`links.source` written at insert and never
  changed, a closed, endpoint-typed `link_kinds` registry (`links.kind` is an FK; a trigger
  checks kind and endpoint types), filename-safe, immutable page titles (`pages.kind` is `memo` or `page`) with a unique app-computed
  `title_key` (NFC + casefold; SQLite cannot fold Unicode — test vectors in §2.5), `pages.kind`
  fixed after insert,
  single writing application, `PRAGMA foreign_keys=ON` and `PRAGMA recursive_triggers=ON` per
  connection (with the latter OFF, `REPLACE` bypasses the append-only DELETE triggers — §2.9),
  `PRAGMA synchronous=FULL`, `PRAGMA trusted_schema=OFF`, SQLite ≥ 3.51.3 for writers, and every write
  transaction starts with `BEGIN IMMEDIATE` (§2.9),
  the wikilink save contract (§2.5, §6.14, D19): saving a body keeps the page's `links(kind='wikilink')`
  equal to what its CommonMark text names (rows added **and deleted**), each auto-created target in its own
  `SAVEPOINT`, an invalid target makes no link and never blocks a save, `#tag` is read and never expanded,
  a `#REDIRECT [[` stub is not scanned (the vectors in §2.5 must keep passing),
  the four integrity checks of §2.8 (`integrity_check`, `foreign_key_check`, the orphan-`entities` query,
  the FTS5 `integrity-check` — each catches what the others cannot; executed on the live file), and `life.db` with its `-wal`/`-shm`
  never in git (finance data cannot be scrubbed from history).
  Exploration tools (Datasette) open the file read-only; nothing that edits rows is pointed at it.
- **`SCHEMA.md` states the current truth and nothing else:** no review rounds, validation records,
  addenda, "superseded" notes, finding ids, version narrative or changelog
  (`tests/schema/nohistory.py` fails if any comes back). When a decision changes, rewrite it in place —
  git is the log. Keep D-numbers stable (they are cited across the document); new decisions get new numbers.
- **Out of scope for now (SCHEMA.md §7):** the markdown export, backups / snapshots /
  restore, CSV dumps and off-box copies. The document is about the schema and its reliability;
  do not reintroduce any of them into `SCHEMA.md` or `tests/` unless the owner reopens it
  (`tests/schema/r12probes.py` fails if their text comes back).

## Empiricism over intuition

SQLite has sharp edges that only running the SQL reveals (CHECK NULL semantics,
missing `strftime` formats, `'weekday N'` modifier direction). Any claim in this
document about what SQLite *does* must be executed by a suite in `tests/`, not assumed. If you
add such a claim, write the probe first, then state the claim and mark it *executed* (the word means
that a suite runs it).
