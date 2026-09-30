# AGENTS.md — rules for anyone (human or agent) working in this repo

## What this repo is

The design document for **Lifelog**, a lifetime-scale single-user SQLite database
(`life.db`). `SCHEMA.md` is the product: goals, storage contract, canonical DDL,
decision log (D1–D19), query cookbook, non-goals, validation records, references.
There is no application code yet; `tests/` holds the validation suites (see below).

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
- After changing DDL or the cookbook, run `python3 tests/run_all.py` (about 15 s): it extracts §3
  from this document, applies it to throwaway databases and runs the adversarial probes, the
  oracles, the cookbook blocks and the document-text checks. Append a new validation record; never edit old records.
  If a suite must change because the document legitimately changed, change it in the same
  edit and say so in the record — `tests/` is validation, not a migration runner (the hard rule
  above still holds). A new contract rule needs a row in the §2.11 table and a key in `lifelog_meta`.
- DDL conventions that must be preserved: UTC ISO-8601 instants and local-day TEXT
  columns with round-trip CHECKs (`date(x) IS x`, `strftime(...) IS x` — the `IS`
  matters, see §8), tombstones instead of deletes (enforced by BEFORE DELETE triggers),
  composite FK `(id, entity_type) → entities(id, type)` in every *entity* domain table
  (`pages`, `events`, `tasks`, `people`, `places`, `accounts` — not `measurements`/`metrics`/
  `balances`/`currencies`/`fx_rates`, which are facts and registries), append-only measurements
  and balances enforced by triggers (measurement corrections use `supersedes_id`; a balance is
  corrected by a newer row for the same account+day; both are retracted with a NULL value/amount),
  importers use `ON CONFLICT … DO NOTHING`, never `OR IGNORE`/`OR REPLACE`, money as
  INTEGER minor units of the account's currency — never REAL (D18), enumerated CHECKs that are
  NAMED so they can be widened later (`CONSTRAINT entities_type CHECK …`), a closed, endpoint-typed `link_kinds` registry (`links.kind` is an FK; a trigger
  checks kind and endpoint types), filename-safe, immutable page titles (`pages.kind` is `memo` or `page`) with a unique app-computed
  `title_key` (NFC + casefold; SQLite cannot fold Unicode — test vectors in §2.5), `pages.kind`
  fixed after insert,
  single writing application, `PRAGMA foreign_keys=ON` and `PRAGMA recursive_triggers=ON` per
  connection (with the latter OFF, `REPLACE` bypasses the append-only DELETE triggers — §8 #6),
  `PRAGMA synchronous=FULL`, and every write transaction starts with `BEGIN IMMEDIATE` (§2.9),
  the wikilink save contract (§2.5, §6.14, D19): saving a body keeps the page's `links(kind='wikilink')`
  equal to what its CommonMark text names (rows added **and deleted**), each auto-created target in its own
  `SAVEPOINT`, an invalid target makes no link and never blocks a save, `#tag` is read and never expanded,
  a `#REDIRECT [[` stub is not scanned (the vectors in §2.5 must keep passing),
  the three integrity checks of §2.8 (`integrity_check`, `foreign_key_check`, the orphan-`entities` query —
  each catches what the others cannot; executed on the live file), and `life.db` with its `-wal`/`-shm`
  never in git (finance data cannot be scrubbed from history).
  Exploration tools (Datasette) open the file read-only; nothing that edits rows is pointed at it.
- Decisions are append-style: amend an existing D-number only by adding an addendum
  that says so; new decisions get new numbers.
- **Out of scope for now (SCHEMA.md §7, round 12):** the markdown export, backups / snapshots /
  restore, CSV dumps and off-box copies. The document is about the schema and its reliability;
  do not reintroduce any of them into `SCHEMA.md` or `tests/` unless the owner reopens it
  (`tests/schema/r12probes.py` fails if their text comes back).

## Empiricism over intuition

SQLite has sharp edges that only running the SQL reveals (CHECK NULL semantics,
missing `strftime` formats, `'weekday N'` modifier direction). Any claim in this
document about what SQLite *does* must have been executed, not assumed. If you add
such a claim, test it first and say so in a §8 validation record.
