# tests/ — the validation suites of SCHEMA.md

`SCHEMA.md` marks a claim *executed* when a suite in this folder runs it. This folder exists so that the rule in `AGENTS.md`
— *after changing DDL, re-run the checks* — can actually be followed. **Nothing here is a migration and nothing touches
`life.db`**: every suite extracts §3 (and the §2 and §6 blocks it needs) from `SCHEMA.md`, builds throwaway databases and
discards them.

```
python3 tests/run_all.py              # every suite, ~15 s
python3 tests/run_all.py --datasette  # + Datasette opens the file read-only (installs it into tests/.venv)
python3 tests/run_all.py --mermaid    # + render every mermaid diagram (needs node + `mmdc` + a Chromium, see below)
```

Needs Python ≥ 3.12 (venv + network once, for `markdown-it-py`) whose `sqlite3` module, like the `sqlite3` CLI, runs SQLite
≥ 3.51.3 built with FTS5 (3.53 was used); a distribution's build may lack FTS5 (`no such module: fts5`). `--mermaid` also needs
`npm i -g @mermaid-js/mermaid-cli` and a Chromium; point `MMDC` / `PUPPETEER_EXECUTABLE_PATH` at them if they are not on `PATH`.

On Windows the venv is `Scripts\python.exe` (the runner looks there too), and the extracted DDL is read as UTF-8 by every
script that opens it — with the locale default (cp1252) the non-ASCII device names of `pages_title_safe` would be corrupted and
the suites would run a DDL that is not §3. The `sqlite3` CLI comes from sqlite.org's tools zip (put its folder on `PATH`); a
newer `sqlite3.dll` in the interpreter's `DLLs` folder lifts Python's own module.

The suites are grouped by **subject**, not by when a rule was added. Each one states its expectation in every label and ends
with `<name>: X/Y met expectations`; a suite that stops on an error reports that as a failed expectation.

| folder | suite | subject |
|---|---|---|
| `schema/` | `dates.py` | instants, local days, the round-trip CHECKs and why `IS`, the zone (§2.1, D10) |
| | `identity.py` | the supertype and its composite FKs, ids by `RETURNING`, `source` on every row, no hard deletes, `updated_at` (§2.2, §2.3, D8, D11) |
| | `named.py` | a person, place or holding is a page: one id, promotion, §6.19 and §6.6 run literally, two Sams (D20) |
| | `pages.py` | kinds and the day rule, filename-safe titles, `title_key` and its vectors, lookups, FTS, why not a collation (§2.4, D5) |
| | `links.py` | the closed kind registry, endpoint types, mirrors, containment, kinds of events and subtasks with their cycle guards (D8, D16, D22) |
| | `facts.py` | metrics and measurements: append-only, supersede, retract, finite values, never `OR IGNORE`/`OR REPLACE` (D7) |
| | `money.py` | currencies, holdings, balances, exactness, and §6.15/§6.16 against an exact-integer oracle (§2.7, D18) |
| | `positions.py` | the location history and a place's point: a fix's CHECKs, NaN and infinity, no fix at 0, 0, append-only, import idempotency, the §6.20 reads and their indexes, the nearest place against a haversine oracle, the antimeridian limit (D21) |
| | `journal.py` | events and tasks, the §6.2 day view, inbox and triage, what stands in for recurrence (D5, D15) |
| | `writers.py` | the `BEGIN IMMEDIATE` race with real threads, pragmas, read-only readers, a hardened connection (§2.6) |
| | `integrity.py` | the four checks of §2.5 on the live file, against real damage |
| | `imports.py` | the import block of §2.8 on 1 000 CSV rows, and its traps |
| | `evolution.py` | named CHECKs, widening, partial dates, the tokenizer switch, comments inside statements (D13, D17, §7) |
| | `cookbook.py` | every §6 block prepares and runs, on a plain and on a hardened connection |
| | `document.py` | the 2075 test, the rules live in the file, current truth only, out-of-scope stays out, the §3 totals |
| | `diagrams.py` | the nine mermaid diagrams say what the DDL says (keys, relationships, the link map, the correction story) |
| | `mutants.py` | 66 broken copies of `SCHEMA.md`, one rule each; the suite that owns the rule must notice |
| | `datasette_ro.py`, `render_diagrams.py` | optional: Datasette is read-only; every diagram renders |
| `wikilinks/` | `wikisave.py` | **reference implementation** of the save contract (a test instrument, not the application) |
| | `check_vectors.py`, `vectors.py` | the extraction vectors (some of them are printed in §2.4) |
| | `title_fuzz.py` | the app-side title predicate equals the DDL's CHECK on generated strings |
| | `probes.py` | the save procedure against the real DDL, incl. 400 random edits and 4 concurrent writers |
| | `docchecks.py` | the save contract as the document prints it: the §2.4 table, §6.13 run literally, §6.5 |
| `lib/` | `docsql.py`, `kit.py` | extraction of §3/§6/sections; fresh databases and the insert conventions (entity first, `RETURNING`, named entities) |

**When the document changes.** A DDL change: run everything. A new rule that spans tables: a `lifelog_meta` key and a row in
the 2075 table of §2.8 (`document.py` fails until both exist); a table's own rule: a comment in its `CREATE` statement. A
changed cookbook block or diagram: the suites that extract it will tell you. If a suite must change because the document
legitimately changed, change it in the same edit and say so in the commit message — a suite loosened to pass proves nothing.
Add a mutant to `mutants.py` for a new rule, so the suite that owns it is shown to notice when it breaks.

**What is not here:** power loss, Windows itself and real data have never been tested.
