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
| 022 | [A mention under an old name counts for its redirect target; a redirect target is never a ghost](022-an-old-name-still-counts.md) | P2 | S | 017 | TODO |
| 016 | [Every rule of the DDL has an expectation that fails when it is removed, and a mutant](016-every-rule-has-a-test-and-a-mutant.md) | P1 | M | 015, 017, 022 | TODO |
| 018 | [An import workspace can never be committed; the threat model names the model-driven import](018-import-workspaces-stay-out-of-git.md) | P1 | S | after 014, 017 (same files) | TODO |
| 019 | [The wikilink contract states every rule and every vector](019-wikilink-contract-stands-without-python.md) | P2 | M | 015 (ordering) | TODO |
| 020 | [The import guide agrees with the imports contract; a captured day takes the note once](020-import-guide-agrees-with-the-contract.md) | P1 | M | 017, 018, 019 | TODO |
| 021 | [Design: what the freeze is, what must hold before it, what changes after](021-freeze-runbook.md) | P2 | M | all others | TODO (ends IN REVIEW (owner)) |

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

## Owner decisions (2026-10-02, for plans 020 and 022)

- A daily note imported for a day whose day page already exists is **appended** to it, once, recorded in `plan.json` (020).
- Prose comes in **only through a vault**; a journal export is converted to one `YYYY-MM-DD.md` per day first (020).
- The days that name someone and backlinks **follow one `redirect` hop** (022).
- A page a `redirect` points at is **not a ghost**: the view drops its redirect exclusion (022).

## Not planned: the next writer

There is no writer and no import path in the repo, while the status line of `docs/README.md` names the capture path
and the first import into the canonical `life.db` as the next step. Choosing or building the next writer is the
owner's decision. The freeze runbook is now plan 021.

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
