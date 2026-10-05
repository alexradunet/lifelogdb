# 046 — Reserve new database paths exclusively before initialization

> Executor: run gates from the repository root and test only temporary synthetic files. Set the index row to IN REVIEW (owner) when done. This is creation safety, not a migration or multi-user initialization system.
>
> Drift check: `git diff --stat 726ffab..HEAD -- internal/db/db.go internal/db/db_test.go`. Plan 045's literal URI helper is expected drift; verify it and rebaseline before editing.

## Status

- **Date / planned at:** 2026-10-05, commit `726ffab`.
- **Priority:** P1. **Effort:** S. **Risk:** MED (reservation and failure cleanup).
- **Status:** IN REVIEW (owner). **Depends on:** [045](045-literal-sqlite-paths.md). **Category:** bug. **Audit finding:** 13.

## Why

`Init` promises to refuse an existing database, but uses a check-then-open sequence. Two initializers can pass the check, then one fails while applying DDL and removes the other initializer's file. Reserve the path atomically and clean up only an artifact this invocation owns. The repository explicitly supports multiple processes of the same writer.

## Current state and conventions

`internal/db/db.go:149–162`:

```go
if _, err := os.Stat(path); err == nil {
    return fmt.Errorf("%s already exists", path)
}
w, err := sql.Open("lifelog", dsn(path, false))
// ...
if _, err := w.Exec(Schema); err != nil {
    w.Close()
    os.Remove(path)
    return fmt.Errorf("applying schema.sql: %w", err)
}
```

`sql.Open` defers actual connection creation. The stat error path also fails to distinguish absence from other errors. `TestInitRefusesExistingFile` only tests a preexisting empty file. `Fresh` and the other db tests initialize disposable files, then open them through the production driver.

Honor [D13](../decisions/D13-migrations-and-freeze.md): apply the one canonical DDL to a fresh file; never create a migration runner. [Connection setup](../contract/connections.md) explains same-writer processes. Preserve canonical DDL, application_id and pragma initialization.

## Scope

Only `internal/db/db.go`, `internal/db/db_test.go`, and plan/index status. A package-private initialization helper taking an apply callback or schema argument is permitted solely for deterministic tests; public `Init` always uses the canonical embedded DDL. No migrations, DDL changes, generic filesystem locking, snapshot publication redesign, or live files.

## Commands

- Init: `go test -mod=readonly -count=1 ./internal/db -run 'TestInit|TestLiteralDatabasePaths'`.
- Consumers: `go test -mod=readonly -count=1 ./internal/importer ./cmd/lifelog`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./...`.

## Steps

1. Strengthen existing-file tests: preserve bytes of empty, nonempty and foreign SQLite files; a directory and non-absence stat failures must not be treated as a free path. First extract a package-private apply callback seam without changing existing behavior; public `Init` delegates with canonical Schema. In `TestInitConcurrentReservation`, have the callback attempt a competing public `Init` before the first invocation executes SQL on its lazily opened handle. The unsafe implementation lets the competitor initialize and then removes its file after the first schema fails; the corrected implementation must refuse the competitor because the path is already reserved. Also test coordinated goroutines without sleeps. Add deterministic apply-failure and replacement-identity cleanup cases. No global mutable hooks.
   **Verify:** init command → existing preservation checks execute; the current reservation/cleanup race must be characterized with a controlled interleaving. If the race cannot be demonstrated deterministically, STOP and report instead of adding a flaky stress-only test.
2. Reserve the literal path with `os.OpenFile` and `O_CREATE|O_EXCL|O_RDWR` before SQLite opens it. Record the reserved file identity, close its OS handle before the driver needs it, and apply canonical Schema with unchanged connection settings. Existing files/symlinks/directories must be refused without opening them as databases. Other filesystem errors retain their cause. Do not use `os.Rename` as a no-overwrite primitive across platforms.
   **Verify:** init command → concurrent reservation and all existing-file cases pass, with one valid initialized database and no deleted winner.
3. On initialization failure, close SQLite before cleanup, and remove only the reserved file whose current identity still matches this invocation's reservation. Do not remove a replacement/competitor file. Report meaningful close/cleanup errors rather than discarding them. Add deterministic failure and replacement-identity tests through the small helper; avoid platform-specific permission tricks. SQLite-owned sidecar cleanup must not remove another invocation's artifacts.
   **Verify:** init command and consumers command → only owned failed artifacts are removed; trial/replay/snapshot callers remain compatible.
4. Recheck that public initialization still uses only `Schema`, the embedded canonical copy. Do not add numbered SQL files or initialization versions.
   **Verify:** final command → exit 0; `git diff -- docs/schema/schema.sql internal/db/schema.sql` is empty.

## Done criteria

- [x] Existing paths are preserved; path reservation is exclusive and tested without sleeps.
- [x] Exactly one concurrent initializer succeeds; loser failures cannot remove the valid database.
- [x] Cleanup checks ownership after closing SQLite and reports failures; no replacement file is deleted.
- [x] Canonical DDL and Init callers remain unchanged in contract; full verification passes.
- [x] Only scope paths changed; plan is IN REVIEW (owner); parent owns the index.

## STOP conditions

Stop if exclusive creation cannot be implemented on a supported local filesystem, the test relies on scheduling/timing, cleanup cannot prove ownership, or atomic publication of a fully initialized file is required (a separate design question). Do not broaden this into startup locking, schema recovery or migrations.

## Git workflow and maintenance

Use an operator-selected branch/worktree. Commit as `db: reserve initialization paths exclusively (plan 046); ran go generate, go vet and go test`; push only on instruction. Review every cleanup path for ownership: an earlier existence check is not proof that the current pathname belongs to this invocation.
