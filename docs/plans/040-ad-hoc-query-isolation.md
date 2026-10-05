# 040 — Keep ad-hoc SQL read-only and isolated from pooled readers

> Executor: run every gate from the repository root. Treat this as defensive boundary work, not an SQL feature expansion. Set the index row to IN REVIEW (owner).
>
> Drift check: `git diff --stat 726ffab..HEAD -- internal/core/read.go internal/core/query.go internal/core/core_test.go internal/core/query_test.go internal/db/db.go internal/db/query.go internal/db/db_test.go internal/api/api_test.go README.md`. Plan 045's URI helper changes are expected prerequisite drift; rebaseline them first.

## Status

- **Date / planned at:** 2026-10-05, commit `726ffab`.
- **Priority:** P1. **Effort:** M. **Risk:** MED (SQL compatibility and connection lifetime).
- **Status:** IN REVIEW (owner). **Depends on:** [045](045-literal-sqlite-paths.md). **Category:** security / bug. **Audit finding:** 7.

## Why

The query action promises one read-only SQL statement. It currently passes arbitrary text to a driver that supports scripts, and returns the connection to the shared reader pool after resetting only two pragmas. Explicit transaction state and other settings can remain active, causing stale reads or long-lived WAL snapshots. Enforce the promised request boundary and make query connection lifetime independent of normal application reads.

## Current state and conventions

`internal/core/read.go:430–438`:

```go
ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()
conn, err := s.DB.R.Conn(ctx)
// ...
defer restoreReader(conn)
rows, err := conn.QueryContext(ctx, q)
```

Cleanup at line 476 executes only `PRAGMA query_only = ON; PRAGMA trusted_schema = OFF`, then returns the connection to the pool. The pinned modernc driver's script execution and `ResetSession` do not enforce the application's one-statement promise or roll back arbitrary manually started transactions. `TestQueryCannotWrite` and `TestQueryRestoresTheReaderPragmas` in `core_test.go` cover only a direct write and those two settings.

Keep the 5-second timeout, row cap, truncation and `Result` JSON shape. [Connection setup](../contract/connections.md) requires read-only connections, `trusted_schema=OFF`, and short reads. `db.dsn` and connection hooks own connection settings; `core.invalid` reports 422. Do not edit the language-neutral connection contract to endorse unsafe query reuse.

## Scope

Only the drift-check paths and plan/index status. New `core/query.go`, `core/query_test.go` and `db/query.go` are optional small helpers. No new SQL engine/dependency, reflection into driver internals, driver fork, schema/migrations, public query language, or ordinary read-pool redesign. Use temporary synthetic databases and files only.

## Commands

- Targeted: `go test -mod=readonly -count=1 ./internal/core -run TestQuery`.
- Integration: `go test -mod=readonly -count=1 ./internal/db ./internal/core ./internal/api`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./...`.

## Steps

1. Add `TestQueryStatementBoundary` and `TestQueryDoesNotLeakReaderState`. Characterize script handling and manually opened transactions on a forced single-reader pool using benign synthetic operations. Check that a subsequent committed store write is visible through ordinary reads and that connection settings are unchanged. Record failures as boundary regressions; do not put misuse recipes in documentation.
   **Verify:** targeted command → new boundary/state assertions fail on current code, while existing SELECT/truncation cases pass.
2. Add a small SQL lexical boundary check before execution. It must distinguish comments, quoted strings/identifiers and a final terminator from actual multiple statements; substring/semicolon regexes are insufficient. Admit single read query forms (`SELECT`, `WITH`, `VALUES`, `EXPLAIN`) and explicitly enumerated read-only introspection pragmas; reject transaction control, attachment, maintenance/export operations, pragma setters and trailing statements. Let SQLite validate query grammar; do not build a general SQL parser. `WITH` bodies must remain protected by read-only/query-only execution. Test legitimate semicolons inside literals and comments, quoted identifiers, trailing comments and empty input.
   **Verify:** targeted command → classification tests pass; supported cookbook queries continue to work. Update the old pragma-restoration test to assert refusal of setters and unchanged reader state, not acceptance of mutation.
3. Use a dedicated short-lived read-only connection/database handle for each ad-hoc query, opened through `internal/db` with plan 045's literal path helper and the existing verified settings. Persist the application path in `DB` if needed; do not expose it in API output. Close rows, connection and handle on every success/error/timeout/truncation path; never return this connection to `DB.R`. Check all `Columns`/row/close errors that affect the result.
   **Verify:** integration command → normal reads see new commits after every query outcome; no ad-hoc transaction survives completion. Query connection/resource tests pass.
4. Add an API boundary regression and document the accepted read/introspection boundary in the application README. Preserve useful read-only cookbook functionality and the existing response shape; narrowing setter/script acceptance is intentional.
   **Verify:** final command → exit 0.

## Done criteria

- [x] Exactly one allowed read statement is executed; state-changing statement classes/setters are refused with 422.
- [x] String/comment punctuation does not cause false statement splits; SELECT/CTE/VALUES/EXPLAIN and supported introspection work.
- [x] Dedicated handles close on all paths; pooled reads remain fresh and their pragmas unchanged.
- [x] Timeout, max rows, truncation and API JSON remain compatible; full verification passes.
- [x] Only scope paths changed; index is IN REVIEW (owner).

## STOP conditions

Stop if a supported cookbook query cannot fit the proposed boundary, if enforcement requires unsafe driver access or a dependency, or if an external-file write risk is claimed without a synthetic probe on the pinned driver. Do not claim stronger SQLite protections than tests establish. Report baseline failures separately.

## Git workflow and maintenance

Use an operator-selected branch/worktree. Commit as `query: isolate and bound ad-hoc reads (plan 040); ran go generate, go vet and go test`; push only when instructed. Keep the introspection allowlist explicit and test changes. Read-only opening of the main file is not permission to execute arbitrary connection-management SQL.
