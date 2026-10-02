# Plan 036: Each action is declared once — its route, owner flag and required fields come from the catalog

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `docs/plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat cad659b..HEAD -- internal/api internal/core/habits.go`
> Plans 026–034 edit these files. Re-read `handler.go`, `import.go`, `habits.go` and `siren.go` in full before Step 1;
> STOP if the catalog (`var catalog`, `var importCatalog`) or the `New`/`mountImport` route tables are no longer as
> described below.

## Status

- **Priority**: P3
- **Effort**: M
- **Risk**: MED (touches every action route; the API tests and the suites guard it)
- **Depends on**: 028 (renames `find` → `find-page`) and 034 (tests that pin the gates); run after both
- **Category**: tech-debt
- **Planned at**: commit `cad659b`, 2026-10-02

## Why this matters

The README's design is "`GET /actions` is the catalog; the CLI's `do` and the MCP tools are generated from it, so a new
action needs no client change". But on the server side each action is written in **four** places that must agree by
hand: the catalog spec (`siren.go`, `import.go`), the route (`handler.go:43-77`, `import.go:53-73`), the `ownerOnly`
wrapper (applied by hand at two routes), and a `required(v, …)` list inside each handler. They already disagree:
`save-body` declares `body` required but its handler checks only `version`; `draft-rules` and `ask` declare required
fields their handlers never check; nothing tests that every catalog action has a route. A forgotten `ownerOnly` on a
new owner action would silently hand it to agents. Separately, the habit check-in's rule "a check-in falls inside a
period" lives in the HTTP handler and reads the periods **outside** the write transaction — an import or another caller
of `core` bypasses it.

## Current state

- `internal/api/siren.go:55-61` — `type spec struct { Name, Title, Description, Method, Path string; Fields []Field; Owner bool }`;
  `catalog` (`siren.go:72-118`) and `importCatalog` (`import.go:15-50`) list every action.
- `internal/api/handler.go:30-82` — `New` mounts resources (GET `/`, `/days`, `/pages/{id}` …) **and** actions with
  `get(path, f)` / `post(path, f)`, where `post` wraps `f(r, src)` with `source(r)`; `post("/metrics", ownerOnly(h.registerMetric))`.
  `internal/api/import.go:52-74` — `mountImport` does the same; `post("/import/replay", ownerOnly(h.replay))`.
- Every POST handler begins with `v, err := form(r)` and then `required(v, …)` (about 25 hand-written lists), e.g.
  `handler.go:575-588` (`createPage`: `required(v, "title")`), `handler.go:590-607` (`saveBody`: `required(v, "version")`).
  `form(r)` (`handler.go:181-205`) reads the body once — a JSON body cannot be read twice.
- GET actions in the catalog: `integrity-check` (`/integrity`), `search` (`/search`), `find-page` (`/pages`, after plan
  028), and in the import catalog `import-status`, `inspect`, `find`. Their handlers read `r.URL.Query()`.
- `internal/api/habits.go:88-122` — `checkIn`: checks `done` is `0`/`1`, reads `h.s.Periods(...)` (read pool), decides
  `on`, then `h.s.Record(...)` (a separate write transaction). `habits.go:170-178` — `unitless` swallows the `Metrics`
  error.
- `internal/core/habits.go` — `Tx.StartHabit`, `Tx.StopHabit`, `Store.Periods`; `Tx.Record` (`write.go`) refuses a
  non-0/1 value on a habit metric.
- Tests: `internal/api/api_test.go`, `internal/api/import_test.go` (from plan 034), `internal/mcp/mcp_test.go` (from plan
  028), and the suites.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| API + MCP + CLI | `go test ./internal/api ./internal/mcp ./cmd/...` | ok |
| Everything | `go generate ./... && go vet ./... && go test ./...` | exit 0 |

## Scope

**In scope**: `internal/api/siren.go`, `internal/api/handler.go`, `internal/api/import.go`, `internal/api/habits.go`,
`internal/api/api_test.go`, `internal/core/habits.go` (new `Tx.CheckIn`), `internal/core/habits_test.go`.

**Out of scope**: the resource (non-action) routes; response shapes; action names, paths, fields and descriptions
(except `save-body`'s `body` becoming optional, Step 2); the CLI and MCP (they read `/actions` and need no change).

## Git workflow

Branch `advisor/036-catalog-once`; message e.g. `api: actions are mounted from the catalog — owner flag and required
fields enforced in one place; a check-in's period is checked in core`, body + `Ran: …` + trailer.

## Steps

### Step 1: A test that the catalog and the routes agree

In `api_test.go` add `TestEveryActionIsServed`: for every action of `GET /actions` (build the API **with** a workspace —
reuse the fixture from `import_test.go`), send its method to its href with path placeholders filled by `1` (or `Q1` for
`close-question`, `x` for `{name}`, `2026-01-01` for `{day}`) and no fields; assert the status is **not** 404 or 405
from the mux (a 404 "no such id" from a handler is fine — distinguish by checking the body is a Siren entity whose
message is not Go's `404 page not found`). And `TestOwnerActionsAreRefusedToAgents`: for every action with `owner: true`,
a POST with `Lifelog-Source: agent:test` → 403.

**Verify**: both pass on the current code (they pin today's behaviour before the refactor).

### Step 2: The catalog carries the handler

- Change the handler signature for **actions** to `type actionFunc func(r *http.Request, src string, v url.Values) (*Entity, error)`.
  For GET actions `v` is `r.URL.Query()`; for POST actions `v` is `form(r)`'s result.
- In `New`, build `handlers := map[string]actionFunc{"capture": h.capture, "create-page": h.createPage, …}` for every
  catalog action (and in `mountImport` for `importCatalog`). Then loop over the specs: for each spec, look up its
  handler (a missing one → `panic("no handler for action " + s.Name)` at startup — a programming error), and register
  `s.Method + " " + s.Path` with a wrapper that: (1) gets `src` via `source(r)` for POST; (2) gets `v`; (3) checks every
  field with `Required && In != "path"` is non-empty (`required(v, names...)`); (4) applies the owner refusal when
  `s.Owner`; (5) calls the handler.
- Remove the per-action `get(...)`/`post(...)` lines and every `required(...)` call that the wrapper now does; keep a
  handler's own **extra** checks (e.g. `done` must be 0 or 1).
- In `siren.go`, make `save-body`'s `body` field `opt(...)` (an empty body is a legal save: it empties the page). Keep
  `version` required.
- Delete `ownerOnly` if nothing else uses it.

**Verify**: `go test ./internal/api ./internal/mcp ./cmd/...` → ok; Step 1's tests still pass.

### Step 3: The check-in's period is a core rule

Add to `internal/core/habits.go`:

```go
// CheckIn records a habit for a day inside one of its periods: done 1, not done 0 (D24).
func (t *Tx) CheckIn(metric, day string, done bool) (int64, error)
```

It checks `IsDay(day)`, then `SELECT EXISTS (SELECT 1 FROM habit_periods h JOIN metrics m ON m.id = h.metric_id WHERE
m.name = ? AND h.start_day <= ? AND coalesce(h.end_day, '9999-12-31') >= ?)` **inside the transaction**, refuses with
`invalid("%s is not a habit on %s: start it first", metric, day)`, then calls `t.Record(Reading{Metric: metric, Day: day,
Value: 0 or 1})`. Add `Store.CheckIn(ctx, source, metric, day string, done bool)`. The API's `checkIn` calls it instead of
reading periods itself. `unitless` returns `(bool, error)` and its caller propagates the error.

Test in `habits_test.go`: a check-in outside every period → 422; inside → ok; a value is stored as 0/1.

**Verify**: `go test ./internal/core ./internal/api` → ok.

### Step 4: Full run

**Verify**: `go generate ./... && go vet ./... && go test ./...` → exit 0; `grep -c "required(v," internal/api/*.go` is
far below today's (report the before/after counts).

## Test plan

`TestEveryActionIsServed`, `TestOwnerActionsAreRefusedToAgents` (written before the refactor, green before and after),
the core `CheckIn` test. Existing API, MCP and suite tests unchanged.

## Done criteria

- [ ] `go generate ./... && go vet ./... && go test ./...` exits 0
- [ ] `grep -n "ownerOnly(" internal/api/*.go` → no match (the flag drives it)
- [ ] `grep -n "h.s.Periods" internal/api/habits.go` → only in read handlers (not in `checkIn`)
- [ ] `git status --short` lists only in-scope files; status row for 036 updated

## STOP conditions

- A handler needs the raw body after `form` read it (it would now be read twice) — report which.
- Step 1's tests fail **before** the refactor — that is an existing routing bug; report it.
- An action's required field is legitimately empty in a flow the tests exercise (like `body`) beyond `save-body` —
  report it instead of making more fields optional.

## Maintenance notes

- A new action is now: a spec in the catalog + an entry in the handler map. Its owner flag and required fields are
  enforced without further code; `TestEveryActionIsServed` fails if the handler is missing.
- Keep resource routes (pages, days, metrics) hand-mounted: they are not actions.
