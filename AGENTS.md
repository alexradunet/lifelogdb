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
made for them; a plan written in a note stays that note's text ([D23](docs/decisions/D23-no-tasks.md)). Recorded periods and
sessions follow [D22](docs/decisions/D22-events.md); money and a location track remain deferred
([D18](docs/decisions/D18-money.md), [D21](docs/decisions/D21-location-history.md)). A proposal that turns `life.db` into a
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

## Engineering priorities and working agreement

Build for a maintainer fifty years from now: understandable code, replaceable dependencies and explicit contracts,
not a promise that today's binary or toolchain lasts forever. Favor **consistent client behavior, ease of change and
fast feedback**. Safeguards should catch failures, not create ceremony.

- Read the relevant code, tests and contract before editing. Inspect the working tree; preserve unrelated and
  uncommitted changes. Keep the diff focused on the requested task, with necessary local refactoring only.
- Within that scope, proceed with implementation fixes and tests. Unless already authorized, explain the tradeoff
  and ask before adding a dependency, making a broad architectural or externally visible behavior change, or
  expanding scope. Contract changes follow the existing process below; routine implementation fixes need no new RFC.
- Prefer the smallest understandable solution. Do not add layers, frameworks, generic repositories, configuration
  knobs or speculative extension points for hypothetical needs. Do not reorganize working code just for uniformity.
- Keep application engineering guidance here and application decisions in [README.md](README.md); the database
  contract remains language-neutral in `docs/`. Explain non-obvious reasons and constraints, not what the syntax does.
- Before finishing, inspect the diff and generated changes. Report what changed, exact checks run and their results,
  and anything blocked or untested. Include checks in a commit message when committing; never imply unrun checks passed.

## Setup and checks

Use the Go version required by [go.mod](go.mod). The ordinary build and baseline tests need no cgo, Python or
`sqlite3` CLI: the pinned `modernc.org/sqlite` supplies SQLite. See [tests/README.md](tests/README.md) for its engine
requirements and schema-evolution probes. Modules need network access once; baseline tests use synthetic local data,
not external services or private files.

| tier | when / budget | run |
|---|---|---|
| focused feedback | while editing; aim for seconds | `go test ./internal/<package> -run '<TestName>'`; `go test -short ./...` for a broader quick check |
| baseline | after **every** change, before completion; aim for about two minutes | `go generate ./... && go vet ./... && go test ./...` |
| additional analysis | Go or dependency changes; also before releases | `staticcheck ./...` and `govulncheck ./...` |
| order dependence | changes to shared state, test helpers or concurrency | `go test -count=1 -shuffle=on ./...`; replay the reported seed with `-shuffle=<seed>` |
| races | extended checks on Linux; particularly concurrency changes | `CGO_ENABLED=1 go test -race -count=1 ./...` |
| diagrams | diagram changes | `LIFELOG_MERMAID=1 go test ./tests -run TestMermaidRender` |

- Run `gofmt` on changed Go files. `go generate` refreshes the embedded schema; never edit that copy directly.
  The baseline includes mutants: `-short` is feedback, not a substitute for completion checks. These budgets are
  design targets, not timeouts or permission to drop tests. Investigate slow suites instead of hiding coverage gaps.
- Staticcheck and govulncheck are separate development tools, not application dependencies. Use reviewed, pinned
  versions compatible with the toolchain; report versions. Do not silently install tools or introduce CI/tooling
  configuration. If unavailable, report the check as not run. Use govulncheck's default text mode for exit-status
  gating; successful JSON output is not a clean vulnerability result. Findings need resolution or explicit disposition,
  not blanket suppressions.
- The race check is a separate development environment: supported Linux target, cgo and a C compiler. It does not
  change the pure-Go release build. Diagrams need `mmdc` and Chromium, as [the suite guide](tests/README.md) describes.
- Support Windows and Linux while both are in use. Validate OS-sensitive paths, file operations and subprocesses on
  both before release; identify the OS actually tested. Do not claim cross-platform coverage from cross-compilation.
- Extended lifetime/stress, fuzz and benchmark runs may take 10–30 minutes and are explicit, not part of every edit.
  Run the relevant extended checks for changes to the behavior they exercise; reserve the whole set for scheduled or
  pre-release validation when available. Keep representative correctness cases in the baseline. Document a runner's
  invocation when implementing it; specifying a workload here does not mean that runner exists or has passed.

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

## Go implementation

- **Ordinary Go first.** Follow [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments): clear names,
  early error returns, cohesive packages, explicit dependencies and small public surfaces. Keep supporting packages
  under `internal/`. Prefer the standard library; justify a dependency by the complexity or risk it removes.
- **Concrete types by default.** Put a small interface at the consumer that actually needs substitution; do not mirror
  every struct with an interface or add a service/repository layer solely for mocks. Use generics only when they make
  actual repeated logic simpler. Share domain behavior, not incidental similarities between unrelated operations.
- **Preserve the existing application boundaries** ([README decisions](README.md#decisions)). Keep CLI, browser and
  MCP adapters thin, using the shared API/action catalog rather than duplicating validation or business rules. Reuse
  the existing core operations; a new surface must not become a second implementation of a write.
- **Contexts and ownership.** Pass the operation's `context.Context` explicitly, first, through blocking calls and SQL.
  Do not replace a request context with `Background` to evade cancellation, or use context values as dependency bags.
  Prefer synchronous APIs. Every goroutine needs an owner, bounded work, a shutdown path and a way to await completion;
  detached recovery work needs an explicitly justified lifetime.
- **Errors are part of the behavior.** Check errors, including setup, scans, `rows.Err()`, commits and output flush/close
  failures where data could be lost. Add useful operation context; use `%w` when exposing the cause is intentional and
  `errors.Is`/`errors.As` for semantics. Do not classify errors by incidental strings. Return expected failures, not
  panics; keep process exits at the executable boundary. Avoid duplicate logging and private bodies, readings or file
  contents in diagnostics; do not expose internal errors indiscriminately to clients.
- **Resources and transactions.** Close rows, files and response bodies and call cancellation functions with clear
  ownership. Use existing transaction helpers; do not mix pool calls with an active transaction's operations or perform
  avoidable network/file processing while holding a write transaction. Do not bypass connection setup for convenience
  ([connection contract](docs/contract/connections.md)).
- **Treat boundaries as untrusted.** Parameterize SQL values; allowlist dynamic identifiers. Bound request metadata,
  decoding and concurrency; stream large payloads. Preserve the existing request, path and authority protections
  ([application decisions](README.md#decisions)); lexical path cleaning alone is not filesystem containment. Test
  malformed input and boundary refusals, not only well-formed examples.
- **Portable and reproducible.** Use `filepath` for filesystem paths, not URL paths; avoid shell-dependent application
  behavior. Respect platform differences in case, separators, symlinks and file replacement. Keep dependency and
  toolchain updates deliberate, reviewed and tested; do not freeze vulnerable versions for the sake of longevity.

## Tests that earn confidence

- **Test-first where it matters.** For a bug, write a reproducer and observe the intended failure before the fix.
  For risky writes, define invariants and failure cases before implementation. Elsewhere, test order is flexible, but
  changed behavior needs tests before completion. If a pre-fix reproduction is impractical, explain why and what the
  regression test proves. A setup failure is not a valid reproduction.
- **Put tests with their subject.** Application tests live beside their packages as `*_test.go`; `tests/` validates
  the database contract ([suite guide](tests/README.md)). Use existing helpers and the contract's own vectors. Name tests
  by behavior; use table-driven subtests when cases share a structure, not as a requirement for every test.
- **Test behavior, not scaffolding.** Assert results, persisted state and meaningful errors, including forbidden side
  effects on failure. Prefer semantic assertions to whole-output snapshots, private call sequences or exact error
  prose unless that representation is the contract. Check setup errors; use `t.Helper`, `t.Cleanup` and useful
  got/want diagnostics. Fail the test from its test goroutine, not a background worker.
- **Real SQLite for storage.** Use fresh files under `t.TempDir()`, the canonical/embedded DDL as appropriate and
  production connection settings. Build application scenarios through the production writer. Direct SQL is appropriate
  for isolated DDL probes, deliberate damage and clearly labeled bulk setup whose equivalence is checked; it must not
  silently bypass the behavior under test. SQL mocks or `:memory:` alone cannot establish file-backed storage behavior.
  Use narrow fakes for external boundaries or deliberate fault injection, not as a replacement for integration tests.
- **Independent answers.** Use hand-checked expected values or a simpler reference model. Do not compute the expected
  result using the production query, normalizer or algorithm being tested. Supplement row counts with relevant field,
  relationship and ordering assertions. Integrity checks supplement these assertions; they do not replace them.
- **Deterministic by construction.** Use fixed dates, local seeded RNGs and isolated temporary state; report seeds and
  failing operations. Synchronize concurrent tests with signals, not sleeps. Use `testing/synctest` for self-contained
  timing/concurrency logic, not as fake time for real database or filesystem I/O. Timeouts guard deadlocks, not speed.
  Parallelize only isolated tests; avoid shared databases, mutable globals, environment or working-directory changes.
- **Exercise failure paths according to risk.** For affected operations test validation boundaries, rollback after partial
  work, cancellation, stale versions, repeated imports/retries, correction chains and reopen/recovery. Use subprocess
  tests for process exits or interruption claims. Do not describe process-interruption tests as power-loss proof.
- **Client parity is a first-class contract.** Run shared behavioral scenarios through applicable CLI, HTTP/HTML and
  MCP entry points, including in-process and remote dispatch where relevant. Verify the same domain outcomes, validation
  and persisted state, allowing only intentional presentation, provenance and permission differences. Assert those
  differences too: owner-only actions must remain unavailable to agents. Include a small set of real transport/process
  smoke tests; direct handler calls alone do not prove adapter wiring. Keep most exhaustive cases at their owning layer
  rather than multiplying every test across every surface.
- **Fuzz and property-test meaningful invariants.** Target text/Unicode parsing, metadata decoders, path handling,
  import normalization and operation sequences. Keep seeds synthetic, cases independent and runs bounded; preserve
  minimized failing inputs as regressions. Ordinary `go test` runs seed cases, not a fuzz campaign. Example of an
  existing target: `go test ./internal/photo -run '^$' -fuzz '^FuzzRead$' -fuzztime=60s`.
- **Coverage is a map, not a score.** Use coverage to find untested behavior, particularly failure paths; no global
  percentage, test-count or one-test-per-function quota. Never weaken assertions, skip a failure, bless changed snapshots
  or add retries just to get green. An incorrect test can change with evidence of the intended contract. Treat flakes
  as defects. Contract-rule mutants remain governed by the documentation rules below.

## Synthetic personas and scale

When adding generators, use these agreed workloads. They exercise existing capabilities, not new schema requirements
or a promise of performance. Generate from scratch, never from private exports or supposedly anonymized records.

| persona | behavior to exercise |
|---|---|
| daily journaler | decades of day pages, people, places, links, search and edits |
| measurement-heavy user | dense readings, habits, corrections, retractions and historical queries |
| mixed-media keeper | notes, long transcripts, document text and selected photo previews |
| messy importer | overlapping imports, duplicates, Unicode, malformed inputs, interruption and retries |

- **Small:** representative cases of all four personas for routine correctness tests. **Lifetime:** a 50-year mixed
  workload with 20 measurements/day, 3 files/day and about 4,000 imported notes in addition to day pages. **Stress:**
  explicitly increased density, long text, high-degree links and import bursts; record the parameters, not just "large".
- Make the logical dataset reproducible: fixed calendar anchor, seeds, generator version, operation order, payload-size
  distributions and known answers. Include gaps, bursts, popular and rare links/terms, corrections and backdated data,
  not just uniform append-only rows. On failure report the scenario, seed and operation; reduce it to a small regression.
- Exercise end-to-end workflows and persisted results, not just successful bulk insertion. Keep a simple independent
  oracle for expected queries/state. Label accelerated SQL loaders and compare manageable equivalent scenarios against
  writer-built fixtures. Do not claim a direct-SQL load measures application import throughput.
- Keep generators, small fixtures and regression inputs in git, not generated databases. Build databases in temporary
  or explicitly selected ignored scratch storage with cleanup. Keep large preview-heavy runs opt-in and report disk
  requirements; use valid synthetic payloads with representative sizes. Metadata-only runs are useful but must be
  labeled: tiny repeated blobs do not establish realistic preview/storage scale.

## Performance evidence, not speculative optimization

- Measure important workflows at small and lifetime sizes: capture/save, day view, search/backlinks, historical
  measurements, import/replay and snapshot/restore. Check correctness as well as cost. Establish reproducible baselines
  before adding caches, indexes or concurrency; contract/schema changes still need the normal evidence and approval.
- Keep fixture construction and correctness-oracle work outside timed query operations. Prefer `for b.Loop()` for new
  Go benchmarks; report allocations. Fully consume results and check errors. Define whether preparation, commit,
  decoding or preview creation is included. Reset mutable state outside timing or measure an explicitly bounded growth
  trace; repeated iterations must not silently turn writes into no-ops or change the workload.
- Compare repeated samples with `benchstat`, not one run; profile with `pprof` before optimizing. An existing benchmark:
  `go test ./internal/importer -run '^$' -bench '^BenchmarkResolveReadingKeys$' -benchmem -count=10`.
- Record scenario/seed, revision, Go and SQLite/driver versions, OS, hardware/storage, connection/maintenance settings
  and cache conditions. Report latency, allocations, import throughput and storage (database, WAL/SHM and relevant
  temporary files). Go allocation counts are not total process memory. Label warm versus fresh-process runs; do not
  call a reopened database a cold-disk measurement without controlling the OS cache.
- Use query plans and work counts to investigate scaling, not brittle exact `EXPLAIN QUERY PLAN` text assertions.
  No universal wall-clock CI gate: introduce a hard budget only for an agreed usability requirement on a defined
  environment. The 50-year workload is a test envelope, not evidence that any implementation already meets it.

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
- **Plans** live in `docs/plans/`.

## Conventions every writer and every DDL change preserves

Their homes are in `docs/`; this list is the checklist, not the rule.

- **Time** (`lifelog_meta.instants` and `.days`, [D10](docs/decisions/D10-time-model.md)): UTC ISO-8601 instants and local-day TEXT columns with round-trip CHECKs
  (`date(x) IS x`, `strftime(...) IS x` — the `IS` matters).
- **Identity and provenance**: follow the `entities`, `entity_names`, typed-extension and `lifelog_meta.source`
  rules in [schema.sql](docs/schema/schema.sql), with the insert conventions in
  [a person or a place](docs/cookbook/person-or-place.md) and [imports](docs/contract/imports.md).
  Review owned names, direct typed foreign keys, stable identity and provenance together.
- **No deletes** (`lifelog_meta.deletes`, [D11](docs/decisions/D11-tombstones.md)): tombstones (BEFORE DELETE triggers);
  only `links` rows are deleted.
- **Append-only facts**: measurements. A reading is corrected with `supersedes_id` and retracted with a
  NULL value (D7).
- **Imports**: `ON CONFLICT … DO NOTHING`, never `OR IGNORE` / `OR REPLACE` ([imports](docs/contract/imports.md)).
- **CHECKs**: every one NAMED (`CONSTRAINT <table>_<rule> CHECK …`), using only functions the minimum
  SQLite has (`lifelog_meta.sqlite`) — no math functions, even where a build has them.
- **Links**: a closed, endpoint-typed `link_kinds` registry (D8).
- **Names and journal identity**: use [titles and wikilinks](docs/contract/titles-and-wikilinks.md), its vectors,
  and [D5](docs/decisions/D05-pages-and-day-pages.md). Check preferred-name changes, retained aliases and journal
  reservations against those rules. Recorded sessions and periods follow [D22](docs/decisions/D22-events.md);
  habits follow [D24](docs/decisions/D24-habits.md).
- **Connections** ([connection setup](docs/contract/connections.md)): one writing application per file; per connection
  `PRAGMA foreign_keys=ON`, `recursive_triggers=ON`, `synchronous=FULL`, `trusted_schema=OFF` (the first three read back and refused
  if wrong); SQLite ≥ 3.51.3 for writers; every write transaction starts with `BEGIN IMMEDIATE`; the driver opens
  no transactions of its own. Readers open the file read-only (`mode=ro`, never `immutable=1`) and set
  `trusted_schema=OFF` too;
  exploration tools (Datasette) likewise, and nothing that edits rows is pointed at it.
- **The wikilink save contract**: implement [titles and wikilinks](docs/contract/titles-and-wikilinks.md) and
  [save a body](docs/cookbook/save-a-body.md), including reference resolution, target savepoints, set equality,
  invalid targets, tags and ordinary REDIRECT prose. Keep the contract's vectors as their sole specification.
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

## Engineering references

These primary sources inform the application policies above; they do not override this repository's database contract
or turn optional techniques into universal Go rules.

- Go: [review comments](https://go.dev/wiki/CodeReviewComments), [module layout](https://go.dev/doc/modules/layout),
  [error wrapping and API boundaries](https://go.dev/blog/go1.13-errors), [context](https://pkg.go.dev/context).
- Testing: [test review comments](https://go.dev/wiki/TestComments), [testing APIs](https://pkg.go.dev/testing),
  [fuzzing](https://go.dev/doc/security/fuzz/), [synctest](https://pkg.go.dev/testing/synctest),
  [race detector and prerequisites](https://go.dev/doc/articles/race_detector).
- Tools: [Staticcheck](https://staticcheck.dev/docs/),
  [govulncheck and its limitations](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck).
- Measurement: [benchmark loops](https://go.dev/blog/testing-b-loop),
  [benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat), [profiling](https://go.dev/doc/diagnostics),
  [SQLite performance methodology](https://sqlite.org/cpu.html), [query-plan caveats](https://sqlite.org/eqp.html).
- Model-based testing: [stateful reference-model example](https://hypothesis.readthedocs.io/en/latest/stateful.html)
  (the technique, not a Python dependency).
