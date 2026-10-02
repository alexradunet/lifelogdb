# Plan 035: Every push runs the checks; staticcheck is clean; dead code is gone

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat cad659b..HEAD -- .github internal/core/core.go internal/core/write.go internal/importer/files.go internal/text/text_test.go tests/identity_test.go AGENTS.md`
> Earlier plans may have removed some of the dead code already — then skip that item and say so.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW
- **Depends on**: best after 026–034 (so the first CI run is on the fixed code); not required
- **Category**: dx
- **Planned at**: commit `cad659b`, 2026-10-02

## Why this matters

AGENTS.md's rule is "Every test must pass", and every executor runs about 30 s of checks by hand. There is no CI
(no `.github/`), no linter, and dead code has already piled up after one big move (`9ac130f`). The earlier "no CI"
verdict in the plans index ("a GitHub workflow needs a SQLite ≥ 3.53 build with FTS5") no longer holds: SQLite is now the
pure-Go `modernc.org/sqlite` (3.53.4, FTS5) fetched as a module, so `go test ./...` runs on any GitHub runner with Go
alone. The repo has a GitHub remote (`origin https://github.com/alexradunet/lifelogdb.git`).

## Current state

- No `.github/` directory.
- `go.mod`: `go 1.27.1`. The checks (AGENTS.md "Setup and checks"): `go generate ./... && go vet ./... && go test ./...`.
  `go generate` copies `docs/schema/schema.sql` into `internal/db/schema.sql`; `internal/db/db_test.go:12-20` fails
  when the copy is stale.
- staticcheck, run by the tests audit on a scratch copy at `9ac130f`, reported:
  - `internal/core/core.go:78` — `func (s *Store) write(ctx context.Context, fn func(*sql.Tx) error) error` unused (U1000)
  - `internal/importer/files.go:32` — `var metricCols = []string{…}` unused (U1000)
  - `internal/text/text_test.go:14` — `codeSpan` unused (U1000)
  - `internal/importer/importer_test.go:183` — SA4006 (value never used) — plan 034 fixes it
  - `tests/identity_test.go:158` — SA4000 (identical expressions) — **a false positive**: the duplicate insert is the
    point of that expectation
- Also unused: `internal/core/write.go:497-516` — `func (t *Tx) ReadingByKey(metric, key string) (…)` (no callers;
  confirm with `grep -rn "ReadingByKey" --include=*.go .`).
- `gofmt -l .` is empty and `go mod tidy -diff` is clean at `9ac130f`.
- Privacy rule (AGENTS.md): tests use synthetic data only; `.gitignore` covers `*.db`. CI adds no data.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| staticcheck (no install) | `go run honnef.co/go/tools/cmd/staticcheck@latest ./...` | no output, exit 0 (after Step 2) |
| Format | `gofmt -l .` | empty |
| Everything | `go generate ./... && go vet ./... && go test ./...` | exit 0 |

## Scope

**In scope**: `.github/workflows/checks.yml` (create), `internal/core/core.go` (delete `write`), `internal/core/write.go`
(delete `ReadingByKey`), `internal/importer/files.go` (delete `metricCols`), `internal/text/text_test.go` (delete
`codeSpan`), `tests/identity_test.go` (one `//lint:ignore` line), `AGENTS.md` (the checks table: one row/sentence).

**Out of scope**: refactoring duplicated key lookups (noted below); dependency bots; any rule change.

## Git workflow

Branch `advisor/035-ci`; message e.g. `ci: go generate, vet, staticcheck and the tests on every push (ubuntu, windows);
dead code removed`, body + `Ran: …` + trailer. Do not push unless the operator says so — the workflow runs on push.

## Steps

### Step 1: Remove the dead code

Delete the four unused symbols listed above (`Store.write`, `Tx.ReadingByKey`, `metricCols`, `codeSpan`), after
`grep -rn "<name>" --include=*.go .` shows only the definition. Remove imports that become unused.

**Verify**: `go vet ./... && go build ./...` → exit 0.

### Step 2: staticcheck clean

Run `go run honnef.co/go/tools/cmd/staticcheck@latest ./...`. Note the version it resolved
(`go run honnef.co/go/tools/cmd/staticcheck@latest -version`). For `tests/identity_test.go:158`, add on the line above
the flagged expression: `//lint:ignore SA4000 the same insert twice is the expectation (a second row is refused)` (read
the test first and word the reason after what it checks). Fix any other finding only if it is dead code or a real bug;
otherwise STOP and report it.

**Verify**: staticcheck prints nothing, exit 0.

### Step 3: The workflow

Create `.github/workflows/checks.yml`:

```yaml
name: checks
on: [push, pull_request]
permissions:
  contents: read
jobs:
  checks:
    strategy:
      matrix:
        os: [ubuntu-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: generate (the schema copy must be current)
        shell: bash
        run: go generate ./... && git diff --exit-code
      - name: format
        shell: bash
        run: test -z "$(gofmt -l .)"
      - name: vet
        run: go vet ./...
      - name: staticcheck
        run: go run honnef.co/go/tools/cmd/staticcheck@<the version from Step 2> ./...
      - name: test
        run: go test ./...
```

Pin staticcheck to the exact version Step 2 resolved (not `@latest`).

**Verify**: `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/checks.yml` → no output (if
actionlint cannot be fetched, check the YAML parses: `python -c "import yaml,sys; yaml.safe_load(open(sys.argv[1]))" .github/workflows/checks.yml`
if Python is present; else skip and say so).

### Step 4: AGENTS.md

In the "Setup and checks" table's first row, after the command, add: `; CI runs the same on every push, plus gofmt and
staticcheck (.github/workflows/checks.yml)`. Keep "Go ≥ 1.27 and nothing else" true (staticcheck runs through `go run`).

### Step 5: Full run

**Verify**: `go generate ./... && go vet ./... && go test ./...` → exit 0; `git status --short` shows the in-scope files.

## Test plan

No new tests. The workflow is the test of the checks; its first run on GitHub is the confirmation (report its URL if the
operator pushes).

## Done criteria

- [ ] `go generate ./... && go vet ./... && go test ./...` exits 0
- [ ] `go run honnef.co/go/tools/cmd/staticcheck@<pinned> ./...` → no output
- [ ] `.github/workflows/checks.yml` exists with a pinned staticcheck version
- [ ] `grep -rn "func (s \*Store) write\|metricCols\|func (t \*Tx) ReadingByKey" --include=*.go .` → no match
- [ ] status row for 035 updated in `docs/plans/README.md`

## STOP conditions

- staticcheck reports a finding that is neither dead code nor the SA4000 false positive — report it.
- The Windows job would need something beyond Go (it should not: pure-Go SQLite).
- The owner has said not to add CI (check the plans index's "Findings considered and rejected" for a newer note).

## Maintenance notes

- Duplicated lookups remain (`entities WHERE source = ? AND import_key = ?` three times; the correction-chain CTE twice
  in `write.go`/`imports.go`) — fold them when one of them next changes.
- The mermaid render (`LIFELOG_MERMAID=1`) is not in CI (it needs node and a Chromium); run it by hand after a diagram
  change, as AGENTS.md says.
