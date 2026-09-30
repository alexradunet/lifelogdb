# tests/ — the validation suites of SCHEMA.md

`SCHEMA.md` marks a claim *executed* when a suite in this folder runs it. This folder exists so that
the rule in `AGENTS.md` — *after changing DDL, re-run the checks* — can actually be followed. **Nothing
here is a migration and nothing touches `life.db`**: every suite extracts §3 (and §2.5, §2.8, §2.11, §6) from
`SCHEMA.md`, builds throwaway databases and discards them.

```
python3 tests/run_all.py              # every suite, ~30 s
python3 tests/run_all.py --datasette  # + Datasette opens the file read-only (installs it into tests/.venv)
python3 tests/run_all.py --mermaid    # + render every mermaid diagram (needs node + `mmdc` + a Chromium, see below)
```

Needs `python3` (venv + network once, for `markdown-it-py`) and the `sqlite3` CLI (3.53 was used). `--mermaid` also needs
`npm i -g @mermaid-js/mermaid-cli` (12.0.0 was used) and a Chromium; point `MMDC` / `PUPPETEER_EXECUTABLE_PATH` at them if they are not on `PATH`.

| folder | suite | proves |
|---|---|---|
| `schema/` | `regress.py` | CHECKs, triggers, FKs, STRICT, FTS behave as the contract says |
| | `finprobes.py` | money: currencies, accounts, balances, fx_rates, exact integers |
| | `r5probes.py` | imports (`ON CONFLICT`), retractions, `recorded_at`, no-delete triggers, named CHECKs |
| | `r6probes.py` | `tz`, `completed_day`, one place per event |
| | `r7probes.py` | `BEGIN IMMEDIATE` race with real threads, pragmas, read-only readers |
| | `r10probes.py` | device-name titles, the **2075 test**, the deferred features' additive paths, the import path |
| | `r11probes.py`, `r11_mutants.py` | one `page` kind (D5): the day rule, one title namespace, lookups without a `kind` predicate, ghosts, the day view, fixed kind; then twelve broken copies of the document, each of which must fail a probe |
| | `r12probes.py`, `r12_mutants.py` | the first three integrity checks of §2.8 run literally on the **live** file (zeroed page, truncation, flipped index entry, flipped value, orphan balance, orphan `entities` row), 25 `lifelog_meta` keys and 22 2075 questions, no export/dump/snapshot text left in §1–§7 or in `tests/`; then nine broken copies of the document, each of which must fail a probe |
| | `r15probes.py`, `r15_mutants.py` | §6.1/§6.9/§6.15 run literally carry ids with `RETURNING` (the mood lands on its memo even with the link sync inside the transaction); the fourth §2.8 check (FTS5 `integrity-check`) catches index drift that `integrity_check` misses; invisible and bidi title characters rejected by DDL and app, unassigned ones by the app; `lifelog_meta.sqlite`, `trusted_schema=OFF` + DEFENSIVE (every §6 block re-run hardened); `pages_fts_au` only on title/body; every CHECK named and droppable; rules inside the `CREATE` statements; no task recurrence; `source` provenance; finite measurement values (NaN on a correction is a retraction); then fifteen broken copies, each of which must fail a probe (needs the tests venv) |
| | `diagrams.py`, `diagrams_mutants.py`, `render_diagrams.py` | the nine mermaid diagrams of §2.4, §2.9, §2.10, §4, §6.14 say what §3 says: tables, columns, types, PK/FK marks, foreign keys and their cardinality, the link map vs `link_kinds`, the correction story executed; then twelve broken copies of the document (the DDL changed under a diagram, or a diagram edited), each of which must fail a check; `render_diagrams.py` (optional) renders every block |
| | `nohistory.py` | SCHEMA.md states the current truth only: no review rounds, validation records, addenda, superseded notes, finding ids, version narrative or changelog; sections 1–8 and D1–D19 in order; then seven broken copies, each of which must be noticed |
| | `fuzz.py`, `nw.py`, `cookbook_doc.py` | recurrence expander and net worth vs independent oracles; every §6 block runs (`HARDENED=1`: on a DEFENSIVE, `trusted_schema=OFF` connection) |
| | `r7probes_ds.py` | Datasette is read-only (optional) |
| `wikilinks/` | `wikisave.py` | **reference implementation** of the §2.5 save contract (a test instrument, not the application) |
| | `check_vectors.py`, `vectors.py` | the extraction vectors (29 of them are printed in §2.5) |
| | `title_fuzz.py` | the app-side title predicate equals the DDL's CHECK on ~43 000 strings |
| | `probes.py` | the save procedure against the real DDL, incl. 400 random edits and 4 concurrent writers |
| | `docchecks.py` | the **document text**: the vector table, §6.14 executed literally, §6.5, stale phrases |
| `lib/` | `docsql.py` | extracts §3, §6 and any section from `SCHEMA.md` |
| `experiments/` | `r10_diff.py` | the old-vs-new CHECK comparison of #12. **Not portable** (paths as they were run), not part of `run_all` — kept as evidence |

**When the document changes.** A DDL change: run everything. A new contract rule: add a row to the §2.11 table and a key to
`lifelog_meta` (the 2075 test will fail until both exist). A changed cookbook block or diagram: the suites that extract it will tell
you. If a suite must change because the document legitimately changed, change the suite in the same edit and say so in the commit
message — a suite that is quietly loosened to pass proves nothing. The document itself carries no record of what was tested: this
folder is the record, and git is the log.

**What is not here:** power loss, Windows itself and real data have never been tested. `probes1.py` keeps the helper prefix and the
early probes that the later suites build on. `experiments/r10_diff.py` is a one-off (the old-vs-new title CHECK comparison), not portable.
