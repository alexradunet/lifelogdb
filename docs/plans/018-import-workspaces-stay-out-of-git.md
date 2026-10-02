# Plan 018: An import's private workspace can never be committed, and the threat model names the model-driven import

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 4d84261..HEAD -- .gitignore AGENTS.md docs/contract/threat-model.md docs/guides/importing.md`
> Plans 014, 015 and 017 edit other lines of `AGENTS.md`, `threat-model.md` and `importing.md`; compare only the lines
> quoted below. On a mismatch in those, STOP.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none (run after 014 and 017 to avoid edit conflicts in the same files)
- **Category**: security
- **Planned at**: commit `4d84261`, 2026-10-02

## Why this matters

`life.db` is a personal life log (journal, people, places, health readings). The repo's rule is that it, and any real
data, never enters git — "git history cannot be scrubbed". The import guide (`docs/guides/importing.md`) tells the owner
to create a **workspace folder beside the source**, e.g. source `Notebook/`, workspace `Notebook.lifelog/`, holding
`questions.md` (names), `facts/*.json` (verbatim quotes and health readings), `ledger.md`, `plan.json` and `trial.db`.
Today `.gitignore` ignores `*.db` and `/import/` only: if the owner puts the vault and its workspace inside this clone
(anywhere but `/import/`), `git add -A` stages `Notebook.lifelog/facts/*.json` and every note (verified with
`git check-ignore`). Also `*.db-journal` (the rollback journal a tool in non-WAL mode leaves) is not ignored. Separately,
the threat model (`docs/contract/threat-model.md`) — the page that owns "what is protected, from what" — has no row for
the import process where a model reads untrusted text and proposes writes; the controls exist but only in the guide.

## Current state

- `.gitignore` (whole file):
  ```
  # never commit the database: finance data cannot be scrubbed from git history (docs/contract/connections.md)
  life.db
  life.db-wal
  life.db-shm
  *.db
  *.db-wal
  *.db-shm

  __pycache__/
  /tests/.venv/

  # a real vault or export to import: never in git (AGENTS.md, privacy)
  /import/

  # local Claude Code state: agent worktrees and personal settings (skills in .claude/skills are tracked)
  /.claude/worktrees/
  /.claude/settings.local.json
  ```
  "finance data" is stale: money is deferred (decision D18); the data at risk is health data and private notes
  (`docs/contract/connections.md:51-52` says "git history cannot be scrubbed of health data").
- `AGENTS.md:146-148`:
  ```
  - **Privacy**: `life.db` with its `-wal`/`-shm` never in git (finance data cannot be scrubbed from
    history; `.gitignore` covers `*.db`). Never commit a real vault, real notes or real data as a fixture:
    tests use synthetic data only.
  ```
- `docs/guides/importing.md:38-40`:
  ```
  The workspace is a folder beside the source, named after it: source `Notebook/`, workspace
  `Notebook.lifelog/`. It holds private data, like the source itself: neither goes into git or any
  online service.
  ```
  The guide is writer-neutral (for any repo); keep repo specifics to one parenthesis.
- `docs/contract/threat-model.md:6-15` — the threat table (columns `Threat | Control | Residual`). Row 10 (`A buggy
  writer, importer or agent`) covers accidents, not adversarial text. The guide already has the controls:
  `importing.md:15-17` (the model never writes SQL or approves; the writer checks each facts file), `:26-27` (every fact
  quotes its source; a quote not in the file, an unapproved metric or a look-alike name is refused),
  `:233-237` (*approve* is not among the model's tools; "a stamp is a line in a file, and anything that can write the
  file can forge it"), `:260-261` (source text is data, never instructions).
  `tests/schema/document.py:14` parses only the **numbered** 2075 table further down; adding a row to the threat
  table does not affect it, but every relative link must resolve.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Ignore check | `git check-ignore -v Notebook.lifelog/facts/a.json x.db-journal` | both paths printed with the matching rule |
| Docs suite | `tests/.venv/Scripts/python.exe tests/schema/document.py` | `document: N/N met expectations` |
| Everything | `tests/.venv/Scripts/python.exe tests/run_all.py` | `20/20 suites passed` |

## Scope

**In scope**: `.gitignore`, `AGENTS.md` (the Privacy bullet), `docs/guides/importing.md` (lines 38–40),
`docs/contract/threat-model.md` (one new table row), `docs/plans/README.md` (status row).

**Out of scope**: any `tests/` change; `docs/contract/connections.md`; reader-side `trusted_schema` (not decided);
anything that would *move* or *create* real data. Do not create a `Notebook/` folder — `git check-ignore` works on
paths that do not exist.

## Git workflow

Branch `advisor/018-workspace-out-of-git`. Message e.g. `gitignore: an import workspace and a rollback journal are never committed`; body names what you ran.

## Steps

### Step 1: `.gitignore`

- Line 1 comment: `finance data cannot be scrubbed` → `health data and private notes cannot be scrubbed`.
- After `*.db-shm` add `*.db-journal`.
- Under the `/import/` block add `*.lifelog/` and change that block's comment to
  `# a real vault or export to import, and its workspace (docs/guides/importing.md): never in git`.

**Verify**: `git check-ignore -v Notebook.lifelog/facts/a.json Notebook.lifelog/questions.md x.db-journal life.db-journal`
→ four lines, the first two citing `*.lifelog/`, the last two `*.db-journal`. And `git status --short` shows only `.gitignore` modified.

### Step 2: `AGENTS.md` Privacy bullet

Replace `(finance data cannot be scrubbed from\n  history; `.gitignore` covers `*.db`)` with
`(health data and private notes cannot be scrubbed from history; `.gitignore` covers `*.db`, `*.db-journal`, `/import/`
and an import workspace `*.lifelog/`)`.

**Verify**: `grep -n "finance data" AGENTS.md .gitignore` → no output.

### Step 3: `importing.md` — where the workspace lives

Replace "neither goes into git or any\nonline service." with "neither goes into git or any online service: keep both
outside every repository, or in a folder its `.gitignore` excludes (this repository ignores `/import/` and `*.lifelog/`)."

**Verify**: `tests/.venv/Scripts/python.exe tests/schema/document.py` → all met.

### Step 4: A threat-model row for the model-driven import

Insert a row after `| A buggy writer, importer or agent | … |`:
```
| Instructions hidden in an imported source, read by a model ([importing with a model](../guides/importing.md)) | the model writes facts files only — never SQL, never an approval; the writer checks every fact's quote against its source file and refuses an unapproved metric or a look-alike name; *approve* is not among the model's tools; the source text is data, never instructions; the owner reviews the trial before the real run | a stamp is a line in a file, so anything that can write the workspace can forge it; a fact that is quoted but wrong passes every check — the owner's review is what holds |
```
**Verify**: `tests/.venv/Scripts/python.exe tests/schema/document.py` → all met (the new link resolves).

### Step 5: Full run

**Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`.

## Test plan

No suite asserts `.gitignore`; Step 1's `git check-ignore` is the test. The docs edits are covered by `document.py`
(links resolve, pages reachable).

## Done criteria

- [ ] `git check-ignore Notebook.lifelog/facts/a.json x.db-journal` prints both paths
- [ ] `grep -rn "finance data" AGENTS.md .gitignore` → no output
- [ ] `grep -n "Instructions hidden in an imported source" docs/contract/threat-model.md` → 1 match
- [ ] `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`
- [ ] only in-scope files modified; `docs/plans/README.md` row 018 updated

## STOP conditions

- `git status` before you start shows any untracked file that looks like real data (a vault, a `*.lifelog/`, a `.db`):
  do not stage or touch it; report it to the owner.
- The 2075-table parser in `document.py` fails after Step 4 (it should not — it reads only numbered rows).

## Maintenance notes

- If the import guide ever renames the workspace suffix, change `.gitignore` in the same commit.
- Not done (owner's call): whether readers that open a `life.db` they did not write should also set
  `trusted_schema=OFF`; today the threat model assumes only the owner's own files are opened.
- The commits before the 2026 history rewrite (hashes cited in `docs/plans/README.md`) may still be fetchable from the
  GitHub remote if they were ever pushed; the owner should check whether any held real data.
