# AGENTS.md — rules for anyone (human or agent) working in this repo

## What this repo is

**The product is `lifelog`, the Go application** ([README](README.md)): one binary at the repo root that writes
`life.db`, a lifetime-scale, single-user SQLite database, and serves it as a hypermedia API, a CLI and an MCP server.
**The database it writes is specified on its own** in [`docs/`](docs/README.md) — goals, the canonical DDL
([`docs/schema/schema.sql`](docs/schema/schema.sql)), the storage contract, the decision log (D1–D27), the query
cookbook, non-goals, research — so that `life.db` can be read, or written by another application, in any language.
The docs state the current truth only.

**What `life.db` is for: a life log and its backup — not a project-management database.** It keeps
what happened and what was measured: a journal of day pages, notes, the people and places in them,
where the owner was, health readings, and the files the owner keeps — as their text and a small picture, the
original left outside ([D9](docs/decisions/D09-binary-files.md)). To-dos, reminders, projects and plans belong to the tools
made for them; a plan written in a note stays that note's text ([D23](docs/decisions/D23-no-tasks.md)). Events, money and a location
track are deferred ([D22](docs/decisions/D22-events.md), [D18](docs/decisions/D18-money.md), [D21](docs/decisions/D21-location-history.md)). A proposal that turns `life.db` into a
planner, a tracker of open work or a finance ledger needs a real incident and the owner's word first. Any developer, in any language, may
build an application around it; the contract they implement is `docs/` and nothing else.

A writer is built against the docs ([building a writer](docs/guides/building-a-writer.md)): it implements the schema and
never defines it. One writer per `life.db` file, many applications around the schema. `lifelog` is that writer for the
owner's file; it answers to `docs/` like any other writer would, and `docs/` never names it — the contract stays
language-neutral, so a rule lives in the docs and the Go code cites it.

| path | what it is | it answers to |
|---|---|---|
| `docs/schema/schema.sql` | the one canonical DDL | real use (below) |
| `docs/architecture/`, `docs/contract/`, `docs/decisions/`, `docs/cookbook/`, `docs/research/` | the contract around it: why, what the DDL cannot hold, the SQL in use, the evidence | real use |
| `docs/guides/` | for people building on the schema: [building a writer](docs/guides/building-a-writer.md), [importing with a model](docs/guides/importing.md) | the contract |
| `docs/issues/`, `docs/rfcs/`, `docs/plans/` | the process records: incidents, proposals, execution plans ([how a change happens](docs/process.md)) | — |
| `tests/` | the validation suites (Go tests): every *executed* claim of the docs, run against the DDL in `docs/` and through the writer's own extraction and save | the docs |
| `cmd/`, `internal/`, `tools/`, `go.mod` | the product: `lifelog` (Go) — hypermedia API, CLI, MCP server ([README](README.md)) | the docs |

## Setup and checks

| what | needs | run |
|---|---|---|
| everything: the application and the suites | Go ≥ 1.27 and nothing else (no cgo, no Python, no `sqlite3` CLI: SQLite is the pure-Go `modernc.org/sqlite`, 3.53.4 with FTS5 — the suites run migrations, which need 3.53; a writer needs only 3.51.3); network once, for the modules | `go generate ./... && go vet ./... && go test ./...` (about 25 s; `-short` skips the mutants) |
| the suites alone | the same | `go test ./tests` ([tests/README.md](tests/README.md)) |
| the diagrams | node, `npm i -g @mermaid-js/mermaid-cli`, a Chromium | `LIFELOG_MERMAID=1 go test ./tests -run TestMermaidRender` |

- What to run after a change: anything — `go generate ./... && go test ./...` (`go generate` copies `schema.sql` into
  `internal/db`, and a test fails while the copy is stale); a diagram → also the mermaid render. Every test must pass.
  Say in the commit message what you ran.

## The one hard rule: no migrations until the schema freeze

**Do not create `db/migrations/`, `0001_init.sql`, or any migration runner** — not in `docs/`, `tests/` or the Go code. Until the
schema is frozen ([D13](docs/decisions/D13-migrations-and-freeze.md)):

- **`docs/schema/schema.sql` is the single canonical init DDL and is edited in place.** Schema changes = edit it
  (and the decisions, cookbook recipes and contract pages it touches) directly.
- Test databases are throwaway: apply `schema.sql` to a fresh file (e.g. `/tmp/…/life.db`), test, discard.
  Never migrate a test DB — recreate it.
- Numbered forward-only migrations (`0002_*.sql`, …) begin **only after** the freeze ([D13](docs/decisions/D13-migrations-and-freeze.md) says what that is),
  and from then on changes are additive-only ([D13](docs/decisions/D13-migrations-and-freeze.md) defines it; principle 4 of the
  [goals](docs/architecture/goals-and-principles.md)). Each migration also edits `schema.sql`, which stays the full
  current DDL; there is never a `0001_init.sql` ([D13](docs/decisions/D13-migrations-and-freeze.md) says how the two are kept equal).
  Anything still in the schema at the freeze stays for good, so cutting happens before it.

## What earns a change (principles 1, 6 and 7 of the [goals](docs/architecture/goals-and-principles.md))

- **Real use drives change.** A new table, column, constraint, trigger or convention needs a real
  incident behind it — a failed import, a bug in the writing application, a question the data could
  not answer — or it must replace something it makes redundant. A hypothetical writer is not an
  incident, and neither is a wish of an application. The next step is the capture path and one real import
  ([imports](docs/contract/imports.md)), not another review.
- **The path of a change** is [docs/process.md](docs/process.md): an [issue](docs/issues/README.md) records the incident,
  a [proposal](docs/rfcs/README.md) argues the change, a [decision](docs/decisions/README.md) records it, then `schema.sql`,
  the pages and the suites change together.
- **One home per concept.** A fact that can be derived from another column is not stored beside it
  (a day page's day is its title; a place's name is its page title).

## Editing the docs

- **One home per rule.** A table's rule is its constraint/trigger and a comment inside its `CREATE`
  statement in `schema.sql` (comments outside are not stored in the file); a rule that spans tables is a
  `lifelog_meta` row (keep them few); `docs/contract/` holds only what the DDL cannot (the wikilink grammar and
  vectors, connection settings, integrity checks, imports); `docs/decisions/` says *why* and cites constraint names
  instead of restating the rule; `docs/cookbook/` shows it in use. Do not restate a rule in a second place — that
  includes this file, an application's docs and code comments: link the page instead.
- **One page per concept, linked.** Pages link each other with relative markdown links
  (`[imports](../contract/imports.md)`); there are no section numbers. Every link must resolve and every page must be
  reachable from `docs/README.md` (the `document` suite checks both). A new decision is a new file in
  `docs/decisions/` from its template, listed in the decision index.
- **Language-neutral.** The contract must be implementable without reading any application's code. Anything a writer
  must compute identically in every language (the title predicate, `title_key`, wikilink and `#tag`
  extraction) is specified in [titles and wikilinks](docs/contract/titles-and-wikilinks.md) with its vectors
  (the suites read them from that page: it is their only copy); an implementation may be an example, never the only statement of a rule.
- **Self-consistency.** Every change to `schema.sql` keeps the contract pages, the entity model, the decisions,
  the cookbook and the totals line in `docs/schema/README.md` in step. The mermaid diagrams are checked by
  the `diagrams` suite: the ER diagrams draw tables, key columns and foreign keys only, and the link map must
  equal `link_kinds`. Each diagram starts with a `%% diagram: <id>` line; keep to `erDiagram`, `flowchart`
  and `stateDiagram-v2` with quoted labels.
- **Suites change with the docs, never to make them pass.** The suites are grouped by subject
  (`tests/README.md`). If a suite must change because the docs legitimately changed, change it in
  the same edit and say so in the commit message — a suite loosened to pass proves nothing, and
  `tests/` is validation, not a migration runner. A new cross-table rule needs a `lifelog_meta` key and
  a row in the 2075 table of the [threat model](docs/contract/threat-model.md); a new rule of any kind gets a mutant in
  `tests/mutants_test.go`. Keep the counts in `tests/README.md` (diagrams, mutants) true.
- **Current truth and nothing else** in every folder of `docs/` except `issues/`, `rfcs/` and `plans/`, which are
  dated records: no review rounds, validation records, addenda, "superseded" notes, finding ids, version narrative
  or changelog. When a decision changes, rewrite it in place —
  git is the log. Keep D-numbers stable (they are cited across the docs and inside `schema.sql`); new decisions get
  new numbers. Tests are named by subject, never by review round.
- **Out of scope for now ([non-goals](docs/architecture/non-goals.md)):** the markdown export, CSV dumps, off-box
  copies and continuous replication. The docs are about the schema and its reliability; do not reintroduce
  any of them into `docs/` or `tests/` unless the owner reopens it. Snapshots are in: a dated `VACUUM INTO` copy
  and its restore check ([D25](docs/decisions/D25-snapshots.md)).
- **Plans** live in `docs/plans/` (the `improve` skill's default `plans/` at the repo root is not used: point it
  there).

## Conventions every writer and every DDL change preserves

Their homes are in `docs/`; this list is the checklist, not the rule.

- **Time** (`lifelog_meta.instants` and `.days`, [D10](docs/decisions/D10-time-model.md)): UTC ISO-8601 instants and local-day TEXT columns with round-trip CHECKs
  (`date(x) IS x`, `strftime(...) IS x` — the `IS` matters).
- **Identity** (the `entities` comment in [schema.sql](docs/schema/schema.sql), D20, D27, D9): every *entity* domain row is keyed by its
  `entities` id through a composite FK `(id, entity_type)` — `pages` to `entities(id, entity_type)`, `people`, `metrics` and `files`
  to `pages(id, entity_type)`, because a person, a metric or a file **is** a page; a place is its page, and a `places` row when it has a point (D16, D21). One id, whose page title
  is its handle and its name (`pages.entity_type`, `ON UPDATE CASCADE` for promotion;
  [a person or a place](docs/cookbook/person-or-place.md)). Ids are carried with `INSERT … RETURNING id`, never
  `last_insert_rowid()` across statements.
- **Provenance**: `source` (the writer: `ui`, `cli`, `api`, `agent:<name>`, `import:<name>`; `schema` for the rows the DDL seeds) is
  required on `entities`, `links`, `measurements` and `habit_periods`, written at insert and never changed; `import_key`
  is unique per `source`.
- **No deletes** (`lifelog_meta.deletes`, [D11](docs/decisions/D11-tombstones.md)): tombstones (BEFORE DELETE triggers);
  only `links` rows are deleted.
- **Append-only facts**: measurements. A reading is corrected with `supersedes_id` and retracted with a
  NULL value (D7).
- **Imports**: `ON CONFLICT … DO NOTHING`, never `OR IGNORE` / `OR REPLACE` ([imports](docs/contract/imports.md)).
- **CHECKs**: every one NAMED (`CONSTRAINT <table>_<rule> CHECK …`), using only functions the minimum
  SQLite has (`lifelog_meta.sqlite`) — no math functions, even where a build has them.
- **Links**: a closed, endpoint-typed `link_kinds` registry (D8).
- **Pages** ([titles and wikilinks](docs/contract/titles-and-wikilinks.md), D5): every page titled, with filename-safe,
  immutable titles and a unique app-computed `title_key` (NFC + casefold, with vectors). The journal is one day page
  per local day, titled `YYYY-MM-DD` (`pages_day_page`) and never promoted (`pages_day_page_plain`); where the owner
  was that day is `at` links to places (D16). There are no events or tasks (D22, D23); nothing repeats (D15). A habit
  is a 0/1 metric with active periods (D24).
- **Connections** ([connection setup](docs/contract/connections.md)): one writing application per file; per connection
  `PRAGMA foreign_keys=ON`, `recursive_triggers=ON`, `synchronous=FULL`, `trusted_schema=OFF` (the first three read back and refused
  if wrong); SQLite ≥ 3.51.3 for writers; every write transaction starts with `BEGIN IMMEDIATE`; the driver opens
  no transactions of its own. Readers open the file read-only (`mode=ro`, never `immutable=1`) and set
  `trusted_schema=OFF` too;
  exploration tools (Datasette) likewise, and nothing that edits rows is pointed at it.
- **The wikilink save contract** ([titles and wikilinks](docs/contract/titles-and-wikilinks.md),
  [save a body](docs/cookbook/save-a-body.md), D19): saving a body keeps the page's
  `links(kind='wikilink')` equal to what its CommonMark text names (rows added **and deleted**), each
  auto-created target in its own `SAVEPOINT`; an invalid target makes no link and never blocks a save;
  `#tag` is read and never expanded; a `#REDIRECT [[` stub is not scanned.
- **Integrity**: the four [integrity checks](docs/contract/integrity-checks.md).
- **Privacy**: `life.db` with its `-wal`/`-shm`, and every snapshot of it, never in git (health data, private notes and pictures cannot be scrubbed from history; `.gitignore` covers `*.db`, `*.db-journal`, `/import/`
  and an import workspace `*.lifelog/`). Never commit a real vault, real notes or real data as a fixture:
  tests use synthetic data only.

## Empiricism over intuition

SQLite has sharp edges that only running the SQL reveals (CHECK NULL semantics, missing `strftime`
formats, `'weekday N'` modifier direction). Any claim in the docs about what SQLite *does* must be
executed by a suite in `tests/`, not assumed. If you add such a claim, write the probe first, then state
the claim and mark it *executed* (the word means that a suite runs it). A suite must not depend on
accidents of one SQLite build (page layout, compile options): it finds what it needs, or says plainly
what it requires.
