# 045 — Open literal filenames through escaped SQLite URIs

> Executor: use temporary synthetic files only, run gates from the repository root, and set the index row to IN REVIEW (owner) when done. Do not change reader/writer connection policies.
>
> Drift check: `git diff --stat 726ffab..HEAD -- internal/db/db.go internal/db/db_test.go cmd/lifelog/main_test.go`.

## Status

- **Date / planned at:** 2026-10-05, commit `726ffab`.
- **Priority:** P1. **Effort:** S. **Risk:** MED (Windows/relative path forms and snapshot settings).
- **Status:** IN REVIEW (owner). **Depends on:** none. **Category:** bug. **Audit finding:** 12.

## Why

A filesystem path is not a URI. Concatenating paths into SQLite's `file:` syntax lets percent escapes and fragment/query characters alter the filename interpreted by SQLite. The application can create/open a different file from the one checked with `os.Stat`. Make all database paths literal while preserving each operation's existing connection options.

## Current state and conventions

`internal/db/db.go:96–109`, `dsn` builds query parameters correctly but not the filename:

```go
return "file:" + filepath.ToSlash(path) + "?" + q.Encode()
```

`Copy` at line 196 has a second raw concatenation:

```go
r, err := sql.Open("lifelog", "file:"+filepath.ToSlash(from)+"?mode=ro&_pragma=trusted_schema(0)")
```

The modernc driver opens with SQLite URI handling enabled. `Open` checks application_id; connection hooks enforce settings; `Copy` intentionally uses a read-only source for `VACUUM INTO`. Do not replace Copy's settings wholesale with the ordinary query-only reader DSN: keep its snapshot semantics.

Use `Fresh`, `TestPragmasAndReaders` and `TestOpenRefusesForeignFile` in `internal/db/db_test.go`; `TestSnapshot` in `cmd/lifelog/main_test.go` covers copy/restore behavior. Honor [connection setup](../contract/connections.md) and [D25](../decisions/D25-snapshots.md). These are application path bugs, not schema rules.

## Scope

Only `internal/db/db.go`, `internal/db/db_test.go`, `cmd/lifelog/main_test.go`, and plan/index status. No DDL/migrations, pragma-policy changes, driver upgrade, file format change, network filesystem support, live database tests, or general URL/client rewrite. Exclusive creation is plan 046.

## Commands

- Paths: `go test -mod=readonly -count=1 ./internal/db -run 'TestLiteralDatabasePaths|TestPragmasAndReaders|TestOpenRefusesForeignFile'`.
- Snapshots: `go test -mod=readonly -count=1 ./cmd/lifelog -run TestSnapshot`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./...`.

## Steps

1. Add `TestLiteralDatabasePaths`: initialize, open, write/read and copy databases whose temporary directory/basename contains spaces, Unicode, percent-escape-looking text and a fragment character. Include relative paths with `t.Chdir`, and OS-valid query characters on platforms that permit them. Assert the literal requested file exists, SQLite reports that same physical file, and a synthetic alternate-name sentinel is untouched. Never use an existing owner file as the sentinel.
   **Verify:** paths command → special-character cases expose raw URI interpretation on the pinned driver. A failing assertion must identify the requested versus actual temporary file, not assume URI behavior.
2. Add one small URI filename serializer that escapes the path exactly once while retaining valid drive letters and separators. Use standard `net/url` APIs; resolving an absolute filesystem path first is acceptable. Serialize query options separately using `url.Values`. Route both `dsn` and Copy's source through this filename helper without changing their current options. Keep `VACUUM INTO`'s destination as a bound SQL parameter, not a concatenated URI.
   **Verify:** paths command → all cases pass, including unchanged pragmas, application_id refusal and Copy operation.
3. Add a snapshot regression for a special-character path using the existing synthetic snapshot setup. Verify restore check, literal destination and untouched alternate sentinel. Re-read all `file:` construction within `internal/db` for remaining production raw paths; test-owned plain-driver probes may remain explicit.
   **Verify:** `rg -n 'file:|filepath.ToSlash' internal/db/db.go` → production filename construction is centralized; snapshots command and final command → exit 0.

## Done criteria

- [ ] Init/Open/Copy operate on the exact literal temporary filenames across tested absolute/relative forms.
- [ ] Special-character alias sentinels stay unchanged; reader/writer/snapshot options remain compatible.
- [ ] Production database filename serialization has one implementation and no double escaping.
- [ ] Full verification passes; only scope paths changed; index is IN REVIEW (owner).

## STOP conditions

Stop if a proposed serializer fails drive-letter/relative tests, changes snapshot/query-only options, needs a new dependency, or relies on a platform URI assumption not established by tests. A network/UNC use case is not permission to reopen the local-disk non-goal.

## Git workflow and maintenance

Use an operator-selected branch/worktree and commit as `db: escape literal SQLite filenames (plan 045); ran go generate, go vet and go test`. Push only when instructed. New database-opening paths must reuse the filename serializer but explicitly select their own connection policy.
