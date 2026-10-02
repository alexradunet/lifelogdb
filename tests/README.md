# tests/ — the validation suites of the docs

The [docs](../docs/README.md) mark a claim *executed* when a suite in this folder runs it. This folder exists so that the rule
in [AGENTS.md](../AGENTS.md) — *after changing DDL, re-run the checks* — can actually be followed. **Nothing here is a migration
and nothing touches `life.db`**: every suite reads `docs/schema/schema.sql` (and the contract and cookbook blocks it needs) from
`docs/`, builds throwaway databases and discards them. `run_all.py` gives every suite one temporary folder and removes it at the end; a suite run on its own
leaves its folder behind. The reference parser is pinned (`markdown-it-py==4.2.0`).

```
python3 tests/run_all.py              # every suite, ~45 s, most of it the mutants
python3 tests/run_all.py --datasette  # + Datasette opens the file read-only (installs it into tests/.venv)
python3 tests/run_all.py --mermaid    # + render every mermaid diagram (needs node + `mmdc` + a Chromium, see below)
```

Needs Python ≥ 3.12 (venv + network once, for `markdown-it-py`) whose `sqlite3` module, like the `sqlite3` CLI, runs SQLite
≥ 3.53 built with FTS5 (the suites run migrations; a writer needs only 3.51.3); a distribution's build may lack FTS5 (`no such module: fts5`). `run_all.py` checks both first and says what is missing. `--mermaid` also needs
`npm i -g @mermaid-js/mermaid-cli` and a Chromium; point `MMDC` / `PUPPETEER_EXECUTABLE_PATH` at them if they are not on `PATH`.

On Windows the venv is `Scripts\python.exe` (the runner looks there too), and the extracted DDL is read as UTF-8 by every
script that opens it — with the locale default (cp1252) the non-ASCII device names of `pages_title_safe` would be corrupted and
the suites would run a DDL that is not `schema.sql`. The `sqlite3` CLI comes from sqlite.org's tools zip (put its folder on `PATH`); a
newer `sqlite3.dll` in the interpreter's `DLLs` folder lifts Python's own module. The runner sets `PYTHONUTF8=1` for every suite, so a failing suite's non-ASCII diagnostic prints instead of crashing.

The suites are grouped by **subject**, not by when a rule was added. Each one states its expectation in every label and ends
with `<name>: X/Y met expectations`; a suite that stops on an error reports that as a failed expectation.

| folder | suite | subject |
|---|---|---|
| `schema/` | `dates.py` | instants, local days, the round-trip CHECKs and why `IS`, the zone (contract/time, D10) |
| | `identity.py` | the supertype and its composite FKs, ids by `RETURNING`, `source` on every row, no hard deletes, `updated_at` (contract/identity-and-provenance, contract/deletion-and-corrections, D8, D11) |
| | `named.py` | a person or a place is a page: one id, promotion, cookbook/person-or-place, cookbook/days-that-name and cookbook/everything-about run literally, two Sams (D16, D20) |
| | `pages.py` | every page titled, the day rule, filename-safe titles, `title_key` and its vectors, lookups, FTS, why not a collation (contract/titles-and-wikilinks, D5) |
| | `links.py` | the closed kind registry, endpoint types, mirrors, `at`, containment over day pages with its cycle guard (D8, D16) |
| | `habits.py` | habit periods: their days, order, no overlap, unitless only, never deleted; cookbook/habits and the cookbook/day-view habit leg — done, not done, not recorded (D24) |
| | `facts.py` | metrics and measurements: append-only, supersede, retract, finite values, never `OR IGNORE`/`OR REPLACE` (D7) |
| | `journal.py` | the day page and capture (cookbook/capture), the cookbook/day-view day view, the days that name someone (cookbook/days-that-name), where I was (cookbook/where-was-i), what stands in for recurrence, events and tasks (D5, D15, D16, D22, D23) |
| | `writers.py` | the `BEGIN IMMEDIATE` race with real threads, pragmas, read-only readers, a hardened connection (contract/connections) |
| | `integrity.py` | the four checks of contract/integrity-checks on the live file, against real damage |
| | `imports.py` | the import block of contract/imports on 1 000 CSV rows, and its traps |
| | `evolution.py` | named CHECKs, widening, partial dates, the tokenizer switch, comments inside statements (D13, D17, architecture/non-goals) |
| | `cookbook.py` | every cookbook block prepares and runs, on a plain and on a hardened connection |
| | `document.py` | the 2075 test, the rules live in the file, the tree holds together (one record per decision, every relative link and anchor resolves, every page reachable from `docs/README.md`), the totals in `schema/README.md` |
| | `diagrams.py` | the seven mermaid diagrams say what the DDL says (keys, relationships, the link map, the correction story) |
| | `mutants.py` | 70 broken copies of the docs tree, one rule each; the suite that owns the rule must notice |
| | `datasette_ro.py`, `render_diagrams.py` | optional: Datasette is read-only; every diagram renders |
| `wikilinks/` | `wikisave.py` | **reference implementation** of the save contract (a test instrument, not the application) |
| | `check_vectors.py`, `vectors.py` | the extraction vectors (some of them are printed in contract/titles-and-wikilinks) |
| | `title_fuzz.py` | the app-side title predicate equals the DDL's CHECK on generated strings |
| | `probes.py` | the save procedure against the real DDL, incl. 400 random edits and 4 concurrent writers |
| | `docchecks.py` | the save contract as the docs print it: the contract/titles-and-wikilinks table, cookbook/save-a-body run literally, cookbook/backlinks |
| `lib/` | `docsql.py`, `kit.py` | reading the tree (`$DOCS`): `schema.sql`, a page, the cookbook blocks by recipe key; fresh databases and the insert conventions (entity first, `RETURNING`, named entities) |

**When the docs change.** A DDL change: run everything. A new rule that spans tables: a `lifelog_meta` key and a row in
the 2075 table of contract/threat-model (`document.py` fails until both exist); a table's own rule: a comment in its `CREATE` statement. A
changed cookbook block or diagram: the suites that extract it will tell you. If a suite must change because the docs
legitimately changed, change it in the same edit and say so in the commit message — a suite loosened to pass proves nothing.
Add a mutant to `mutants.py` for a new rule, so the suite that owns it is shown to notice when it breaks.

**What is not here:** power loss, Windows itself and real data have never been tested.
