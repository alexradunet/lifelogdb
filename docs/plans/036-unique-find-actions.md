# 036 — Give page lookup and import lookup distinct actions

> Executor: work from the repository root, run all gates, and stop on unexplained drift. Set the index row to IN REVIEW (owner); do not retire this record yourself.
>
> Drift check: `git diff --stat 726ffab..HEAD -- internal/api/siren.go internal/api/import.go internal/api/api_test.go internal/mcp/mcp.go internal/mcp/mcp_test.go cmd/lifelog/main_test.go README.md`.

## Status

- **Date / planned at:** 2026-10-05, commit `726ffab`.
- **Priority:** P1. **Effort:** S. **Risk:** MED (one model-facing action name changes).
- **Status:** IN REVIEW (owner). **Depends on:** none. **Category:** bug / tests. **Audit finding:** 3.

## Why

The ordinary catalog's `find` takes `title` and opens a page. The workspace catalog's `find` takes `text` and searches similar names. The CLI picks the first matching action, while MCP registers both and the SDK replaces the first tool with the second. Catalog-based clients therefore disagree precisely when an import workspace is enabled.

## Current state and conventions

`internal/api/siren.go:122`:

```go
{"find", "Find page", "Open the page with this title (any case or normalisation).",
    "GET", "/pages", []Field{req("title", "text", "Title")}, false},
```

`internal/api/import.go:25`:

```go
{"find", "Find", "Look a name up before writing it: people, places, pages and metrics that match exactly, with the same words, more words or fewer words.",
    "GET", "/import/find", []Field{req("text", "text", "Name")}, false},
```

`specOf` searches ordinary specifications before import specifications. `importFind` calls `h.importEntity("find", ...)`, but that first argument is an entity class label, not an action lookup: leave it unchanged. `internal/mcp/mcp.go:44` maps hyphens to underscores and calls `AddTool` without detecting collisions. `cmd/lifelog/main.go`'s `do` returns the first action with the requested name.

Use `TestActionsAreOfferedOnlyWhereLegal` and `TestReplayDryRunAction` for synthetic handler/workspace setup. The pinned MCP SDK supports in-memory transports, `ListTools` and `CallTool`; exercise the real server rather than mocking tool registration. Preserve the hypermedia architecture of [D14](../decisions/D14-ui-and-tools.md).

## Scope

Only `internal/api/siren.go`, `internal/api/import.go`, `internal/api/api_test.go`, `internal/mcp/mcp.go`, new `internal/mcp/mcp_test.go`, `cmd/lifelog/main_test.go`, `README.md`, and plan/index status. Keep `/pages` and `/import/find` URLs and field shapes unchanged. No SDK upgrade, transport rewrite, import-guide vocabulary change, or alias that recreates a duplicate tool.

## Commands

- API/CLI: `go test -mod=readonly -count=1 ./internal/api ./cmd/lifelog`.
- MCP: `go test -mod=readonly -count=1 ./internal/mcp`.
- SDK reference: `go doc github.com/modelcontextprotocol/go-sdk/mcp.NewInMemoryTransports`.
- Final: `go generate ./... && go vet -mod=readonly ./... && go test -mod=readonly ./...`.

## Steps

1. Add `TestCatalogNamesAreUnique` with and without a workspace. Check action-name uniqueness and uniqueness after MCP's hyphen-to-underscore mapping. Add a synthetic workspace assertion that page lookup and import lookup have distinct fields and URLs.
   **Verify:** API/CLI command → the new workspace uniqueness assertion fails on the two `find` entries.
2. Retain ordinary `find`; rename the import action to `import-find`. Update actual import action-name references, not the `importEntity("find", ...)` class label. Preserve the route and language-neutral guide's conceptual word “find”. Add a small duplicate-normalized-tool-name check in `mcp.New` that returns an error rather than letting `AddTool` replace a tool; include the built-in `get` and `get_day` names in the reserved-name set. Test collisions in a private validator with synthetic action lists.
   **Verify:** API/CLI command → all pass; `rg -n 'import-find|import_find' internal/api internal/mcp` shows the intended wiring.
3. Add `TestFindToolsWithWorkspace` in `internal/mcp/mcp_test.go`: connect using in-memory transports, list tools, and successfully call `find` with `title` and `import_find` with `text`. Confirm owner-only tools remain absent. Add a CLI catalog dispatch regression using the existing command test style, and document the import action name in the application's README.
   **Verify:** MCP command, API/CLI command, then final command → exit 0; MCP tests actually execute rather than reporting “no test files”.

## Done criteria

- [ ] Both catalogs and the resulting MCP tool list have unique names.
- [ ] CLI and MCP reach both lookup functions with their correct parameters.
- [ ] HTTP URLs and owner-only behavior remain unchanged.
- [ ] New MCP integration tests and the full suite pass; only scope paths changed.
- [ ] Index status is IN REVIEW (owner).

## STOP conditions

Stop if a second collision is found that requires renaming unrelated public tools, if the installed SDK cannot run the proposed transport test, or if backward compatibility requires a duplicate `find` alias. Report the compatibility choice instead of silently picking one.

## Git workflow and maintenance

Use an operator-selected branch/worktree. Commit as `api: separate import and page lookup (plan 036); ran go generate, go vet and go test`; push only when instructed. Every new catalog action must pass both raw-name and normalized-tool-name uniqueness tests.
