# Plan 037: Import status grows linearly, and no answer floods a small model's context

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat cad659b..HEAD -- internal/importer internal/core/read.go internal/api/handler.go internal/api/import.go`
> Plans 029–036 edit these files. Re-read `status.go`, `apply.go` (`prepare`, `write`, `lookAlike`), `inspect.go`
> (`Inspect`), `read.go` (`PageByID`) and `handler.go` (`pageEntity`, `series`) before starting; STOP if a function this
> plan names is gone.

## Status

- **Priority**: P2
- **Effort**: M–L
- **Risk**: MED (status must keep reporting exactly what it reports today; payload fields are added, not removed)
- **Depends on**: 029–032 (same importer files), 026 (it moved `Query`), 036 if it landed (handler signatures)
- **Category**: perf
- **Planned at**: commit `cad659b`, 2026-10-02

## Why this matters

The guide tells the model to "start every turn with *status*" (`docs/guides/importing.md:270`). Measured by the
architecture audit at `9ac130f` on synthetic data (46 k pages, 1 M readings): *status* re-reads and regex-parses the whole
`ledger.md` once per done file (inside each dry run), so it is O(files²): **2 000 done files → 4.8 s per call; 6 000 →
26.9 s**, paid on every model turn. It also builds an SQL string by concatenation and caps it at 100 000 rows without
checking the cap. Separately, answers sent to the model are unbounded: a page with many backlinks is **3–7 MB** of JSON
(Home with 25 k `at` backlinks: 7.25 MB), a lifetime series of one metric 12 MB, and *inspect* returns every row of a
CSV — far beyond a local model's context. Each new name in a facts file re-reads every page name (~140 ms at 46 k pages)
inside the write transaction.

## Current state

- `internal/importer/status.go:147-213` — `mismatches`: for each `[x]`/`[?]` line, `w.Check(ctx, s, l.File)`; then
  `w.LoadFacts`, `w.ReadSource`, and `pos[i] = wholeIndex(collapse(src), collapse(wr.Quote))` (`collapse(src)` recomputed
  per write, line 177); then (`:192-212`):
  ```go
  src := strings.ReplaceAll(st.Source, "'", "")
  res, err := s.Query(ctx, `SELECT import_key FROM entities WHERE source = '`+src+`' AND import_key IS NOT NULL
                            UNION ALL SELECT import_key FROM measurements WHERE source = '`+src+`' AND import_key IS NOT NULL`, 100000)
  if err == nil { … count keys not expected … }
  if res, err := s.Query(ctx, `SELECT (SELECT count(*) FROM entities WHERE source LIKE 'agent:%') + … `, 1); err == nil { … }
  ```
  (`res.Truncated` is never read; errors skip the checks.)
- `internal/importer/apply.go` — `prepare(file)` (`:63-97`): `w.ledgerState(file)` (reads and parses `ledger.md`),
  `w.Gate("rules.md")`, `w.Rules()`, `w.ApprovedMetrics()`, `w.LoadFacts(file)`, `w.ReadSource(file)`,
  `w.checkStatic(...)`. `Check` and `Apply` call `prepare` then `write`.
- `internal/importer/apply.go:295-318` — `lookAlike(t, rules, title)`: `names, err := t.Names()` (every live page) on
  every call; `relation(title, cand)` recomputes the title's words for every candidate.
- `internal/core/read.go:12-27` — `type Page struct { … Out []Edge \`json:"links"\`; In []Edge \`json:"backlinks"\` }`;
  `PageByID` (`:45-91`) reads all backlinks with no LIMIT (the one-hop redirect query, `ORDER BY pg.title`).
- `internal/api/handler.go:332-373` — `pageEntity`: `Properties: p` (with `links`/`backlinks`) **and** one embedded
  `Link` per edge in `Entities`; `save-body` prefilled with the body.
- `internal/api/handler.go:448-485` — `series`: `?from=` unbounded; every reading in `Properties["readings"]` **and** one
  embedded link per reading.
- `internal/importer/inspect.go:89-110` — `Inspect`: a CSV returns `in.Rows = rows` (all rows); other text returns the
  whole file.
- Hot-path regexps compiled per call: `internal/importer/facts.go:426` (`regexp.MustCompile(\`^Q\d+$\`)` per waiting id —
  plan 030 hoisted the one in `parseValue`), `internal/importer/files.go:143` (in `ProposeMetric`).
- Plan 026 made `Store.Query` refuse anything but one read and run on its own connection; internal code should use
  typed reads, not `Query`.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Importer/API/core | `go test ./internal/...` | ok |
| Benchmark (Step 1) | `go test ./internal/importer -run '^$' -bench BenchmarkStatus -benchtime 3x` | prints ns/op for each size |
| Everything | `go generate ./... && go vet ./... && go test ./...` | exit 0 |

## Scope

**In scope**: `internal/importer/status.go`, `internal/importer/apply.go` (`prepare`, `Check`/`Apply` plumbing,
`lookAlike`), `internal/importer/inspect.go`, `internal/importer/facts.go` (line ~426 only), `internal/importer/files.go`
(line ~143 only), `internal/importer/importer_test.go` (+ a benchmark), `internal/core/read.go` (`PageByID` cap, new
`Backlinks`), `internal/core/imports.go` (new typed reads), `internal/api/handler.go` (`pageEntity`, `series`, a new
`GET /pages/{id}/backlinks`), `internal/api/import.go` (`inspect` fields), `internal/api/siren.go` (`inspect` spec fields
if it lives there — it lives in `import.go`'s `importCatalog`), `internal/api/api_test.go`.

**Out of scope**: the habit completion query (plan 038 — it is a cookbook recipe); memoising dry runs across calls (a
later step if linear is not enough); the integrity check's cost (it is the contract's `PRAGMA integrity_check`;
considered and rejected); `plan.json` rewrites per appended note (one-time cost, rejected).

## Git workflow

Branch `advisor/037-status-and-payloads`; message e.g. `import: status parses the workspace once and reads keys through
core; pages, series and inspect are capped with a link to the rest`, body (include Step 1/5 benchmark numbers) +
`Ran: …` + trailer.

## Steps

### Step 1: Measure first

Add `BenchmarkStatus` to `internal/importer/importer_test.go`: a synthetic source of N tiny notes (`Notes/n%04d.md`,
body `Met Sam.`), ledger made, rules approved, each file given a facts file with one `page` write (`{"page":{"title":"Sam"},"quote":"Met Sam."}`)
applied, then `b.ResetTimer()` and `w.Status(ctx, s, "")` per iteration. Run it as sub-benchmarks for N = 100 and N = 400.
Record both ns/op in the commit body.

**Verify**: it runs; the 400 case is roughly 16× the 100 case or worse (quadratic).

### Step 2: One snapshot of the workspace per status

- Add an unexported `type snapshot struct { states map[string]string; gate string; rules *Rules; approved map[string]Metric }`
  and `func (w *Workspace) snapshot() (*snapshot, error)` that reads the ledger, the rules gate, the rules and the approved
  metrics **once** (rules/metrics may be absent: keep the same errors `prepare` gives today, but lazily — only when a file
  is checked).
- Split `prepare(file)` into `prepareWith(snap, file)` (everything that is per file) and `prepare(file)` =
  `snap, err := w.snapshot(); … prepareWith(snap, file)`. Behaviour of `Check`/`Apply` for one file is unchanged.
- Add `func (w *Workspace) checkWith(ctx, s, snap, file) (*Report, error)` and use it in `mismatches` with one snapshot
  built at the start of `Status`.
- In `mismatches`, compute `c := collapse(src)` once per file and reuse it for every write.

**Verify**: `go test ./internal/importer` → ok (every status test unchanged); the benchmark's 400 case is now ≤ ~5× the
100 case.

### Step 3: Typed reads instead of built SQL

In `internal/core/imports.go` add:
- `func (s *Store) ImportKeys(ctx context.Context, source string, each func(key string) error) error` — streams
  `SELECT import_key FROM entities WHERE source = ? AND import_key IS NOT NULL UNION ALL SELECT import_key FROM measurements WHERE source = ? AND import_key IS NOT NULL`
  with bound parameters, no row cap.
- `func (s *Store) AgentRows(ctx context.Context) (int64, error)` — the three counts summed.
Use them in `status.go` instead of the two `s.Query` calls; an error is a mismatch (`"checking rows outside the facts: " + err.Error()`),
never silently skipped.

**Verify**: `grep -n "s.Query(" internal/importer/status.go` → no match; `go test ./internal/importer` → ok.

### Step 4: Look-alikes load names once per transaction

Give `lookAlike` a cache: add to the per-run state of `write` (`apply.go`) a `names []core.Name` loaded on first use with
`t.Names()`, and the precomputed word set of each candidate; when the run creates a person, place or page, append it to the
cache so a later write in the same file still sees it. `relation(title, cand)` takes the title's words computed once per
call. (Check the type name `t.Names()` returns and reuse it.)

Test: an existing test that expects a look-alike refusal still refuses; add one where a file creates `Sam Example` and a
later write in the same file names `Sam Exampel` → refused as a look-alike (the cache saw the first).

**Verify**: `go test ./internal/importer` → ok.

### Step 5: Pages, series and inspect are capped

- `core.Page`: add `BacklinksTotal int \`json:"backlinks_total"\``. `PageByID` reads at most 100 backlinks
  (`LIMIT 100`) and counts the total with the same `WHERE` (`SELECT count(*) …`). Add
  `func (s *Store) Backlinks(ctx context.Context, id int64, offset, limit int) ([]Edge, error)` with the same query and
  `LIMIT ? OFFSET ?`.
- `pageEntity`: when `BacklinksTotal > len(p.In)`, add `link("backlinks", "/pages/{id}/backlinks?offset=100", "All
  backlinks")`. Mount `GET /pages/{id}/backlinks` returning an entity whose `Entities` are the edges of
  `offset..offset+100` and a `next` link when more remain.
- `series`: refuse a range longer than 3 660 days (`&core.Error{Status: 422, Msg: "a series is at most 10 years: narrow from/to"}`)
  and embed at most the 200 most recent readings as links (properties keep the whole range's readings).
- `Inspect`: add optional fields `offset` and `limit` (default 0 and 200) to the `inspect` spec; a CSV returns rows
  `offset..offset+limit` plus `RowsTotal int \`json:"rows_total"\``; a text file over 64 KiB returns its first 64 KiB and
  `Truncated bool \`json:"truncated"\``. Update the action's description to say so.

Tests (`api_test.go`): a page with 150 backlinks → `properties.backlinks` has 100 entries, `backlinks_total` 150, a
`backlinks` link; following it returns the remaining 50. An importer test: a 500-row CSV inspected → 200 rows,
`rows_total` 500.

**Verify**: `go test ./internal/...` → ok.

### Step 6: Hoist the per-call regexps

Move `regexp.MustCompile(\`^Q\d+$\`)` (`facts.go`) and the name regexp in `ProposeMetric` (`files.go`) to package-level
`var`s.

**Verify**: `go vet ./internal/importer` → exit 0.

### Step 7: Full run and the numbers

**Verify**: `go generate ./... && go vet ./... && go test ./...` → exit 0; re-run the Step 1 benchmark and put both
before/after numbers in the commit body.

## Test plan

`BenchmarkStatus` (before/after), the look-alike-in-one-file test, the backlinks paging test, the CSV inspect test;
every existing status test unchanged.

## Done criteria

- [ ] `go generate ./... && go vet ./... && go test ./...` exits 0
- [ ] benchmark: N=400 is at most ~5× N=100 (numbers in the commit body)
- [ ] `grep -n "s.Query(" internal/importer/*.go` → no match
- [ ] a page's JSON never carries more than 100 backlinks (test)
- [ ] `git status --short` lists only in-scope files; status row for 037 updated

## STOP conditions

- Splitting `prepare` changes what *status* reports for any existing test — report the difference.
- A client in this repo (CLI `human` view, MCP, the HTML template) breaks on the capped `backlinks` — report it.
- The benchmark is still quadratic after Step 2 — report where the time goes (`go test -cpuprofile`) instead of adding
  caches elsewhere.

## Maintenance notes

- If status is still slow at tens of thousands of done files, memoise each file's dry-run verdict by (facts sha, source
  sha, a write generation bumped by every commit) — deferred.
- New list endpoints should follow the same pattern: a cap, a total, a `next` link.
