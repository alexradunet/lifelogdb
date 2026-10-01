# AGENTS.md — rules for anyone (human or agent) working in this repo

## What this repo is

**Lifelog is a schema first.** The product is `SCHEMA.md`: the design of `life.db`, a lifetime-scale,
single-user SQLite database — goals, storage contract, canonical DDL, decision log (D1–D23), query
cookbook, non-goals, references. It states the current truth only.

**What `life.db` is for: a life log and its backup — not a project-management database.** It keeps
what happened and what was measured: a journal of day pages, notes, the people and places in them,
where the owner was, and health readings. To-dos, reminders, projects and plans belong to the tools
made for them; a plan written in a note stays that note's text (SCHEMA.md D23). Events, money, location
history and attachments are deferred (D22, D18, D21, D9). A proposal that turns `life.db` into a
planner, a tracker of open work or a finance ledger needs a real incident and the owner's word first. Any developer, in any language, may
build an application around it; the contract they implement is `SCHEMA.md` and nothing else.

`app/` is the **first official application**: a Go binary (a CLI now; a REST API and an MCP server
later) and the reference writer of a `life.db`. Its own decisions (A1–A9) are in `app/README.md`. It
implements the schema; it never defines it.

| path | what it is | it answers to |
|---|---|---|
| `SCHEMA.md` | the contract: §3 is the one canonical DDL | real use (below) |
| `tests/` | the validation suites: every *executed* claim of `SCHEMA.md`, and the reference implementation of the wikilink save contract (`tests/wikilinks/wikisave.py`) | `SCHEMA.md` |
| `app/` | the Go writer; embeds §3 as `app/internal/db/schema.sql` | `SCHEMA.md` and `tests/` |
| `app/skills/` | instructions a model follows to drive the CLI: `import` (any source, an Obsidian vault included) | `app/` |
| `app/IMPORT.md` | the reference for imports: the workspace, facts files, the CLI's checks, gates and replay | `app/` |

**One writer per database, many applications around the schema.** Principle 3 (single writing
application) is a rule about *one `life.db` file*: whatever application writes a given file is the only
thing that writes it, and every other tool opens it read-only. It does not say only `app/` may ever
exist. Another developer's writer for their own `life.db` is fine if it follows the whole contract.
Two writers on one file are not.

## Setup and checks

| what | needs | run |
|---|---|---|
| the suites (`tests/`) | Python **≥ 3.12** (`Connection.setconfig`); its `sqlite3` module and the `sqlite3` CLI both on SQLite **≥ 3.51.3 with FTS5** (3.53 is used); network once, for the venv | `python3 tests/run_all.py` (about 15 s) |
| the diagrams | node, `npm i -g @mermaid-js/mermaid-cli`, a Chromium | `python3 tests/run_all.py --mermaid` |
| the app (`app/`) | Go as pinned in `app/mise.toml` (`mise install`) and `go.mod` | `go vet ./... && go test ./...` in `app/` |

- Run `python3 tests/run_all.py` **before** `go test ./...`: it creates `tests/.venv`. Without the venv
  the Go parity tests (`internal/wiki`) **skip** instead of failing, so a green `go test` proves less.
- A distribution's SQLite may be too old or built without FTS5 (`no such module: fts5`). Build the
  amalgamation with `--enable-fts5` and put its `bin/` on `PATH` and `lib/` on `LD_LIBRARY_PATH`.
- What to run after a change: §3 DDL, §2 or §6 → the suites **and** `go generate ./internal/db && go test ./...`
  in `app/`; a diagram → the suites with `--mermaid`; `app/` → `gofmt -l .`, `go vet ./...`, `go test ./...`.
  Every suite must end green. Say in the commit message what you ran.

## The one hard rule: no migrations until the schema freeze

**Do not create `db/migrations/`, `0001_init.sql`, or any migration runner** — not in `SCHEMA.md`,
`tests/` or `app/`. Until the schema is frozen (decision D13):

- `SCHEMA.md` **§3 is the single canonical init DDL and is edited in place.** Schema changes = edit §3
  (and the decision log, cookbook and contract sections it touches) directly in the document.
- Test databases are throwaway: extract §3's ```sql block, apply it to a fresh file
  (e.g. `/tmp/…/life.db`), test, discard. Never migrate a test DB — recreate it.
- Numbered forward-only migrations (`0002_*.sql`, …) begin **only after** real data exists in a
  canonical `life.db`, and from then on changes are additive-only (`ADD COLUMN` / `CREATE TABLE` / new
  indexes — principle 4 in SCHEMA.md §1). Anything still in the schema at the freeze stays for good, so
  cutting happens before it.

## What earns a change (SCHEMA.md §1, principles 1, 6 and 7)

- **Real use drives change.** A new table, column, constraint, trigger or convention needs a real
  incident behind it — a failed import, a bug in the writing application, a question the data could
  not answer — or it must replace something it makes redundant. A hypothetical writer is not an
  incident, and neither is a wish of `app/`. The next step is the capture path and one real import
  (§2.7), not another review.
- **One home per concept.** A fact that can be derived from another column is not stored beside it
  (a day page's day is its title; a place's name is its page title).

## Editing SCHEMA.md

- **One home per rule.** A table's rule is its constraint/trigger and a comment inside its `CREATE`
  statement (comments outside are not stored in the file); a rule that spans tables is a
  `lifelog_meta` row (keep them few); §2 holds only what the DDL cannot (time, the wikilink grammar and
  vectors, connection settings, integrity checks, imports); §5 says *why* and cites constraint names
  instead of restating the rule; §6 shows it in use. Do not restate a rule in a second place — that
  includes this file, `app/README.md` and code comments: cite the section instead.
- **Language-neutral.** The contract must be implementable without reading `app/`. Anything a writer
  must compute identically in every language (the title predicate, `title_key`, wikilink and `#tag`
  extraction) is specified in §2.4 with vectors in `tests/wikilinks/vectors.py`; Go code may be an
  example, never the only statement of a rule.
- **Self-consistency.** Every change to §3's DDL keeps §2, §4, §5, §6 and the totals line under §3 in
  step. The mermaid diagrams (§2.3, §2.6, §4, §6.13) are checked by `tests/schema/diagrams.py`:
  the ER diagrams draw tables, key columns and foreign keys only, and the link map must equal
  `link_kinds`. Each diagram starts with a `%% diagram: <id>` line; keep to `erDiagram`, `flowchart`
  and `stateDiagram-v2` with quoted labels.
- **Suites change with the document, never to make it pass.** The suites are grouped by subject
  (`tests/README.md`). If a suite must change because the document legitimately changed, change it in
  the same edit and say so in the commit message — a suite loosened to pass proves nothing, and
  `tests/` is validation, not a migration runner. A new cross-table rule needs a `lifelog_meta` key and
  a row in the 2075 table of §2.7; a new rule of any kind gets a mutant in `tests/schema/mutants.py`.
  Keep the counts in `tests/README.md` (diagrams, mutants) true.
- **Current truth and nothing else:** no review rounds, validation records, addenda, "superseded"
  notes, finding ids, version narrative or changelog (`tests/schema/document.py` fails if any comes
  back). When a decision changes, rewrite it in place — git is the log. Keep D-numbers stable (they are
  cited across the document); new decisions get new numbers. Tests are named by subject, never by review
  round.
- **Out of scope for now (SCHEMA.md §7):** the markdown export, backups / snapshots / restore, CSV
  dumps and off-box copies. The document is about the schema and its reliability; do not reintroduce
  any of them into `SCHEMA.md`, `tests/` or `app/` unless the owner reopens it
  (`tests/schema/document.py` fails if their text comes back).

## Conventions every writer and every DDL change preserves

Their homes are in `SCHEMA.md`; this list is the checklist, not the rule.

- **Time** (§2.1, D10): UTC ISO-8601 instants and local-day TEXT columns with round-trip CHECKs
  (`date(x) IS x`, `strftime(...) IS x` — the `IS` matters).
- **Identity** (§2.2, D20): every *entity* domain row is keyed by its `entities` id through a composite
  FK `(id, entity_type)` — `pages` to `entities(id, entity_type)`, `people` to `pages(id, entity_type)`,
  because a person **is** a page; a place is its page alone (D16). One id, whose page title is its
  handle and its name (`pages.entity_type`, `ON UPDATE CASCADE` for promotion; §6.14). Ids are carried with `INSERT … RETURNING id`, never `last_insert_rowid()` across
  statements.
- **Provenance**: `source` (the writer: `ui`, `cli`, `api`, `agent:<name>`, `import:<name>`) is
  required on `entities`, `links` and `measurements`, written at insert and never changed; `import_key`
  is unique per `source`.
- **No deletes** (§2.3, D11): tombstones (BEFORE DELETE triggers); only `links` rows are deleted.
- **Append-only facts**: measurements. A reading is corrected with `supersedes_id` and retracted with a
  NULL value (D7).
- **Imports**: `ON CONFLICT … DO NOTHING`, never `OR IGNORE` / `OR REPLACE` (§2.7).
- **CHECKs**: every one NAMED (`CONSTRAINT <table>_<rule> CHECK …`), using only functions the minimum
  SQLite has (`lifelog_meta.sqlite`) — no math functions, even where a build has them.
- **Links**: a closed, endpoint-typed `link_kinds` registry (D8).
- **Pages** (§2.4, D5): every page titled, with filename-safe, immutable titles and a unique app-computed
  `title_key` (NFC + casefold; vectors in §2.4). The journal is one day page per local day, titled
  `YYYY-MM-DD` (`pages_day_page`); where the owner was that day is `at` links to places (D16). There
  are no events or tasks (D22, D23); nothing repeats (D15).
- **Connections** (§2.6): one writing application per file; per connection `PRAGMA foreign_keys=ON`,
  `recursive_triggers=ON`, `synchronous=FULL`, `trusted_schema=OFF`, read back and refused if wrong;
  SQLite ≥ 3.51.3 for writers; every write transaction starts with `BEGIN IMMEDIATE`; the driver opens
  no transactions of its own. Readers open the file read-only (`mode=ro`, never `immutable=1`);
  exploration tools (Datasette) likewise, and nothing that edits rows is pointed at it.
- **The wikilink save contract** (§2.4, §6.13, D19): saving a body keeps the page's
  `links(kind='wikilink')` equal to what its CommonMark text names (rows added **and deleted**), each
  auto-created target in its own `SAVEPOINT`; an invalid target makes no link and never blocks a save;
  `#tag` is read and never expanded; a `#REDIRECT [[` stub is not scanned.
- **Integrity**: the four checks of §2.5.
- **Privacy**: `life.db` with its `-wal`/`-shm` never in git (finance data cannot be scrubbed from
  history; `.gitignore` covers `*.db`). Never commit a real vault, real notes or real data as a fixture:
  tests use synthetic data only.

## Working on `app/` (Go, the first official application)

- **The schema is embedded, never written.** `app/internal/db/schema.sql` is generated from §3
  (`go generate ./internal/db`); `TestSchemaIsSection3` fails while they differ. Never edit it by hand,
  never put DDL, `ALTER` or a migration in Go code.
- **Every write goes through `internal/store`**, inside `db.Write` (one `BEGIN IMMEDIATE` transaction;
  the DSN sets `_txlock=immediate` and the connection checks its pragmas at open). The CLI, and later
  the REST API and MCP server, are thin faces over it (A2); none of them writes SQL of its own.
  Read-only work uses `db.OpenReadOnly`.
- **Parity with the reference.** `internal/wiki` must agree with `tests/wikilinks/` on the §2.4
  vectors and on generated inputs (A5). A change to either side changes both, in the same commit.
- **The CLI prints JSON on stdout, errors on stderr**, so people and models read the same output. A
  flag or command change updates `app/skills/*/SKILL.md` in the same commit.
- **Importing data is a skill, not a script.** Asked to import anything (a vault, an export, lab
  results), read `app/skills/import/SKILL.md` first and follow it; never write an importer for one
  source (A7). Code is added only for what every import needs and a model does badly (`inspect`,
  `find`, `measure`, and facts files checked and applied whole by `batch`, A9). The workspace beside a
  source (`<source>.lifelog/`) holds private data, like the source itself: neither ever goes into git
  (`/import/` is ignored).
- **Dependencies stay few** (A1, A3): the standard library first; `modernc.org/sqlite` (pure Go, no cgo,
  FTS5 built in) is the driver. A new dependency needs a reason in `app/README.md`.
- **Style**: `gofmt`, `go vet`, tests beside the code, comments that cite the `SCHEMA.md` section they
  implement rather than restating it. A new application decision gets the next A-number in
  `app/README.md`; the build order there is kept current.

## Building another application on the schema

A developer writing their own Lifelog application (any language) needs, from this repo:

1. **§3** as the init DDL, applied to a new file, verbatim — `PRAGMA application_id` marks it as a
   Lifelog database.
2. **§2** as the writer's obligations, chiefly §2.6 (connection setup, `BEGIN IMMEDIATE`, version
   floor) and §2.4 (titles, `title_key`, the save contract).
3. **§6** as the canonical reads and writes; `lifelog_meta` and the comments inside `.schema` as the
   in-file summary.
4. **The vectors** in `tests/wikilinks/vectors.py` as a conformance suite; `tests/wikilinks/wikisave.py`
   is a readable reference implementation of the save contract.

Such an application changes nothing here. If it finds a rule that is ambiguous, untestable or missing,
that is an incident under "What earns a change": fix `SCHEMA.md` (and its suite), not just the app.

## Empiricism over intuition

SQLite has sharp edges that only running the SQL reveals (CHECK NULL semantics, missing `strftime`
formats, `'weekday N'` modifier direction). Any claim in `SCHEMA.md` about what SQLite *does* must be
executed by a suite in `tests/`, not assumed. If you add such a claim, write the probe first, then state
the claim and mark it *executed* (the word means that a suite runs it). A suite must not depend on
accidents of one SQLite build (page layout, compile options): it finds what it needs, or says plainly
what it requires.
