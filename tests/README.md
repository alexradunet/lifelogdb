# tests/ — the validation suites of SCHEMA.md

`SCHEMA.md` §8 records what was tested in each review round; this folder is what ran. It exists so that
the rule in `AGENTS.md` — *after changing DDL, re-run the checks* — can actually be followed. **Nothing
here is a migration and nothing touches `life.db`**: every suite extracts §3 (and §2.5, §2.8, §2.11, §6) from
`SCHEMA.md`, builds throwaway databases and discards them.

```
python3 tests/run_all.py              # every suite, ~15 s
python3 tests/run_all.py --datasette  # + Datasette opens the file read-only (installs it into tests/.venv)
python3 tests/run_all.py --mermaid    # + render every mermaid diagram (needs node + `mmdc` + a Chromium, see below)
```

Needs `python3` (venv + network once, for `markdown-it-py`) and the `sqlite3` CLI (3.53 was used). `--mermaid` also needs
`npm i -g @mermaid-js/mermaid-cli` (12.0.0 was used) and a Chromium; point `MMDC` / `PUPPETEER_EXECUTABLE_PATH` at them if they are not on `PATH`.

| folder | suite | proves | record |
|---|---|---|---|
| `schema/` | `regress.py` | CHECKs, triggers, FKs, STRICT, FTS behave as the contract says | #1–#6 |
| | `finprobes.py` | money: currencies, accounts, balances, fx_rates, exact integers | #6 |
| | `r5probes.py` | imports (`ON CONFLICT`), retractions, `recorded_at`, no-delete triggers, named CHECKs | #7 |
| | `r6probes.py` | `tz`, `completed_day`, one place per event | #8 |
| | `r7probes.py` | `BEGIN IMMEDIATE` race with real threads, pragmas, read-only readers | #9 |
| | `r10probes.py` | device-name titles, the **2075 test**, the deferred features' additive paths, the import path | #12 |
| | `r11probes.py`, `r11_mutants.py` | notes and wiki merged into one `page` kind (D5 addendum 5): the day rule, one title namespace, lookups without a `kind` predicate, ghosts, the day view, fixed kind; then twelve broken copies of the document, each of which must fail a probe | #13 |
| | `r12probes.py`, `r12_mutants.py` | round 12 narrowed the document to the schema and its reliability: the three integrity checks of §2.8 run literally on the **live** file (zeroed page, truncation, flipped index entry, flipped value, orphan balance, orphan `entities` row), 23 `lifelog_meta` keys and 20 2075 questions, no export/dump/snapshot text left in §1–§7 or in `tests/`; then nine broken copies of the document, each of which must fail a probe | #14 |
| | `diagrams.py`, `diagrams_mutants.py`, `render_diagrams.py` | the nine mermaid diagrams of §2.4, §2.9, §2.10, §4, §6.14 say what §3 says: tables, columns, types, PK/FK marks, foreign keys and their cardinality, the link map vs `link_kinds`, the correction story executed; then twelve broken copies of the document (the DDL changed under a diagram, or a diagram edited), each of which must fail a check; `render_diagrams.py` (optional) renders every block | #15 |
| | `fuzz.py`, `nw.py`, `cookbook_doc.py` | recurrence expander and net worth vs independent oracles; every §6 block runs | #4–#9 |
| | `r7probes_ds.py` | Datasette is read-only (optional) | #9 |
| `wikilinks/` | `wikisave.py` | **reference implementation** of the §2.5 save contract (a test instrument, not the application) | #10 |
| | `check_vectors.py`, `vectors.py` | the extraction vectors (29 of them are printed in §2.5) | #10 |
| | `title_fuzz.py` | the app-side title predicate equals the DDL's CHECK on ~43 000 strings | #10, #12 |
| | `probes.py` | the save procedure against the real DDL, incl. 400 random edits and 4 concurrent writers | #10 |
| | `docchecks.py` | the **document text**: the vector table, §6.14 executed literally, §6.5, stale phrases | #10, #12 |
| `lib/` | `docsql.py` | extracts §3, §6 and any section from `SCHEMA.md` | — |
| `experiments/` | `r10_diff.py` | the old-vs-new CHECK comparison of #12. **Not portable** (paths as they were run), not part of `run_all` — kept as evidence | #12 |

**When the document changes.** A DDL change: run everything, append a validation record, never edit an old one (AGENTS.md). A new
contract rule: add a row to the §2.11 table and a key to `lifelog_meta` (the 2075 test will fail until both exist). A changed
cookbook block: the suites that extract it will tell you. If a suite must change because the document legitimately
changed, change the suite in the same edit and say so in the record — a suite that is quietly loosened to pass proves nothing.

**What is not here:** records #1–#3 were exploratory scripts that were never saved; `probes1.py` keeps the helper prefix and the
round-4 probes. Power loss, Windows itself and real data have never been tested (§8 #11, #12). Round 12 removed the backup harness
(`tests/backups/`) and the one-off experiments behind record #11 with the contract they tested; they are in git history (record #14).
