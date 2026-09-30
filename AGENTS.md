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
  (`ADD COLUMN` / `CREATE TABLE` / new indexes — see principle 4 in SCHEMA.md §1). Anything still
  in the schema at the freeze stays for good, so cutting happens before it.

## What earns a change (SCHEMA.md §1, principles 1, 6 and 7)

- **Real use drives change.** A new table, column, constraint, trigger or convention needs a real
  incident behind it — a failed import, a bug in the writing application, a question the data could
  not answer — or it must replace something it makes redundant. A hypothetical writer is not an
  incident. The next step is the capture path and one real import (§2.8), not another review.
- **One home per concept.** A fact that can be derived from another column is not stored beside it
  (open task = `completed_at IS NULL`; a place's name is its page title).

## Editing SCHEMA.md

- **One home per rule.** A table's rule is its constraint/trigger and a comment inside its `CREATE`
  statement (comments outside are not stored in the file); a rule that spans tables is a
  `lifelog_meta` row (keep them few); §2 holds only what the DDL cannot (time, the wikilink grammar
  and vectors, connection settings, integrity checks, imports); §5 says *why* and cites constraint
  names instead of restating the rule; §6 shows it in use. Do not restate a rule in a second place.
- Every change to §3's DDL must keep the document self-consistent: §2, §4, §5, §6 and the totals
  line under §3. The mermaid diagrams (§2.3, §2.6, §2.7, §4, §6.13) are checked by
  `tests/schema/diagrams.py`: the ER diagrams draw tables, key columns and foreign keys only, and the
  link map must equal `link_kinds`. Each diagram starts with a `%% diagram: <id>` line; keep to
  `erDiagram`, `flowchart` and `stateDiagram-v2` with quoted labels, and run
  `python3 tests/run_all.py --mermaid` after editing one (it renders them).
- After changing DDL or the cookbook, run `python3 tests/run_all.py` (about 15 s). The suites are
  grouped by subject (`tests/README.md`). If a suite must change because the document legitimately
  changed, change it in the same edit and say so in the commit message — a suite loosened to pass
  proves nothing, and `tests/` is validation, not a migration runner (the hard rule above still
  holds). A new cross-table rule needs a `lifelog_meta` key and a row in the 2075 table of §2.8; a
  new rule of any kind gets a mutant in `tests/schema/mutants.py`.
- DDL conventions that must be preserved: UTC ISO-8601 instants and local-day TEXT columns with
  round-trip CHECKs (`date(x) IS x`, `strftime(...) IS x` — the `IS` matters, §2.1); tombstones
  instead of deletes (BEFORE DELETE triggers; only `links` rows are deleted); every *entity* domain row
  keyed by its `entities` id through a composite FK `(id, entity_type)` — `pages`, `events` and `tasks`
  to `entities(id, type)`, and `people`, `places` and `holdings` to `pages(id, entity_type)`, because a
  person, place or holding **is** a page: one id, whose page title is its handle and its name
  (`pages.entity_type`, `ON UPDATE CASCADE` for promotion; §2.2, §6.19, D20); append-only
  measurements and balances (measurement corrections use `supersedes_id`; a balance is corrected by a
  newer row for the same holding+day; both are retracted with a NULL value/amount); importers use
  `ON CONFLICT … DO NOTHING`, never `OR IGNORE`/`OR REPLACE`; money as INTEGER minor units of the
  holding's currency — never REAL (D18); every CHECK NAMED (`CONSTRAINT <table>_<rule> CHECK …`),
  using only functions the minimum SQLite has (`lifelog_meta.sqlite`); ids carried with
  `INSERT … RETURNING id`, never `last_insert_rowid()` across statements; `source` (the writer:
  `ui`, `cli`, `api`, `agent:<name>`, `import:<name>`) required on `entities`, `links`,
  `measurements` and `balances`, written at insert and never changed; a closed, endpoint-typed
  `link_kinds` registry; filename-safe, immutable page titles (`pages.kind` is `memo` or `page`, fixed)
  with a unique app-computed `title_key` (NFC + casefold; vectors in §2.4); nothing repeats (D15);
  single writing application, `PRAGMA foreign_keys=ON`, `recursive_triggers=ON`, `synchronous=FULL`,
  `trusted_schema=OFF` per connection, SQLite ≥ 3.51.3 for writers, every write transaction starts
  with `BEGIN IMMEDIATE` (§2.6); the wikilink save contract (§2.4, §6.13, D19): saving a body keeps
  the page's `links(kind='wikilink')` equal to what its CommonMark text names (rows added **and
  deleted**), each auto-created target in its own `SAVEPOINT`, an invalid target makes no link and
  never blocks a save, `#tag` is read and never expanded, a `#REDIRECT [[` stub is not scanned; the
  four integrity checks of §2.5; and `life.db` with its `-wal`/`-shm` never in git (finance data
  cannot be scrubbed from history). Exploration tools (Datasette) open the file read-only; nothing
  that edits rows is pointed at it.
- **`SCHEMA.md` states the current truth and nothing else:** no review rounds, validation records,
  addenda, "superseded" notes, finding ids, version narrative or changelog
  (`tests/schema/document.py` fails if any comes back). When a decision changes, rewrite it in place —
  git is the log. Keep D-numbers stable (they are cited across the document); new decisions get new
  numbers. Tests are named by subject, never by review round.
- **Out of scope for now (SCHEMA.md §7):** the markdown export, backups / snapshots /
  restore, CSV dumps and off-box copies. The document is about the schema and its reliability;
  do not reintroduce any of them into `SCHEMA.md` or `tests/` unless the owner reopens it
  (`tests/schema/document.py` fails if their text comes back).

## Empiricism over intuition

SQLite has sharp edges that only running the SQL reveals (CHECK NULL semantics,
missing `strftime` formats, `'weekday N'` modifier direction). Any claim in this
document about what SQLite *does* must be executed by a suite in `tests/`, not assumed. If you
add such a claim, write the probe first, then state the claim and mark it *executed* (the word means
that a suite runs it).
