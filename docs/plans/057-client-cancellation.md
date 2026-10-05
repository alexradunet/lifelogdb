# 057 — Propagate CLI and MCP cancellation through requests and uploads

> Executor: use synthetic handlers/files and deterministic channel barriers. Cancellation is not rollback of an already committed write. Set plan/index IN REVIEW (owner) after all gates.
>
> Drift check: `git diff --stat 0460379..HEAD -- internal/client/client.go internal/client/client_test.go internal/mcp/mcp.go internal/mcp/mcp_test.go cmd/lifelog/main.go cmd/lifelog/main_test.go`. Rebaseline 050's adapter changes first.

## Status

- **Planned at:** `0460379`, 2026-10-05.
- **Priority:** P2. **Effort:** M. **Risk:** MED (request lifetimes and multipart goroutines).
- **Status:** IN REVIEW (owner). **Depends on:** [050](050-mcp-numeric-contract.md).
- **Category:** bug. **Deep-audit finding:** 11. **Confidence:** HIGH, source-confirmed; cancellation races not runtime-audited.

## Why

The CLI creates a signal context and the MCP SDK supplies per-call contexts, but the client creates background HTTP requests. Interrupting a command can leave a queued write or upload running. The same cancellation must reach remote transport, the in-process handler and the multipart producer.

## Current state and conventions

`internal/client/client.go:61,134,148` uses `http.NewRequest` in `Get`, `DoFiles` and `Do`. `DoFiles` streams through `io.Pipe` with a producer goroutine. `cmd/lifelog/main.go:207` creates `signal.NotifyContext` but dispatch helpers do not receive it. MCP's `call` accepts `ctx` and calls `c.Do(a, args)` without it; built-in `get`/`get_day` do likewise.

Use `TestFindToolsWithWorkspace` for SDK setup and `cmd/lifelog/main_test.go` for command fixtures. The [connection contract](../contract/connections.md) and shared-handler architecture remain unchanged.

## Scope

Only paths in the drift command plus plan/index. Create `internal/client/client_test.go`. Context-bearing methods may retain thin compatibility wrappers so unrelated API tests need not change. No transport retry policy, database transaction redesign, arbitrary new timeouts, DDL or migrations.

## Commands

- Focus: `go test -mod=readonly -count=1 ./internal/client ./internal/mcp ./cmd/lifelog -run 'TestClientCancellation|TestMCPCancellation|TestCommandCancellation|TestMCPNumeric|TestFindTools'`.
- Packages: `go test -mod=readonly -count=1 ./internal/client ./internal/mcp ./cmd/lifelog ./internal/api`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./... && git diff --check`.

## Steps and tests

1. Add channel-coordinated tests for cancel-before-send, a blocked GET/POST, multipart upload after producer start, and a write waiting behind another transaction. Test both in-process and `httptest.Server` transport. Use generous timeout guards only to fail hung tests, never sleeps to establish ordering.
   **Verify:** focus command → baseline cannot observe cancellation in the handler/queued write. Clean up all blocked handlers even on failure.
2. Add context-bearing Get/Do/DoFiles/Catalog paths using `http.NewRequestWithContext`. Ensure the in-process transport checks pre-cancellation and passes the same context to the handler. Wire the CLI signal context through all production dispatch helpers, catalog discovery, file batches and import-status requests. Stop a batch before starting the next file after cancellation.
   **Verify:** focus command → handler contexts cancel and a queued write does not later commit.
3. Pass each MCP tool-call context into discovery/action requests and built-in reads; propagate server startup/shutdown context for catalog loading as appropriate. Preserve numeric and owner-tool behavior from 050. Do not substitute a global canceled context for later independent tool calls.
   **Verify:** focus command → canceling one SDK call aborts only that call; a later call succeeds.
4. Make cancellation close/unblock both pipe sides and stop the producer from opening further files. Check copy/write/close errors and ensure files/goroutines finish through explicit completion signals in tests. Preserve a useful cancellation error, including the limitation that a write committed before cancellation stays committed.
   **Verify:** packages and final commands → exit 0; no hanging producer after cancellation or early server refusal.

## Done criteria

- [x] All production CLI/MCP request paths propagate their initiating context.
- [x] Canceled queued writes do not commit later; multipart producers and files terminate cleanly.
- [x] Later independent MCP calls and normal file/capture commands still work.
- [x] All gates pass; no scope expansion; index IN REVIEW (owner).

Implementation evidence (2026-10-05): context-aware compatibility methods propagate CLI signals and MCP call cancellation; multipart cleanup joins its producer. Synthetic local/remote requests, queued writes, early refusals, batches and independent SDK calls pass. Parent focused checks and integrated generation/vet/uncached full tests passed.

## STOP conditions

Stop if the pinned SDK cannot propagate call cancellation or the database driver does not honor the queued transaction context; report exact behavior before broad redesign. Do not promise undo-after-commit. Stop on unexplained drift or two failed gates.

## Git workflow and maintenance

Use an operator-selected branch/worktree; no commit/push unless instructed. Suggested commit: `client: carry cancellation through all surfaces (plan 057)`, listing checks. New dispatch helpers must take a context explicitly; compatibility wrappers are not for new production call sites.
