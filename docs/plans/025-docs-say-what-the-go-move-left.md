# Plan 025: The docs and the plans index say what is true after the move to Go

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat cad659b..HEAD -- docs/plans/README.md docs/README.md docs/contract/connections.md docs/contract/imports.md AGENTS.md tests/README.md README.md tools/copyschema/main.go`
> `docs/plans/README.md` changed after `cad659b` by design (the advisor added the rows of plans 025–042 and their
> notes). For every file, compare the quoted lines below with the live files; on a mismatch in a quoted line, STOP.

## Status

- **Priority**: P1 (every later plan's executor reads the index this plan fixes)
- **Effort**: S
- **Risk**: LOW (prose only; one suite expectation reads the docs status line)
- **Depends on**: none. Run it **first** of plans 025–042.
- **Category**: docs
- **Planned at**: commit `cad659b`, 2026-10-02

## Why this matters

Commit `9ac130f` moved the Go module from `app/` to the repo root and ported the validation suites from Python to Go
tests. Several current-truth texts were not updated. The plans index — the first thing every executor reads —
still says "There is no application in this repo" and gives a Python command (`run_all.py`, `20/20 suites`) as the
gate every plan ends with; that command can no longer run. The docs status line still calls the capture path "the
next step" although it exists. Two contract pages still claim *executed* for things the port stopped executing
(the `sqlite3 -readonly` and Datasette read-only behaviour, the CSV `.import` step), which breaks the repo's
empiricism rule (AGENTS.md: "Any claim in the docs about what SQLite *does* must be executed by a suite").

## Current state

Repo rules that bind this plan (from `AGENTS.md`):
- "**Current truth and nothing else** in every folder of `docs/` except `issues/`, `rfcs/` and `plans/`, which are
  dated records". So: closed plans 001–024 and issues are **not** rewritten. Only the **index**
  `docs/plans/README.md` (its context and next-writer paragraphs) is current guidance and is rewritten.
- "`docs/` never names [the app]": the docs stay writer-neutral. Do not add `lifelog`, `internal/`, `cmd/`, MCP or
  Siren to any page under `docs/` other than `docs/plans/`.
- "Any claim in the docs about what SQLite *does* must be executed by a suite in `tests/` … mark it *executed* (the
  word means that a suite runs it)."

Files and the exact lines to change:

1. `docs/plans/README.md` — the block starting at the line `**Context the executors must know:**` (line ~31 once
   this run's index entries were added):
   ```
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
   ```
   Line 8: `Three runs of the improve skill, all on 2026-10-02.` Section "## The next writer" (line ~128):
   ```
   The owner chose it on 2026-10-02: `app/`, in Go (plan 023). The capture path exists; the first import into the
   canonical `life.db` through it is the next step. The freeze runbook is plan 021.
   ```
   Table rows 023 and 024 have titles containing `app/`.
2. `docs/README.md:3` (one long line) contains: `the next step is the capture path and the first import into the
   canonical \`life.db\`, not another review.` and lines 8–9: `everything else (view generators, AI
   features, sync or merge between copies of the database) is explicitly out of scope.` — which contradicts
   `docs/guides/importing.md` (an import driven by a model) and `docs/architecture/non-goals.md:19`, whose row is
   `| Generic view system, AI generation | Cut from product scope by the owner | …`.
3. `docs/contract/connections.md:37-39`:
   ```
   Readers need no setup but must be **read-only**: open the file with `?mode=ro` (SQLite then
   refuses every write — `attempt to write a readonly database`, executed) or `sqlite3 -readonly`.
   Datasette does this by itself. Under WAL a reader sees the
   ```
   The port dropped the suite that executed `sqlite3 -readonly` and the Datasette check (commit `9ac130f` message:
   "dropped: the Datasette suite and the sqlite3 -readonly CLI check").
4. `docs/contract/imports.md:3-4`: `Every step was executed on 1 000 synthetic rows:` — but step 2's
   `sqlite3 scratch.db ".import --csv weights.csv staging"` is no longer run: `tests/imports_test.go:37-48` builds the
   `staging` table by hand. Step 1's command `sqlite3 -readonly life.db "VACUUM INTO '/tmp/trial.db'"` is followed
   by `(*executed*)`, but the suite runs `VACUUM INTO` on a Go `mode=ro` connection and only checks that the command
   *string* is present (`tests/imports_test.go:130`, and mutant `tests/mutants_test.go:147` edits that string —
   **keep the string `sqlite3 -readonly life.db "VACUUM INTO` in the page**, or that expectation and mutant break).
5. `AGENTS.md:38` says the full run takes "about 25 s"; `tests/README.md:10-11` says `~20 s` and `~5 s`. Measured
   at `9ac130f`: `go test ./...` 23–30 s; `go test -short ./...` 6–7 s.
6. `README.md:21`: `go generate ./... && go build -o lifelog ./cmd/lifelog   # Go >= 1.27, no cgo` and the next
   lines call `lifelog …` as if it were on `PATH` (the built file is `./lifelog`, on Windows without `.exe`).
7. `tools/copyschema/main.go:1-2`: `// copyschema copies docs/schema/schema.sql into internal/db so the binary can
   embed it (go:embed cannot reach outside the module).` Since `9ac130f` the module is the repo root; the real
   limit is that a `//go:embed` pattern may not contain `..` (it cannot reach a parent directory of the package).

The `document` suite requires `docs/README.md` to keep a one-line status (`tests/document_test.go:303-304`,
regexp `indexStatus`). Keep the line starting `**Status:**`.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Everything | `go generate ./... && go vet ./... && go test ./...` | exit 0 |
| Docs suites only | `go test ./tests -run 'TestSuites/(document|imports|writers)' -v` | each prints `X/X met expectations` |

## Scope

**In scope** (the only files you may modify):
- `docs/plans/README.md` (the context block, line 8, the "The next writer" section, the titles of rows 023/024,
  — the rows and notes of plans 025–042 are already there; leave them)
- `docs/README.md` (the status line and the scope sentence only)
- `docs/contract/connections.md` (lines 37–39 only)
- `docs/contract/imports.md` (line 4 and the `(*executed*)` marker of step 1 only)
- `AGENTS.md` (the timing in the checks table only)
- `tests/README.md` (the two timings only)
- `README.md` (the build line only)
- `tools/copyschema/main.go` (the comment only)

**Out of scope** (do NOT touch):
- `docs/plans/0*.md` (the plan records, 001–024) and `docs/issues/*.md` — dated records; they stay as written.
- Any page under `docs/` other than the three listed — and never add the app's name to them.
- `tests/*.go` — no suite changes in this plan (re-adding an opt-in `sqlite3` CLI probe is a separate decision).

## Git workflow

- Branch: `advisor/025-docs-after-the-go-move`
- One commit. Message style of the repo (lowercase subject, `area: what is now true`, a body, then `Ran: …`), e.g.:
  `docs: the plans index, the status line and two contract pages say what is true after the move to Go`
  with a body listing the files, then `Ran: go generate ./... && go vet ./... && go test ./...`, then the
  `Co-Authored-By` trailer the session asks for.
- Do not push.

## Steps

### Step 1: Rewrite the plans index context block

In `docs/plans/README.md`, replace the bullet list under `**Context the executors must know:**` (the four bullets
quoted above) and the paragraph after it with:

```
**Context the executors must know:**
- The repo holds the writer: one Go module at the root (`cmd/`, `internal/`, `tools/`, `go.mod`; moved from `app/`
  in `9ac130f`). Plans 001–013 touched `SCHEMA.md`; plans 014–022 touch `docs/` and `tests/`; plans 023 on also
  touch the Go code.
- Every plan ends with the full run: `go generate ./... && go vet ./... && go test ./...` → exit 0 (all suites and
  every mutant). Plans written before `9ac130f` name the Python runner (`tests/run_all.py`, `20/20 suites`); it no
  longer exists, and this command replaces it.
- Suites change with the document, never to make it pass (AGENTS.md).
- Mutants live in `tests/mutants_test.go`; their count is in `tests/README.md`. A plan that adds mutants reads the
  current count before editing it.
```

followed by the single line `Execute in the order below unless dependencies say otherwise.` (this replaces the old
paragraph about `tests/schema/mutants.py`; its history stays in the dependency notes of 009–022). Replace line 8 (`Three runs of the improve skill, all on
2026-10-02.`) with `Runs of the improve skill and the owner's own plans (023, 024), all on 2026-10-02.`

**Verify**: `grep -n "There is no application\|run_all.py\|20/20" docs/plans/README.md` → the only hits are inside
the sentence that says the Python runner no longer exists (and in the rows/records of plans 001–022 if any).

### Step 2: The next-writer paragraph and rows 023/024

Replace the body of `## The next writer` with:

```
The owner chose it on 2026-10-02: a Go writer (plan 023), at the repo root since `9ac130f`. Its capture path and
its import flow exist; the first import into the canonical `life.db` is the next step. The freeze runbook is plan 021.
```

In the table, change the titles of rows 023 and 024 so they no longer say `app/` (e.g. `The writer: one Go binary
serving a hypermedia API, a CLI and an MCP server` and `Habits, renames and the import flow in the writer`). Leave
their links, priority, effort and status cells unchanged.

**Verify**: `grep -n "app/" docs/plans/README.md` → hits only in the dated text about runs 008–013 (the "remove
`app/`" history) and none in rows 023/024 or "The next writer".

### Step 3: The docs status line and scope sentence

In `docs/README.md` line 3, replace `the next step is the capture path and the first import into the canonical
\`life.db\`, not another review.` with `a capture path exists; the next step is the first import into the
canonical \`life.db\`, not another review.` Do not name any application.

In lines 8–9 replace `everything else (view generators, AI features, sync or merge between copies of the database)
is explicitly out of scope.` with `everything else (generic view generators, AI generation of views or content, sync
or merge between copies of the database) is explicitly out of scope ([non-goals](architecture/non-goals.md)).`

**Verify**: `go test ./tests -run 'TestSuites/document' -v` → `document: N/N met expectations` (no failure).

### Step 4: Contract claims that lost their execution

`docs/contract/connections.md` lines 37–39: keep the `mode=ro` sentence and its `executed`; make the CLI and
Datasette clause guidance, not an executed claim:

```
Readers need no setup but must be **read-only**: open the file with `?mode=ro` (SQLite then
refuses every write — `attempt to write a readonly database`, executed). The `sqlite3` shell's `-readonly` flag
and Datasette open the file the same way, by their own documentation (not executed here). Under WAL a reader sees the
```

`docs/contract/imports.md` line 4: replace `Every step was executed on 1 000 synthetic rows:` with
`Every step was executed on 1 000 synthetic rows, through a SQLite connection; the two \`sqlite3\` shell commands
are the shell's documented way to do the same (not executed here):`. In step 1, change the `(*executed*)` after the
command to `(the read-only copy is *executed*)`. Keep the command string itself byte-for-byte.

**Verify**: `go test ./tests -run 'TestSuites/(imports|writers|document)' -v` → each `N/N`; and
`go test ./tests -run 'TestMutants/.*imports' -v` → every imports mutant reported caught.

### Step 5: Timings, the build line, the generator comment

- `AGENTS.md`: `(about 25 s; \`-short\` skips the mutants)` → `(about 30 s; \`-short\` skips the mutants, about 7 s)`.
- `tests/README.md` lines 10–11: `~20 s` → `~30 s`, `~5 s` → `~7 s`.
- `README.md:21`: `go generate ./... && go build -o lifelog ./cmd/lifelog   # Go >= 1.27, no cgo` →
  `go generate ./... && go install ./cmd/lifelog          # Go >= 1.27, no cgo; puts lifelog on PATH (GOBIN)`.
- `tools/copyschema/main.go:1-2`: `(go:embed cannot reach outside the module)` → `(a go:embed pattern cannot name a
  parent directory)`.

**Verify**: `go vet ./tools/...` → exit 0; `gofmt -l tools` → empty.

### Step 6: Full run

**Verify**: `go generate ./... && go vet ./... && go test ./...` → exit 0. `git status --short` lists only the
in-scope files.

## Test plan

No new tests: this plan changes prose. The `document`, `imports` and `writers` suites and their mutants are the
check that nothing they anchor on moved.

## Done criteria

- [ ] `go generate ./... && go vet ./... && go test ./...` exits 0
- [ ] `grep -n "There is no application in this repo" docs/plans/README.md` → no match
- [ ] `grep -rn "the next step is the capture path" docs/README.md` → no match
- [ ] `grep -n "Datasette does this by itself" docs/contract/connections.md` → no match
- [ ] `grep -n 'sqlite3 -readonly life.db "VACUUM INTO' docs/contract/imports.md` → still one match
- [ ] `git diff cad659b -- docs/README.md docs/contract | grep "^+" | grep -i "lifelog \|internal/\|cmd/\|mcp\|siren"`
      → no match (the docs still name no application)
- [ ] `git status --short` lists only in-scope files; `docs/plans/README.md` status row for 025 updated

## STOP conditions

- A quoted line is not in the file as quoted (someone edited it since `cad659b`).
- A suite fails after Step 3 or 4 and the failure names a string this plan removed — report which; do not change
  the suite.
- You find yourself wanting to edit a plan record (`docs/plans/0NN-*.md`) or an issue: STOP, those are records.

## Maintenance notes

- If the owner later wants the CLI/Datasette claims executed again, the way is an opt-in test like
  `LIFELOG_MERMAID=1` (`tests/render_test.go`) gated on the tool being on `PATH` — then the "(not executed here)"
  wording goes back to *executed*.
- Plans 021, 023 and 024 still name `app/` and the Python gate inside their own text; the index's context block now
  tells executors how to read them. Plan 021's done criteria must be re-read with `go test ./...` in place of
  `run_all.py` when the owner closes it.
