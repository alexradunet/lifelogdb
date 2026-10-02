# Implementation Plans

Two runs of the improve skill, both on 2026-10-02.

- **Plans 001–007** — against commit `6058f24`, a read-only audit of `SCHEMA.md`; every gap verified by
  execution on throwaway databases built from §3. All DONE.
- **Plans 008–012** — against commit `3e2fcf4`, a deep audit of the whole repo (`SCHEMA.md`, `tests/`
  and the Go app). The owner then decided to remove `app/` (plan 008); the plans that remain cover
  `SCHEMA.md` and `tests/` only. Findings that lived only in `app/` are listed at the bottom, so a
  future writer can avoid them.

**Context the executors must know:**
- There is no application in this repo once plan 008 lands. Plans touch `SCHEMA.md` and `tests/`;
  no plan runs a Go command.
- Every plan ends with the full suite run:
  `tests/.venv/Scripts/python.exe tests/run_all.py` (Windows;
  `python3 tests/run_all.py` elsewhere) → `20/20 suites passed`.
  Suites change with the document, never to make it pass (AGENTS.md).
- `fixes.md` is superseded by plans 001–007; plan 012 deletes it.

Execute in the order below unless dependencies say otherwise. Plans 009, 010 and 011 each add mutants
to `tests/schema/mutants.py` and edit the count in `tests/README.md`: run them one after another and
always **read the current mutants count before editing it** (it is 66 at commit `3e2fcf4`).

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
| 008 | Remove `app/` and every reference to it | P1 | S | — | TODO |
| 009 | §6.16: a re-sent habit period carries its `end_day` | P1 | S | 008 (ordering only) | TODO |
| 010 | Cookbook reads of where the owner was skip tombstones (§6.2, §6.9, §6.11) | P2 | S | after 009 (mutants count) | TODO |
| 011 | A redirect stub may point at a person or a place | P2 | M | **owner decision A/B**; after 010 (mutants count) | TODO (awaiting owner: write "owner chose A" here to release it) |
| 012 | The suites leave nothing in TEMP, pin their parser; `fixes.md` goes | P2 | S | — | TODO |

Status values: TODO | IN PROGRESS | DONE | BLOCKED (with one-line reason) |
REJECTED (with one-line rationale — finding fixed independently or approach
abandoned)

## Dependency notes

- 008 first: the other plans assume no `app/` (no Go gate to run).
- 009 → 010 → 011 in that order: each adds mutants and edits `tests/README.md`'s count; 009 and 011 do
  not share other files, 010 and 011 both edit `tests/schema/links.py`.
- 011 is held until the owner chooses option A (widen `redirect`) or B (keep it, forbid promoting a
  redirect target). The plan explains both.
- 012 is independent; it edits `tests/README.md` but not the mutant count.

## Not planned: the freeze and the next writer

With `app/` gone there is no writer and no import path in the repo, while `SCHEMA.md`'s status line names
the capture path and the first import into the canonical `life.db` as the next step. Choosing or
building the next writer, and with it a freeze runbook (which write is the point of no return), is the
owner's decision; no plan is written until that choice exists.

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
