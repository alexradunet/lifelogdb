# Implementation Plans

Plans are dated records ([how a change happens](../process.md)): each one cites the docs as they were at the commit it
names. Plans 001–013 were written when the whole design was one file, `SCHEMA.md`; its sections now live in
[docs/](../README.md) (§2 → `contract/`, §3 → `schema/schema.sql`, §5 → `decisions/`, §6 → `cookbook/`, §7 →
`architecture/non-goals.md`, §8 → `research/`). New plans go in this folder, numbered on from the last.

Three runs of the improve skill, all on 2026-10-02.

- **Plans 001–007** — against commit `6058f24`, a read-only audit of `SCHEMA.md`; every gap verified by
  execution on throwaway databases built from §3. All DONE.
- **Plans 008–012** — against commit `3e2fcf4`, a deep audit of the whole repo (`SCHEMA.md`, `tests/`
  and the Go app). The owner then decided to remove `app/` (plan 008); the plans that remain cover
  `SCHEMA.md` and `tests/` only. Findings that lived only in `app/` are listed at the bottom, so a
  future writer can avoid them.
- **Plans 014–022** — against commit `4d84261`, a deep audit of the `docs/` tree (after the split of `SCHEMA.md`)
  and `tests/`: six parallel audits (DDL, docs consistency, cookbook SQL, suites, the wikilink contract and the
  guides, security/DX/direction), every finding re-checked against the files and most by execution on throwaway
  databases. The owner chose all nine plans and decided four questions (below).
- **Plans 025–042** — against commit `cad659b`, a deep audit of the Go writer (at the repo root since `9ac130f`), the
  suites (ported to Go) and the docs: six parallel audits (core/db/text, the import flow, API/MCP security, tests and
  tooling, architecture and performance at lifetime scale, docs drift and direction). The top findings were re-read in
  the code, and the `/query` write path was reproduced on a throwaway database. The owner chose every finding
  group and three design plans (040–042).

**The commit hashes cited by plans 001–013 and by this index before plan 014 no longer resolve**: history was
rewritten before the docs split, so `6058f24`, `3e2fcf4`, `430ea6e`, `4b3b6db`, `6442e03`, `6ce208d`, `6f02944`,
`961b040` and `0364503` are pre-rewrite names. Their drift checks cannot run; the plans stand as records of what was
decided, and the "details in git history at `3e2fcf4`" for the `app/` findings below are no longer reachable locally.

**Context the executors must know:**
- There is no application in this repo. Plans 001–013 touched `SCHEMA.md`; plans 014 on touch `docs/` and `tests/`.
- Every plan ends with the full suite run:
  `tests/.venv/Scripts/python.exe tests/run_all.py` (Windows;
  `python3 tests/run_all.py` elsewhere) → `20/20 suites passed`.
  Suites change with the document, never to make it pass (AGENTS.md).
- `fixes.md` was carried out by plans 001–007 and deleted by plan 012.

Execute in the order below unless dependencies say otherwise. Plans 009, 010 and 011 each add mutants
to `tests/schema/mutants.py` and edit the count in `tests/README.md`: run them one after another and
always **read the current mutants count before editing it** (it is 66 at commit `3e2fcf4`, 70 at `4d84261`).
The same holds for plans 017, 020, 022 and 016, which all add mutants.

## Execution order & status

| Plan | Title | Priority | Effort | Depends on | Status |
|------|-------|----------|--------|------------|--------|
| 001 | State the freeze gate's current truth — the trial import happened | P1 | S | — | DONE |
| 002 | Enforce `habit_periods.source` like the other three provenance columns | P1 | S | — | DONE |
| 004 | §6.15 must send imported bodies through the wikilink save contract | P1 | S | — | DONE |
| 005 | Make SCHEMA.md fully writer-neutral | P1 | M | — | DONE |
| 003 | Scope the no-deletes rule to life data; state the registries' own rule | P2 | S | — | DONE |
| 006 | Reference hygiene and wording nits — R63/R73, D12, D13, R71, WAL backports | P2 | S | — | DONE |
| 007 | Document the idempotent re-run of a habit period (§6.16) | P3 | S | — | DONE |
| 008 | Remove `app/` and every reference to it | P1 | S | — | DONE (`4b3b6db`) |
| 009 | §6.16: a re-sent habit period carries its `end_day` | P1 | S | 008 (ordering only) | DONE (`430ea6e`) |
| 010 | Cookbook reads of where the owner was skip tombstones (§6.2, §6.9, §6.11) | P2 | S | after 009 (mutants count) | DONE (`6f02944`) |
| 011 | A redirect stub may point at a person or a place | P2 | M | **owner decision A/B**; after 010 (mutants count) | DONE, option A (`6442e03`) |
| 012 | The suites leave nothing in TEMP, pin their parser; `fixes.md` goes | P2 | S | — | DONE (`0364503`) |
| 013 | Keep the LLM-assisted import process as a writer-neutral guide (`IMPORTING.md`) | P1 | M | 008 | DONE (`961b040`, `6ce208d`) |
| 014 | [Make the text the freeze will keep forever say only true things](014-pre-freeze-text-hygiene.md) | P1 | S | — | DONE (`26c20b5`) |
| 015 | [The suite runner states what it needs, checks it first, and never hangs](015-runner-states-and-checks-its-needs.md) | P1 | S | 014 (ordering) | DONE (`fee9a39`) |
| 017 | [The cookbook recipes a writer copies return the right rows and write safely](017-cookbook-recipes-return-the-right-rows.md) | P1 | S | 015 (ordering) | DONE (`feca0eb`) |
| 022 | [A mention under an old name counts for its redirect target; a redirect target is never a ghost](022-an-old-name-still-counts.md) | P2 | S | 017 | DONE (`0edb0e9`) |
| 016 | [Every rule of the DDL has an expectation that fails when it is removed, and a mutant](016-every-rule-has-a-test-and-a-mutant.md) | P1 | M | 015, 017, 022 | DONE (`5d13a1e`) |
| 018 | [An import workspace can never be committed; the threat model names the model-driven import](018-import-workspaces-stay-out-of-git.md) | P1 | S | after 014, 017 (same files) | DONE (`8f47c0e`) |
| 019 | [The wikilink contract states every rule and every vector](019-wikilink-contract-stands-without-python.md) | P2 | M | 015 (ordering) | DONE (`6c8632a`) |
| 020 | [The import guide agrees with the imports contract; a captured day takes the note once](020-import-guide-agrees-with-the-contract.md) | P1 | M | 017, 018, 019 | DONE (`cd0f050`) |
| 021 | [Design: what the freeze is, what must hold before it, what changes after](021-freeze-runbook.md) | P2 | M | all others | IN REVIEW (owner) (`a55fcac`) |
| 023 | [The writer: `app/`, one Go binary serving a hypermedia API, a CLI and an MCP server](023-the-writer-app.md) | P1 | L | — | IN PROGRESS (v1 built; first real use next) |
| 024 | [Habits, renames and the import flow in `app/`](024-habits-renames-import.md) | P1 | L | 023 | IN PROGRESS (built; the first real import next) |
| 025 | [The docs and the plans index say what is true after the move to Go](025-docs-say-what-the-go-move-left.md) | P1 | S | — (run first) | TODO |
| 026 | [`POST /query` and the MCP `query` tool run one read and nothing else](026-query-is-really-read-only.md) | P1 | M | — | TODO |
| 027 | [`lifelog serve` answers only the owner's own browser and tools on this machine](027-serve-answers-only-its-own-pages.md) | P1 | S–M | — | TODO |
| 028 | [The MCP tools take what a model sends; every action has its own name](028-mcp-tools-take-what-models-send.md) | P1 | S | — | TODO |
| 029 | [The real run gets exactly what the trial has, or says loudly that it did not](029-the-real-run-is-the-trial.md) | P1 | M | before the first real import; before 030 | TODO |
| 030 | [A fact passes only on its own words; a reading's key comes from the source alone](030-a-fact-passes-only-on-its-own-words.md) | P1 | M | 029; before the first real import | TODO |
| 031 | [A vault's notes arrive whole and linked; a deleted page stays deleted](031-vault-notes-arrive-whole.md) | P1 | M | 029, 030 | TODO |
| 032 | [The owner's approval stamps what the owner saw; workspace files survive editors and errors](032-the-owners-gate-shows-what-it-stamps.md) | P2 | M | 029, 031 | TODO |
| 033 | [The core writes hold their own rules](033-core-writes-hold-their-own-rules.md) | P2 | M | 026, 029, 031 | TODO |
| 034 | [Every safety claim of the README has a test that fails when it breaks](034-the-safety-claims-have-tests.md) | P2 | M | 026–029 | TODO |
| 035 | [Every push runs the checks; staticcheck is clean; dead code is gone](035-every-push-runs-the-checks.md) | P2 | S | best after 026–034 | TODO |
| 036 | [Each action is declared once, in the catalog](036-each-action-is-declared-once.md) | P3 | M | 028, 034 | TODO |
| 037 | [Import status grows linearly; no answer floods a small model's context](037-status-and-pages-stay-small-at-lifetime-scale.md) | P2 | M–L | 026, 029–032 (036 if landed) | TODO |
| 038 | [The habit completion recipe reads only the days it counts (issue 0008)](038-habit-completion-reads-only-its-days.md) | P2 | S | — | TODO |
| 039 | [A body's text is decoded once, as CommonMark does (issue 0009)](039-text-is-decoded-once.md) | P2 | S | — | TODO |
| 040 | [Design + prototype: the import procedure comes from the guide, over MCP](040-the-import-procedure-comes-from-the-guide.md) | P2 | M | 028 | TODO |
| 041 | [Design: issue 0001 gets its proposal (renames)](041-renames-get-a-recipe.md) | P2 | S | 033 (cites it) | TODO |
| 042 | [Decision prep: five questions the freeze will make permanent](042-the-freeze-questions-get-answers.md) | P2 | S | owner decisions (Phase B) | TODO (Phase A ready for the owner) |

Status values: TODO | IN PROGRESS | IN REVIEW (owner) | DONE | BLOCKED (with one-line reason) |
REJECTED (with one-line rationale — finding fixed independently or approach
abandoned)

## Dependency notes

- 008 first: the other plans assume no `app/` (no Go gate to run).
- 009 → 010 → 011 in that order: each adds mutants and edits `tests/README.md`'s count; 009 and 011 do
  not share other files, 010 and 011 both edit `tests/schema/links.py`.
- 011 is held until the owner chooses option A (widen `redirect`) or B (keep it, forbid promoting a
  redirect target). The plan explains both.
- 012 is independent; it edits `tests/README.md` but not the mutant count.
- **014–022, in the table's order.** 014 first: it rewrites text in `schema.sql` and `AGENTS.md` that later plans
  anchor on. 015 before the suite plans (its preflight and `PYTHONUTF8` make failures readable). 017 → 022 → 016:
  all three add mutants and edit `tests/schema/named.py`; 016 also tests `ghost_pages` and the cookbook recipes that
  017 and 022 change, so it goes last of the three. 018 and 020 edit `docs/guides/importing.md` after 017 (the
  `person` row), 018 before 020. 019 is independent of 016–018 but must precede 020 (020's frontmatter sentence relies
  on 019's scan rule). 021 is last: it cites the state the others leave.
- Plans 014–022 each say "Planned at `4d84261`" in their drift check; an earlier plan of this run landing first is
  expected drift, and each plan names which.

- **025–042, in the table's order.** 025 first: it rewrites this index's context block (the Go gate replaces
  `run_all.py`). 026, 027 and 028 are independent of each other and of the import plans. **029 and 030 must land before
  the first real import**: reading keys and replay semantics become permanent then (each plan has a STOP if a real run
  already happened). 029 → 030 → 031 → 032 share `internal/importer/importer_test.go` and parts of `vault.go`/`status.go`:
  run them one after another. 033 follows 026/029/031 (same `internal/core` and `internal/db` files). 034 tests what
  026–029 make true. 036 and 037 rework files every earlier plan touched: run them late. 038 and 039 are independent; each
  adds an issue (0008, 0009 — 030 adds 0007): if a number is taken, take the next free one. 040 extends 028's MCP tests.
  041 needs only the owner's reading; 042 Phase B waits for the owner's table.

## Owner decisions (2026-10-02, for plans 020 and 022)

- A daily note imported for a day whose day page already exists is **appended** to it, once, recorded in `plan.json` (020).
- Prose comes in **only through a vault**; a journal export is converted to one `YYYY-MM-DD.md` per day first (020).
- The days that name someone and backlinks **follow one `redirect` hop** (022).
- A page a `redirect` points at is **not a ghost**: the view drops its redirect exclusion (022).

## The next writer

The owner chose it on 2026-10-02: `app/`, in Go (plan 023). The capture path exists; the first import into the
canonical `life.db` through it is the next step. The freeze runbook is plan 021.

## Findings considered and rejected

From the first run (001–007):

- **`metrics.name` is mutable while `metrics.unit` is fixed** — by design (D7).
- **`at` links from non-day pages are not rejected by the DDL** — a documented D16 cost; §7 lists the fix.
- **markdown-it-py loses a code span after an unclosed `[`** — documented in §2.4.
- **§2.2's "import_key is unique per source" vs the measurements index** — both correct in their homes.
- **No triggers guarding registry deletion** — resolved by wording (plan 003).
- **Performance / security at this scale** — not applicable (~5 GB/lifetime, D7).

From the second run (008–012), schema side:

- **`INSERT OR REPLACE` or deferred foreign keys can rewrite a used metric's unit or a link kind's
  structure** — the writer conventions already forbid `OR REPLACE` (§2.3, §2.7); no incident. Reopen if a
  writer ever does it.
- **An embedded NUL byte passes the GLOB-based CHECKs** (`metrics.name`, `source`) — no write path can
  produce one; reopen if one appears.
- **A link's `note`, `created_at` and `id` are updatable, and a mirrored edge's note can diverge** — no
  writer updates links; reopen with the first one that does.
- **`ghost_pages` lists an empty day page that only a measurement's `captured_with_id` references** —
  latent until a mood-only capture exists; handle it with that capture path.
- **§2.4's "any case" for `#REDIRECT` is ambiguous** (the Python reference matches Unicode case variants
  such as `#REDİRECT`; an ASCII-only writer does not) — exotic; settle it with the next writer's vectors.
- **The `Cn` title rule depends on the writer's Unicode version** (Python 3.12 has Unicode 15.0) — rare in
  practice; state a version when a second writer exists.

From the second run, findings that lived only in `app/` (dropped with it — a future writer should not
repeat them; details in git history at `3e2fcf4` and in this run's audit):

- import commands ignored a `--db` given after the subcommand, and `import status` reported green with no
  database; a habit with a later period broke `import metrics`/`replay`; two readings of one metric on one
  day in one facts file collided on their derived key; facts checks were plain substring tests (a
  one-letter quote, "1.5" inside "11.5"); `batch apply` committed before checking the ledger (a `[-]`
  file got imported) and wrote the ledger non-atomically; mood's 1–5 range was unchecked and `habit
  start` accepted a scale metric; `obsidian apply` trusted every field of `plan.json` and exited 0 on
  failed notes; `import status` missed removed person/place/page/link writes and treated answered
  questions as open; the approval stamp did not cover the file's content; corrections were not
  replayable; page writes skipped the look-alike check; the ledger parser dropped BOM'd or `*` lines;
  table links lost their `\|`; `capture --key` was ignored; the Go parity tests skipped on Windows
  because they called `python3`; 24 of 39 deliberate breakages of the A9 guards survived `go test`.

From the third run (014–022):

- **A change of `entities.entity_type` is not re-checked against existing links** (a place demoted to a page keeps
  its `at` links; a redirect stub promoted keeps its `redirect`) — a recorded deferral in
  `architecture/non-goals.md` ("Re-checking links when an entity changes type"); plan 017 adds the recipe-level guard
  for the stub, the DB trigger waits for a real case.
- **An `UPDATE` of an id (`entities.id`, `pages.id`) leaves `pages_fts` stale** — no write path changes an id, any
  link or reading on the id blocks it, and the FTS and orphan integrity checks report it.
- **`entities.created_at` is updatable** — same reasoning as the rejected "a link's `created_at` is updatable".
- **The symmetric mirror fails under an explicit `INSERT OR ABORT`/`OR FAIL`/`OR ROLLBACK` into `links`** (the outer
  conflict clause overrides the trigger's `OR IGNORE`; executed) — it fails closed, and no writer writes an explicit
  `OR ABORT`. Reopen with the first writer or ORM that does: the fix is `WHERE NOT EXISTS` in `links_mirror_insert`.
- **`pages_fts_delete` can never fire** — kept for now; plan 021 asks the owner (Q2).
- **The day view's "(edited)" flag appears on ~40% of freshly written non-day pages** (an INSERT then an UPDATE of the
  body in one transaction, `'now'` differs) — cosmetic; settle it with the UI.
- **A tombstoned day page still shows its mood reading** in the day view and mood-over-time — a reading is retracted,
  not tombstoned (D7); whether tombstoning a day should retract its readings is a UI/owner question, no incident yet.
- **A ghost whose only referrer was tombstoned is never listed** by `ghost_pages` — links of a tombstoned page stay by
  design (D11); revisit with the cleanup UI.
- **Habit edge cases** (a completion range with `from > to` returns one row; a same-day stop and restart is refused as
  an overlap; a check-in of 2 reads as "not recorded") — the app holds 0/1 (D24); minor.
- **`writers.py`'s timing margins** (50 ms) could flake on a loaded machine — did not flake in repeated and parallel runs.
- **Unpinned `datasette` and `mdurl`** — optional suite / transitive dependency; low value.
- **`.agents/skills` and `.claude/skills` are two copies of the improve skill** — identical today.
- **`tests/wikilinks/mktable.py`** is unlisted and its label list lacks one row — a helper nobody runs; delete it when next touched.
- **The `Cluj` fixture in `named.py`** — a real city, like the other generic places; not personal data.
- **No CI** — a GitHub workflow needs a SQLite ≥ 3.53 build with FTS5; worth doing once a second contributor exists.

From the fourth run (025–042):

- **"No CI"** (third run) — reversed: the suites need only Go now (pure-Go SQLite), so plan 035 adds a workflow.
- **A successful HTML form POST answers 200, not 303** (a reload could re-post a capture) — the browser asks before
  re-posting, and a redirect would drop the Result panel that shows what a save skipped.
- **Owner-only actions trust the `Lifelog-Source` header on the socket** — any local process that can reach the socket can
  also run the CLI as the owner; the gate exists for MCP models, whose source the server fixes. Cross-site browsers are
  closed by plan 027.
- **Raw error messages and the database path in API answers** — a local, single-user tool; the messages help the owner
  and the agent. The path stops mattering once `/query` cannot attach (plan 026).
- **The integrity check is a cheap-looking GET/MCP tool** (minutes on a 5 GB file) — it is the contract's
  `PRAGMA integrity_check`, and the guide has the model run it before an import.
- **`plan.json` is rewritten after every appended note** — a one-time cost of a vault import.
- **Source paths with Windows device names or trailing dots, symlinks inside the source** — the source tree is the
  owner's, and facts and the ledger require exact ledger names; reopen if a source ever comes from someone else.
- **Entity keys and note-path keys share one namespace with an unescaped `|`** — `|` cannot occur in a Windows file name;
  reopen for a source on another filesystem.
- **A link target `[[-0001-01-01]]` is skipped where the recipe's `date()` test would make a day page** — negligible.
- **Editing a redirect stub's body through `save-body`** — left for the rename decision (plan 041).
- **`writers` suite timing margins** — no flake in eight runs, `GOMAXPROCS=1` included (third run said the same).
- **Duplicate vector-table parsers in `tests/` and `internal/text`**, **links outside `docs/` unchecked** — small; fold
  them when one changes.
- **The mermaid render has not run since the port** (`LIFELOG_MERMAID=1`) — an owner action with node and a Chromium,
  not a plan.
- **Dependencies** — every direct module is at its latest; `govulncheck` clean; nothing to do.

## Open questions for the owner (plan 021)

- **Q1. A new file after the freeze.** (a) `schema.sql` stays the full current DDL, each migration also edits it, and a
  suite proves `schema.sql` equals the frozen DDL plus migrations (same `sqlite_master`) — *recommended*: a new file is
  still one command; or (b) `schema.sql` is frozen as `0001`, a new file is `0001` plus every migration.
- **Q2. `pages_fts_delete`** can never fire while `pages_no_delete` exists. Keep it (a guard for an owner who drops
  that trigger) or cut it before the freeze?
- **Q3.** The non-goal "Agent CLI/API" reads as cut while D14 and principle 3 make it part of the writer — delete or
  reword the row?
  **Answered (owner, 2026-10-02):** deleted — the writer of plan 023 is that CLI, API and agent surface.
- **Q4.** The export/snapshot/off-box non-goal row's reopen trigger is the freeze: decide it (checklist item 3 of
  [process](../process.md#before-the-freeze)).
- **Q5.** Should readers that open a `life.db` they did not write also set `trusted_schema=OFF`?
- **Q6. Pages that still equate the freeze with the first real data** (D13 now defines it as the first unreplayable
  write; a replayable import is real data but is rebuilt, not migrated). Out of scope for plan 021, owner to decide:
  `architecture/non-goals.md:21` ("Before the first real data enters a canonical `life.db` (the freeze, D13) at the
  latest" — under the new definition this trigger fires before the freeze, so Q4 and checklist item 3 of process.md
  need the wording aligned); `architecture/goals-and-principles.md:24` ("Once real data exists, schema changes are
  additive"); `schema/schema.sql:3` ("numbered migrations begin only after real data exists (D13)"); `schema/schema.sql:29`, the stored `lifelog_meta` 'evolution' row ("after the first real data: numbered forward-only SQL migrations, additive only ..."), which every file keeps forever and which now disagrees with D13; `contract/threat-model.md:44`, 2075 table row 16 ("How does the schema change after real data exists?" -> `evolution`); `README.md:3`, the status line ("No canonical database exists yet; until one does, schema.sql is edited in place (D13)" - under the new D13 a canonical file holding only replayable imports can exist while schema.sql is still edited in place).
  **Answered (owner, 2026-10-02):** every page now uses D13's meaning — "the freeze" is the first row that cannot be
  replayed from an import. Reworded: the non-goals row's trigger ("Before the freeze"), principle 4, the header and the
  stored `evolution` row of `schema.sql` (which defines the freeze inline, for the 2075 reader), 2075 question 16, and the
  status line of `docs/README.md`.
