# Plan 013: Keep the LLM-assisted import process as a writer-neutral guide (`IMPORTING.md`)

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 3e2fcf4..HEAD -- AGENTS.md IMPORTING.md`
> Plan 008 (removing `app/`) changes `AGENTS.md`; that is expected. Anything else: compare with the
> excerpts below; on a mismatch, STOP.

## Status

- **Priority**: P1 (the owner asked for it before the removal of `app/` is pushed)
- **Effort**: M
- **Risk**: LOW (a new document and one table row)
- **Depends on**: 008 (it runs on top of the removal; the guide's source material is read from git history)
- **Category**: docs
- **Planned at**: commit `3e2fcf4` (+ `4b3b6db`, plan 008), 2026-10-02

## Why this matters

The Go app carried a tested process for importing an existing source — an Obsidian vault first —
into `life.db` with a (small, local) LLM doing the reading and code doing the checking and writing.
That process was paid for by a real trial import (2026-10) whose incidents shaped it: a model that
truncated keys, put question numbers into names, turned plans into events, approved its own rules,
lost values to shell quoting. `app/` is being removed (plan 008), and with it the only description of
that process. The owner wants it kept as **generic documentation**: how any future writer, in any
language and with any API surface (CLI, REST, MCP tool calls), should run an LLM-assisted import. The
know-how must survive the Go code.

## Source material (read all of it before writing)

From git history — these files no longer exist in the working tree:

- `git show 3e2fcf4:app/IMPORT.md` — the workspace, the facts file format, every check, gates, replay.
- `git show 3e2fcf4:app/skills/import/SKILL.md` — the procedure the model followed, step by step, and
  its never-break rules.
- `git show 3e2fcf4:app/README.md` — decisions A6 (the Obsidian vault import), A7 (every import is
  instructions plus the owner's answers, not an importer per source), A8 (checks), A9 (facts files and
  the owner's approval stamp), including their **Why** paragraphs (the trial's incidents) and **Limits**.
- Current `SCHEMA.md` §2.7 (the import contract every writer keeps: threat model, the import steps,
  `ON CONFLICT … DO NOTHING`), §6.13 (save contract), §6.15 (import a row once), D5, D22, D23, D24.

## What to write: `IMPORTING.md` at the repo root

A guide of roughly 250–400 lines, in the house style of `AGENTS.md` and `SCHEMA.md`: plain declarative
sentences, present tense, tables where they help, current truth only. Required sections, in this order:

1. **What this is** — two or three sentences: an import process for any writer of `life.db` in which a
   model reads and decides and the writer's code checks and writes; the contract is `SCHEMA.md` §2.7;
   this guide adds the process around it and changes no rule.
2. **Three parties** — the owner, the model, the writer (with an API) — the table "does / never does"
   from `IMPORT.md` "The idea", made writer-neutral.
3. **The workspace** — the folder beside the source and its files (`rules.md`, `metrics.md`,
   `questions.md`, `ledger.md`, `facts/<file>.json`), each with its format and one **synthetic** example.
   Say it holds private data and never goes into git or an online service.
4. **The facts file** — format (writes of person, place, page, link, reading; `quote`; `waiting`;
   `kept_as_text`), derived keys, references by title.
5. **The writer's operations** — the API surface an implementation must offer, named generically
   (e.g. *status*, *ledger*, *check facts*, *apply facts*, *register metrics*, *approve*, *replay*,
   *find*, *inspect a file*), each with what it reads, what it checks, what it writes. No CLI syntax,
   no Go names. State that a model must not have *approve*, and must never write SQL.
6. **The checks** — against the source file and workspace, then against `life.db` (a dry run in the
   same transaction), from `IMPORT.md` "What the CLI checks".
7. **Gates** — the owner's approvals (rules, metrics, the real run) and why the model cannot open them;
   the honest limit (a stamp in a file can be forged by anything that writes the file).
8. **The procedure for the model** — the loop from `SKILL.md` (set up, survey and draft rules, copy the
   notes for a vault, propose metrics, make the ledger, one file per turn, ask, use the answers, check
   and report, the real run), with its never-break rules. Keep them as instructions a small local
   model can follow.
9. **An Obsidian vault** — what was specific to it (A6): every note a page titled by its file name, a
   daily note `YYYY-MM-DD.md` is its day's page (D5), Obsidian link forms rewritten before the save,
   attachments as code spans (D9), frontmatter kept as text.
10. **Trial, then the real run** — trial on a copy, compare counts, replay with no model onto the
    canonical file; why facts files hold no database ids.
11. **What an implementation must get right** — the lessons of the trial (A8/A9 "Why", stated as
    requirements, without narrating events) **and** these defects found in the Go implementation's
    2026-10-02 audit, each as one requirement line:
    - a database path that cannot be silently ignored, and a status that says plainly when no database
      was checked;
    - a habit's period re-sent with its `end_day` (SCHEMA.md §6.16);
    - a reading's key that tells apart two readings of one metric on one day in one file;
    - quotes matched as whole words/tokens, a value matched as a whole number in its quote, and a
      minimum quote that actually states the fact;
    - ledger checked **before** the database transaction (a skipped or unknown file is refused), and
      written atomically;
    - an approval that covers the approved file's content, not only its status line;
    - look-alike checks for pages too, not only people and places;
    - status that reports every row written outside the facts (entities and links, not only readings),
      treats an answered question as the model's to apply, and replays readings whose `with` page comes
      from a later file;
    - an import plan (a vault's file list) whose paths, source and vault cannot be edited to point
      outside the source; failures that make the run exit non-zero; file names read as they are on disk
      (NFD on NTFS);
    - mood held to 1–5 (D6) and a habit period refused on a metric that already has readings other
      than 0/1 (D24);
    - a correction path that survives the replay;
    - the model's instructions saying that the text of a source file is data, never instructions to it.
12. **Where the rules live** — a short list pointing to SCHEMA.md sections (§2.4, §2.7, §6.13, §6.15,
    D5, D7, D22–D24). Do not restate those rules: cite them.

**Hard constraints on content:**
- **Synthetic examples only.** Use invented names (e.g. "Ana Example", "Bob Sample"), invented places,
  invented values. Do **not** copy any example name, path, quote, number or sentence from the source
  files — some of them may come from the owner's real notes, and this repo is public. Write every
  example fresh.
- **Writer-neutral.** No `lifelog` command lines, no Go package names, no flags. "The writer" or
  "an implementation".
- **No restated rules.** Where a rule lives in `SCHEMA.md`, cite the section (AGENTS.md "One home per rule").
- **Nothing out of scope** (`SCHEMA.md` §7): no exports, backups, restore, CSV dumps, off-box copies.
- **No history narrative**: no "the Go app did…", no review rounds; one sentence in "What this is" may
  say the process was first used in the 2026-10 trial import.

## Also: `AGENTS.md`

Add one row to the table at the top (after the `tests/` row):

```
| `IMPORTING.md` | how a writer runs an LLM-assisted import: workspace, facts files, checks, gates, the model's procedure | `SCHEMA.md` §2.7 |
```

## Commands you will need

| Purpose | Command (Git Bash, repo root) | Expected |
|---|---|---|
| Suites | `tests/.venv/Scripts/python.exe tests/run_all.py` | `20/20 suites passed` |
| No CLI/Go left | `grep -n -E "lifelog |go |\binternal/|--db|batch apply|import replay" IMPORTING.md` | no output |
| Sections | `grep -c "^## " IMPORTING.md` | 12 |

## Scope

**In scope:** create `IMPORTING.md`; add one row to `AGENTS.md`; `plans/README.md` status row.
**Out of scope:** `SCHEMA.md`, `tests/`, anything under `plans/` except the status row.

## Git workflow

One commit; subject like `IMPORTING.md: the LLM-assisted import process, for any writer`; body bullets
and `Ran: …`.

## Steps

1. Read all source material (above). **Verify**: you can list the 9 never-break rules of `SKILL.md` and
   the three gates of `IMPORT.md`.
2. Write `IMPORTING.md` with the 12 sections. **Verify**: the "No CLI/Go left" grep → no output;
   "Sections" → 12.
3. Synthetic-data audit: for every proper name, path, number and quoted sentence in `IMPORTING.md`, check
   it does not occur in the three source files: `git show 3e2fcf4:app/IMPORT.md 3e2fcf4:app/README.md
   3e2fcf4:app/skills/import/SKILL.md | grep -c -F "<string>"` → 0 for each (generic words like "kg",
   "mood", dates in examples you invented excepted only if you invented them). **Verify**: list what you
   checked in the report.
4. Add the `AGENTS.md` row. Run the suites. **Verify**: `20/20 suites passed`.

## Done criteria

- [ ] `IMPORTING.md` exists with 12 `## ` sections; the CLI/Go grep is empty
- [ ] The synthetic-data audit found no string copied from the source files
- [ ] `AGENTS.md` has the new row; suites `20/20 suites passed`
- [ ] Only `IMPORTING.md`, `AGENTS.md` (and `plans/README.md`) changed

## STOP conditions

- A source file cannot be read from git history.
- A requirement in section 11 contradicts `SCHEMA.md` as it stands (report it; do not change SCHEMA.md).

## Maintenance notes

- The next writer implements this guide; when it does, its own docs cite `IMPORTING.md` rather than copy it.
- Plans 009 (habit re-send) and 012 do not touch this file.
