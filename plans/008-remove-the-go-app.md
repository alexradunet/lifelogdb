# Plan 008: Remove `app/` and every reference to it

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 3e2fcf4..HEAD -- app/ AGENTS.md .gitignore`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live files before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1 (the owner's decision; every other plan in this batch assumes it landed)
- **Effort**: S
- **Risk**: LOW — `SCHEMA.md` and `tests/` do not depend on `app/` (checked: no suite reads a file under `app/`)
- **Depends on**: none
- **Category**: tech-debt
- **Planned at**: commit `3e2fcf4`, 2026-10-02

## Why this matters

The owner has decided to remove the Go application (`app/`: a CLI, its import skill and its
docs). The repository's product is `SCHEMA.md`; `app/` was its first writer. After the removal,
nothing in the repo may still describe `app/` as present, tell a contributor to run `go test`,
or point at files that no longer exist — `AGENTS.md` is read by every agent that works here, so
a stale instruction in it misleads every future session. `SCHEMA.md` is already writer-neutral:
where it says "the app" it means *whatever writing application* a developer builds, and that
wording stays.

## Current state

- `app/` — the whole folder is tracked: `cmd/`, `internal/`, `skills/import/SKILL.md`,
  `README.md` (decisions A1–A9), `IMPORT.md`, `go.mod`, `go.sum`, `mise.toml`.
- `.gitignore:11` — `/app/lifelog` (the built binary). Every other line stays.
- `AGENTS.md` — the only other file that names `app/` (verified with
  `git grep -n "app/" -- . ':!app' ':!plans' ':!fixes.md' ':!.agents'`). The lines to change:
  - `AGENTS.md:17-19` — the paragraph starting "`` `app/` is the **first official application** ``".
  - `AGENTS.md:25-27` — three table rows: `` | `app/` | ``, `` | `app/skills/` | ``, `` | `app/IMPORT.md` | ``.
  - `AGENTS.md:31` — "It does not say only `app/` may ever exist."
  - `AGENTS.md:41` — setup-table row `` | the app (`app/`) | Go as pinned … ``.
  - `AGENTS.md:43-44` — bullet "Run `python3 tests/run_all.py` **before** `go test ./...` …".
  - `AGENTS.md:47-49` — bullet "What to run after a change: … `go generate ./internal/db && go test ./...` …".
  - `AGENTS.md:54` — "`` `tests/` or `app/`. Until the schema is frozen ``".
  - `AGENTS.md:69-70` — "A hypothetical writer is not an incident, and neither is a wish of `app/`."
  - `AGENTS.md:82` — "includes this file, `app/README.md` and code comments".
  - `AGENTS.md:83` and `:85-86` — "implementable without reading `app/`" and "Go code may be an example".
  - `AGENTS.md:105` — "into `SCHEMA.md`, `tests/` or `app/` unless the owner reopens it".
  - `AGENTS.md:147-170` — the whole section `## Working on \`app/\` (Go, the first official application)`,
    from its heading down to (not including) `## Building another application on the schema`.
  - `AGENTS.md:186` — "fix `SCHEMA.md` (and its suite), not just the app."
- Out of the grep on purpose: `plans/001-007` are DONE records of an earlier review (they mention
  `app/` as history); `fixes.md` is handled by plan 012; `.agents/` and `.claude/` hold a tooling skill.

Style of `AGENTS.md`: short declarative sentences, present tense, current truth only, no
"removed on …" narrative (git is the log).

## Commands you will need

| Purpose | Command (Git Bash, repo root) | Expected on success |
|---|---|---|
| Suites | `tests/.venv/Scripts/python.exe tests/run_all.py` (elsewhere `python3 tests/run_all.py`) | last line `20/20 suites passed` |
| References left | `git grep -n -E "app/|go test|go generate|go vet|gofmt|mise" -- AGENTS.md .gitignore SCHEMA.md tests` | no output |

The suites need the `sqlite3` CLI (SQLite ≥ 3.51.3 with FTS5) on `PATH`; on the owner's machine it is
`/c/Users/Alex/bin/sqlite3`.

## Scope

**In scope:** delete `app/` (all of it); edit `AGENTS.md`; edit `.gitignore`; `plans/README.md` status row.

**Out of scope (do NOT touch):**
- `SCHEMA.md` — its "the app" means any writer; it is writer-neutral by design.
- `tests/` — no suite depends on `app/`; labels like "stricter app" or "app-side title predicate"
  mean any writer's predicate.
- `plans/001`–`007`, `fixes.md`, `.agents/`, `.claude/`, `skills-lock.json`.

## Git workflow

- Branch: `advisor/008-remove-the-go-app`.
- One commit. Message style (match `git log`): a subject that states the result, a blank line,
  bullets per file, then `Ran: …` with what you ran and its result. Example subject:
  `remove app/: the repo is the schema and its suites`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Record the suites' baseline

Run the suites before changing anything.

**Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`.

### Step 2: Delete `app/`

`git rm -r -q app`

**Verify**: `git ls-files app | wc -l` → `0`; `ls app` → "No such file or directory" (if `ls` still lists
ignored build files, delete them: `rm -rf app`).

### Step 3: `.gitignore`

Delete the line `/app/lifelog`. Leave every other line (including `/import/` and the `*.db` rules) as it is.

**Verify**: `grep -c "app/" .gitignore` → `0`.

### Step 4: `AGENTS.md`

Make exactly these edits (find each by its quoted text; line numbers are from commit `3e2fcf4`):

1. Replace the paragraph at lines 17–19 (`` `app/` is the **first official application**: … It
   implements the schema; it never defines it. ``) with:

   ```
   There is no application in this repo. A writer is built against `SCHEMA.md` (see "Building another
   application on the schema" below): it implements the schema and never defines it.
   ```
2. Delete the three table rows for `app/`, `app/skills/` and `app/IMPORT.md` (lines 25–27). Keep the
   header and the `SCHEMA.md` and `tests/` rows.
3. Line 31: replace "It does not say only `app/` may ever" + line 32 "exist." with "It does not say only
   one application may ever exist." (keep the rest of the paragraph).
4. Setup table: delete the row `` | the app (`app/`) | … | `go vet ./... && go test ./...` in `app/` | ``.
5. Delete the bullet "Run `python3 tests/run_all.py` **before** `go test ./...` …" (two lines, 43–44).
6. Replace the bullet starting "What to run after a change:" (lines 47–49) with:

   ```
   - What to run after a change: §3 DDL, §2 or §6 → the suites; a diagram → the suites with `--mermaid`.
     Every suite must end green. Say in the commit message what you ran.
   ```
7. Line 53–54: "— not in `SCHEMA.md`,\n`tests/` or `app/`." → "— not in `SCHEMA.md` or `tests/`."
8. Lines 69–70: "and neither is a wish of `app/`." → "and neither is a wish of an application."
9. Line 82: "includes this file, `app/README.md` and code comments" → "includes this file, an
   application's docs and code comments".
10. Line 83: "implementable without reading `app/`." → "implementable without reading any application's
    code." Lines 85–86: "Go code may be an\n  example" → "an implementation may be an\n  example".
11. Line 105: "into `SCHEMA.md`, `tests/` or `app/` unless" → "into `SCHEMA.md` or `tests/` unless".
12. Delete the whole section from the heading `## Working on \`app/\` (Go, the first official
    application)` up to (not including) `## Building another application on the schema`, including the
    blank line that precedes the next heading only once (one blank line must remain between sections).
13. Line 186: "not just the app." → "not just the application."

Re-wrap a paragraph only where your edit made a line much longer than ~110 characters; do not
re-wrap untouched lines.

**Verify**:
- `git grep -n -E "app/|go test|go generate|go vet|gofmt|mise|Go binary|internal/" -- AGENTS.md` → no output.
- `grep -c "^## " AGENTS.md` → one less than before the edit (`git show HEAD:AGENTS.md | grep -c "^## "` minus 1).

### Step 5: Run the suites again

**Verify**: `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`, and
`git grep -n -E "app/|go test|go generate|go vet|gofmt|mise" -- AGENTS.md .gitignore SCHEMA.md tests` → no output.

## Test plan

No new tests: this removes code and documentation only. The 20 suites are the regression check that
`SCHEMA.md` and `tests/` stand alone.

## Done criteria

- [ ] `git ls-files app | wc -l` → `0`
- [ ] `git grep -n -E "app/|go test|go generate|go vet|gofmt|mise" -- AGENTS.md .gitignore SCHEMA.md tests` → no output
- [ ] `tests/.venv/Scripts/python.exe tests/run_all.py` → `20/20 suites passed`
- [ ] `git status --short` lists only `app/` deletions, `AGENTS.md`, `.gitignore` and `plans/README.md`
- [ ] `plans/README.md` status row for 008 updated

## STOP conditions

- A suite fails in Step 1 (before any change): the baseline is broken; report it.
- A suite fails after the removal: something in `tests/` read `app/` after all; report which suite and line.
- `git grep` finds `app/` in `SCHEMA.md` or `tests/`: those files are out of scope; report the lines.
- The `AGENTS.md` text at a listed location differs from the quote here.

## Maintenance notes

- The application decisions (A1–A9), the import skill and `IMPORT.md` live on in git history
  (`git show 3e2fcf4:app/README.md`). A future writer should read A8/A9 (what the 2026-10 trial import
  taught about LLM-driven imports) before designing its own import path.
- `SCHEMA.md`'s status line says the next step is the capture path and the first import into the
  canonical `life.db`. That stays true, but there is now no writer in the repo to take it; choosing
  or building one is the owner's next decision, before the freeze (D13).
- Reviewer: check that no sentence in `AGENTS.md` still assumes a Go toolchain or a CLI.
