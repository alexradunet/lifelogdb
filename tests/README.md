# tests/ — the validation suites of the docs

The [docs](../docs/README.md) mark a claim *executed* when a suite in this folder runs it. The suites are Go tests
(package `tests`, in the module at the repo root), so the rule in [AGENTS.md](../AGENTS.md) — *after changing DDL,
re-run the checks* — is one command. **Nothing here is a migration and nothing touches `life.db`**: every suite reads
`docs/schema/schema.sql` (and the contract and cookbook blocks it needs) from `docs/`, builds throwaway databases in
a temporary folder and discards them.

```
go test ./tests                                  # every suite and every mutant, ~20 s
go test -short ./tests                           # the suites without the mutants, ~5 s
go test ./tests -run 'TestSuites/pages' -v       # one suite, with its count of expectations
go test ./tests -run 'TestMutants/.*habits' -v   # the mutants of one suite
LIFELOG_MERMAID=1 go test ./tests -run TestMermaidRender   # + render every mermaid diagram (needs mmdc + a Chromium)
```

Needs Go ≥ 1.27 and nothing else: SQLite is the driver's own build (`modernc.org/sqlite`, pure Go, SQLite 3.53.4 with
FTS5); the `writers` suite checks the floor of `lifelog_meta.sqlite` on it. The suites run migrations (`ALTER TABLE …
DROP CONSTRAINT`), which need 3.53; a writer needs only 3.51.3. The rendering test also needs
`npm i -g @mermaid-js/mermaid-cli` and a Chromium; point `MMDC` / `PUPPETEER_EXECUTABLE_PATH` at them if they are
not on `PATH`.

The suites are grouped by **subject**, not by when a rule was added. Each states its expectation in every label;
`-v` prints `<name>: X/Y met expectations`, and every expectation not met is a test failure. A suite that stops on
the document (a broken block makes a later step impossible) reports that as one failed expectation.

| file | suite | subject |
|---|---|---|
| `dates_test.go` | `dates` | instants, local days, the round-trip CHECKs and why `IS`, the zone (lifelog_meta.instants and .days, D10) |
| `id_guards_test.go` | `id-guards` | file-backed declared/rowid/_rowid_/oid identity guards; forbidden combined edits, no-ops, revisions, mirrors, FTS and reopen controls |
| `name_grammar_test.go` | `name-grammar` | documented raw/NFC addressability vectors under mutation; canonical key and bracket-storage probes, separate from production pipeline tests |
| `sessions_test.go` | `recorded-sessions`, `measurement-scopes` | shared endpoint vectors, immutable session identity/provenance, revisions/kinds, correction scope/liveness and actual semantic damage probes |
| `periods_test.go` | `recorded-periods` | boundary storage/revision/ID guards and shared membership vectors |
| `identity_test.go` | `identity` | the supertype and its composite FKs, ids by `RETURNING`, `source` on every row, no hard deletes, `updated_at` (schema.sql, D8, D10, D11) |
| `names_test.go` | `names`, `name-ownership` | mandatory deferred preferred ownership; direct reserved names and immutable owner/key/id; actual alias/preferred/spelling/body FTS maintenance and journal-name reservations |
| `named_test.go` | `named` | a person or a place is a page: one id, promotion, cookbook/person-or-place, cookbook/days-that-name and cookbook/everything-about run literally, two Sams (D16, D20) |
| `files_test.go` | `files` | a file is a page: one id, the hash, type and preview of the original and their CHECKs (`IS` for the JPEG bytes: `substr` of an empty blob is NULL), what never changes, never deleted, the graph (an embed, a caption, `about`, `part-of`, refused redirect), a ghost promoted, cookbook/keep-a-file as a writer runs it (D9) |
| `places_test.go` | `places` | where a place is: its point, radius and `link_days` and their CHECKs (NaN, infinity and 0°, 0° refused), fixed by UPDATE, never deleted; cookbook/place-of-a-photo run literally: a point given once, the match against a haversine oracle (the smallest circle, then the nearest), the antimeridian limit, the day linked and the photo shown once (D21) |
| `pages_test.go` | `pages` | every page titled, the day rule, filename-safe titles, normalized owned keys and their vectors, lookups, FTS, why not a collation (contract/titles-and-wikilinks, D5) |
| `renames_test.go` | `renames` | cookbook/rename-a-page run literally: id, prose, day, provenance, typed detail and incident links retained; direct aliases, owned-name selection, case-only rename and no-effect refusals; writer parity (contract/titles-and-wikilinks, D5) |
| `links_test.go` | `links` | the closed kind registry, endpoint types, mirrors, `at`, containment over day pages with its cycle guard (D8, D16) |
| `habits_test.go` | `habits` | habit periods: their days, order, no overlap, unitless only, never deleted; cookbook/habits and the cookbook/day-view habit leg — done, not done, not recorded (D24) |
| `facts_test.go` | `facts` | metrics and measurements: append-only, supersede, retract, finite values, never `OR IGNORE`/`OR REPLACE` (D7); categories as pages and cookbook/metrics-by-category (D26) |
| `journal_test.go` | `journal` | the day page and capture (cookbook/capture), the cookbook/day-view day view, the days that name someone (cookbook/days-that-name), where I was (cookbook/where-was-i), what stands in for recurrence, events and tasks (D5, D15, D16, D22, D23) |
| `writers_test.go` | `writers` | the `BEGIN IMMEDIATE` race with real concurrent connections, pragmas, read-only readers and their `trusted_schema=OFF`, a hardened connection (contract/connections) |
| `integrity_test.go` | `integrity` | the four checks of contract/integrity-checks on the live file, against real damage |
| `imports_test.go` | `imports` | the import block of contract/imports on 1 000 CSV rows, and its traps |
| `snapshots_test.go` | `snapshots` | cookbook/take-a-snapshot on a live file: `VACUUM INTO` through a read-only connection while the writer writes, its target, the restore check left byte for byte, the restore and the old `-wal` beside it (D25) |
| `evolution_test.go` | `evolution` | named CHECKs, widening, partial dates, the tokenizer switch, comments inside statements (D13, D17, architecture/non-goals) |
| `cookbook_test.go` | `cookbook` | every cookbook block prepares and runs, on a plain and on a hardened connection; every block that only reads runs on a reader (`mode=ro`, `trusted_schema=OFF`) |
| `document_test.go` | `document` | the 2075 test, the rules live in the file, the tree holds together (one record per decision, every relative link and anchor resolves, every page reachable from `docs/README.md`), the totals in `schema/README.md` |
| `diagrams_test.go` | `diagrams` | the seven mermaid diagrams say what the DDL says (keys, relationships, the link map, the correction story) |
| `wikilinks_test.go` | `doc-save-contract` | the save contract as the docs print it: the vector table of contract/titles-and-wikilinks, cookbook/save-a-body run literally (and equal to a writer's own save after 400 random edits), cookbook/backlinks |
| | `save-contract` | the save contract through a writer's own save against the DDL: invalid targets, the `SAVEPOINT` backstop, set equality, ordinary REDIRECT prose, revival, 400 random edits against a rebuild, 4 concurrent writers, every vector |
| | `title-fuzz` | writer acceptance is a subset of the DDL's filename checks over 60 000 generated strings; DB-only names independently fail reference addressability |
| `mutants_test.go` | `TestMutants` | 281 broken copies of the docs tree, one rule each; a completed owning suite must fail the mutant's explicit rule witness |
| `render_test.go` | `TestMermaidRender` | optional: every diagram renders |
| `kit_test.go`, `suites_test.go` | | reading the tree (a page, the cookbook blocks by recipe key, an overlay of broken files for a mutant); fresh databases and the insert conventions (entity first, `RETURNING`, named entities); the runner |

**The vectors have one home**: the table of [titles and wikilinks](../docs/contract/titles-and-wikilinks.md). The
suites read them from the page (with the boundary cases the line under the table states) and run them through the
writer's extraction (`internal/text`) and its save (`internal/core`); `internal/text` has its own tests against the
same table. Every save path here runs against the DDL under test, so a mutant of `schema.sql` reaches it.

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
