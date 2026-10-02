# Plan 034: Every safety claim of the README has a test that fails when the claim breaks

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat cad659b..HEAD -- internal/api internal/core/core_test.go internal/core/read.go cmd/lifelog tests/mutants_test.go tests/README.md`
> Plans 026–033 add code and tests in these places — expected. Read the current files before editing; STOP only if a
> function this plan names no longer exists.

## Status

- **Priority**: P2 (P1 if 026–029 have landed: this protects them)
- **Effort**: M
- **Risk**: LOW (tests and two tiny refactors for testability)
- **Depends on**: 026, 027, 028, 029 (it tests what they make true; run after them)
- **Category**: tests
- **Planned at**: commit `cad659b`, 2026-10-02

## Why this matters

The README's safety claims — "`register-metric` and `replay` are refused to an `agent:*` writer and are never MCP
tools", "`approve` refuses without an interactive terminal", MCP rows are written as `agent:<name>`, "`POST /query` …
5 s and 500 rows at most", the save contract's per-target `SAVEPOINT` (D19: an invalid target "never blocks a save") —
are not protected by tests. The tests audit applied 26 single mutations to a copy of the app at `9ac130f`; **14
survived** every test, including: removing `ownerOnly` from `register-metric` and `replay`, making `ownerOnly` a no-op,
letting MCP expose owner actions, writing MCP rows as `cli`, disabling the TTY check of `approve`, replacing the
`SAVEPOINT`/`ROLLBACK TO` of the save with no-ops, aborting the whole save on a target error, raising the row cap to
50 000 and the timeout to 5 000 h, ignoring the `Lifelog-Source` header, and disabling the "replay into the trial"
refusal. The port from Python (commit `9ac130f`) also dropped the 14 "switch" mutants that proved the save's own
probes could fail. After this plan each of those mutations fails a test.

## Current state

- Package coverage at `9ac130f` (`go test -cover ./...`): api 34.6 %, core 57.7 %, db 68.2 %, importer 66.4 %,
  text 97.1 %; `cmd/lifelog`, `internal/client`, `internal/mcp`, `tools/copyschema` 0 %. (Plan 028 adds MCP tests,
  including "no owner tool is listed".)
- `internal/api/handler.go:89-97` — `ownerOnly` refuses `src` with prefix `agent:` (403). Mounted on
  `post("/metrics", ownerOnly(h.registerMetric))` (`handler.go:72`) and `post("/import/replay", ownerOnly(h.replay))`
  (`internal/api/import.go:73`).
- `internal/api/handler.go:168-178` — `source(r)`: the `Lifelog-Source` header, else `ui` for an HTML request, else `api`.
- `cmd/lifelog/main.go:152-158` — for `mcp`, `o.source = "agent:" + name` (name defaults to `mcp`).
  `main.go:393-395` — `approve` refuses unless `term.IsTerminal` on stdin and stdout. Both live inside `run`/`importOwner`,
  which open files and read the terminal — not unit-testable as they are.
- `internal/importer/replay.go:33-35` — `if same(target, w.TrialDB()) { return nil, refuse("the target is the trial database itself") }`.
- `internal/core/write.go:62-80` — `syncWikilinks`'s per-target `SAVEPOINT target` / `ROLLBACK TO target` /
  `RELEASE target` (read the current lines; plan 033 may have shifted them).
- `internal/core/read.go` — `Query` with a 5 s timeout (`context.WithTimeout(ctx, 5*time.Second)`); plan 026 changed the
  rest of `Query`.
- `internal/core/core_test.go:105-118` — `TestSaveRevivesATombstonedTarget` checks only the reported `r.Revived`, not the
  row: a mutation that skips the revive passed every `internal/core` test.
- `internal/api/api_test.go:133-138` — `must` panics instead of failing the test.
- `internal/importer/importer_test.go:183` — the first `Apply`'s result `r` is assigned and never checked (staticcheck
  SA4006).
- `tests/mutants_test.go:267` — `t.Logf("mutants: %d", len(mutants))`; `tests/README.md:47` says
  `129 broken copies of the docs tree` — nothing checks the two agree (AGENTS.md: "Keep the counts in `tests/README.md` …
  true").
- Test patterns: `internal/api/api_test.go` (`fresh`, `find`, `must`, `httptest`), `internal/core/core_test.go`
  (`fresh`, `status`), `internal/importer/importer_test.go` (`setup` fixture).

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Packages | `go test ./internal/... ./cmd/...` | ok |
| Suites | `go test ./tests` | ok |
| Everything | `go generate ./... && go vet ./... && go test ./...` | exit 0 |

## Scope

**In scope**: `internal/api/api_test.go`, `internal/api/import_test.go` (create), `internal/core/core_test.go`,
`internal/core/read.go` (make the query timeout a package variable only), `internal/importer/importer_test.go`
(line ~183 only), `cmd/lifelog/main.go` (extract two small pure functions), `cmd/lifelog/main_test.go` (create),
`tests/mutants_test.go` (one count check).

**Out of scope**: production behaviour (no rule changes here — if a test shows a rule is broken, STOP); `docs/`; the
writers suite's timing (considered and rejected: no flake in eight runs).

## Git workflow

Branch `advisor/034-safety-tests`; message e.g. `tests: owner-only gates, MCP provenance, approve's terminal check,
query limits, the save's savepoint and the replay target each fail a test when broken`, body listing the mutations you
re-ran (Step 8) + `Ran: …` + trailer.

## Steps

### Step 1: `must` fails the test instead of panicking

Change `internal/api/api_test.go`'s `must(e, err)` to `must(t *testing.T, e *api.Entity, err error) *api.Entity`
calling `t.Helper()` and `t.Fatal(err)`; update its call sites (mechanical: `must(c.Get(...))` →
`must(t, c.Get(...))` — Go allows passing a multi-value call only as the sole argument, so write
`e, err := c.Get(...); e = must(t, e, err)` where needed, or keep a two-arg helper `mustT := func(e *api.Entity, err error) *api.Entity { … }` closed over `t` in each test).

**Verify**: `go test ./internal/api` → ok.

### Step 2: The owner-only gates

In `internal/api/api_test.go`: `TestOwnerOnlyActionsRefuseAgents` — a client `client.InProcess(h, "agent:test")`
posting `register-metric` (`name=steps`, `unit=count`) → `*client.Error` with `Status == 403`, and
`GET /metrics` shows no `steps`; the same with source `cli` → 200.

Create `internal/api/import_test.go` (package `api_test`) with a workspace fixture (copy the minimum of
`internal/importer/importer_test.go`'s `setup`: a temp source folder with one note, `importer.Open(src + ".lifelog")`,
`w.Setup("")`, `db.Open(w.TrialDB())`, `api.New(&core.Store{DB: d}, w)`). `TestReplayIsTheOwners`: `replay` with
`to=<tmp>/life.db` as `agent:test` → 403 and the target file does not exist; as `cli` → not 403 (any other status is
fine: the workspace has no approved rules).

**Verify**: `go test ./internal/api -run 'OwnerOnly|ReplayIsTheOwners' -v` → PASS.

### Step 3: Provenance per surface

`TestSourceComesFromTheSurface` in `api_test.go`: `create-page` through `client.InProcess(h, "cli")` → the row's
`source` is `cli`; through a raw `httptest` POST without the header and with `Accept: application/vnd.siren+json` →
`api`; with `Accept: text/html` → `ui`; with header `Lifelog-Source: agent:x` → `agent:x`. Read sources with the
`query` action (`SELECT title, source FROM entities JOIN pages USING (id)`).

In `cmd/lifelog/main.go`, extract `func sourceFor(cmd, agent, source string) string` (returns `"agent:"+name` for
`mcp`, with `name` defaulting to `mcp`, else `source`) and `func checkTerminal(stdin, stdout bool) error` (the
`approve` refusal). Use both where the code is today. Create `cmd/lifelog/main_test.go` (package `main`):
`sourceFor("mcp", "", "cli") == "agent:mcp"`, `sourceFor("mcp", "lmstudio", "cli") == "agent:lmstudio"`,
`sourceFor("capture", "", "cli") == "cli"`; `checkTerminal(false, true)`, `checkTerminal(true, false)` return errors,
`checkTerminal(true, true)` returns nil. Also test `parse`: `--db` after the subcommand is honoured
(`parse([]string{"day", "--db", "x.db"}).db == "x.db"`).

**Verify**: `go test ./cmd/lifelog -v` → PASS.

### Step 4: The query limits

In `internal/core/read.go`, replace the literal timeout with a package variable `var queryTimeout = 5 * time.Second`.
In `core_test.go`: `TestQueryLimits` — a recursive CTE yielding 600 rows with `maxRows` 500 → 500 rows and
`Truncated`; set `queryTimeout = 200 * time.Millisecond` (restore with `t.Cleanup`), run a CTE that never ends
(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n) SELECT count(*) FROM n`) → an error within 2 s.
In `api_test.go`: `POST /query` of a 600-row CTE → `properties.rows` has 500 entries and `truncated` is true (this
pins the API's 500).

**Verify**: `go test ./internal/core ./internal/api -run 'QueryLimits|Query' -v` → PASS.

### Step 5: The save's savepoint, through the writer

`TestAnInvalidTargetNeverBlocksASave` in `core_test.go`: on a fresh store, add a trigger the test owns —
`s.DB.W.Exec("CREATE TRIGGER boom BEFORE INSERT ON pages WHEN NEW.title = 'Boom' BEGIN SELECT RAISE(ABORT, 'boom'); END")`
— then create a page with body `see [[Boom]] and [[Fine]]`. Expect: no error; the page exists with that body; a
`wikilink` row to `Fine` exists; no page `Boom`; `Skipped` (or the equivalent field of the returned `Sync`) names
`Boom`; and the orphan check (`SELECT count(*) FROM entities WHERE id NOT IN (SELECT id FROM pages UNION SELECT id FROM people)`)
is 0 — the `entities` row of the failed target was rolled back to its savepoint.

Strengthen `TestSaveRevivesATombstonedTarget`: also assert `SELECT deleted_at FROM entities WHERE id = <old's id>` is
NULL.

**Verify**: `go test ./internal/core -run 'InvalidTarget|Revives' -v` → PASS.

### Step 6: The replay refuses the trial; the unchecked Apply

- In `internal/importer/importer_test.go`, a test (or a case in the replay test) that `f.w.Replay(ctx, f.s, f.trial)`
  returns an error mentioning `trial`.
- At line ~183 assert the first `Apply`'s summary (print it once with `t.Log`, then assert the exact summary you saw,
  e.g. `strings.Contains(r.Summary, "new")`).

**Verify**: `go test ./internal/importer` → ok.

### Step 7: The mutant count in `tests/README.md` is checked

In `tests/mutants_test.go`, after the loop: read `tests/README.md` (path relative to the test: `README.md`), find the
number in the `mutants_test.go` row with `regexp.MustCompile(\`\| (\d+) broken copies\`)`, and `t.Errorf` when it
differs from `len(mutants)`. Skip the check under `-short` only if the mutant loop is skipped there too (match the
existing `testing.Short()` handling in that file).

**Verify**: `go test ./tests -run TestMutants` → ok; temporarily change the README number to 128 → fails; restore.

### Step 8: Re-run the mutations that survived

For each mutation below, apply it in a scratch copy of the repo (`git worktree add <tmp> HEAD` or a copy — never in
the working tree you commit from), run `go test -short ./internal/... ./cmd/... ./tests`, confirm **at least one test
fails**, then discard the copy:
1. `ownerOnly(h.registerMetric)` → `h.registerMetric`;
2. `ownerOnly(h.replay)` → `h.replay`;
3. `if strings.HasPrefix(src, "agent:")` in `ownerOnly` → `if false && strings.HasPrefix(src, "agent:")`;
4. in `sourceFor`, `"agent:" + name` → `"cli"`;
5. in `checkTerminal`, always return nil;
6. the save's `SAVEPOINT`/`ROLLBACK TO`/`RELEASE` statements → `SELECT 1`;
7. the save returns the target error instead of skipping it;
8. the API's row cap 500 → 50000;
9. `queryTimeout` → `5000 * time.Hour`;
10. `source()` ignores the header;
11. the `same(target, w.TrialDB())` refusal removed.
Record the list and each failing test's name in the commit body.

**Verify**: all 11 mutations killed.

### Step 9: Full run

**Verify**: `go generate ./... && go vet ./... && go test ./...` → exit 0.

## Test plan

As in Steps 2–7; patterns `internal/api/api_test.go`, `internal/core/core_test.go`, `internal/importer/importer_test.go`.

## Done criteria

- [ ] `go generate ./... && go vet ./... && go test ./...` exits 0
- [ ] `go test -cover ./cmd/lifelog` reports > 0 %
- [ ] the 11 mutations of Step 8 each fail at least one test (listed in the commit body)
- [ ] `grep -n "panic(err)" internal/api/api_test.go` → no match
- [ ] `git status --short` lists only in-scope files; status row for 034 updated

## STOP conditions

- A new test shows a claimed rule is actually broken (e.g. an agent **can** run `replay`) — STOP and report: that is a
  bug for its own plan, not something to make pass here.
- Extracting `sourceFor`/`checkTerminal` changes any CLI behaviour — report.
- Step 5's trigger cannot be created because the schema forbids it (`trusted_schema` or defensive mode) — try it on the
  writer connection `s.DB.W`; if still refused, report the error.

## Maintenance notes

- A new owner-only action needs a line in `TestOwnerOnlyActionsRefuseAgents`.
- The trigger trick of Step 5 is the Go replacement of the Python "no validation" switch: it makes a target fail **in
  the database** while the writer's predicate accepts it. Keep it when the predicate changes.
