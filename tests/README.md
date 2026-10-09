# tests/ — the validation suites of the docs

The [docs](../docs/README.md) mark a claim *executed* when a suite in this folder runs it. The suites are Go tests
(package `tests`, in the module at the repo root), so the rule in [AGENTS.md](../AGENTS.md) — *after changing DDL,
re-run the checks* — is one command. **Nothing here is a migration and nothing touches `life.db`**: every suite reads
`docs/schema/schema.sql` (and the contract and cookbook blocks it needs) from `docs/`, builds throwaway databases in
a temporary folder and discards them.

```
go test ./tests                                  # every suite and every mutant
go test -short ./tests                           # the suites without the mutants
go test ./tests -run 'TestSuites/pages' -v       # one suite, with its count of expectations
go test ./tests -run 'TestMutants/.*habits' -v   # the mutants of one suite
LIFELOG_MERMAID=1 go test ./tests -run TestMermaidRender   # + render every mermaid diagram (needs mmdc + a Chromium)
```

The manually triggered [Windows validation workflow](../.github/workflows/windows-validation.yml) runs generation,
vet and the complete baseline, including mutants, on `windows-2025` using the Go version in `go.mod`.
It uses read-only repository permissions and does not publish a release.

Needs Go ≥ 1.27 and nothing else: SQLite is the driver's own build (`modernc.org/sqlite`, pure Go, SQLite 3.53.4 with
FTS5); the `writers` suite checks the floor of `lifelog_meta.sqlite` on it. The schema-evolution probes execute `ALTER TABLE …
DROP CONSTRAINT` on disposable files and need 3.53; they are not a migration runner. A writer needs only 3.51.3. The rendering test also needs
`npm i -g @mermaid-js/mermaid-cli` and a Chromium; point `MMDC` / `PUPPETEER_EXECUTABLE_PATH` at them if they are
not on `PATH`.

The suites are grouped by **subject**, not by when a rule was added. Each states its expectation in every label;
`-v` prints `<name>: X/Y met expectations`, and every expectation not met is a test failure. A suite that stops on
the document (a broken block makes a later step impossible) reports that as one failed expectation.

| file | suite | subject |
|---|---|---|
| `dates_test.go` | `dates` | instants, local days, the round-trip CHECKs and why `IS`, the zone (lifelog_meta.instants and .days, D10) |
| `schema_safeguards_test.go`, `schema_bounds_test.go` | `schema-safeguards` | NUL-free constrained text; the bounds of import keys, person names, units, notes and task labels, with the empty-key incident; a tombstone never precedes creation; `entities.body` last; the atomic Mood seed; no `OR IGNORE`/`OR REPLACE` in a trigger; immutable typed-detail/habit identities and entity creation evidence; rowid-alias atomicity; Unicode search distinctions; cleanup excludes retained session and measurement references |
| `schema_adversarial_test.go` | `schema-adversarial` | Scoped import replay after tombstones; new-row refusal and multi-row atomicity; skipped duplicate validation limits; statement ABORT versus caller transaction rollback |
| `graph_model_test.go` | `graph-model`, `FuzzGraphTransitions` | Independent directed/undirected graph model, NULL/empty notes, self-links, retry/edit/delete sequences, which endpoints a change advances (the page a link leaves, both of a friendship, none for a wikilink's insert or delete), savepoint and transaction rollback, typed batch refusal and read-only reopen |
| `temporal_model_test.go` | `temporal-model` | Recursive lifecycle reads, independent correction-chain cutoff model, bounded calendar/deadline model with persisted overrides, tombstones, recurrence ends and extreme years |
| `storage_resilience_test.go` | `storage-resilience` | FTS term-position equality across trigger reordering, rejected edits, rollback, reopen and rebuild; WAL reader body/index snapshot isolation and checkpoint release |
| `connection_fixture_test.go` | `TestSchemaFixtureUsesLiteralFilename` | Literal URI-sensitive filenames in isolated schema fixtures and read-only reopen |
| `id_guards_test.go` | `id-guards` | file-backed declared/rowid/_rowid_/oid identity guards; forbidden combined edits, no-ops, revisions, mirrors, FTS and reopen controls |
| `name_grammar_test.go` | `name-grammar` | documented raw/NFC addressability vectors under mutation; canonical key and bracket-storage probes, separate from production pipeline tests |
| `sessions_test.go` | `recorded-sessions`, `measurement-scopes` | shared endpoint vectors, immutable session identity/provenance, revisions/kinds, correction scope/liveness and actual semantic damage probes |
| `periods_test.go` | `recorded-periods` | boundary storage/revision/ID guards and shared membership vectors; the boundary grammar as one hand-checked vector table (precision, calendar validity, one qualifier, `..` only as an end, NUL) through both columns on insert and update |
| `planning_test.go` | `planning` | file-backed one-off cardinality, retained project context, immutable IDs/provenance/cadence, revisions, retry preservation, atomic stop/outcome rules and deliberate semantic damage |
| `planning_boundaries_test.go` | `planning-boundaries` | named CHECK/NUL/exact-time boundaries; shared calendar membership and reminder-clock vectors, including leap clamps, range edges and non-hour transitions |
| `planning_cookbook_test.go` | `planning-cookbook` | literal creation/materialization/edit/stop recipes, dual stale versions and merged deadline reads with moved, undated, skipped and tombstoned slots |
| `identity_test.go` | `identity` | the supertype and its composite FKs, ids by `RETURNING`, `source` on every row, no hard deletes, `updated_at` and the edit revisions: creation is not an edit, a link advances only the page it leaves (schema.sql, D8, D10, D11, D12) |
| `names_test.go` | `names`, `name-ownership` | mandatory deferred preferred ownership; direct reserved names and immutable owner/key/id; actual alias/preferred/spelling/body FTS maintenance and journal-name reservations |
| `named_test.go` | `named` | a person or a place is a page: one id, promotion, cookbook/person-or-place, cookbook/days-that-name and cookbook/everything-about run literally, two Sams (D16, D20) |
| `files_test.go` | `files` | a file is a page: one id, the hash, type and preview of the original and their CHECKs (`IS` for the JPEG bytes: `substr` of an empty blob is NULL), what never changes, never deleted, the graph (an embed, a caption, `about`, `part-of`, refused redirect), a ghost promoted, cookbook/keep-a-file as a writer runs it (D9) |
| `places_test.go` | `places` | where a place is: its point, radius and `link_days` and their CHECKs (NaN, infinity and 0°, 0° refused), fixed by UPDATE, never deleted; cookbook/place-of-a-photo run literally: a point given once, the match against a haversine oracle (the smallest circle, then the nearest, then stable id), the antimeridian limit, the day linked and the photo shown once (D21) |
| `pages_test.go` | `pages` | every page titled, the day rule, filename-safe titles, normalized owned keys and their vectors, lookups, FTS, why not a collation (contract/titles-and-wikilinks, D5) |
| `renames_test.go` | `renames` | cookbook/rename-a-page run literally: id, prose, day, provenance, typed detail and incident links retained; direct aliases, owned-name selection, case-only rename and no-effect refusals; writer parity (contract/titles-and-wikilinks, D5) |
| `links_test.go` | `links` | the closed kind registry, endpoint types, mirrors, `at`, containment over day pages with its cycle guard (D8, D16) |
| `habits_test.go` | `habits` | habit periods: their days, order, no overlap, unitless only (a scale names its range as its unit, so Mood and every registered scale are refused), never deleted; cookbook/habits and the cookbook/day-view habit leg — done, not done, not recorded and invalid data (D24) |
| `facts_test.go` | `facts`, `measurement-query-plan` | metrics and measurements: append-only, supersede, retract, finite values, never `OR IGNORE`/`OR REPLACE` (D7); categories as pages and cookbook/metrics-by-category (D26); the independent query-plan suite retains the 20,000-reading index-use and count probe plus retained-reference access paths and the day and backlink lookups (`entities_day`, `links_to`) |
| `journal_test.go` | `journal` | `entities.is_journal`, the one stored definition of a journal day page, with its SQLite premises (a CHECK and a trigger's NEW/OLD read a generated column); the day page and capture (cookbook/capture), the cookbook/day-view day view (a page is "(edited)" by its revision, not by comparing clocks), the days that name someone (cookbook/days-that-name), where I was (cookbook/where-was-i), observed habits and birthdays kept distinct from explicit planning (D5, D15, D16, D22, D23) |
| `writers_test.go` | `writers` | the `BEGIN IMMEDIATE` race with real concurrent connections, pragmas, read-only readers and their `trusted_schema=OFF`, a hardened connection (contract/connections) |
| `integrity_test.go`, `semantic_integrity_test.go` | `integrity`, `physical-integrity` | semantic ownership, mirrors, correction cycles, habit validity, journal names, FK and FTS damage on small files; an independent fixture of 3,000 synthetic pages for zeroed pages, truncation, live-index damage and changed body bytes |
| `imports_test.go` | `imports` | the import block of contract/imports on 1 000 CSV rows, and its traps |
| `snapshots_test.go` | `snapshots` | cookbook/take-a-snapshot on a live file: `VACUUM INTO` through a read-only connection while the writer writes, its target, the restore check left byte for byte, the restore and the old `-wal` beside it (D25) |
| `evolution_test.go`, `evolution_compatibility_test.go` | `evolution` | named CHECKs, widening, partial dates, the tokenizer switch, reader compatibility, comments inside statements (D13, D17, architecture/non-goals) |
| `cookbook_test.go`, `cookbook_measurements_test.go` | `cookbook` | measurement lifecycle and recorded-time cutoffs; every cookbook block prepares and runs, on a plain and on a hardened connection; every block that only reads runs on a reader (`mode=ro`, `trusted_schema=OFF`) |
| `document_test.go` | `document` | the 2075 test, the rules live in the file, the tree holds together (one record per decision, every relative link and anchor resolves, every page reachable from `docs/README.md`), the totals in `schema/README.md` |
| `diagrams_test.go` | `diagrams` | the seven mermaid diagrams say what the DDL says (keys, relationships, the link map, the correction story) |
| `wikilinks_test.go` | `doc-save-contract` | the save contract as the docs print it: the vector table of contract/titles-and-wikilinks, cookbook/save-a-body run literally (and equal to a writer's own save after 400 random edits), cookbook/backlinks |
| | `save-contract` | the save contract through a writer's own save against the DDL: invalid targets, the `SAVEPOINT` backstop, set equality, ordinary REDIRECT prose, revival, 400 random edits against a rebuild, 4 concurrent writers, every vector |
| | `title-fuzz` | writer acceptance is a subset of the DDL's filename checks over 60 000 generated strings; DB-only names independently fail reference addressability |
| `mutants_test.go` | `TestMutants` | 481 broken copies of the docs tree, one rule each; a completed owning suite must fail the mutant's explicit rule witness |
| `render_test.go` | `TestMermaidRender` | optional: every diagram renders |
| `kit_test.go`, `suites_test.go` | | reading the tree (a page, the cookbook blocks by recipe key, an overlay of broken files for a mutant); fresh databases and the insert conventions (entity first, `RETURNING`, named entities); the runner |

**The vectors have one home per subject**: the table of [titles and wikilinks](../docs/contract/titles-and-wikilinks.md),
[exact time](../docs/contract/exact-time.md), and [planning calendar/reminder clocks](../docs/contract/planning.md).
The wikilink suites read them from the page (with the boundary cases the line under the table states) and run them through the
writer's extraction (`internal/text`) and its save (`internal/core`); `internal/text` has its own tests against the
same table. Every save path here runs against the DDL under test, so a mutant of `schema.sql` reaches it.
Planning suites compare file-backed SQLite admission and the writer's reminder resolver with the contract's
independent expected answers. Boundary probes roll back every case, including unexpected success under a mutant.
The core planning tests exercise the production writer, deadline merging, retries, rollback and snapshot reopen.
Its calendar fuzz target compares bounded expansion with an independent day-by-day oracle; baseline runs its seeds.
Run a bounded campaign explicitly with
`go test ./internal/core -run '^$' -fuzz '^FuzzTaskCalendarWindow$' -fuzztime=20s -parallel=2`.

The graph model runs three deterministic 160-operation sequences in the baseline. Its separate fuzz target accepts
minimizable byte-encoded operations, caps each input at 64 transitions, and creates a fresh hardened file for every
input. Seeds run normally; a campaign is explicit:
`go test ./tests -run '^$' -fuzz '^FuzzGraphTransitions$' -fuzztime=30s -parallel=2`.
The temporal model compares 40 correction chains at 17 clock cutoffs with independent insertion-order histories,
and deadline results with an anchor-generated calendar oracle. These bounded models supplement the contract's literal
vectors and rule mutants; agreement does not prove every possible input or SQLite version correct.

**Fixture ownership.** Semantic fact and integrity mutants run their small behavior fixtures; the baseline
`measurement-query-plan` and `physical-integrity` suites own the large query-plan and byte-corruption fixtures.
Every original probe still runs in the baseline. Exact-time vectors share one baseline and roll back a savepoint
after each column probe, including a successful write under a mutant, so no case changes the next one's setup.

**When the docs change.** A DDL change: run everything. A new rule that spans tables: a `lifelog_meta` key and a row
in the 2075 table of contract/threat-model (`document` fails until both exist); a table's own rule: a comment in its
`CREATE` statement. A changed cookbook block or diagram: the suites that extract it will tell you. If a suite must
change because the docs legitimately changed, change it in the same edit and say so in the commit message — a suite
loosened to pass proves nothing. Add a mutant to `mutants_test.go` for a new rule, so the suite that owns it is shown
to notice when it breaks. Each mutant names an exact relevant expectation label in the registry; the runner retains
failed labels separately from diagnostic details and credits only that named failure in a normally completed suite,
after a clean baseline. Missing witnesses, unrelated failures and stopped suites receive no credit.
`TestMutantWitness` checks these false-credit controls, including an unchanged mutation.

The writer ordering probes use a zero-timeout lock refusal while the first transaction is open, then an explicit
commit signal before the second writer resolves the existing row. Reader progress is tested with a known open
snapshot and a 30-second deadlock guard, not a speed threshold. These probes do not measure automatic busy waiting.

**What is not here:** power loss and real data have never been tested.

The Linux storage-wrapper regression lives beside its implementation in
[`internal/db/io_failure_linux_test.go`](../internal/db/io_failure_linux_test.go).
Run `go test ./internal/db -run '^TestFilesystemWriteFailure$' -count=1` to exercise
child-local file-size limits that produce actual `SQLITE_IOERR_WRITE` errors, followed by
transaction rollback, reopen/retry and failed snapshot cleanup. This runs in the Linux baseline;
it does not simulate disk exhaustion, sync failures, torn writes or power loss.

## Synthetic scale runner

`tools/lifescale` builds fresh file-backed databases through the production writer and emits a JSON report.
The normal baseline includes only the small fixture and its independent known answers; larger profiles are opt-in.

```
go run ./tools/lifescale -profile small -seed 2075 -samples 5
go run ./tools/lifescale -profile lifetime -seed 2075 -samples 5 -keep -storage "describe CPU, disk and filesystem"
go run ./tools/lifescale -profile stress -seed 2075 -samples 5 -keep -storage "describe CPU, disk and filesystem"
go run ./tools/lifescale -profile small -previews -seed 2075
```

Progress goes to stderr; redirect stdout to a JSON file outside the repository to retain measurements.
For recorded comparisons, build the runner with `go build -o /tmp/lifescale ./tools/lifescale` and run that binary
(choose an appropriate executable path on Windows). Build metadata records the revision and dirty state;
`go run` leaves the revision unavailable rather than identifying an unrelated checkout.
The runner creates a uniquely named subdirectory under the system temporary directory (or `-dir`), cleans it
afterward by default, and prints its location. `-keep` retains the synthetic database, snapshot and restored copy.
It never opens an existing user database. Ctrl-C cancels construction and normal cleanup still runs.

Profiles have fixed parameters: **small** is 14 days, four readings/day, one file/day and eight imported notes;
**lifetime** is 1970-01-01 through 2019-12-31 (18,262 days), 20 readings/day, three files/day and 4,000 notes;
**stress** uses the same calendar, 80 readings/day, six files/day, 16,000 notes and four times the text sizes.
Recorded dates include gaps, bursts and later writes of older facts. The fixture includes a journal, person/place
references, a popular topic, rare search terms, a habit, Unicode notes, keyed retries, value corrections,
retractions and a malformed batch with a valid prefix that must roll back. Notes arrive in batches of 50.
The JSON records generator version, seed, payload-size schedule and a digest of the ordered logical inputs;
write-clock timestamps are intentionally excluded from reproducibility.

Default runs are explicitly **metadata-only**: files have unique synthetic text originals and no previews.
Allow several GB of free space for lifetime metadata runs and substantially more for stress, including the live
database/WAL plus snapshot and restored copies. These are planning allowances, not measured storage results.
`-previews` instead generates unique valid JPEG images at 640×480, 1024×768 and 1600×900, quality 75.
A lifetime preview run can require tens of GB per database and over 100 GB of free scratch space for all copies;
stress previews require more. Preview generation and decoding also increase run time. Small previews establish
correctness, not lifetime storage capacity. Do not run large preview profiles without choosing adequate scratch storage.

The report includes Go/SQLite/driver/revision, OS/architecture/CPU, operator-supplied storage details, writer
pragmas, Go allocation deltas, raw timing samples and database/WAL/SHM/copy sizes. Fixture construction is timed
separately from capture/save, day view, rare search, popular backlinks, historical metric series and keyed root
replay. The latter measures `core.Record` retries, **not** workspace import throughput. Capture/save includes
commit; each sample performs a real new capture/edit. Queries are warm after fixture validation. Snapshot and
copy/open restore each run once, regardless of `-samples`; restored contents and integrity are checked outside
timing. Reopening a file is not a cold-disk benchmark, and Go allocation counts are not total process memory.
Compare repeated reports under the same environment; there is no universal wall-clock pass/fail threshold.
